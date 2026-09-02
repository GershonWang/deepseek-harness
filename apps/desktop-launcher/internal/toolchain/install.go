package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
)

// downloadTimeout 覆盖"下载挂起"场景：超时后放弃整个安装。
const downloadTimeout = 10 * time.Minute

// InstallProgress 安装进度回调。
// phase: "downloading" | "verifying" | "extracting" | "linking" | "done" | "error"
// percent: 0-100，仅 downloading 阶段有准确值，其他阶段为估算值
type InstallProgress func(phase string, percent int, message string)

// noopProgress 空进度回调。
func noopProgress(string, int, string) {}

// InstallOptions 安装选项。
type InstallOptions struct {
	// Activate 安装后是否自动设为激活版本（首次安装默认为 true，
	// 已装其他版本时默认为 false，不覆盖用户当前激活版本）。
	Activate *bool
	// Progress 进度回调，可为 nil。
	Progress InstallProgress
}

// InstallTool 安装指定工具的指定版本。
// 若 version 为空，安装推荐版本。
// 若已安装，不重复下载，直接返回。
func InstallTool(dir string, toolID, version string, opts *InstallOptions) error {
	tool, ok := LookupTool(toolID)
	if !ok {
		return fmt.Errorf("unknown tool: %s", toolID)
	}

	tv := tool.LatestVersion()
	if version != "" {
		v, found := tool.FindVersion(version)
		if !found {
			return fmt.Errorf("tool %s has no version %s", toolID, version)
		}
		tv = v
	}

	progress := noopProgress
	activate := false
	if opts != nil {
		if opts.Progress != nil {
			progress = opts.Progress
		}
		if opts.Activate != nil {
			activate = *opts.Activate
		}
	}

	// 已安装则直接返回（不重复下载）
	if IsInstalled(dir, toolID, tv.Version) {
		progress("done", 100, "已安装")
		return nil
	}

	// 先装依赖（单层）
	for _, depID := range tool.Dependencies {
		if len(ListVersions(dir, depID)) == 0 {
			progress("downloading", 0, fmt.Sprintf("安装依赖: %s", depID))
			if err := InstallTool(dir, depID, "", &InstallOptions{
				Progress: progress,
			}); err != nil {
				progress("error", 0, fmt.Sprintf("依赖安装失败: %s", err))
				return fmt.Errorf("install dependency %s: %w", depID, err)
			}
		}
	}

	return installVersion(dir, toolID, tv, progress, activate)
}

// installVersion 安装已解析好的工具版本（下载/校验/解包/激活）。
// 供 InstallTool 与测试复用：测试可注入自定义 URL/SHA256。
func installVersion(dir, toolID string, tv ToolVersion, progress InstallProgress, activate bool) error {
	// 记录安装前是否已有其他版本：首次安装自动激活，已有版本时不覆盖当前激活。
	hadOther := len(ListVersions(dir, toolID)) > 0

	root, err := downloadAndExtract(dir, toolID, tv, progress)
	if err != nil {
		return err
	}

	if activate || !hadOther {
		progress("linking", 90, "设置为当前版本")
		if err := SetActiveVersion(dir, toolID, tv.Version); err != nil {
			return err
		}
	}

	progress("done", 100, fmt.Sprintf("安装完成: %s %s", toolID, tv.Version))
	_ = root
	return nil
}

// archiveFormat 根据下载 URL 后缀推断归档格式，返回 "tar.gz" / "zip" / "tar.xz"。
// 未知后缀兜底为 tar.gz（历史工具全部是 tar.gz）。查询参数会先剥离。
func archiveFormat(url string) string {
	u := url
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	switch {
	case strings.HasSuffix(u, ".tar.xz"):
		return "tar.xz"
	case strings.HasSuffix(u, ".zip"):
		return "zip"
	default:
		return "tar.gz"
	}
}

// downloadAndExtract 下载归档（tar.gz / zip / tar.xz），校验 sha256，
// 原子解包到 <dir>/<id>-<version>。
// 返回最终目录路径。
func downloadAndExtract(dir string, toolID string, tv ToolVersion, progress InstallProgress) (string, error) {
	if tv.SHA256 == "" {
		return "", fmt.Errorf("tool %s version %s: sha256 not pinned", toolID, tv.Version)
	}

	// 检查下载缓存：文件名带归档后缀，便于区分与人工排查。
	format := archiveFormat(tv.URL)
	cachePath := filepath.Join(cacheDir(dir), tv.SHA256+"."+format)
	var data []byte
	var cacheErr error

	if _, statErr := os.Stat(cachePath); statErr == nil {
		progress("verifying", 30, "使用缓存...")
		if data, cacheErr = os.ReadFile(cachePath); cacheErr == nil {
			// 校验缓存的 sha256
			if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != strings.ToLower(tv.SHA256) {
				data = nil
				_ = os.Remove(cachePath)
			}
		}
	}

	// 没有缓存就下载
	if data == nil {
		var err error
		progress("downloading", 0, "下载中...")
		data, err = downloadWithProgress(tv.URL, func(pct int) {
			progress("downloading", pct, fmt.Sprintf("下载中 %d%%", pct))
		})
		if err != nil {
			progress("error", 0, fmt.Sprintf("下载失败: %s", err))
			return "", fmt.Errorf("download %s: %w", tv.URL, err)
		}

		// 校验 sha256
		progress("verifying", 75, "校验 sha256...")
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != strings.ToLower(tv.SHA256) {
			progress("error", 0, "sha256 校验失败")
			return "", fmt.Errorf("sha256 mismatch for %s", toolID)
		}

		// 写入缓存
		if err := os.MkdirAll(cacheDir(dir), 0o755); err == nil {
			_ = os.WriteFile(cachePath, data, 0o644)
			// 简单的缓存清理：超过 500MB 就删最老的
			go pruneCache(cacheDir(dir), 500*1024*1024)
		}
	}

	// 解压到临时目录
	progress("extracting", 85, "解压中...")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(dir, ".install-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	if err := extractArchive(format, data, tmp); err != nil {
		progress("error", 0, fmt.Sprintf("解压失败: %s", err))
		return "", fmt.Errorf("extract %s: %w", toolID, err)
	}

	// 顶层目录剥离：多数 tarball 解出唯一顶层目录，上移一层作为工具根；
	// 少数（如 fzf、lazygit）直接解出单个可执行文件或散文件，此时以解压
	// 目录本身为根。两种布局都归一到 <id>-<version> 目录。
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return "", fmt.Errorf("read extracted %s: %w", toolID, err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("empty tarball for %s", toolID)
	}

	root := versionDir(dir, toolID, tv.Version)
	if err := os.RemoveAll(root); err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		// 唯一顶层目录：上移一层。
		if err := os.Rename(filepath.Join(tmp, entries[0].Name()), root); err != nil {
			return "", err
		}
	} else {
		// 单文件/多文件：直接以解压目录为根。
		if err := os.Rename(tmp, root); err != nil {
			return "", err
		}
	}

	// 保存 tool.yml 元数据到安装目录（方便后续读取）
	if err := writeToolMetadata(root, toolID, tv); err != nil {
		// 元数据写入失败不影响安装
	}

	return root, nil
}

// writeToolMetadata 把工具版本元数据写到安装目录的 tool.yml。
func writeToolMetadata(root, id string, tv ToolVersion) error {
	content := fmt.Sprintf("id: %s\nversion: %s\nbin_rel: %s\nlib_rel: %s\nsha256: %s\n",
		id, tv.Version, tv.BinRel, tv.LibRel, tv.SHA256)
	return os.WriteFile(filepath.Join(root, "tool.yml"), []byte(content), 0o644)
}

// downloadWithProgress 带进度的下载。
func downloadWithProgress(url string, onProgress func(int)) ([]byte, error) {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}

	total := resp.ContentLength
	var buf bytes.Buffer
	buf.Grow(int(total))

	reader := &progressReader{
		Reader:   resp.Body,
		Total:    total,
		OnUpdate: onProgress,
	}

	if _, err := io.Copy(&buf, reader); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// progressReader 包装 io.Reader 以报告进度百分比。
type progressReader struct {
	Reader   io.Reader
	Total    int64
	Received int64
	OnUpdate func(int)
	lastPct  int
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.Received += int64(n)
	if r.Total > 0 && r.OnUpdate != nil {
		pct := int(float64(r.Received) / float64(r.Total) * 100)
		if pct != r.lastPct {
			r.lastPct = pct
			r.OnUpdate(pct)
		}
	}
	return n, err
}

// pruneCache 清理下载缓存，保留不超过 maxSize 字节，删最老的。
func pruneCache(dir string, maxSize int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type item struct {
		path    string
		size    int64
		modTime time.Time
	}
	var items []item
	var total int64

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(dir, e.Name())
		items = append(items, item{p, info.Size(), info.ModTime()})
		total += info.Size()
	}

	if total <= maxSize {
		return
	}

	// 按修改时间排序（最老的先删）
	// 冒泡排序：n 很小（几十到几百个文件），简单就行
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].modTime.Before(items[j-1].modTime); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	for _, it := range items {
		if total <= maxSize {
			break
		}
		_ = os.Remove(it.path)
		total -= it.size
	}
}

// extractArchive 按归档格式分发解压到 dest。支持的格式由 archiveFormat 决定。
func extractArchive(format string, data []byte, dest string) error {
	switch format {
	case "zip":
		return extractZip(data, dest)
	case "tar.xz":
		return extractTarXz(data, dest)
	default:
		return extractTarGz(data, dest)
	}
}

// extractTarGz 从字节数据解压 tar.gz 到 dest。
func extractTarGz(data []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTar(tar.NewReader(gz), dest)
}

// extractTarXz 从字节数据解压 tar.xz 到 dest（flutter 等使用 xz 压缩的发行包）。
func extractTarXz(data []byte, dest string) error {
	xr, err := xz.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return extractTar(tar.NewReader(xr), dest)
}

// extractTar 解压 tar 流到 dest，统一做路径逃逸与符号链接越界防护。
func extractTar(tr *tar.Reader, dest string) error {
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// 防路径逃逸。
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe tar path: %s", hdr.Name)
		}
		target := filepath.Join(dest, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			_ = f.Close()
		case tar.TypeSymlink:
			if filepath.IsAbs(hdr.Linkname) {
				return fmt.Errorf("unsafe tar symlink: %s -> %s", hdr.Name, hdr.Linkname)
			}
			linkParent := filepath.Dir(clean)
			resolved := filepath.Clean(filepath.Join(linkParent, hdr.Linkname))
			if strings.HasPrefix(resolved, "..") || filepath.IsAbs(resolved) {
				return fmt.Errorf("unsafe tar symlink: %s -> %s", hdr.Name, hdr.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// extractZip 从字节数据解压 zip 到 dest（bun/deno/kotlin/dart 使用 zip 发行包）。
func extractZip(data []byte, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		// 防路径逃逸（zip 内条目名可能是绝对路径或 ..）。
		clean := filepath.Clean(f.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe zip path: %s", f.Name)
		}
		target := filepath.Join(dest, clean)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode()&0o777)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// InstallDir 返回按需安装根目录（home 下 .dsh-tools）。
func InstallDir(home string) string {
	return filepath.Join(home, ".dsh-tools")
}

package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ulikunitz/xz"
)

// downloadTimeout 覆盖"下载挂起"场景：超时后放弃整个安装。
const downloadTimeout = 10 * time.Minute

// maxResumeRetries 下载失败后的重试次数（含断点续传场景）。
// 每次失败后退避重试；服务端不支持 Range 时回退为完整下载再失败。
const maxResumeRetries = 3

// InstallProgress 安装进度回调。
// phase: "downloading" | "verifying" | "extracting" | "linking" | "done" | "error"
// percent: 0-100，仅 downloading 阶段有准确值，其他阶段为估算值
type InstallProgress func(phase string, percent int, message string)

// noopProgress 空进度回调。
func noopProgress(string, int, string) {}

// friendlyError 把原始安装错误归类为面向用户的友好提示。
// 返回 "分类：提示" 格式的字符串，原始错误通过 %w 包装保留。
func friendlyError(err error) error {
	if err == nil {
		return nil
	}
	msg := classifyError(err)
	return fmt.Errorf("%s：%w", msg, err)
}

// classifyError 根据错误类型返回人类可读的分类描述与建议。
func classifyError(err error) string {
	// 磁盘空间不足
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		if errno, ok := pathErr.Err.(syscall.Errno); ok {
			if errno == syscall.ENOSPC {
				return "磁盘空间不足，请清理后重试"
			}
			if errno == syscall.EACCES || errno == syscall.EPERM {
				return "写入权限不足，请检查安装目录权限"
			}
		}
	}

	// 网络类错误
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return "下载超时，请检查网络后重试"
		}
		// DNS 失败、连接被拒等
		return "网络连接失败，请检查网络后重试"
	}

	errMsg := err.Error()
	switch {
	case strings.Contains(errMsg, "sha256 mismatch") || strings.Contains(errMsg, "sha256"):
		return "文件校验失败，可能下载不完整或被篡改"
	case strings.Contains(errMsg, "no such host") || strings.Contains(errMsg, "TLS"):
		return "网络连接失败，请检查网络后重试"
	case strings.Contains(errMsg, "extract"):
		return "文件解压失败，归档可能已损坏"
	}

	return "安装失败"
}

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
		return friendlyError(err)
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
// 原子解包到 <dir>/<id>-<version>。支持 HTTP Range 断点续传，
// 下载过程中文件落地（而非全量驻留内存），适配大文件场景。
func downloadAndExtract(dir string, toolID string, tv ToolVersion, progress InstallProgress) (string, error) {
	if tv.SHA256 == "" {
		return "", fmt.Errorf("tool %s version %s: sha256 not pinned", toolID, tv.Version)
	}

	format := archiveFormat(tv.URL)
	cachePath := filepath.Join(cacheDir(dir), tv.SHA256+"."+format)

	// 1) 缓存命中 → 直接从缓存解压
	if _, statErr := os.Stat(cachePath); statErr == nil {
		progress("verifying", 30, "使用缓存...")
		if valid, _ := verifyFileSHA256(cachePath, tv.SHA256); valid {
			return extractFromFile(cachePath, format, dir, toolID, tv, progress)
		}
		_ = os.Remove(cachePath)
	}

	// 2) 无缓存 → 断点续传下载到 part 文件
	partPath := partPathForURL(tv.URL)
	// 若旧 part 文件校验已正确，直接复用（避免重新下载）
	if info, statErr := os.Stat(partPath); statErr == nil && info.Size() > 0 {
		if valid, _ := verifyFileSHA256(partPath, tv.SHA256); valid {
			// part 居然就是完整的：移入缓存
			if err := moveOrCopy(partPath, cachePath); err == nil {
				return extractFromFile(cachePath, format, dir, toolID, tv, progress)
			}
		}
		// part 文件损坏或不完整：保留它（断点续传会从尾部继续），
		// 但若大小为 0 或明显损坏则删除重来
	}

	progress("downloading", 0, "下载中...")
	if err := downloadToFile(tv.URL, partPath, func(pct int) {
		progress("downloading", pct, fmt.Sprintf("下载中 %d%%", pct))
	}); err != nil {
		progress("error", 0, fmt.Sprintf("下载失败: %s", err))
		return "", fmt.Errorf("download %s: %w", tv.URL, err)
	}

	// 3) 校验 sha256
	progress("verifying", 75, "校验 sha256...")
	if valid, _ := verifyFileSHA256(partPath, tv.SHA256); !valid {
		_ = os.Remove(partPath)
		progress("error", 0, "sha256 校验失败")
		return "", fmt.Errorf("sha256 mismatch for %s", toolID)
	}

	// 4) 移入缓存目录
	if err := os.MkdirAll(cacheDir(dir), 0o755); err == nil {
		if err := moveOrCopy(partPath, cachePath); err == nil {
			// 缓存清理：超过 500MB 就删最老的
			go pruneCache(cacheDir(dir), 500*1024*1024)
		} else {
			// 移入缓存失败：直接用 part 文件解压
			cachePath = partPath
		}
	} else {
		cachePath = partPath
	}

	return extractFromFile(cachePath, format, dir, toolID, tv, progress)
}

// moveOrCopy 优先 rename，跨分区失败时退回复制+删除源。
func moveOrCopy(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	_ = os.Remove(src)
	return nil
}

// extractFromFile 从归档文件流式解压到工具版本目录。
// 不把整个归档读进内存，大文件（几百 MB 到几 GB）场景下节省显著内存。
func extractFromFile(archivePath, format, dir, toolID string, tv ToolVersion, progress InstallProgress) (string, error) {
	progress("extracting", 85, "解压中...")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(dir, ".install-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	if err := extractArchiveFromFile(format, archivePath, tmp); err != nil {
		progress("error", 0, fmt.Sprintf("解压失败: %s", err))
		return "", fmt.Errorf("extract %s: %w", toolID, err)
	}

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
		if err := os.Rename(filepath.Join(tmp, entries[0].Name()), root); err != nil {
			return "", err
		}
	} else {
		if err := os.Rename(tmp, root); err != nil {
			return "", err
		}
	}

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

// downloadToFile 下载到指定文件路径（支持 HTTP Range 断点续传 + 指数退避重试）。
func downloadToFile(url, destPath string, onProgress func(int)) error {
	return downloadToFileWithRetries(url, destPath, onProgress, 0)
}

func downloadToFileWithRetries(url, destPath string, onProgress func(int), attempt int) error {
	err := downloadFileResumable(url, destPath, onProgress)
	if err == nil {
		return nil
	}
	if attempt >= maxResumeRetries-1 {
		return err
	}
	// 指数退避：200ms → 400ms → 800ms
	delay := time.Duration(1<<attempt) * 200 * time.Millisecond
	time.Sleep(delay)
	return downloadToFileWithRetries(url, destPath, onProgress, attempt+1)
}

// downloadFileResumable 执行一次断点续传下载到 destPath。
// 若 destPath 已存在且服务端支持 Range，则从已有字节处追加；
// 服务端不支持或文件损坏时从头下载。
func downloadFileResumable(url, destPath string, onProgress func(int)) error {
	var existingSize int64
	if info, err := os.Stat(destPath); err == nil {
		existingSize = info.Size()
	}

	client := &http.Client{Timeout: downloadTimeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if existingSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	resuming := resp.StatusCode == http.StatusPartialContent
	if !resuming && resp.StatusCode != http.StatusOK {
		// 如 416 Range Not Satisfiable：清掉坏 part 让下次重试从头开始
		if existingSize > 0 {
			_ = os.Remove(destPath)
		}
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}

	var total int64
	if resuming {
		// Content-Range: bytes 1000-1999/2000 → 解析出总大小
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if idx := strings.LastIndex(cr, "/"); idx >= 0 {
				fmt.Sscanf(cr[idx+1:], "%d", &total)
			}
		}
	} else {
		total = resp.ContentLength
		// 不支持续传：清掉旧文件从头开始
		_ = os.Remove(destPath)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	flag := os.O_CREATE | os.O_WRONLY
	if resuming {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(destPath, flag, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	startBytes := existingSize
	if !resuming {
		startBytes = 0
	}
	reader := &progressReader{
		Reader:   resp.Body,
		Total:    total,
		Received: startBytes,
		OnUpdate: onProgress,
	}

	_, err = io.Copy(f, reader)
	return err
}

// verifyFileSHA256 校验文件的 sha256 是否匹配。
func verifyFileSHA256(path, expected string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == strings.ToLower(expected), nil
}

// partPathForURL 返回 URL 对应的断点续传临时文件路径，
// 存放在系统临时目录下的 dsh-tools-downloads 子目录。
func partPathForURL(url string) string {
	sum := sha256.Sum256([]byte(url))
	name := hex.EncodeToString(sum[:])[:16] + ".part"
	return filepath.Join(os.TempDir(), "dsh-tools-downloads", name)
}

// copyFile 复制文件，跨分区 rename 失败时使用。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
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

// extractArchive 按归档格式分发解压到 dest（从内存字节数据）。
// 保留以兼容现有调用；新代码优先用 extractArchiveFromFile（流式，更省内存）。
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

// extractArchiveFromFile 从文件流式解压，避免大文件全量进内存。
func extractArchiveFromFile(format, archivePath, dest string) error {
	switch format {
	case "zip":
		return extractZipFromFile(archivePath, dest)
	case "tar.xz":
		return extractTarXzFromFile(archivePath, dest)
	default:
		return extractTarGzFromFile(archivePath, dest)
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
	return extractZipEntries(zr.File, dest)
}

// extractTarGzFromFile 从文件流式解压 tar.gz，避免全量进内存。
func extractTarGzFromFile(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTar(tar.NewReader(gz), dest)
}

// extractTarXzFromFile 从文件流式解压 tar.xz。
func extractTarXzFromFile(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	xr, err := xz.NewReader(f)
	if err != nil {
		return err
	}
	return extractTar(tar.NewReader(xr), dest)
}

// extractZipFromFile 从文件流式解压 zip（zip.OpenReader 按需读取各条目，
// 中央目录会全量加载，但那通常只有几十 KB）。
func extractZipFromFile(path, dest string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	return extractZipEntries(zr.File, dest)
}

// extractZipEntries 是 zip 解压的共享实现：遍历 zip 文件条目并解压到 dest。
// 供 extractZip（内存版）和 extractZipFromFile（文件流式版）共用。
func extractZipEntries(files []*zip.File, dest string) error {
	for _, f := range files {
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

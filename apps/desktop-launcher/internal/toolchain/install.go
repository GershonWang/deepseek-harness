package toolchain

import (
	"archive/tar"
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
)

// downloadTimeout 覆盖"下载挂起"场景：超时后放弃整个安装。
const downloadTimeout = 5 * time.Minute

// InstallDir 返回按需安装根目录（home 下 .dsh-tools）。
func InstallDir(home string) string {
	return filepath.Join(home, ".dsh-tools")
}

// Install 下载 url 的 tar.gz，校验 sha256 后原子解包到
// <dir>/<name>-<version>，并更新 <dir>/current/<name> 软链。
// 任一环节失败不残留半成品；已存在旧版本时先移除旧软链与旧目录。
func Install(dir, name, version, url, sha256Hex string) error {
	downloaded, err := download(url)
	if err != nil {
		return err
	}
	if sum := sha256.Sum256(downloaded); hex.EncodeToString(sum[:]) != strings.ToLower(sha256Hex) {
		return fmt.Errorf("sha256 mismatch for %s", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(dir, ".install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := extractTarGz(downloaded, tmp); err != nil {
		return err
	}
	// tar 顶层目录剥离：解包出的唯一目录上移一层。
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return fmt.Errorf("unexpected tarball layout for %s", name)
	}
	root := filepath.Join(dir, name+"-"+version)
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(tmp, entries[0].Name()), root); err != nil {
		return err
	}
	current := filepath.Join(dir, "current", name)
	if err := os.MkdirAll(filepath.Dir(current), 0o755); err != nil {
		return err
	}
	// 已存在旧软链（如升级）时先移除，避免 os.Symlink 报 EEXIST。
	_ = os.Remove(current)
	return os.Symlink(root, current)
}

// ListInstalled 返回当前已安装的工具名列表（按 current 软链）。
func ListInstalled(dir string) []string {
	cur := filepath.Join(dir, "current")
	entries, err := os.ReadDir(cur)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Remove 删除 <dir>/<name>-<version>（current 软链目标）与 <dir>/current/<name>。
func Remove(dir, name string) error {
	current := filepath.Join(dir, "current", name)
	target, err := os.Readlink(current)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(current); err != nil && !os.IsNotExist(err) {
		return err
	}
	if target != "" {
		return os.RemoveAll(target)
	}
	return nil
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<30))
}

func extractTarGz(data []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
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
			// 符号链接目标可能是相对路径（如 ../foo），需相对于链接所在目录
			// 解析后再检查是否逃逸出解压根目录。绝对路径目标一律拒绝。
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

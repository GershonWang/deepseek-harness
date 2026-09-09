package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// fakeTar 构建 "<dir>/bin/<name>" 的 tar.gz 并返回其 sha256。
func fakeTar(t *testing.T, dir, name string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("#!/bin/sh\necho fake-" + name + "\n")
	_ = tw.WriteHeader(&tar.Header{
		Name: dir + "/bin/" + name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(content)
	_ = tw.Close()
	_ = gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func TestInstallVersion_AtomicAndListed(t *testing.T) {
	home := t.TempDir()
	dir := InstallDir(home)
	blob, sum := fakeTar(t, "go", "go")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()

	tv := ToolVersion{Version: "1.23.2", URL: srv.URL + "/go.tar.gz", SHA256: sum, BinRel: "bin"}
	if err := installVersion(dir, "go", tv, noopProgress, false); err != nil {
		t.Fatalf("installVersion: %v", err)
	}
	root := filepath.Join(dir, "go-1.23.2")
	if _, err := os.Stat(filepath.Join(root, "bin", "go")); err != nil {
		t.Fatalf("extracted binary missing: %v", err)
	}
	current, err := os.Readlink(filepath.Join(dir, "current", "go"))
	if err != nil || current != root {
		t.Fatalf("current symlink: %v -> %q", err, current)
	}
	names := ListInstalled(dir)
	if len(names) != 1 || names[0] != "go" {
		t.Fatalf("ListInstalled: got %v", names)
	}
}

func TestInstallVersion_SecondVersionKeepsActive(t *testing.T) {
	home := t.TempDir()
	dir := InstallDir(home)
	blob, sum := fakeTar(t, "go", "go")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()

	// 首次安装：无其他版本，自动激活 1.23.2。
	if err := installVersion(dir, "go", ToolVersion{Version: "1.23.2", URL: srv.URL + "/go.tar.gz", SHA256: sum, BinRel: "bin"}, noopProgress, false); err != nil {
		t.Fatal(err)
	}
	// 同工具加装 1.24.0（activate=false）：不应覆盖当前激活。
	if err := installVersion(dir, "go", ToolVersion{Version: "1.24.0", URL: srv.URL + "/go.tar.gz", SHA256: sum, BinRel: "bin"}, noopProgress, false); err != nil {
		t.Fatalf("install second version: %v", err)
	}
	if got := ActiveVersion(dir, "go"); got != "1.23.2" {
		t.Fatalf("当前激活应保持 1.23.2, got %q", got)
	}
	// 显式 activate=true 才能切到 1.24.0。
	if err := SetActiveVersion(dir, "go", "1.24.0"); err != nil {
		t.Fatalf("切换激活: %v", err)
	}
	if got := ActiveVersion(dir, "go"); got != "1.24.0" {
		t.Fatalf("切换后应激活 1.24.0, got %q", got)
	}
}

func TestInstallVersion_ShaMismatch(t *testing.T) {
	home := t.TempDir()
	dir := InstallDir(home)
	blob, _ := fakeTar(t, "go", "go")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	tv := ToolVersion{
		Version: "1.23.2",
		URL:     srv.URL + "/go.tar.gz",
		SHA256:  "0000000000000000000000000000000000000000000000000000000000000000",
		BinRel:  "bin",
	}
	if err := installVersion(dir, "go", tv, noopProgress, false); err == nil {
		t.Fatal("expected sha256 mismatch error")
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Fatal("failed install must leave no install dir")
	}
}

// singleFileTar 构建根目录直接是单个可执行文件的 tar.gz（fzf/lazygit 风格）。
func singleFileTar(t *testing.T, name string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("#!/bin/sh\necho fake-" + name + "\n")
	_ = tw.WriteHeader(&tar.Header{
		Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(content)
	_ = tw.Close()
	_ = gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func TestInstallVersion_SingleFileTarball(t *testing.T) {
	home := t.TempDir()
	dir := InstallDir(home)
	blob, sum := singleFileTar(t, "fzf")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	tv := ToolVersion{Version: "0.55.0", URL: srv.URL + "/fzf.tar.gz", SHA256: sum, BinRel: "."}
	if err := installVersion(dir, "fzf", tv, noopProgress, false); err != nil {
		t.Fatalf("单文件 tarball 安装: %v", err)
	}
	// 可执行文件应在 <dir>/fzf-0.55.0/fzf，且 bin/fzf 软链已建立。
	if !isExecutableFile(filepath.Join(dir, "fzf-0.55.0", "fzf")) {
		t.Fatal("单文件 tarball 未正确归一到版本目录")
	}
	if _, err := os.Lstat(filepath.Join(dir, "bin", "fzf")); err != nil {
		t.Fatalf("bin/fzf 软链缺失: %v", err)
	}
}

func isExecutableFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func TestUninstall(t *testing.T) {
	home := t.TempDir()
	dir := InstallDir(home)
	blob, sum := fakeTar(t, "rg", "rg")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	tv := ToolVersion{Version: "14.1.0", URL: srv.URL + "/rg.tar.gz", SHA256: sum, BinRel: "bin"}
	if err := installVersion(dir, "rg", tv, noopProgress, false); err != nil {
		t.Fatalf("installVersion: %v", err)
	}
	if err := Uninstall(dir, "rg", "14.1.0"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if names := ListInstalled(dir); len(names) != 0 {
		t.Fatalf("after remove, ListInstalled: %v", names)
	}
}

// --- 断点续传测试 ---

func TestDownloadToFile_Full(t *testing.T) {
	content := []byte("hello world this is a test file")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "test.bin")
	if err := downloadToFile(srv.URL, dest, nil); err != nil {
		t.Fatalf("downloadToFile: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("content mismatch: got %q, want %q", data, content)
	}
}

func TestDownloadToFile_Resume(t *testing.T) {
	content := make([]byte, 2000)
	for i := range content {
		content[i] = byte(i % 256)
	}
	rangeRequests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHdr := r.Header.Get("Range")
		if rangeHdr != "" {
			rangeRequests++
			// 解析 bytes=start-
			var start int
			fmt.Sscanf(rangeHdr, "bytes=%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(content)-1, len(content)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(content[start:])
			return
		}
		w.Write(content)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "test.bin")
	// 先写入前 500 字节（模拟上次中断）
	if err := os.WriteFile(dest, content[:500], 0o644); err != nil {
		t.Fatal(err)
	}

	if err := downloadToFile(srv.URL, dest, nil); err != nil {
		t.Fatalf("downloadToFile resume: %v", err)
	}
	if rangeRequests == 0 {
		t.Fatal("expected at least one Range request, got 0")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("resumed content mismatch: len=%d, want len=%d", len(data), len(content))
	}
}

func TestDownloadToFile_NoRangeSupport(t *testing.T) {
	content := []byte("full download content here")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 服务端忽略 Range 头，始终返回 200 + 完整内容
		w.Write(content)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "test.bin")
	// 写入部分垃圾数据模拟中断
	if err := os.WriteFile(dest, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := downloadToFile(srv.URL, dest, nil); err != nil {
		t.Fatalf("downloadToFile: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("content mismatch after non-resume download")
	}
}

func TestDownloadToFile_RetryOnFailure(t *testing.T) {
	attempts := 0
	content := []byte("eventual success")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// 前两次失败：直接关闭连接（模拟网络错误）
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "fail", 500)
				return
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.Write(content)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "test.bin")
	err := downloadToFile(srv.URL, dest, nil)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if attempts < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", attempts)
	}
	data, _ := os.ReadFile(dest)
	if !bytes.Equal(data, content) {
		t.Fatal("content mismatch after retry")
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	content := []byte("test content for sha")
	sum := sha256.Sum256(content)
	hexSum := hex.EncodeToString(sum[:])

	path := filepath.Join(t.TempDir(), "sha.bin")
	os.WriteFile(path, content, 0o644)

	valid, err := verifyFileSHA256(path, hexSum)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !valid {
		t.Fatal("expected valid")
	}

	valid, _ = verifyFileSHA256(path, "0000000000000000000000000000000000000000000000000000000000000000")
	if valid {
		t.Fatal("expected invalid")
	}
}

func TestPartPathForURL(t *testing.T) {
	p1 := partPathForURL("https://example.com/a.tar.gz")
	p2 := partPathForURL("https://example.com/b.zip")
	if p1 == p2 {
		t.Fatal("different URLs should have different part paths")
	}
	if filepath.Ext(p1) != ".part" {
		t.Fatalf("expected .part extension, got %s", filepath.Ext(p1))
	}
}

// --- 错误分类测试 ---

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "网络超时",
			err:  &url.Error{Op: "Get", URL: "http://x", Err: timeoutErr{}},
			want: "下载超时",
		},
		{
			name: "sha256 不匹配",
			err:  fmt.Errorf("sha256 mismatch for foo"),
			want: "文件校验失败",
		},
		{
			name: "解压失败",
			err:  fmt.Errorf("extract foo: unexpected EOF"),
			want: "文件解压失败",
		},
		{
			name: "DNS 失败",
			err:  &url.Error{Op: "Get", URL: "http://x", Err: fmt.Errorf("no such host")},
			want: "网络连接失败",
		},
		{
			name: "磁盘空间不足",
			err:  &os.PathError{Op: "write", Path: "/tmp/x", Err: syscall.ENOSPC},
			want: "磁盘空间不足",
		},
		{
			name: "权限不足",
			err:  &os.PathError{Op: "open", Path: "/root/x", Err: syscall.EACCES},
			want: "写入权限不足",
		},
		{
			name: "未知错误",
			err:  fmt.Errorf("something went wrong"),
			want: "安装失败",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyError(tt.err)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("期望包含 %q, 实际得到 %q", tt.want, got)
			}
		})
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// --- 流式解压测试 ---

func TestExtractArchiveFromFile_TarGz(t *testing.T) {
	dir := t.TempDir()
	// 构造 tar.gz 文件
	blob, _ := fakeTar(t, "test", "mybin")
	tarPath := filepath.Join(dir, "test.tar.gz")
	if err := os.WriteFile(tarPath, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	if err := extractArchiveFromFile("tar.gz", tarPath, dest); err != nil {
		t.Fatalf("流式解压失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "test", "bin", "mybin")); err != nil {
		t.Fatalf("解压后文件缺失: %v", err)
	}
}

func TestExtractArchiveFromFile_Zip(t *testing.T) {
	// 构造一个 zip 文件
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")

	// 用标准库写一个 zip
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("subdir/hello.txt")
	_, _ = w.Write([]byte("hello world"))
	_ = zw.Close()
	_ = f.Close()

	dest := filepath.Join(dir, "out")
	if err := extractArchiveFromFile("zip", zipPath, dest); err != nil {
		t.Fatalf("流式 zip 解压失败: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "subdir", "hello.txt")); err != nil || string(data) != "hello world" {
		t.Fatalf("zip 解压内容不对: %v, data=%q", err, string(data))
	}
}

func TestExtractArchiveFromFile_PathEscape(t *testing.T) {
	dir := t.TempDir()
	// 构造含路径逃逸的 zip
	zipPath := filepath.Join(dir, "bad.zip")
	f, _ := os.Create(zipPath)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../escape.txt")
	_, _ = w.Write([]byte("bad"))
	_ = zw.Close()
	_ = f.Close()

	dest := filepath.Join(dir, "out")
	err := extractArchiveFromFile("zip", zipPath, dest)
	if err == nil {
		t.Fatal("应检测到路径逃逸并报错")
	}
}

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

// TestInstallTool_AlreadyInstalledHonorsActivate 固定「目标版本已安装 + Activate」这条路径
// （AUDIT N7）。它是「更新下载完成却停在旧版本」的死局出口：目标版本已在磁盘上时无条件早退，
// 会让用户无论点多少次更新都收不到切换，却拿到成功提示。
func TestInstallTool_AlreadyInstalledHonorsActivate(t *testing.T) {
	dir := t.TempDir()
	goTool, ok := LookupTool("go")
	if !ok {
		t.Fatal("catalog 应含 go")
	}
	recommended := goTool.LatestVersion().Version
	const old = "1.0.0"
	mkToolVersion(t, dir, "go", old, "go")
	mkToolVersion(t, dir, "go", recommended, "go")
	if err := SetActiveVersion(dir, "go", old); err != nil {
		t.Fatalf("激活 %s: %v", old, err)
	}

	// 不带 Activate：并存安装不覆盖当前激活（见 TestInstallVersion_SecondVersionKeepsActive）
	if err := InstallTool(dir, "go", "", nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	if got := ActiveVersion(dir, "go"); got != old {
		t.Fatalf("未要求激活时当前版本应保持 %s, got %q", old, got)
	}

	// 带 Activate：目标版本已安装也要完成切换
	activate := true
	if err := InstallTool(dir, "go", "", &InstallOptions{Activate: &activate}); err != nil {
		t.Fatalf("install with activate: %v", err)
	}
	if got := ActiveVersion(dir, "go"); got != recommended {
		t.Fatalf("要求激活时当前版本应为 %s, got %q", recommended, got)
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
	// 契约是失败的安装不留下任何「已安装」痕迹。下载脚手架 <tools>/.downloads
	// 允许存在（part 就落在那里，校验失败时已被删除），但除此以外不许有残留：
	// 根目录里出现 <id>-<version> 或 current 才是真正的脏安装。
	if IsInstalled(dir, "go", tv.Version) {
		t.Fatal("failed install must not be listed as installed")
	}
	if _, err := os.Readlink(currentLink(dir, "go")); err == nil {
		t.Fatal("failed install must not activate a version")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return // 连根目录都没建，更干净
		}
		t.Fatalf("read install dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != ".downloads" {
			t.Fatalf("failed install left install-dir residue: %s", e.Name())
		}
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
	dir := t.TempDir()
	p1 := partPathForURL(dir, "https://example.com/a.tar.gz")
	p2 := partPathForURL(dir, "https://example.com/b.zip")
	if p1 == p2 {
		t.Fatal("different URLs should have different part paths")
	}
	if filepath.Ext(p1) != ".part" {
		t.Fatalf("expected .part extension, got %s", filepath.Ext(p1))
	}
	// 必须落在安装根目录下的私有子目录，不能再用全局可写的 os.TempDir()：
	// part 文件名可由公开索引推算，共享目录里的同名链接会被 O_TRUNC 跟随写入。
	if got, want := filepath.Dir(p1), filepath.Join(dir, ".downloads"); got != want {
		t.Fatalf("part dir = %s, want %s", got, want)
	}
	legacy := filepath.Join(os.TempDir(), "dsh-tools-downloads")
	if strings.HasPrefix(p1, legacy+string(os.PathSeparator)) {
		t.Fatalf("part path must not live in the shared temp dir: %s", p1)
	}
}

// TestDownloadToFile_RejectsSymlinkPart 覆盖 N9：末段被换成符号链接、且服务端
// 支持断点续传（206）时，下载必须失败，链接指向的目标文件内容不得被追加写入。
// 服务端若不支持 Range，代码会先 os.Remove 掉链接本身再从零重建，走不到 O_NOFOLLOW。
func TestDownloadToFile_RejectsSymlinkPart(t *testing.T) {
	content := []byte("attacker controlled payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rangeHdr := r.Header.Get("Range"); rangeHdr != "" {
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

	// part 目录用 TempDir 而不是 downloadsDir()：后者指向真实的用户缓存，
	// 测试不该往里写东西。
	partDir := t.TempDir()
	// 目标文件必须非空，否则不会发 Range 请求、也就落不到 append 分支。
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("victim original"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	part := filepath.Join(partDir, "planted.part")
	if err := os.Symlink(victim, part); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if err := downloadToFile(srv.URL, part, nil); err == nil {
		t.Fatal("download through a symlinked part file must fail")
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(got) != "victim original" {
		t.Fatalf("symlink target was modified: %q", got)
	}
}

// TestEnsurePrivateDir_RejectsSymlinkDir 覆盖 N9 的目录一侧：
// .downloads 本身被换成指向别处的链接时必须失败，否则文件会落到链接目标目录。
func TestEnsurePrivateDir_RejectsSymlinkDir(t *testing.T) {
	parent := t.TempDir()
	link := filepath.Join(parent, "downloads")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := ensurePrivateDir(link); err == nil {
		t.Fatal("symlinked downloads dir must be rejected")
	}
	// 正常路径仍应可创建（0700）
	ok := filepath.Join(parent, "real")
	if err := ensurePrivateDir(ok); err != nil {
		t.Fatalf("plain dir should be accepted: %v", err)
	}
	info, err := os.Stat(ok)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("downloads dir perms = %o, want 700", perm)
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

// withLimits 临时收紧包级资源上限。上限声明为包级变量就是为了让用例用几十字节的
// 归档走通真实的下载与解压路径，而不是只测一个无法触发的分支。
func withLimits(t *testing.T, archiveBytes, extractBytes int64, entries int) {
	t.Helper()
	oldArchive, oldExtract, oldEntries := maxArchiveBytes, maxExtractBytes, maxArchiveEntries
	maxArchiveBytes, maxExtractBytes, maxArchiveEntries = archiveBytes, extractBytes, entries
	t.Cleanup(func() {
		maxArchiveBytes, maxExtractBytes, maxArchiveEntries = oldArchive, oldExtract, oldEntries
	})
}

// TestCopyCapped 覆盖限量拷贝原语：恰好等于上限不算超限，多 1 字节即报错。
func TestCopyCapped(t *testing.T) {
	cases := []struct {
		name    string
		content string
		limit   int64
		wantErr bool
	}{
		{"小于上限", "abc", 4, false},
		{"恰好等于上限", "abcd", 4, false},
		{"超过上限 1 字节", "abcde", 4, true},
		{"上限为 0 且无内容", "", 0, false},
		{"上限为 0 且有内容", "a", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			n, err := copyCapped(&buf, strings.NewReader(tc.content), tc.limit, errArchiveTooLarge)
			if tc.wantErr {
				if !errors.Is(err, errArchiveTooLarge) {
					t.Fatalf("want errArchiveTooLarge, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if n != int64(len(tc.content)) || buf.String() != tc.content {
				t.Fatalf("写了 %d 字节 %q, want %q", n, buf.String(), tc.content)
			}
		})
	}
}

// TestDownloadToFile_RejectsOversizeBody 覆盖 N8：服务端不声明长度时，只能按实际
// 写入量判断，超限立即中止且不重试（重试只会再下载一遍并再撞上限）。
func TestDownloadToFile_RejectsOversizeBody(t *testing.T) {
	withLimits(t, 64, maxExtractBytes, maxArchiveEntries)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		// 先 Flush 且写出超过几 KiB 缓冲，让响应走分块传输、不带 Content-Length：
		// Go 会给小于缓冲阈值的响应自动补 Content-Length，那样只覆盖到声明长度
		// 预检，测不到「按实际写入量判定」这条路径。
		w.(http.Flusher).Flush()
		_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "a.part")
	err := downloadToFile(srv.URL, dest, nil)
	if !errors.Is(err, errArchiveTooLarge) {
		t.Fatalf("want errArchiveTooLarge, got %v", err)
	}
	if requests != 1 {
		t.Fatalf("超限是确定性失败，不应重试：请求 %d 次", requests)
	}
	// 上限+1 字节：既证明判定发生在拷贝路径上，也说明越限后立刻停写，
	// 不会把整个响应落盘。
	info, statErr := os.Stat(dest)
	if statErr != nil {
		t.Fatalf("stat part: %v", statErr)
	}
	if want := maxArchiveBytes + 1; info.Size() != want {
		t.Fatalf("part 大小 = %d, want %d", info.Size(), want)
	}
}

// TestDownloadToFile_RejectsDeclaredOversize 覆盖 N8 的快速失败路径：
// 声明长度超限时不必真下载完就能失败。
func TestDownloadToFile_RejectsDeclaredOversize(t *testing.T) {
	withLimits(t, 64, maxExtractBytes, maxArchiveEntries)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4096")
		_, _ = w.Write([]byte("small"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "a.part")
	err := downloadToFile(srv.URL, dest, nil)
	if !errors.Is(err, errArchiveTooLarge) {
		t.Fatalf("want errArchiveTooLarge, got %v", err)
	}
	if !strings.Contains(err.Error(), "declared length") {
		t.Fatalf("应说明是声明长度超限: %v", err)
	}
}

// TestDownloadAndExtract_RemovesOversizePart 覆盖 N8 的收尾：超限的 part 没有
// 续传价值，必须清掉，否则该工具每次安装都会停在同一处。
func TestDownloadAndExtract_RemovesOversizePart(t *testing.T) {
	withLimits(t, 64, maxExtractBytes, maxArchiveEntries)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 同样走分块，确保 part 文件真的落过盘：否则「不存在」可能是因为压根没建，
		// 断言就失去意义。
		w.(http.Flusher).Flush()
		_, _ = w.Write(bytes.Repeat([]byte("x"), 4096))
	}))
	defer srv.Close()

	dir := t.TempDir()
	tv := ToolVersion{Version: "1.0.0", URL: srv.URL, SHA256: strings.Repeat("0", 64)}
	_, err := downloadAndExtract(dir, "testsize", tv, noopProgress)
	if !errors.Is(err, errArchiveTooLarge) {
		t.Fatalf("want errArchiveTooLarge, got %v", err)
	}
	if _, statErr := os.Stat(partPathForURL(dir, srv.URL)); !os.IsNotExist(statErr) {
		t.Fatalf("超限的 part 应被清掉: %v", statErr)
	}
}

// tarWithFiles 构建含指定内容的 tar，用于解压预算用例。
func tarWithFiles(t *testing.T, files map[string]int) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, size := range files {
		content := bytes.Repeat([]byte("y"), size)
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtractTar_RejectsOversizeContent 覆盖 N8：解压写入总量超限即中止，
// 挡住压缩比极高的解压炸弹。
func TestExtractTar_RejectsOversizeContent(t *testing.T) {
	withLimits(t, maxArchiveBytes, 100, maxArchiveEntries)
	data := tarWithFiles(t, map[string]int{"a.txt": 60, "b.txt": 60})
	dest := t.TempDir()

	err := extractTar(tar.NewReader(bytes.NewReader(data)), dest)
	if !errors.Is(err, errExtractTooLarge) {
		t.Fatalf("want errExtractTooLarge, got %v", err)
	}
}

// TestExtractTar_RejectsTooManyEntries 覆盖 N8 的条目数上限：空条目不占字节预算，
// 但一样会消耗 inode。
func TestExtractTar_RejectsTooManyEntries(t *testing.T) {
	withLimits(t, maxArchiveBytes, maxExtractBytes, 2)
	data := tarWithFiles(t, map[string]int{"a.txt": 0, "b.txt": 0, "c.txt": 0})
	dest := t.TempDir()

	err := extractTar(tar.NewReader(bytes.NewReader(data)), dest)
	if !errors.Is(err, errExtractTooLarge) {
		t.Fatalf("want errExtractTooLarge, got %v", err)
	}
}

// TestExtractZip_RejectsOversizeContent 覆盖 zip 一侧同样受预算约束。
func TestExtractZip_RejectsOversizeContent(t *testing.T) {
	withLimits(t, maxArchiveBytes, 100, maxArchiveEntries)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("a.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("z"), 200)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	if err := extractZipEntries(zr.File, t.TempDir()); !errors.Is(err, errExtractTooLarge) {
		t.Fatalf("want errExtractTooLarge, got %v", err)
	}
}

package toolchain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

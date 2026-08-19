package main

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

func TestInstallTool_AtomicAndListed(t *testing.T) {
	home := t.TempDir()
	blob, sum := fakeTar(t, "go", "go")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()

	if err := InstallTool(ToolInstallDir(home), "go", "1.23.2", srv.URL+"/go.tar.gz", sum); err != nil {
		t.Fatalf("InstallTool: %v", err)
	}
	root := filepath.Join(ToolInstallDir(home), "go-1.23.2")
	if _, err := os.Stat(filepath.Join(root, "bin", "go")); err != nil {
		t.Fatalf("extracted binary missing: %v", err)
	}
	current, err := os.Readlink(filepath.Join(ToolInstallDir(home), "current", "go"))
	if err != nil || current != root {
		t.Fatalf("current symlink: %v -> %q", err, current)
	}
	names := ListTools(ToolInstallDir(home))
	if len(names) != 1 || names[0] != "go" {
		t.Fatalf("ListTools: got %v", names)
	}
}

func TestInstallTool_ShaMismatch(t *testing.T) {
	home := t.TempDir()
	blob, _ := fakeTar(t, "go", "go")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	err := InstallTool(ToolInstallDir(home), "go", "1.23.2", srv.URL+"/go.tar.gz", "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected sha256 mismatch error")
	}
	if _, statErr := os.Stat(ToolInstallDir(home)); statErr == nil {
		t.Fatal("failed install must leave no install dir")
	}
}

func TestRemoveTool(t *testing.T) {
	home := t.TempDir()
	blob, sum := fakeTar(t, "rg", "rg")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	dir := ToolInstallDir(home)
	if err := InstallTool(dir, "rg", "14.1.0", srv.URL+"/rg.tar.gz", sum); err != nil {
		t.Fatalf("InstallTool: %v", err)
	}
	if err := RemoveTool(dir, "rg"); err != nil {
		t.Fatalf("RemoveTool: %v", err)
	}
	if names := ListTools(dir); len(names) != 0 {
		t.Fatalf("after remove, ListTools: %v", names)
	}
}

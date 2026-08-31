package hosttools

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeExe 写一个可执行 shell 脚本，运行 --version 时输出指定首行。
func fakeExe(t *testing.T, path, version string) {
	t.Helper()
	content := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '" + version + "'\nelse exit 1\nfi\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestProbe(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "go")
	fakeExe(t, exe, "go version go1.23.2 linux/amd64")
	v, ok := Probe(exe)
	if !ok || v != "go version go1.23.2 linux/amd64" {
		t.Fatalf("Probe: ok=%v v=%q", ok, v)
	}
}

func TestProbe_Missing(t *testing.T) {
	if _, ok := Probe(filepath.Join(t.TempDir(), "nope")); ok {
		t.Fatal("不存在的命令应返回 ok=false")
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	// go 根目录：bin/go
	goRoot := filepath.Join(root, "go-1.23")
	if err := os.MkdirAll(filepath.Join(goRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeExe(t, filepath.Join(goRoot, "bin", "go"), "go version go1.23.2 linux/amd64")
	// node 根目录：直接含可执行 node
	nodeRoot := filepath.Join(root, "node-v20")
	if err := os.MkdirAll(nodeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeExe(t, filepath.Join(nodeRoot, "node"), "v20.11.0")
	// 空目录：跳过
	empty := filepath.Join(root, "empty")
	_ = os.MkdirAll(empty, 0o755)
	// 不存在的路径：跳过
	missing := filepath.Join(root, "missing")

	got := Discover([]string{goRoot, nodeRoot, empty, missing})
	byName := map[string]Discovered{}
	for _, d := range got {
		byName[d.Name] = d
	}
	if len(got) != 2 {
		t.Fatalf("应发现 2 个工具链, got %+v", got)
	}
	if d := byName["go-1-23"]; d.Tool != "go" || d.Version != "go version go1.23.2 linux/amd64" {
		t.Fatalf("go 发现结果错误: %+v", d)
	}
	if d := byName["node-v20"]; d.Tool != "node" || d.Version != "v20.11.0" {
		t.Fatalf("node 发现结果错误: %+v", d)
	}
}

func TestPrimaryExecutable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 根目录名与 bin 下命令名一致时优先取它。
	fakeExe(t, filepath.Join(root, "bin", filepath.Base(root)), "x")
	tool, exe := primaryExecutable(root)
	if tool != filepath.Base(root) || exe == "" {
		t.Fatalf("应取同名命令: tool=%q exe=%q", tool, exe)
	}
}

func TestDefaultRootDirs_IncludesHomeTools(t *testing.T) {
	dirs := DefaultRootDirs("/home/u")
	found := false
	for _, d := range dirs {
		if d == filepath.Join("/home/u", "tools") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应包含 ~/tools: %v", dirs)
	}
}

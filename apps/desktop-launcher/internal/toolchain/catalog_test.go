package toolchain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalog_Lookup(t *testing.T) {
	if _, ok := Lookup("go"); !ok {
		t.Fatal("catalog 应含 go")
	}
	if it, ok := Lookup("jdk21"); !ok || it.SHA256 == "" {
		t.Fatalf("jdk21 应已填实 sha256: %+v", it)
	}
	if _, ok := Lookup("nonexistent"); ok {
		t.Fatal("未知项不应命中")
	}
}

func TestCatalog_NoDuplicatedBundledTools(t *testing.T) {
	// 容器已内置 node/git/python,不应重复收录。
	bundled := map[string]bool{"node": true, "git": true, "python3": true, "curl": true, "jq": true}
	for _, it := range Catalog() {
		if bundled[it.Name] {
			t.Errorf("catalog 不应收录容器已内置的 %s", it.Name)
		}
	}
}

func TestLinkBin_LinksExecutables(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "current", "go")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 可执行 go/go fmt 与不可执行 README
	if err := os.WriteFile(filepath.Join(root, "bin", "go"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "gofmt"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	it := CatalogItem{Name: "go", BinRel: "bin"}
	if err := LinkBin(dir, it); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"go", "gofmt"} {
		if _, err := os.Lstat(filepath.Join(dir, "bin", want)); err != nil {
			t.Errorf("bin 缺软链 %s: %v", want, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "bin", "README")); !os.IsNotExist(err) {
		t.Errorf("README 不应被软链: %v", err)
	}
}

func TestConflicts(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	mk := func(dir, name string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), mode); err != nil {
			t.Fatal(err)
		}
	}
	mk(a, "java", 0o755)
	mk(a, "javac", 0o755)
	mk(a, "note.txt", 0o644)
	mk(b, "java", 0o755)
	mk(b, "go", 0o755)
	got := Conflicts(a, b)
	if len(got) != 1 || got[0] != "java" {
		t.Fatalf("冲突应只有 java, got %v", got)
	}
}

func TestCatalogStatuses(t *testing.T) {
	dir := t.TempDir()
	// 预置一个已安装的 go: current/go -> go-1.23.2
	root := filepath.Join(dir, "go-1.23.2")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(dir, "current", "go")); err != nil {
		t.Fatal(err)
	}
	byName := map[string]CatalogStatus{}
	for _, cs := range CatalogStatuses(dir) {
		byName[cs.Name] = cs
	}
	if cs := byName["go"]; cs.State != "installed" || cs.InstalledVersion != "1.23.2" {
		t.Fatalf("go 应已安装且版本 1.23.2: %+v", cs)
	}
	if cs := byName["jdk21"]; cs.State != "installable" || !cs.Pinned {
		t.Fatalf("jdk21 应可安装且已填 sha256: %+v", cs)
	}
}

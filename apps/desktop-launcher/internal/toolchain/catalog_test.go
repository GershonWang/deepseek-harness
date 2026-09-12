package toolchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLookupTool(t *testing.T) {
	if _, ok := LookupTool("go"); !ok {
		t.Fatal("catalog 应含 go")
	}
	if it, ok := LookupTool("jdk21"); !ok || it.LatestVersion().SHA256 == "" {
		t.Fatalf("jdk21 应已填实 sha256: %+v", it)
	}
	if _, ok := LookupTool("nonexistent"); ok {
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

func TestReconcileBinLinks_RebuildsAndCleansStale(t *testing.T) {
	dir := t.TempDir()
	// 两个已装工具：go 用 bin/ 子目录，rg 用根目录直接含可执行
	goRoot := filepath.Join(dir, "go-1.23.2")
	if err := os.MkdirAll(filepath.Join(goRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(goRoot, "bin", "go"), []byte("x"), 0o755)
	os.WriteFile(filepath.Join(goRoot, "bin", "gofmt"), []byte("x"), 0o755)
	rgRoot := filepath.Join(dir, "ripgrep-14.1.0")
	if err := os.MkdirAll(rgRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(rgRoot, "rg"), []byte("x"), 0o755)
	if err := os.MkdirAll(filepath.Join(dir, "current"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.Symlink(goRoot, filepath.Join(dir, "current", "go"))
	os.Symlink(rgRoot, filepath.Join(dir, "current", "ripgrep"))

	// 预置一个失效软链（模拟旧布局/手动破坏）
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.Symlink("/nonexistent/stale", filepath.Join(dir, "bin", "stale"))

	if err := ReconcileBinLinks(dir); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"go", "gofmt", "rg"} {
		if _, err := os.Lstat(filepath.Join(dir, "bin", want)); err != nil {
			t.Errorf("自愈后缺软链 %s: %v", want, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "bin", "stale")); !os.IsNotExist(err) {
		t.Errorf("失效软链 stale 应被清理: %v", err)
	}
}

// TestReconcileBinLinks_RenamesArchiveBinary 覆盖归档内二进制名与命令名不一致的发行包
// （如 yq 归档内是 yq_linux_amd64）：BinNames 映射必须让对外命令名可执行，归档内原名
// 不被暴露（否则用户拿到的命令名会带平台后缀），空值映射的辅助脚本也不能混进 PATH。
func TestReconcileBinLinks_RenamesArchiveBinary(t *testing.T) {
	idx, err := ParseIndex([]byte(`{"version":1,"updated_at":"2026-08-31T00:00:00Z","tools":[` +
		`{"id":"renametool","name":"Renametool","category":"modern-cli","description":"test",` +
		`"provides":["renametool"],"dependencies":[],"versions":[{"version":"1.0.0",` +
		`"url":"https://example.com/r.tar.gz","sha256":"` + strings.Repeat("0", 64) + `",` +
		`"bin_rel":".","bin_names":{"renametool_linux_amd64":"renametool",` +
		`"install-helper.sh":""}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	setCatalog(idx.Tools, idx.CategoryLabels)
	defer restoreBuiltin(t)

	dir := t.TempDir()
	root := filepath.Join(dir, "renametool-1.0.0")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "renametool_linux_amd64"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "install-helper.sh"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(dir, "current", "renametool")); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileBinLinks(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "bin", "renametool")); err != nil {
		t.Errorf("应暴露映射后的命令名 renametool: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "bin", "renametool_linux_amd64")); !os.IsNotExist(err) {
		t.Errorf("归档内原名不应被暴露: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "bin", "install-helper.sh")); !os.IsNotExist(err) {
		t.Errorf("空值映射的辅助脚本不应被暴露: %v", err)
	}
}

func TestToolStatuses(t *testing.T) {
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
	byID := map[string]ToolStatus{}
	for _, cs := range ToolStatuses(dir) {
		byID[cs.ID] = cs
	}
	if cs := byID["go"]; !cs.Installed || cs.ActiveVersion != "1.23.2" {
		t.Fatalf("go 应已安装且版本 1.23.2: %+v", cs)
	}
	if cs := byID["jdk21"]; cs.Installed || cs.AvailableVersion == "" {
		t.Fatalf("jdk21 应未安装且已给出推荐版本: %+v", cs)
	}
}

func TestHasUpdateAndOutdated(t *testing.T) {
	dir := t.TempDir()
	goTool, ok := LookupTool("go")
	if !ok {
		t.Fatal("catalog 应含 go")
	}
	latest := goTool.LatestVersion().Version
	// 安装一个旧版本（不是最新版），应标记为 HasUpdate
	oldRoot := filepath.Join(dir, "go-1.21.0")
	if err := os.MkdirAll(oldRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldRoot, filepath.Join(dir, "current", "go")); err != nil {
		t.Fatal(err)
	}

	// 用一个已装但版本等于最新版的工具做对照（模拟）
	// 这里直接验证 OutdatedTools 和 HasUpdate
	outdated := OutdatedTools(dir)
	hasGo := false
	for _, id := range outdated {
		if id == "go" {
			hasGo = true
		}
	}
	if latest != "1.21.0" && !hasGo {
		t.Fatalf("go 1.21.0 应标记为可更新（最新版=%s）", latest)
	}
	// 最新版就是 1.21.0 时 HasUpdate 应为 false（边界情况跳过强校验）

	byID := map[string]ToolStatus{}
	for _, cs := range ToolStatuses(dir) {
		byID[cs.ID] = cs
	}
	goStatus := byID["go"]
	if latest != "1.21.0" && !goStatus.HasUpdate {
		t.Fatalf("go 应有 HasUpdate=true, got %+v", goStatus)
	}
	if latest == "1.21.0" && goStatus.HasUpdate {
		t.Fatalf("go 已是最新版不应有 HasUpdate")
	}
}

func TestCatalog_Uv(t *testing.T) {
	it, ok := LookupTool("uv")
	if !ok {
		t.Fatal("catalog 应含 uv")
	}
	v := it.LatestVersion()
	if v.SHA256 == "" {
		t.Fatal("uv 应已填实 sha256")
	}
	if v.URL != "https://github.com/astral-sh/uv/releases/download/0.12.6/uv-x86_64-unknown-linux-gnu.tar.gz" {
		t.Fatalf("uv URL 应指向官方 0.12.6 gnu tarball: %+v", v)
	}
	if v.BinRel != "." {
		t.Fatalf("uv tarball 单顶层目录剥离后可执行在根: BinRel 应为 .: %+v", v)
	}
	if v.Version != "0.12.6" {
		t.Fatalf("uv 版本应为 0.12.6: %+v", v)
	}
}

// TestCatalog_EveryCategoryHasLabel 固定清单侧的不变量：工具用到的每个分类都必须在
// 同一份索引里声明中文标签。标签随索引下发后，"页签显示英文 ID" 不再由客户端保证，
// 而是这份清单自己必须守住的事。
func TestCatalog_EveryCategoryHasLabel(t *testing.T) {
	idx, err := ParseIndex(indexJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.CategoryLabels) == 0 {
		t.Fatal("内置索引应声明 category_labels")
	}
	for _, tool := range idx.Tools {
		if idx.CategoryLabels[tool.Category] == "" {
			t.Errorf("分类 %s（工具 %s）缺 category_labels 条目", tool.Category, tool.ID)
		}
	}
}

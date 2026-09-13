package toolchain

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// indexRefPattern 匹配 git 的不可变引用形式：40 位小写十六进制提交哈希。
var indexRefPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// restoreBuiltin 把有效目录恢复为内置兜底，避免远程测试污染其他用例。
func restoreBuiltin(t *testing.T) {
	t.Helper()
	idx, err := ParseIndex(indexJSON)
	if err != nil {
		t.Fatal(err)
	}
	setCatalog(idx.Tools, idx.CategoryLabels)
}

// testIndex 返回一份含自定义工具 ID 的索引 JSON，用于区分远程与内置。
func testIndex(toolID string) []byte {
	return []byte(`{"version":1,"updated_at":"2026-08-31T00:00:00Z",` +
		`"category_labels":{"modern-cli":"索引声明的现代 CLI"},` +
		`"tools":[{"id":"` + toolID + `","name":"Test","category":"modern-cli",` +
		`"description":"test","provides":["` + toolID + `"],"dependencies":[],` +
		`"versions":[{"version":"1.0.0","url":"https://example.com/t.tar.gz",` +
		`"sha256":"` + "0000000000000000000000000000000000000000000000000000000000000000" + `","bin_rel":"bin"}]}],` +
		`"bundles":[]}`)
}

func TestParseIndex_Valid(t *testing.T) {
	idx, err := ParseIndex(testIndex("zzz"))
	if err != nil {
		t.Fatal(err)
	}
	if idx.Version != 1 || len(idx.Tools) != 1 || idx.Tools[0].ID != "zzz" {
		t.Fatalf("parse result wrong: %+v", idx)
	}
}

func TestParseIndex_UnsupportedVersion(t *testing.T) {
	if _, err := ParseIndex([]byte(`{"version":99,"tools":[]}`)); err == nil {
		t.Fatal("version 99 应报错")
	}
}

func TestLoadIndex_FetchesAndCaches(t *testing.T) {
	restoreBuiltin(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write(testIndex("remote-tool"))
	}))
	defer srv.Close()
	t.Setenv("DSH_TOOLCHAIN_INDEX_URL", srv.URL)

	dir := t.TempDir()
	src, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if src != "remote" {
		t.Fatalf("首次应来自 remote, got %q", src)
	}
	if _, ok := LookupTool("remote-tool"); !ok {
		t.Fatal("有效目录应含 remote-tool")
	}
	if got := CategoryLabels()["modern-cli"]; got != "索引声明的现代 CLI" {
		t.Fatalf("分类标签应随索引生效, got %q", got)
	}
	if _, err := os.Stat(indexCachePath(dir)); err != nil {
		t.Fatal("应写入缓存 index.json")
	}
	if _, err := os.Stat(indexMetaPath(dir)); err != nil {
		t.Fatal("应写入缓存 meta")
	}

	// 缓存未过期：第二次直接读缓存，不再请求远程。
	src, err = LoadIndex(dir)
	if err != nil || src != "cache" {
		t.Fatalf("第二次应来自 cache, got %q err=%v", src, err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("缓存未过期不应再拉取, hits=%d", hits)
	}
	restoreBuiltin(t)
}

func TestLoadIndex_FallbackToCacheWhenRemoteDown(t *testing.T) {
	restoreBuiltin(t)
	// 先写入一份缓存（用死服务器拉不到，但缓存已存在且"过期"）。
	dir := t.TempDir()
	if err := storeIndex(dir, testIndex("cached-tool")); err != nil {
		t.Fatal(err)
	}
	// 把 meta 改成 48h 前，制造过期缓存。
	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	if err := os.WriteFile(indexMetaPath(dir), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	// 死服务器：连接被拒。
	t.Setenv("DSH_TOOLCHAIN_INDEX_URL", "http://127.0.0.1:1/")

	src, err := LoadIndex(dir)
	if err == nil {
		t.Fatal("远程不可达应返回错误")
	}
	if src != "cache" {
		t.Fatalf("远程失败应回退缓存, got %q", src)
	}
	if _, ok := LookupTool("cached-tool"); !ok {
		t.Fatal("回退缓存后有效目录应含 cached-tool")
	}
	restoreBuiltin(t)
}

func TestLoadIndex_FallbackToBuiltin(t *testing.T) {
	restoreBuiltin(t)
	t.Setenv("DSH_TOOLCHAIN_INDEX_URL", "http://127.0.0.1:1/")
	dir := t.TempDir() // 无缓存
	src, err := LoadIndex(dir)
	if err == nil {
		t.Fatal("远程不可达应返回错误")
	}
	if src != "builtin" {
		t.Fatalf("无缓存时应回退内置, got %q", src)
	}
	if _, ok := LookupTool("go"); !ok {
		t.Fatal("内置兜底应含 go")
	}
}

func TestIndexCachePath(t *testing.T) {
	if indexCachePath(filepath.Join("a", "b")) != filepath.Join("a", "b", "index.json") {
		t.Fatal("indexCachePath 应为 <dir>/index.json")
	}
}

// TestDefaultIndexURL_PinnedToCommit 守卫默认索引地址不退回可变引用。
//
// 索引同时提供每个工具的下载地址与 sha256，两者出自同一份数据，因此 sha256 只证明
// 归档与清单一致（完整性），不证明清单本身可信（来源认证）。引用若仍是分支名，
// 控制该分支即可整体替换索引及其哈希。固定到提交哈希后，索引内容由客户端自身的
// 发布过程锚定；代价是索引更新需要重新发版，这是安全性与更新敏捷性之间的取舍。
func TestDefaultIndexURL_PinnedToCommit(t *testing.T) {
	const rawPrefix = "https://raw.githubusercontent.com/GershonWang/deepseek-harness/"
	rest, ok := strings.CutPrefix(defaultIndexURL, rawPrefix)
	if !ok {
		t.Fatalf("默认索引地址不再位于预期的发布位置：%s", defaultIndexURL)
	}
	ref, path, ok := strings.Cut(rest, "/")
	if !ok {
		t.Fatalf("默认索引地址缺少文件路径：%s", defaultIndexURL)
	}
	if !indexRefPattern.MatchString(ref) {
		t.Fatalf("默认索引引用 %q 不是 40 位提交哈希；可变引用会让索引内容被整体替换", ref)
	}
	if path != "apps/desktop-launcher/internal/toolchain/tools/index.json" {
		t.Fatalf("默认索引地址指向意外路径：%s", path)
	}
}

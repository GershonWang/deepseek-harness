// 大版本线分组与两级版本选择的边界测试。
//
// 这些用例钉住的是「界面上到底出现哪些选项」——分组算错不会报错，只会让用户在卡片上
// 看不到、切不回、或卸不掉某个版本，而磁盘上那份安装仍占着空间。因此逐条把设计约定
// （同线只留最新 + 已装、跨线全列、未装线只给最新）写成可执行断言。
package toolchain

import (
	"testing"
)

// groupOf 取出某个工具指定大版本线的分组；不存在返回 nil。
func groupOf(t *testing.T, dir, id, major string) *VersionGroup {
	t.Helper()
	for _, g := range statusOf(t, dir, id).Groups {
		if g.Major == major {
			return &g
		}
	}
	return nil
}

// TestVersionGroups_SingleMajorNoSplit 固定「单线工具不出现大版本下拉」：界面据
// len(Groups) > 1 决定是否渲染一级下拉，因此单大版本工具必须恰好归为一组，
// 否则所有单版本工具（多数）都会多挂一个恒定不变的下拉，白占卡片宽度。
func TestVersionGroups_SingleMajorNoSplit(t *testing.T) {
	dir := t.TempDir()
	mkToolVersion(t, dir, "bat", "0.26.1", "bat")

	ts := statusOf(t, dir, "bat")
	if len(ts.Groups) != 1 {
		t.Fatalf("单线工具应恰好归为一组, got %d 组: %+v", len(ts.Groups), ts.Groups)
	}
	if g := ts.Groups[0]; len(g.Versions) != 1 || g.Versions[0] != "0.26.1" {
		t.Fatalf("已装即最新时应只有一项, got %v", g.Versions)
	}
}

// TestVersionGroups_OldPatchShowsLatestAndInstalled 固定设计里那条核心约定：
// 装了旧小版本时，二级只列「该线最新」与「本机已装」两项——同线的历史小版本彼此
// 只差补丁，全列出来会淹没真正要选的项。
//
// 用 uv 造场景：它只有单条线，因此「清单里的版本」与「已装版本」的关系最直白——
// 装 0.12.6（旧）而清单最新是 0.12.19，二级应恰好是这两项。清单里若有第三个版本
// （如未来的 0.12.20）也不该出现，因为它既非最新也未安装。
func TestVersionGroups_OldPatchShowsLatestAndInstalled(t *testing.T) {
	dir := t.TempDir()
	mkToolVersion(t, dir, "uv", "0.12.6", "uv")
	if err := SetActiveVersion(dir, "uv", "0.12.6"); err != nil {
		t.Fatal(err)
	}

	ts := statusOf(t, dir, "uv")
	if len(ts.Groups) != 1 {
		t.Fatalf("uv 是单线工具，应恰好一组, got %+v", ts.Groups)
	}
	g := ts.Groups[0]
	tool, ok := LookupTool("uv")
	if !ok {
		t.Fatal("catalog 应含 uv")
	}
	latest := tool.LatestVersion().Version
	want := []string{latest, "0.12.6"}
	if len(g.Versions) != len(want) || g.Versions[0] != want[0] || g.Versions[1] != want[1] {
		t.Fatalf("应恰好列出该线最新与已装 %v, got %v", want, g.Versions)
	}
}

// TestVersionGroups_InstalledIsLatestIsSingleEntry 固定「已装就是最新时只有一项」：
// 同一版本不该同时以「最新」和「已装」两种身份重复出现。
func TestVersionGroups_InstalledIsLatestIsSingleEntry(t *testing.T) {
	dir := t.TempDir()
	mkToolVersion(t, dir, "jdk", "21.0.12.1", "java")
	if err := SetActiveVersion(dir, "jdk", "21.0.12.1"); err != nil {
		t.Fatal(err)
	}

	g := groupOf(t, dir, "jdk", "21")
	if g == nil {
		t.Fatal("应有 21 线分组")
	}
	if len(g.Versions) != 1 || g.Versions[0] != "21.0.12.1" {
		t.Fatalf("已装即最新应只有一项, got %v", g.Versions)
	}
	if g.Active != "21.0.12.1" {
		t.Fatalf("该线应标为激活, got %q", g.Active)
	}
}

// TestVersionGroups_UninstalledMajorShowsOnlyLatest 固定「该大版本没装时只显示其
// 最新版本号」：用户在本机没装过 JDK 8 时，8 线的二级下拉只有 8u504 一项，
// 不该把清单里该线的其它版本铺开。
func TestVersionGroups_UninstalledMajorShowsOnlyLatest(t *testing.T) {
	dir := t.TempDir()
	mkToolVersion(t, dir, "jdk", "21.0.12.1", "java")
	if err := SetActiveVersion(dir, "jdk", "21.0.12.1"); err != nil {
		t.Fatal(err)
	}

	g := groupOf(t, dir, "jdk", "8")
	if g == nil {
		t.Fatal("未安装的 8 线也应出现在一级下拉里")
	}
	if len(g.Installed) != 0 {
		t.Fatalf("8 线不应有已装版本, got %v", g.Installed)
	}
	if len(g.Versions) != 1 || g.Versions[0] != "8u504" {
		t.Fatalf("未装的线应只列该线最新 8u504, got %v", g.Versions)
	}
	if g.Active != "" {
		t.Fatalf("未安装的线不应标为激活, got %q", g.Active)
	}
}

// TestVersionGroups_CrossMajorNotUpdate 固定「跨大版本不提示更新」的那条设计决定：
// 激活 8u504（8 线唯一且在装的版本）时不得提示可更新，因为 21 属于另一条线，
// 那是换工具链而非打补丁。同一条线内低于最新才提示。
func TestVersionGroups_CrossMajorNotUpdate(t *testing.T) {
	t.Run("跨大版本不提示", func(t *testing.T) {
		dir := t.TempDir()
		mkToolVersion(t, dir, "jdk", "8u504", "java")
		if err := SetActiveVersion(dir, "jdk", "8u504"); err != nil {
			t.Fatal(err)
		}
		if ts := statusOf(t, dir, "jdk"); ts.HasUpdate {
			t.Fatalf("激活 8u504 不应提示更新（跨大版本）, UpdateTarget=%q", ts.UpdateTarget)
		}
	})
	t.Run("同线内低于最新才提示", func(t *testing.T) {
		dir := t.TempDir()
		mkToolVersion(t, dir, "jdk", "21.0.9", "java")
		if err := SetActiveVersion(dir, "jdk", "21.0.9"); err != nil {
			t.Fatal(err)
		}
		ts := statusOf(t, dir, "jdk")
		if !ts.HasUpdate {
			t.Fatal("21.0.9 低于该线最新 21.0.12.1，应提示更新")
		}
		// 更新目标必须是该线的最新版，而不是清单首项之外的别的东西。
		if ts.UpdateTarget != "21.0.12.1" {
			t.Fatalf("更新目标应为该线最新 21.0.12.1, got %q", ts.UpdateTarget)
		}
	})
}

// TestVersionGroups_OrphanVersionKept 固定孤儿版本必须保留：装了一个已从清单下架
// （或手工放进目录）的版本时，它要单独成组、可见、可切换、可卸载。归组逻辑一旦
// 漏掉它，用户在卡片上既看不到也删不掉，磁盘上那份安装仍占着空间。
func TestVersionGroups_OrphanVersionKept(t *testing.T) {
	dir := t.TempDir()
	// 99.0.0 不在清单里：兜底规则把它归入 "99" 线，该线因此只有这一个版本。
	mkToolVersion(t, dir, "jdk", "99.0.0", "java")
	if err := SetActiveVersion(dir, "jdk", "99.0.0"); err != nil {
		t.Fatal(err)
	}

	orphan := groupOf(t, dir, "jdk", "99")
	if orphan == nil {
		t.Fatal("孤儿版本应单独成组并保留，否则用户看不到也卸不掉")
	}
	if len(orphan.Versions) != 1 || orphan.Versions[0] != "99.0.0" {
		t.Fatalf("孤儿组应列出该版本自身, got %v", orphan.Versions)
	}
	if orphan.Active != "99.0.0" {
		t.Fatalf("孤儿组应标为激活, got %q", orphan.Active)
	}
	if !statusOf(t, dir, "jdk").Installed {
		t.Fatal("只有孤儿版本时工具仍应记为已安装")
	}
}

// TestVersionGroups_ActiveOrphanDoesNotCrash 固定「激活版本指向孤儿时不崩」：
// updateOf 需在激活版本所属的组里找目标，若该组不在 groups 里（理论上不该发生）
// 也不能返回越界或错误目标。这里断言它能正常给出状态而不是 panic。
func TestVersionGroups_ActiveOrphanDoesNotCrash(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"99.0.0", "21.0.9"} {
		mkToolVersion(t, dir, "jdk", v, "java")
	}
	// 激活那个孤儿版本：其余线（21）都比它旧，跨线不该被判为更新。
	if err := SetActiveVersion(dir, "jdk", "99.0.0"); err != nil {
		t.Fatal(err)
	}

	ts := statusOf(t, dir, "jdk")
	if ts.ActiveVersion != "99.0.0" {
		t.Fatalf("激活版本应为孤儿 99.0.0, got %q", ts.ActiveVersion)
	}
	if ts.HasUpdate {
		t.Fatalf("激活孤儿版本不应被跨线判为可更新, UpdateTarget=%q", ts.UpdateTarget)
	}
	if len(ts.InstalledVersions) != 2 {
		t.Fatalf("两个已装版本都应可见, got %v", ts.InstalledVersions)
	}
}

// TestVersionGroups_UninstallOrphan 固定孤儿版本可卸载：可见之外还要真的卸得掉，
// 否则「保留孤儿」只是把问题从看不见变成删不掉。
func TestVersionGroups_UninstallOrphan(t *testing.T) {
	dir := t.TempDir()
	mkToolVersion(t, dir, "jdk", "99.0.0", "java")
	mkToolVersion(t, dir, "jdk", "21.0.12.1", "java")
	if err := SetActiveVersion(dir, "jdk", "99.0.0"); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(dir, "jdk", "99.0.0"); err != nil {
		t.Fatalf("孤儿版本应可卸载: %v", err)
	}
	if IsInstalled(dir, "jdk", "99.0.0") {
		t.Fatal("卸载后孤儿版本目录应已移除")
	}
	if groupOf(t, dir, "jdk", "99") != nil {
		t.Fatal("卸载后孤儿组应消失")
	}
}

// TestVersionGroups_DeclaredMajorOverridesFallback 固定「清单显式声明优先于首段兜底」：
// 这是 Go 与 Rust 必须归一条线的原因——它们的功能版本在第二段（1.26/1.27）或根本
// 不区分（1.98/1.99），按首段切会把它们并成 "1"；而 JDK 8 的 `8u504` 又必须切出 "8"。
// 两种情形都要求显式声明说了算，否则装了 1.23.2 的用户会掉进孤儿组而收不到更新。
func TestVersionGroups_DeclaredMajorOverridesFallback(t *testing.T) {
	dir := t.TempDir()
	// go 在清单里显式声明 major="1"；本机装 1.23.2（兜底也会算成 "1"）。
	mkToolVersion(t, dir, "go", "1.23.2", "go")
	if err := SetActiveVersion(dir, "go", "1.23.2"); err != nil {
		t.Fatal(err)
	}

	ts := statusOf(t, dir, "go")
	if len(ts.Groups) != 1 {
		t.Fatalf("go 应归一条线（显式 major=1）, got %+v", ts.Groups)
	}
	g := ts.Groups[0]
	if g.Major != "1" {
		t.Fatalf("go 的大版本线应取显式声明 1, got %q", g.Major)
	}
	// 清单里的 1.27.1 与本机 1.23.2 同线，因此必须提示更新——这正是归一条线的目的。
	if !ts.HasUpdate {
		t.Fatal("已装 1.23.2 应收到同线最新 1.27.1 的更新提示")
	}
	if ts.UpdateTarget != "1.27.1" {
		t.Fatalf("更新目标应为 1.27.1, got %q", ts.UpdateTarget)
	}
}

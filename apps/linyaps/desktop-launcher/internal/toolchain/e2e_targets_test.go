package toolchain

import (
	"strings"
	"testing"
)

// TestE2ETargets_EveryVersionByDefault 断言默认展开到每个工具的全部版本线——审计的价值
// 就在覆盖推荐版本之外的那几条，只装推荐版本的旧行为是 AUDIT N22 的成因。
func TestE2ETargets_EveryVersionByDefault(t *testing.T) {
	targets, err := e2eTargets("", "")
	if err != nil {
		t.Fatalf("默认展开失败: %v", err)
	}
	want := 0
	for _, tool := range Catalog() {
		want += len(tool.Versions)
	}
	if len(targets) != want {
		t.Fatalf("默认目标数 %d，期望 %d（每个工具的全部版本线）", len(targets), want)
	}
	for _, tool := range Catalog() {
		for _, version := range tool.Versions {
			target := e2eTarget{tool: tool, version: version}
			if !containsTarget(targets, target) {
				t.Errorf("默认展开漏掉 %s", target)
			}
		}
	}
}

// TestE2ETargets_IDFilter 断言 ID 过滤只留下指定工具的版本线。
func TestE2ETargets_IDFilter(t *testing.T) {
	tool, ok := LookupTool("jdk")
	if !ok {
		t.Fatal("清单里没有 jdk")
	}
	targets, err := e2eTargets(" jdk ", "")
	if err != nil {
		t.Fatalf("ID 过滤失败: %v", err)
	}
	if len(targets) != len(tool.Versions) {
		t.Fatalf("jdk 目标数 %d，期望 %d", len(targets), len(tool.Versions))
	}
	for _, target := range targets {
		if target.tool.ID != "jdk" {
			t.Errorf("ID 过滤后仍出现 %s", target)
		}
	}
}

// TestE2ETargets_VersionFilterCoversNonRecommended 断言版本过滤能精确命中非推荐版本线：
// 这是「改过某条旧版本地址后只复查它」的用法，旧用例按 ID 过滤做不到。
func TestE2ETargets_VersionFilterCoversNonRecommended(t *testing.T) {
	tool, ok := LookupTool("jdk")
	if !ok || len(tool.Versions) < 2 {
		t.Skip("清单里的 jdk 不足两条版本线，无法验证非推荐版本过滤")
	}
	nonRecommended := tool.Versions[1].Version
	targets, err := e2eTargets("", "jdk@"+nonRecommended)
	if err != nil {
		t.Fatalf("版本过滤失败: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("版本过滤得到 %d 条目标，期望 1 条", len(targets))
	}
	if targets[0].String() != "jdk@"+nonRecommended {
		t.Fatalf("版本过滤得到 %s，期望 jdk@%s", targets[0], nonRecommended)
	}
}

// TestE2ETargets_IDAndVersionFiltersIntersect 断言两个过滤器同时给出时取交集，而不是
// 其中一个静默失效。
func TestE2ETargets_IDAndVersionFiltersIntersect(t *testing.T) {
	tool, ok := LookupTool("jdk")
	if !ok || len(tool.Versions) < 2 {
		t.Skip("清单里的 jdk 不足两条版本线")
	}
	pick := tool.Versions[1].Version
	targets, err := e2eTargets("jdk", "jdk@"+pick)
	if err != nil {
		t.Fatalf("交集过滤失败: %v", err)
	}
	if len(targets) != 1 || targets[0].String() != "jdk@"+pick {
		t.Fatalf("交集过滤得到 %v，期望只剩 jdk@%s", targets, pick)
	}
}

// TestE2ETargets_TrimsAndDedupes 断言空白被忽略、重复项只跑一次：审计命令多为手敲，
// 重复条目会白下载一份归档。
func TestE2ETargets_TrimsAndDedupes(t *testing.T) {
	targets, err := e2eTargets(" jdk , jdk ", "")
	if err != nil {
		t.Fatalf("重复 ID 过滤失败: %v", err)
	}
	tool, _ := LookupTool("jdk")
	if len(targets) != len(tool.Versions) {
		t.Fatalf("重复 ID 展开出 %d 条目标，期望 %d", len(targets), len(tool.Versions))
	}

	pick := tool.Versions[0].Version
	targets, err = e2eTargets("", "jdk@"+pick+", jdk@"+pick)
	if err != nil {
		t.Fatalf("重复版本过滤失败: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("重复版本过滤得到 %d 条目标，期望 1", len(targets))
	}
}

// TestE2ETargets_RejectsInvalidFilters 断言配置错误一律报错退出：审计跑空会被误读成
// 「清单没问题」，比失败更危险。
func TestE2ETargets_RejectsInvalidFilters(t *testing.T) {
	jdk, ok := LookupTool("jdk")
	if !ok || len(jdk.Versions) < 2 {
		t.Skip("清单里的 jdk 不足两条版本线")
	}
	cases := []struct {
		name     string
		ids      string
		versions string
		want     string
	}{
		{"未知工具 ID", "nope", "", "没有工具 nope"},
		{"未知版本号", "", "jdk@0.0.0", "没有版本 0.0.0"},
		{"缺少 @ 分隔", "", "jdk", "<工具ID>@<版本>"},
		{"版本过滤里的未知工具", "", "nope@1.0.0", "没有工具 nope"},
		{"与 ID 过滤矛盾", "python", "jdk@" + jdk.Versions[1].Version, "矛盾"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e2eTargets(tc.ids, tc.versions)
			if err == nil {
				t.Fatalf("期望报错（%s），实际通过", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误信息 %q 未包含 %q", err.Error(), tc.want)
			}
		})
	}
}

// containsTarget 判断目标列表里是否已有同一工具的同一条版本线。
func containsTarget(targets []e2eTarget, want e2eTarget) bool {
	for _, target := range targets {
		if target.String() == want.String() {
			return true
		}
	}
	return false
}

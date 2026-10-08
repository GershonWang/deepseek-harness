package toolchain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// e2eTarget 是一次端到端审计要真实安装的目标：某个工具的一条版本线。
type e2eTarget struct {
	tool    Tool
	version ToolVersion
}

// String 返回 `工具ID@版本号`，让日志与失败信息直接定位到具体版本线。
func (t e2eTarget) String() string {
	return t.tool.ID + "@" + t.version.Version
}

// e2eTargets 按环境变量展开待审计的目标列表。
//
// 为什么默认遍历每个工具的全部版本而不是只装推荐版本：索引里 43 个工具共 55 条版本线，
// 全量下载比只装推荐版本多约三分之一，而推荐版本之外的版本线此前没有任何下载级覆盖——
// 镜像站轮换或 sha256 抄错一个字符，只会在用户点安装时暴露（AUDIT N22）。因此「只跑
// 推荐版本」不是默认行为，而是显式收窄。
//
// 收窄方式：ids 按工具 ID 过滤（逗号分隔，空为全部）；versions 按 `<工具ID>@<版本>` 精确
// 指定（逗号分隔）。两者都给出时取交集，交集为空即报错，避免「以为跑了其实没跑」。
func e2eTargets(ids, versions string) ([]e2eTarget, error) {
	idFilter, err := parseE2EToolIDs(ids)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(versions) != "" {
		return parseE2EVersions(versions, idFilter)
	}
	var out []e2eTarget
	for _, tool := range Catalog() {
		if idFilter != nil && !idFilter[tool.ID] {
			continue
		}
		for _, version := range tool.Versions {
			out = append(out, e2eTarget{tool: tool, version: version})
		}
	}
	return out, nil
}

// parseE2EToolIDs 解析工具 ID 过滤；空值返回 nil（表示不过滤），未收录的 ID 立即报错。
func parseE2EToolIDs(ids string) (map[string]bool, error) {
	if strings.TrimSpace(ids) == "" {
		return nil, nil
	}
	out := map[string]bool{}
	for _, id := range strings.Split(ids, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := LookupTool(id); !ok {
			return nil, fmt.Errorf("清单里没有工具 %s", id)
		}
		out[id] = true
	}
	return out, nil
}

// parseE2EVersions 解析 `<工具ID>@<版本>` 过滤，逐项校验工具与版本都存在并去掉重复项。
//
// idFilter 非 nil 时要求 ID 在其中：版本过滤与 ID 过滤矛盾时失败，而不是静默跑空内存疑
// 「审计通过」。
func parseE2EVersions(versions string, idFilter map[string]bool) ([]e2eTarget, error) {
	var out []e2eTarget
	seen := map[string]bool{}
	for _, entry := range strings.Split(versions, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		id, ver, ok := strings.Cut(entry, "@")
		if !ok || id == "" || ver == "" {
			return nil, fmt.Errorf("版本过滤项 %q 不是 <工具ID>@<版本> 形式", entry)
		}
		tool, ok := LookupTool(id)
		if !ok {
			return nil, fmt.Errorf("清单里没有工具 %s（版本过滤项 %q）", id, entry)
		}
		if idFilter != nil && !idFilter[id] {
			return nil, fmt.Errorf("版本过滤项 %q 与 DSH_TC_E2E_IDS 矛盾：该工具已被排除", entry)
		}
		version, found := tool.FindVersion(ver)
		if !found {
			return nil, fmt.Errorf("工具 %s 没有版本 %s", id, ver)
		}
		target := e2eTarget{tool: tool, version: version}
		if seen[target.String()] {
			continue
		}
		seen[target.String()] = true
		out = append(out, target)
	}
	return out, nil
}

// TestE2E_CatalogInstall 按索引逐条版本线真实安装，端到端验证：地址可达、归档 sha256 与
// 清单一致、解压布局与 bin_rel/bin_names 声明相符、声明过的命令确实出现在 bin/ 且软链指向
// 真实文件。
//
// 存在的理由：市场清单里写的是固定 URL，而镜像站会轮换版本（Apache dlcdn 只保留当前
// 版本，旧地址静默 404），旧版本地址腐坏要等用户点安装才暴露。这里提供一条主动审计路径。
//
// 默认跳过：依赖外网并会按工具体积下载（当前清单全量约 4.7 GB）。设 DSH_TC_E2E=1 启用；
// DSH_TC_E2E_IDS 按工具 ID 收窄，DSH_TC_E2E_VERSIONS 按 `<工具ID>@<版本>` 收窄（例如改过
// jdk 的某条版本线后只跑它：DSH_TC_E2E_VERSIONS=jdk@8u504），两者可同时给出并取交集。
func TestE2E_CatalogInstall(t *testing.T) {
	if os.Getenv("DSH_TC_E2E") != "1" {
		t.Skip("需要 DSH_TC_E2E=1 且可访问外网")
	}
	targets, err := e2eTargets(os.Getenv("DSH_TC_E2E_IDS"), os.Getenv("DSH_TC_E2E_VERSIONS"))
	if err != nil {
		t.Fatalf("端到端审计目标无效: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("没有待审计的目标：检查 DSH_TC_E2E_IDS / DSH_TC_E2E_VERSIONS")
	}
	var declared int64
	for _, target := range targets {
		declared += target.version.Size
	}
	t.Logf("端到端审计 %d 条版本线，索引声明总大小 %s", len(targets), humanSize(declared))

	for _, target := range targets {
		// 每个目标用独立目录：bin/ 只可能来自本次安装，声明之外的多余软链一眼可见。
		dir := t.TempDir()
		if err := InstallTool(dir, target.tool.ID, target.version.Version, nil); err != nil {
			t.Errorf("%s: 安装失败: %v", target, err)
			continue
		}
		linked := map[string]bool{}
		entries, err := os.ReadDir(filepath.Join(dir, "bin"))
		if err != nil {
			t.Errorf("%s: 读 bin/ 失败: %v", target, err)
			continue
		}
		for _, e := range entries {
			linked[e.Name()] = true
			linkTarget, err := os.Readlink(filepath.Join(dir, "bin", e.Name()))
			if err != nil {
				t.Errorf("%s: bin/%s 不是软链: %v", target, e.Name(), err)
				continue
			}
			if _, err := os.Stat(linkTarget); err != nil {
				t.Errorf("%s: bin/%s 指向的目标不存在: %v", target, e.Name(), err)
			}
		}
		for _, cmd := range target.tool.Provides {
			if !linked[cmd] {
				t.Errorf("%s: 声明的命令 %s 未出现在 bin/（实际 %v）", target, cmd, keys(linked))
			}
		}
		t.Logf("OK %s 声明 %v", target, target.tool.Provides)
	}
}

// keys 返回集合里的键，仅用于失败信息里报出实际暴露的命令。
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// humanSize 把索引声明的字节数格式化成便于阅读的量级：只跑一两条小版本线时用 GB 会
// 打印成 "0.0 GB"，看起来像清单没写 size。
func humanSize(bytes int64) string {
	if bytes < 1<<30 {
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	}
	return fmt.Sprintf("%.2f GB", float64(bytes)/(1<<30))
}

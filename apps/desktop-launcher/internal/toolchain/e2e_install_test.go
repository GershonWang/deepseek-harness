package toolchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_CatalogInstall 按索引逐个真实安装，端到端验证：地址可达、归档 sha256 与清单
// 一致、解压布局与 bin_rel/bin_names 声明相符、声明过的命令确实出现在 bin/ 且软链指向
// 真实文件。
//
// 存在的理由：市场清单里写的是固定 URL，而镜像站会轮换版本（Apache dlcdn 只保留当前
// 版本，旧地址静默 404），腐坏要等用户点安装才暴露。这里提供一条主动审计路径。
//
// 默认跳过：依赖外网并会按工具体积下载（整份清单约 2GB）。设 DSH_TC_E2E=1 启用，
// 可用 DSH_TC_E2E_IDS 逗号分隔只跑指定工具，便于改清单后抽查。
func TestE2E_CatalogInstall(t *testing.T) {
	if os.Getenv("DSH_TC_E2E") != "1" {
		t.Skip("需要 DSH_TC_E2E=1 且可访问外网")
	}
	var tools []Tool
	if filter := os.Getenv("DSH_TC_E2E_IDS"); filter != "" {
		for _, id := range strings.Split(filter, ",") {
			tool, ok := LookupTool(strings.TrimSpace(id))
			if !ok {
				t.Fatalf("清单里没有工具 %s", id)
			}
			tools = append(tools, tool)
		}
	} else {
		tools = Catalog()
	}

	for _, tool := range tools {
		// 每个工具用独立目录：bin/ 只可能来自本次安装，声明之外的多余软链一眼可见。
		dir := t.TempDir()
		if err := InstallTool(dir, tool.ID, "", nil); err != nil {
			t.Errorf("%s: 安装失败: %v", tool.ID, err)
			continue
		}
		linked := map[string]bool{}
		entries, err := os.ReadDir(filepath.Join(dir, "bin"))
		if err != nil {
			t.Errorf("%s: 读 bin/ 失败: %v", tool.ID, err)
			continue
		}
		for _, e := range entries {
			linked[e.Name()] = true
			target, err := os.Readlink(filepath.Join(dir, "bin", e.Name()))
			if err != nil {
				t.Errorf("%s: bin/%s 不是软链: %v", tool.ID, e.Name(), err)
				continue
			}
			if _, err := os.Stat(target); err != nil {
				t.Errorf("%s: bin/%s 指向的目标不存在: %v", tool.ID, e.Name(), err)
			}
		}
		for _, cmd := range tool.Provides {
			if !linked[cmd] {
				t.Errorf("%s: 声明的命令 %s 未出现在 bin/（实际 %v）", tool.ID, cmd, keys(linked))
			}
		}
		t.Logf("OK %s %s 声明 %v", tool.ID, tool.LatestVersion().Version, tool.Provides)
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

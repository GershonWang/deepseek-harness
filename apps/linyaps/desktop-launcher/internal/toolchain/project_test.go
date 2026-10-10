package toolchain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProjectConfig(t *testing.T) {
	data := []byte(`# 项目工具配置
tools:
  go: "1.23.2"      # 内联注释
  gcc: 12.3.0
  python: '3.11'
auto_switch: true
auto_prompt: false
`)
	cfg, err := ParseProjectConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools["go"] != "1.23.2" || cfg.Tools["gcc"] != "12.3.0" || cfg.Tools["python"] != "3.11" {
		t.Fatalf("tools 解析错误: %+v", cfg.Tools)
	}
	if !cfg.AutoSwitch {
		t.Fatal("auto_switch 应为 true")
	}
	if cfg.AutoPrompt {
		t.Fatal("auto_prompt 应为 false")
	}
}

func TestParseProjectConfig_Defaults(t *testing.T) {
	cfg, err := ParseProjectConfig([]byte("tools:\n  go: 1.23.2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoSwitch || !cfg.AutoPrompt {
		t.Fatal("默认 auto_switch/auto_prompt 应为 true")
	}
}

func TestParseProjectConfig_Errors(t *testing.T) {
	if _, err := ParseProjectConfig([]byte("auto_switch: maybe\n")); err == nil {
		t.Fatal("非法 bool 应报错")
	}
	if _, err := ParseProjectConfig([]byte("tools: [a, b]\n")); err == nil {
		t.Fatal("tools 带值应报错")
	}
	if _, err := ParseProjectConfig([]byte("no colon here\n")); err == nil {
		t.Fatal("无冒号行应报错")
	}
}

func TestFindProjectConfig_WalksUp(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ConfigFileName), []byte("tools:\n  go: 1.23.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, cfg, err := FindProjectConfig(sub)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, ConfigFileName) || cfg == nil || cfg.Tools["go"] != "1.23.2" {
		t.Fatalf("应找到根目录配置: path=%q cfg=%+v", path, cfg)
	}
}

func TestFindProjectConfig_NotFound(t *testing.T) {
	_, cfg, err := FindProjectConfig(t.TempDir())
	if err != nil || cfg != nil {
		t.Fatalf("未找到时应返回 nil config: cfg=%+v err=%v", cfg, err)
	}
}

func TestResolveProject(t *testing.T) {
	// 预置 go-1.23.2 已装并激活，node-24.9.0 已装但未激活，python 未装。
	// 三个 ID 都取自清单：ResolveProject 只认清单里的工具（见其注释与 S6）。
	home := t.TempDir()
	dir := InstallDir(home)
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, ConfigFileName),
		[]byte("tools:\n  go: 1.23.2\n  node: 24.9.0\n  python: 3.11\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkVer := func(id, ver string, active bool) {
		root := filepath.Join(dir, id+"-"+ver)
		if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(root, "bin", id), []byte("x"), 0o755)
		if active {
			if err := SetActiveVersion(dir, id, ver); err != nil {
				t.Fatal(err)
			}
		}
	}
	mkVer("go", "1.23.2", true)
	mkVer("node", "24.9.0", false)

	cfg, pins, err := ResolveProject(proj, home)
	if err != nil || cfg == nil {
		t.Fatalf("ResolveProject: cfg=%v err=%v", cfg, err)
	}
	byID := map[string]ProjectPin{}
	for _, p := range pins {
		byID[p.ID] = p
	}
	if p := byID["go"]; !p.Installed || !p.Active {
		t.Fatalf("go 应已装且激活: %+v", p)
	}
	if p := byID["node"]; !p.Installed || p.Active {
		t.Fatalf("node 应已装但未激活: %+v", p)
	}
	if p := byID["python"]; p.Installed || p.Active {
		t.Fatalf("python 应未装: %+v", p)
	}

	// ApplyProject(autoSwitch=true) 应把 node 切到 24.9.0。
	switched, err := ApplyProject(proj, home, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(switched) != 1 || switched[0] != "node" {
		t.Fatalf("应只切换 node, got %v", switched)
	}
	if ActiveVersion(dir, "node") != "24.9.0" {
		t.Fatal("node 激活版本应为 24.9.0")
	}
}

// 越界或不在清单里的 ID 不得产生 pin：它们会一路走到 SetActiveVersion 的 os.Remove
// 与 Uninstall 的 os.RemoveAll，带 `../` 的 ID 会删掉安装目录之外的文件（审计 S6）。
func TestResolveProject_SkipsUnknownAndTraversalIDs(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, ConfigFileName),
		[]byte("tools:\n  go: 1.23.2\n  ../../evil: 1.0.0\n  no-such-tool: 2.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, pins, err := ResolveProject(proj, home)
	if err != nil || cfg == nil {
		t.Fatalf("ResolveProject: cfg=%v err=%v", cfg, err)
	}
	// 配置本身原样保留（用户看得到自己写了什么），只有 pin 被过滤。
	if len(cfg.Tools) != 3 {
		t.Fatalf("配置应原样保留 3 项, got %v", cfg.Tools)
	}
	if len(pins) != 1 || pins[0].ID != "go" {
		t.Fatalf("只应留下清单里的 go, got %+v", pins)
	}
}

// validToolID 是删除动作前的最后一道守卫：即使调用方绕过 ResolveProject，
// 越界 ID 也必须被拒。
func TestValidToolID(t *testing.T) {
	cases := map[string]bool{
		"go":            true,
		"jdk21":         true,
		"golangci-lint": true,
		"":              false,
		".":             false,
		"..":            false,
		"../evil":       false,
		"a/b":           false,
		`a\b`:           false,
	}
	for id, want := range cases {
		if got := validToolID(id); got != want {
			t.Errorf("validToolID(%q) = %v, want %v", id, got, want)
		}
	}
}

// SetActiveVersion 与 Uninstall 都带 validToolID 守卫：越界 ID 必须报错，
// 且安装目录之外的路径一个字节都不能动。
func TestDestructiveOpsRejectTraversalIDs(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 安装目录之外、越界 ID 会命中的文件：守卫失效时它会被删掉。
	outside := filepath.Join(base, "victim-1.0.0")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetActiveVersion(dir, "../victim", "1.0.0"); err == nil {
		t.Error("SetActiveVersion 应拒绝越界 ID")
	}
	if err := Uninstall(dir, "../victim", "1.0.0"); err == nil {
		t.Error("Uninstall 应拒绝越界 ID")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("安装目录之外的文件被动了: %v", err)
	}
}

func TestApplyProject_AutoSwitchOff(t *testing.T) {
	home := t.TempDir()
	dir := InstallDir(home)
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, ConfigFileName), []byte("tools:\n  go: 1.24.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 已装 1.23.2 并激活，配置期望 1.24.0（未装）。
	root := filepath.Join(dir, "go-1.23.2")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "bin", "go"), []byte("x"), 0o755)
	if err := SetActiveVersion(dir, "go", "1.23.2"); err != nil {
		t.Fatal(err)
	}
	switched, err := ApplyProject(proj, home, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(switched) != 0 {
		t.Fatalf("auto_switch=false 不应切换, got %v", switched)
	}
}

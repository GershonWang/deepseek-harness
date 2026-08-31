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
	// 预置 go-1.23.2 已装并激活，gcc-12.3.0 已装但未激活，python 未装。
	home := t.TempDir()
	dir := InstallDir(home)
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, ConfigFileName),
		[]byte("tools:\n  go: 1.23.2\n  gcc: 12.3.0\n  python: 3.11\n"), 0o644); err != nil {
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
	mkVer("gcc", "12.3.0", false)

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
	if p := byID["gcc"]; !p.Installed || p.Active {
		t.Fatalf("gcc 应已装但未激活: %+v", p)
	}
	if p := byID["python"]; p.Installed || p.Active {
		t.Fatalf("python 应未装: %+v", p)
	}

	// ApplyProject(autoSwitch=true) 应把 gcc 切到 12.3.0。
	switched, err := ApplyProject(proj, home, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(switched) != 1 || switched[0] != "gcc" {
		t.Fatalf("应只切换 gcc, got %v", switched)
	}
	if ActiveVersion(dir, "gcc") != "12.3.0" {
		t.Fatal("gcc 激活版本应为 12.3.0")
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

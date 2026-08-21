package appenv

import (
	"os"
	"testing"
)

func TestResolve_OverrideBin(t *testing.T) {
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	t.Setenv("DSH_DESKTOP_PORT", "8080")
	r := Resolve()
	if r.Config.Command != "/custom/dsh" {
		t.Errorf("expected /custom/dsh, got %s", r.Config.Command)
	}
	if r.Config.Args[0] != "web" || r.Config.Args[1] != "--port" || r.Config.Args[2] != "8080" {
		t.Errorf("unexpected args: %v", r.Config.Args)
	}
}

func TestResolve_DefaultPort(t *testing.T) {
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	os.Unsetenv("DSH_DESKTOP_PORT")
	r := Resolve()
	// 默认预留一个空闲 loopback 端口，保证 harness 重启时复用同一端口。
	if r.Port == "" || r.Port == "0" {
		t.Errorf("expected a reserved port, got %s", r.Port)
	}
}

func TestResolve_ExplicitPort(t *testing.T) {
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	t.Setenv("DSH_DESKTOP_PORT", "18080")
	r := Resolve()
	if r.Port != "18080" {
		t.Errorf("expected explicit port 18080, got %s", r.Port)
	}
}

func TestResolve_LogDir(t *testing.T) {
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	t.Setenv("DSH_DESKTOP_LOG_DIR", "/tmp/test-logs")
	r := Resolve()
	if r.Config.LogDir != "/tmp/test-logs" {
		t.Errorf("expected /tmp/test-logs, got %s", r.Config.LogDir)
	}
}

func TestResolveNode_Override(t *testing.T) {
	t.Setenv("DSH_DESKTOP_NODE", "/usr/local/bin/node22")
	if n := resolveNode(); n != "/usr/local/bin/node22" {
		t.Errorf("expected override, got %s", n)
	}
}

func TestResolveNode_Default(t *testing.T) {
	os.Unsetenv("DSH_DESKTOP_NODE")
	n := resolveNode()
	if n != "node" && n != "node.exe" {
		t.Errorf("expected node or node.exe, got %s", n)
	}
}

func TestDshToolsEnv(t *testing.T) {
	bin, ld := dshToolsEnv("/home/u")
	if bin != "/home/u/.dsh-tools/bin" || ld != "/home/u/.dsh-tools/lib" {
		t.Fatalf("dshToolsEnv: bin=%q ld=%q", bin, ld)
	}
}

func TestConfigureChildEnv_PrependsToolsWhenPresent(t *testing.T) {
	home := t.TempDir()
	bin := home + "/.dsh-tools/bin"
	lib := home + "/.dsh-tools/lib"
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	oldLd := os.Getenv("LD_LIBRARY_PATH")
	os.Unsetenv("LD_LIBRARY_PATH")
	defer func() {
		_ = os.Setenv("PATH", oldPath)
		_ = os.Setenv("LD_LIBRARY_PATH", oldLd)
	}()
	ConfigureChildEnv(home)
	if got := os.Getenv("PATH"); got != bin+string(os.PathListSeparator)+oldPath {
		t.Fatalf("PATH not prepended: %q", got)
	}
	if got := os.Getenv("LD_LIBRARY_PATH"); got != lib {
		t.Fatalf("LD_LIBRARY_PATH not set: %q", got)
	}
}

func TestConfigureChildEnv_SkipsWhenAbsent(t *testing.T) {
	home := t.TempDir() // 无 .dsh-tools
	oldPath := os.Getenv("PATH")
	defer func() { _ = os.Setenv("PATH", oldPath) }()
	ConfigureChildEnv(home)
	if got := os.Getenv("PATH"); got != oldPath {
		t.Fatalf("PATH changed when tools dir absent: %q", got)
	}
}

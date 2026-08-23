package appenv

import (
	"os"
	"path/filepath"
	"strings"
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

func TestHostToolBins_OrderingAndFallback(t *testing.T) {
	base := t.TempDir()
	// jdk: <base>/jdk/bin; rg: <base>/rg 直接含可执行; empty: 无 bin
	for _, d := range []string{"jdk/bin", "rg", "empty"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "rg", "rg"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := hostToolBins(base)
	want := []string{filepath.Join(base, "jdk", "bin"), filepath.Join(base, "rg")}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("hostToolBins = %v, want %v", got, want)
	}
}

func TestConfigureChildEnv_HostBinsPrependFirst(t *testing.T) {
	home := t.TempDir()
	toolsBin := home + "/.dsh-tools/bin"
	if err := os.MkdirAll(toolsBin, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	defer func() { _ = os.Setenv("PATH", oldPath) }()

	base := t.TempDir()
	hostBin := filepath.Join(base, "jdk", "bin")
	if err := os.MkdirAll(hostBin, 0o755); err != nil {
		t.Fatal(err)
	}
	prev := hostToolsBase
	hostToolsBase = base
	defer func() { hostToolsBase = prev }()

	ConfigureChildEnv(home)
	got := os.Getenv("PATH")
	wantPrefix := hostBin + string(os.PathListSeparator) + toolsBin + string(os.PathListSeparator)
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("PATH 顺序应为 宿主>按需>现有, got: %q", got)
	}
}

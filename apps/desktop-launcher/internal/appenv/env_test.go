package appenv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolve_OverrideBin(t *testing.T) {
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	t.Setenv("DSH_DESKTOP_PORT", "8080")
	// overlay 落到临时目录：Resolve 会写这个文件，测试不能碰真实的 ~/.cache。
	t.Setenv("DSH_DESKTOP_LOG_DIR", t.TempDir())
	r := Resolve()
	if r.Config.Command != "/custom/dsh" {
		t.Errorf("expected /custom/dsh, got %s", r.Config.Command)
	}
	// launcher 自己的 flag 必须先于 app 的 --port（见 harnessArgs 的顺序约束），
	// 所以首项是 web、末两项才是 --port <n>。
	if r.Config.Args[0] != "web" {
		t.Errorf("expected web first, got %v", r.Config.Args)
	}
	if n := len(r.Config.Args); r.Config.Args[n-2] != "--port" || r.Config.Args[n-1] != "8080" {
		t.Errorf("expected trailing --port 8080, got %v", r.Config.Args)
	}
}

// 监护声明必须落到 argv 上：harness 由 launcher 的 Supervisor 管生命周期，
// dsh-market 的一键重启会用同一个 --port 与它竞态。
func TestResolve_InjectsSupervisorOverlay(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	t.Setenv("DSH_DESKTOP_PORT", "8080")
	t.Setenv("DSH_DESKTOP_LOG_DIR", dir)

	r := Resolve()
	overlay := filepath.Join(dir, supervisorOverlayName)
	want := []string{"web", "--patch", overlay, "--port", "8080"}
	if !slices.Equal(r.Config.Args, want) {
		t.Fatalf("Args = %v, want %v", r.Config.Args, want)
	}

	body, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatalf("overlay not written: %v", err)
	}
	// 断言行地址与字段名：二者是 dsh-market 的第三方契约，改名即静默失效。
	if !strings.Contains(string(body), "- id: dsh-market") {
		t.Errorf("overlay missing target row: %s", body)
	}
	if !strings.Contains(string(body), "allowRestart: false") {
		t.Errorf("overlay missing allowRestart: false: %s", body)
	}
}

// 入口脚本前缀决定 harness 能否被拉起，overlay 的位置决定它是否被解析：
// 两种错位都会让这条路径失去保护或直接启动失败。
func TestHarnessArgs_EntryPrefixOrder(t *testing.T) {
	t.Setenv("DSH_DESKTOP_LOG_DIR", t.TempDir())
	overlay := filepath.Join(os.Getenv("DSH_DESKTOP_LOG_DIR"), supervisorOverlayName)

	cases := []struct {
		name  string
		entry string
		want  []string
	}{
		{"no entry", "", []string{"web", "--patch", overlay, "--port", "1"}},
		{"entry prefix", "/opt/h/bin.js", []string{"/opt/h/bin.js", "web", "--patch", overlay, "--port", "1"}},
	}
	for _, c := range cases {
		if got := harnessArgs(c.entry, "1"); !slices.Equal(got, c.want) {
			t.Errorf("%s: harnessArgs = %v, want %v", c.name, got, c.want)
		}
	}
}

// overlay 写不进去时省略 --patch，而不是拿一个不存在的路径去启动，或让整个
// harness 起不来——监护本身仍然有效，只是退回"两个重启者"的旧行为。
func TestHarnessArgs_OmitsOverlayWhenUnwritable(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSH_DESKTOP_LOG_DIR", notADir)

	got := harnessArgs("", "1234")
	want := []string{"web", "--port", "1234"}
	if !slices.Equal(got, want) {
		t.Fatalf("harnessArgs = %v, want %v", got, want)
	}
}

func TestResolve_DefaultPort(t *testing.T) {
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	t.Setenv("DSH_DESKTOP_LOG_DIR", t.TempDir())
	os.Unsetenv("DSH_DESKTOP_PORT")
	r := Resolve()
	// 默认预留一个空闲 loopback 端口，保证 harness 重启时复用同一端口。
	if r.Port == "" || r.Port == "0" {
		t.Errorf("expected a reserved port, got %s", r.Port)
	}
}

func TestResolve_ExplicitPort(t *testing.T) {
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	t.Setenv("DSH_DESKTOP_LOG_DIR", t.TempDir())
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

func TestPackagedGitExecPath_Present(t *testing.T) {
	root := t.TempDir()
	files := filepath.Join(root, "files")
	gitCore := filepath.Join(files, "lib", "git-core")
	if err := os.MkdirAll(gitCore, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(files, "bin", "dsh-desktop-launcher")
	got, ok := packagedGitExecPath(exe)
	if !ok || got != gitCore {
		t.Fatalf("packagedGitExecPath(%q) = %q,%v want %q,true", exe, got, ok, gitCore)
	}
}

func TestPackagedGitExecPath_Absent(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "files", "bin", "dsh-desktop-launcher")
	if got, ok := packagedGitExecPath(exe); ok {
		t.Fatalf("packagedGitExecPath(%q) = %q,true want absent", exe, got)
	}
}

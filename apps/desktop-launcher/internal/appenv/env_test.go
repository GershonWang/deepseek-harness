package appenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

// 启动进度上报插件必须与 overlay 一起落地：插件文件写不出来时只省略插入行，
// 监护声明照旧生效——这是"体验增强不阻断启动"的落点。
func TestWriteSupervisorOverlay_InjectsStartupProgress(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_DESKTOP_LOG_DIR", dir)

	path, err := writeSupervisorOverlay()
	if err != nil {
		t.Fatalf("writeSupervisorOverlay: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("overlay not written: %v", err)
	}
	plugin := filepath.Join(dir, startupProgressFileName)
	if _, err := os.Stat(plugin); err != nil {
		t.Fatalf("startup progress plugin not written: %v", err)
	}
	// 绝对路径是 harness 能加载它的前提；带引号是因为运行时目录可能含空格。
	if !strings.Contains(string(body), "- id: "+startupProgressRowID) {
		t.Errorf("overlay missing progress row: %s", body)
	}
	if !strings.Contains(string(body), strconv.Quote(plugin)) {
		t.Errorf("overlay missing quoted plugin path %q: %s", plugin, body)
	}
	// 监护声明与进度行共存：前者失效会让 dsh-market 与 supervisor 抢端口。
	if !strings.Contains(string(body), "allowRestart: false") {
		t.Errorf("overlay missing market patch: %s", body)
	}
}

// 插件源码必须包含与壳解析器约定的前缀；前缀一旦漂移，加载页会静默退回粗粒度阶段。
func TestStartupProgressPlugin_ContractPrefix(t *testing.T) {
	if !strings.Contains(startupProgressPluginSource, "dsh-desktop: startup ") {
		t.Error("plugin source lost the stderr prefix contract with supervisor")
	}
	// 该文件由壳写进 ~/.cache 后直接被 harness import：任何裸包名 import 都会让
	// 这条 entry 加载失败并中止启动。
	if regexp.MustCompile(`(?m)^\s*import\s`).MatchString(startupProgressPluginSource) {
		t.Error("plugin must not import modules: it is loaded from the launcher runtime dir")
	}
}

func TestStartupProgressOverlayRow_EmptyPath(t *testing.T) {
	if got := startupProgressOverlayRow(""); got != "" {
		t.Errorf("empty plugin path must omit the row, got %q", got)
	}
}

// TestConfigureChildEnv_Idempotent 回归：安装/切换/卸载工具会反复调用
// ConfigureChildEnv，固定段必须只出现一次——无条件前置会让 PATH 与
// LD_LIBRARY_PATH 随操作次数线性膨胀，永不收敛。
func TestConfigureChildEnv_Idempotent(t *testing.T) {
	home := t.TempDir()
	bin := home + "/.dsh-tools/bin"
	lib := home + "/.dsh-tools/lib"
	for _, d := range []string{bin, lib} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldPath := os.Getenv("PATH")
	oldLd := os.Getenv("LD_LIBRARY_PATH")
	os.Unsetenv("LD_LIBRARY_PATH")
	defer func() {
		_ = os.Setenv("PATH", oldPath)
		_ = os.Setenv("LD_LIBRARY_PATH", oldLd)
	}()

	ConfigureChildEnv(home)
	oncePath, onceLd := os.Getenv("PATH"), os.Getenv("LD_LIBRARY_PATH")
	for i := 0; i < 3; i++ {
		ConfigureChildEnv(home)
	}
	if got := os.Getenv("PATH"); got != oncePath {
		t.Fatalf("重复调用后 PATH 变化:\n 一次 %q\n 四次 %q", oncePath, got)
	}
	if got := os.Getenv("LD_LIBRARY_PATH"); got != onceLd {
		t.Fatalf("重复调用后 LD_LIBRARY_PATH 变化:\n 一次 %q\n 四次 %q", onceLd, got)
	}
	if n := strings.Count(os.Getenv("PATH"), bin); n != 1 {
		t.Fatalf(".dsh-tools/bin 在 PATH 中出现 %d 次，期望 1 次: %q", n, os.Getenv("PATH"))
	}
	if n := strings.Count(os.Getenv("LD_LIBRARY_PATH"), lib); n != 1 {
		t.Fatalf(".dsh-tools/lib 在 LD_LIBRARY_PATH 中出现 %d 次，期望 1 次: %q", n, os.Getenv("LD_LIBRARY_PATH"))
	}
}

// TestConfigureChildEnv_KeepsForeignEntries 去重只针对本次前置的段：
// 用户或宿主自己加进 PATH 的目录必须原样保留，空段按 POSIX 语义丢弃。
func TestConfigureChildEnv_KeepsForeignEntries(t *testing.T) {
	home := t.TempDir()
	toolsBin := home + "/.dsh-tools/bin"
	if err := os.MkdirAll(toolsBin, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	defer func() { _ = os.Setenv("PATH", oldPath) }()

	sep := string(os.PathListSeparator)
	_ = os.Setenv("PATH", "/opt/user-tools"+sep+""+sep+"/usr/bin")
	ConfigureChildEnv(home)
	got := os.Getenv("PATH")
	if !strings.HasPrefix(got, toolsBin+sep) {
		t.Fatalf("工具链目录应前置: %q", got)
	}
	if !strings.HasSuffix(got, "/opt/user-tools"+sep+"/usr/bin") {
		t.Fatalf("用户自有的 PATH 段应原样保留: %q", got)
	}
	if strings.Contains(got, sep+sep) {
		t.Fatalf("空段应被丢弃（POSIX 下空段表示当前目录）: %q", got)
	}
}

// stubHostEscape 覆盖宿主根挂载点与启动器解析：真实挂载点只有沙箱内才有，
// 声明条件必须能在任意测试机上确定性复现。
func stubHostEscape(t *testing.T, rootfs string, launcherFound bool) {
	t.Helper()
	prevRootfs, prevLookPath := hostRootfsBase, lookPath
	hostRootfsBase = rootfs
	lookPath = func(string) (string, error) {
		if launcherFound {
			return "/usr/bin/" + hostLauncher, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { hostRootfsBase, lookPath = prevRootfs, prevLookPath })
}

// clearHostEscapeEnv 清空两个声明变量并在测试后还原，使断言不依赖运行环境取值。
func clearHostEscapeEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{hostRootfsEnv, hostLauncherEnv} {
		prev, had := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(key, prev)
				return
			}
			_ = os.Unsetenv(key)
		})
	}
}

// 声明是与 harness 侧 dsh-launch-environment 的跨语言契约：两个变量成对出现，
// 缺一不可；缺失时 harness 保持沙箱内行为，因此绝不声明一条用不了的通道。
func TestConfigureHostEscapeEnv_DeclaresChannel(t *testing.T) {
	clearHostEscapeEnv(t)
	rootfs := t.TempDir()
	stubHostEscape(t, rootfs, true)

	configureHostEscapeEnv()

	if got := os.Getenv(hostRootfsEnv); got != rootfs {
		t.Fatalf("%s = %q, want %q", hostRootfsEnv, got, rootfs)
	}
	if got := os.Getenv(hostLauncherEnv); got != hostLauncher {
		t.Fatalf("%s = %q, want %q", hostLauncherEnv, got, hostLauncher)
	}
}

func TestConfigureHostEscapeEnv_SkipsUnusableChannel(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "rootfs-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		rootfs string
		found  bool
	}{
		{name: "宿主根不存在", rootfs: filepath.Join(t.TempDir(), "absent"), found: true},
		{name: "宿主根不是目录", rootfs: notADir, found: true},
		{name: "启动器不在 PATH", rootfs: t.TempDir(), found: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearHostEscapeEnv(t)
			stubHostEscape(t, tc.rootfs, tc.found)

			configureHostEscapeEnv()

			if _, ok := os.LookupEnv(hostRootfsEnv); ok {
				t.Fatalf("不该声明 %s", hostRootfsEnv)
			}
			if _, ok := os.LookupEnv(hostLauncherEnv); ok {
				t.Fatalf("不该声明 %s", hostLauncherEnv)
			}
		})
	}
}

// 两个变量同时是使用者手工调试与临时关闭的入口：已存在的取值一律不覆盖。
func TestConfigureHostEscapeEnv_KeepsUserValues(t *testing.T) {
	t.Run("空值即关闭", func(t *testing.T) {
		clearHostEscapeEnv(t)
		stubHostEscape(t, t.TempDir(), true)
		_ = os.Setenv(hostRootfsEnv, "")
		_ = os.Setenv(hostLauncherEnv, "custom-launcher")

		configureHostEscapeEnv()

		if got := os.Getenv(hostRootfsEnv); got != "" {
			t.Fatalf("%s 被覆盖为 %q", hostRootfsEnv, got)
		}
		if got := os.Getenv(hostLauncherEnv); got != "custom-launcher" {
			t.Fatalf("%s 被覆盖为 %q", hostLauncherEnv, got)
		}
	})

	t.Run("只覆盖一项时补齐另一项", func(t *testing.T) {
		clearHostEscapeEnv(t)
		stubHostEscape(t, t.TempDir(), true)
		_ = os.Setenv(hostRootfsEnv, "/custom/rootfs")

		configureHostEscapeEnv()

		if got := os.Getenv(hostRootfsEnv); got != "/custom/rootfs" {
			t.Fatalf("%s 被覆盖为 %q", hostRootfsEnv, got)
		}
		if got := os.Getenv(hostLauncherEnv); got != hostLauncher {
			t.Fatalf("%s = %q, want %q", hostLauncherEnv, got, hostLauncher)
		}
	})
}

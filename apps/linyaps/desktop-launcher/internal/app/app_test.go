package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/connector"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/domain"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/hosttools"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/i18n"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/preflight"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/supervisor"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/toolchain"
)

// zhText 用真实 Zh 字典渲染，供这些组装函数（通知、来源、列表拼接）的断言固定语言。
//
// 断言沿用迁移前的中文期望值：既钉住组装逻辑（分隔符、分支、顺序），也钉住字典里的
// 中文没有在搬迁中走样——比断言键名更能发现"文案搬错了位置"。
func zhText(key string, args ...any) string { return i18n.T(i18n.Zh, key, args...) }

// testApp 构造一个不会真正执行 dsh 的 App：RunDoctor 命令不存在，
// exec 立即失败返回错误报告，便于在测试中同步验证自动诊断标志的转换。
func testApp() *App {
	return &App{
		conn:            connector.New(),
		preflightRunner: preflight.NewRunner("dsh-doctor-no-such-bin", "", nil, "/tmp/test-dsh-home"),
	}
}

func TestMaybeStartStartupDoctor(t *testing.T) {
	cases := []struct {
		name        string
		prev, cur   domain.HarnessState
		mode        domain.Mode
		wantTrigger bool
	}{
		{"starting to failed triggers", domain.StateStarting, domain.StateFailed, domain.ModeContainer, true},
		{"running to failed triggers", domain.StateRunning, domain.StateFailed, domain.ModeContainer, true},
		{"stopped to failed triggers", domain.StateStopped, domain.StateFailed, domain.ModeContainer, true},
		{"failed to failed does not repeat", domain.StateFailed, domain.StateFailed, domain.ModeContainer, false},
		{"failed to starting is exit edge, not trigger", domain.StateFailed, domain.StateStarting, domain.ModeContainer, false},
		{"external never triggers on failed edge", domain.StateStarting, domain.StateFailed, domain.ModeExternal, false},
		{"external failed to failed no trigger", domain.StateFailed, domain.StateFailed, domain.ModeExternal, false},
		{"no state change no trigger", domain.StateStopped, domain.StateStopped, domain.ModeContainer, false},
	}
	for _, c := range cases {
		if got := maybeStartStartupDoctor(c.prev, c.cur, c.mode); got != c.wantTrigger {
			t.Errorf("%s: maybeStartStartupDoctor(%v,%v,%v) = %v, want %v",
				c.name, c.prev, c.cur, c.mode, got, c.wantTrigger)
		}
	}
}

func TestExitFailedEdge(t *testing.T) {
	cases := []struct {
		name      string
		prev, cur domain.HarnessState
		want      bool
	}{
		{"failed to starting resets", domain.StateFailed, domain.StateStarting, true},
		{"failed to running resets", domain.StateFailed, domain.StateRunning, true},
		{"failed to failed stays", domain.StateFailed, domain.StateFailed, false},
		{"starting to failed is trigger edge, not reset", domain.StateStarting, domain.StateFailed, false},
		{"stopped to stopped nothing", domain.StateStopped, domain.StateStopped, false},
	}
	for _, c := range cases {
		if got := exitFailedEdge(c.prev, c.cur); got != c.want {
			t.Errorf("%s: exitFailedEdge(%v,%v) = %v, want %v", c.name, c.prev, c.cur, got, c.want)
		}
	}
}

// doctorFlags 带锁一次性读取四个自动诊断标志。
func doctorFlags(a *App) (running, ready, doneOnce bool, errText string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startupDoctorRunning, a.startupDoctorReady, a.startupDoctorDoneOnce, a.startupDoctorError
}

// waitStartupDoctor 轮询直到自动诊断完成（ready 且不在运行），超时失败。
func waitStartupDoctor(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		running, ready, _, _ := doctorFlags(a)
		if ready && !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待自动诊断完成超时: running=%v ready=%v", running, ready)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTrackStartupDoctor_TriggerOnFailedEdge(t *testing.T) {
	a := testApp()
	a.trackStartupDoctor(domain.StateStarting, domain.StateFailed)
	// 触发是同步置位的：调用返回后 doneOnce 必为 true。
	if _, _, doneOnce, _ := doctorFlags(a); !doneOnce {
		t.Fatalf("触发后 doneOnce 应为 true")
	}
	// 后台诊断最终完成并落盘 ready。
	waitStartupDoctor(t, a)
}

func TestTrackStartupDoctor_NoRepeatWhileFailed(t *testing.T) {
	a := testApp()
	a.trackStartupDoctor(domain.StateStarting, domain.StateFailed)
	waitStartupDoctor(t, a)
	// failed→failed 不应重新触发诊断：标志保持完成态（错误触发会先置 running）。
	a.trackStartupDoctor(domain.StateFailed, domain.StateFailed)
	running, ready, doneOnce, _ := doctorFlags(a)
	if running {
		t.Fatalf("failed→failed 不应重新启动诊断, running=%v", running)
	}
	if !ready || !doneOnce {
		t.Fatalf("failed→failed 后应保持完成态: ready=%v doneOnce=%v", ready, doneOnce)
	}
}

func TestTrackStartupDoctor_ResetOnExitFailed(t *testing.T) {
	a := testApp()
	a.trackStartupDoctor(domain.StateStarting, domain.StateFailed)
	waitStartupDoctor(t, a)
	// 用户 Restart:failed→starting 应清空本周期全部诊断标志。
	a.trackStartupDoctor(domain.StateFailed, domain.StateStarting)
	running, ready, doneOnce, errText := doctorFlags(a)
	if running || ready || doneOnce || errText != "" {
		t.Fatalf("退出失败态后应重置全部诊断标志: running=%v ready=%v doneOnce=%v err=%q",
			running, ready, doneOnce, errText)
	}
}

func TestDoctorEnv_StripsSafeModeAndPointsDshHome(t *testing.T) {
	t.Setenv("DSH_SAFE_MODE", "plugins")
	t.Setenv("DSH_HOME", "/should-be-overridden")
	a := &App{home: "/home/tester", preflightRunner: preflight.NewRunner("node", "", nil, "/home/tester/.dsh")}
	env := map[string]string{}
	for _, kv := range a.preflightRunner.Env() {
		key, value, _ := strings.Cut(kv, "=")
		env[key] = value
	}
	if _, ok := env["DSH_SAFE_MODE"]; ok {
		t.Fatalf("doctorEnv 不应携带 DSH_SAFE_MODE, got %q", env["DSH_SAFE_MODE"])
	}
	if got := env["DSH_HOME"]; got != "/home/tester/.dsh" {
		t.Fatalf("DSH_HOME 应指向 a.home/.dsh, got %q", got)
	}
}

func TestRunDoctor_ParsesOutputDespiteNonZeroExit(t *testing.T) {
	// dsh doctor --json 用退出码表达诊断结论（1 = 发现 fatal 问题）。
	// RunDoctor 必须解析 stdout 的 JSON，即使命令退出码非零——这是"发现问题"
	// 而不是"命令失败"。用 sh 模拟：打印合法 JSON 后以退出码 1 退出。
	dir := t.TempDir()
	script := filepath.Join(dir, "mock-doctor-exit1.sh")
	body := `#!/bin/sh
echo '{"dshHome":"/tmp/fake","generatedAt":"x","summary":{"total":1,"ok":0,"failed":1,"fatal":1,"fixable":1},"checks":[{"id":"plugin-dynamic-load","name":"n","category":"plugin","severity":"fatal","result":{"ok":false,"message":"bad plugin","fixable":true,"suggestedLevel":2}}]}'
exit 1
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// doctor 的运行环境统一由 preflight.Runner 持有：可执行文件是 sh，mock 脚本
	// 作为 doctor 的 cli.js 传入，exec 出来即 exec.Command(sh, script, ...)。
	a := &App{
		home:            t.TempDir(),
		preflightRunner: preflight.NewRunner("sh", script, nil, t.TempDir()),
	}
	report := a.RunDoctor()
	if report.Error != "" {
		t.Fatalf("不应报 Error（退出码非零但 JSON 可解析）, got %q", report.Error)
	}
	if report.Fatal != 1 || report.Failed != 1 || len(report.Checks) != 1 {
		t.Fatalf("应解析出 1 个 fatal 失败项, got Fatal=%d Failed=%d checks=%d",
			report.Fatal, report.Failed, len(report.Checks))
	}
	if report.Checks[0].ID != "plugin-dynamic-load" || !report.Checks[0].Fixable {
		t.Fatalf("检查项解析错误: %+v", report.Checks[0])
	}
}

// 客户端插件加载失败（iframe 里的插件树起不来，harness 进程却健康）必须被当成一次
// 启动失败：快照带上原因、iframe 目标清空、并触发一次自动诊断。
func TestReportClientBootFailure_RecordsAndTriggersDoctor(t *testing.T) {
	a := clientFailureTestApp(t)
	defer a.sup.Stop()
	defer a.stopDoctor()

	status := a.ReportClientBootFailure("  web boot: 1 entry did not activate  ")

	if status.ClientFailure != "web boot: 1 entry did not activate" {
		t.Fatalf("快照应带上（去空白后的）失败原因, got %q", status.ClientFailure)
	}
	if status.Target != "" {
		t.Errorf("客户端失败时不应再给出 iframe 目标, got %q", status.Target)
	}
	if !doctorTriggered(a) {
		t.Error("上报应触发一次自动诊断")
	}
}

// 空原因也要给出可读文案：桥只能确认"失败"而拿不到细节时，失败页不能空着。
func TestReportClientBootFailure_EmptyReasonFallsBack(t *testing.T) {
	a := clientFailureTestApp(t)
	defer a.sup.Stop()
	defer a.stopDoctor()

	if status := a.ReportClientBootFailure("   "); status.ClientFailure == "" {
		t.Error("空原因应回退为兜底文案")
	}
}

// 外置模式的页面没有注入桥：一条伪造或滞留的上报不能把壳从外部服务切到失败页。
func TestReportClientBootFailure_IgnoredInExternalMode(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 探针要求响应体含 harness 启动标记：这里回一份最小首页，证明"连上的是 harness"。
		_, _ = w.Write([]byte(`<html><script>globalThis["__DSH_BOOT__"] = {}</script></html>`))
	}))
	defer ok.Close()

	a := clientFailureTestApp(t)
	defer a.sup.Stop()
	if err := a.conn.BeginExternal(ok.URL); err != nil {
		t.Fatalf("外部探针应成功, got %v", err)
	}
	defer a.conn.EndExternal()

	if status := a.ReportClientBootFailure("web boot: 1 entry did not activate"); status.ClientFailure != "" {
		t.Fatalf("外置模式不应受理上报, got %q", status.ClientFailure)
	}
	if doctorTriggered(a) {
		t.Error("外置模式不应触发自动诊断")
	}
}

// 用户重启/停止/切安全模式都意味着上一轮界面失败已经过去：标记清空，且下一次
// 失败要能重新触发诊断（done-once 复位）。
func TestClearClientFailure_ResetsFailureAndDoctorCycle(t *testing.T) {
	a := &App{conn: connector.New()}
	a.mu.Lock()
	a.clientFailure = "web boot: 1 entry did not activate"
	a.startupDoctorDoneOnce = true
	a.mu.Unlock()

	a.clearClientFailure()

	a.mu.Lock()
	failure, once := a.clientFailure, a.startupDoctorDoneOnce
	a.mu.Unlock()
	if failure != "" || once {
		t.Fatalf("应清空失败标记并复位诊断周期, failure=%q once=%v", failure, once)
	}
}

// clientFailureTestApp 构造一个不会真正拉起 harness、也不会真正跑通 doctor 的 App
// （doctor 命令不存在），只用于验证客户端失败上报的状态流转。
func clientFailureTestApp(t *testing.T) *App {
	t.Helper()
	return &App{
		conn:            connector.New(),
		sup:             supervisor.NewSupervisor(supervisor.Config{Command: "dsh-doctor-no-such-bin", LogDir: t.TempDir()}, supervisor.DefaultOptions()),
		home:            t.TempDir(),
		preflightRunner: preflight.NewRunner("dsh-doctor-no-such-bin", "", nil, t.TempDir()),
	}
}

// doctorTriggered 读取本失败周期是否已触发过自动诊断。
func doctorTriggered(a *App) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startupDoctorDoneOnce
}

// writeBusyLoop 写一个纯忙循环的 sh 脚本（无孙进程，SIGKILL 即彻底退出），
// 模拟"诊断/修复仍在运行"的长任务。
func writeBusyLoop(t *testing.T) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "mock-busy-loop.sh")
	body := "#!/bin/sh\nwhile :; do :; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

// waitDoctorRegistered 轮询直到 doctor 子进程已登记（cancel/done 非 nil）。
func waitDoctorRegistered(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.mu.Lock()
		registered := a.doctorCancel != nil && a.doctorDone != nil
		a.mu.Unlock()
		if registered {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("doctor 未在 3 秒内登记")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// assertDoctorUntracked 断言 doctor 追踪引用已清空（cancel/done 均为 nil）。
func assertDoctorUntracked(t *testing.T, a *App) {
	t.Helper()
	a.mu.Lock()
	cancel, done := a.doctorCancel, a.doctorDone
	a.mu.Unlock()
	if cancel != nil || done != nil {
		t.Fatalf("doctor 追踪应已清空, cancel=%v done=%v", cancel != nil, done != nil)
	}
}

func TestOnShutdown_CancelsRunningDoctor(t *testing.T) {
	// 回归：窗口关闭（OnShutdown）必须同样取消正在运行的 doctor，
	// 否则 `dsh doctor` 子进程孤儿化、残留于 ll-cli ps。OnShutdown 委托
	// Shutdown（sup.Stop + stopDoctor），sup 用不存在的命令构造（spawn 立即
	// 失败，Stop 安全快速），仅验证 doctor 被取消。
	a := &App{
		conn:            connector.New(),
		sup:             supervisor.NewSupervisor(supervisor.Config{Command: "dsh-doctor-no-such-bin", LogDir: t.TempDir()}, supervisor.DefaultOptions()),
		home:            t.TempDir(),
		preflightRunner: preflight.NewRunner("sh", writeBusyLoop(t), nil, t.TempDir()),
	}
	defer a.sup.Stop()

	done := make(chan struct{})
	go func() { a.RunDoctor(); close(done) }()

	waitDoctorRegistered(t, a)
	a.OnShutdown(context.Background())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OnShutdown 后 RunDoctor 未在 5 秒内返回（doctor 未被取消）")
	}
	assertDoctorUntracked(t, a)
}

func TestRunDoctorRepair_CancellableViaStopDoctor(t *testing.T) {
	// 回归：修复进程（dsh doctor --repair）此前用无 context 的 exec.Command，
	// 关闭窗口时不可取消而残留。现在纳入 doctor 追踪，stopDoctor 必须能取消它。
	a := &App{
		home:            t.TempDir(),
		preflightRunner: preflight.NewRunner("sh", writeBusyLoop(t), nil, t.TempDir()),
	}

	done := make(chan struct{})
	var result string
	go func() { result = a.RunDoctorRepair(1); close(done) }()

	waitDoctorRegistered(t, a)
	a.stopDoctor()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stopDoctor 后 RunDoctorRepair 未在 5 秒内返回（修复未被取消）")
	}
	if result == "" {
		t.Fatalf("被取消的修复应返回错误文本, got %q", result)
	}
	assertDoctorUntracked(t, a)
}

func TestShutdown_CancelsRunningDoctor(t *testing.T) {
	// Shutdown 与 OnShutdown 共用清理路径，同样取消正在运行的 doctor。
	a := &App{
		conn:            connector.New(),
		sup:             supervisor.NewSupervisor(supervisor.Config{Command: "dsh-doctor-no-such-bin", LogDir: t.TempDir()}, supervisor.DefaultOptions()),
		home:            t.TempDir(),
		preflightRunner: preflight.NewRunner("sh", writeBusyLoop(t), nil, t.TempDir()),
	}
	defer a.sup.Stop()

	done := make(chan struct{})
	go func() { a.RunDoctor(); close(done) }()

	waitDoctorRegistered(t, a)
	a.Shutdown()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown 后 RunDoctor 未在 5 秒内返回（doctor 未被取消）")
	}
	assertDoctorUntracked(t, a)
}

func TestClassifyRuntimeSource(t *testing.T) {
	const bundled = "/opt/apps/com.deepseek.dsh-desktop/files/bin"
	home := "/home/tester"
	cases := []struct {
		name    string
		path    string
		bundled string
		want    string
	}{
		{"空路径不提示", "", bundled, ""},
		{"宿主导入挂载基址", hosttools.MountBase + "/node/bin/node", bundled, "宿主导入"},
		{"随包 bin 前缀", bundled + "/node", bundled, "随包"},
		{"市场自管目录不提示", home + "/.dsh-tools/bin/node", bundled, ""},
		{"系统运行时兜底", "/usr/bin/node", bundled, "系统"},
		{"bundled 为空时不命中随包", bundled + "/node", "", "系统"},
	}
	for _, c := range cases {
		if got := classifyRuntimeSource(zhText, c.path, home, c.bundled); got != c.want {
			t.Errorf("%s: classifyRuntimeSource(%q) = %q, want %q", c.name, c.path, got, c.want)
		}
	}
}

func TestAnnotateRuntime(t *testing.T) {
	catalog := []toolchain.ToolStatus{
		// node：Checks 已覆盖且命中随包前缀 → 提示随包来源。
		{ID: "node", Provides: []string{"node", "npm"}},
		// py：首个命令解析到市场自管目录（不提示），应继续尝试下一个命令。
		{ID: "py", Provides: []string{"python3", "python"}},
		// 假命令：Checks 探测失败，且 PATH 上不存在 → 不提示。
		{ID: "ghost", Provides: []string{"dsh-annotate-ghost-cmd"}},
		// 已安装工具：仓库状态语义优先，不混入运行时提示。
		{ID: "go", Provides: []string{"go"}, Installed: true},
	}
	checks := []domain.ToolCheck{
		{Name: "node", OK: true, Version: "24.9.0",
			Path: "/opt/apps/com.deepseek.dsh-desktop/files/bin/node"},
		{Name: "python3", OK: true, Version: "3.12.14",
			Path: "/home/tester/.dsh-tools/bin/python3"},
		{Name: "python", OK: true, Version: "3.10.12", Path: "/usr/bin/python"},
		{Name: "dsh-annotate-ghost-cmd", OK: false, Err: "not found"},
	}
	annotateRuntime(zhText, catalog, checks, "/home/tester", "/opt/apps/com.deepseek.dsh-desktop/files/bin")

	if got := catalog[0]; got.RuntimeCmd != "node" || got.RuntimeVersion != "24.9.0" || got.RuntimeSource != "随包" {
		t.Fatalf("node 应命中随包来源: %+v", got)
	}
	if got := catalog[1]; got.RuntimeCmd != "python" || got.RuntimeVersion != "3.10.12" || got.RuntimeSource != "系统" {
		t.Fatalf("py 应跳过市场自管命令继续命中 python: %+v", got)
	}
	if got := catalog[2]; got.RuntimeCmd != "" || got.RuntimeVersion != "" || got.RuntimeSource != "" {
		t.Fatalf("探测失败的命令不应提示: %+v", got)
	}
	if got := catalog[3]; got.RuntimeCmd != "" {
		t.Fatalf("已安装工具不应提示运行时来源: %+v", got)
	}
}

// mkMarketVersion 在临时 HOME 的市场目录里预置一个工具版本（含 bin/<cmd>）。
// 用例以此把「旧版本已激活、推荐版本已下载」的状态摆出来，全程不联网。
func mkMarketVersion(t *testing.T, dir, id, version, cmd string) {
	t.Helper()
	bindir := filepath.Join(dir, id+"-"+version, "bin")
	if err := os.MkdirAll(bindir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bindir, cmd), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// waitFor 轮询条件成立，超时即判定失败。异步安装完成的通知路径没有回调可等，
// 因此按可观察的磁盘/环境状态轮询。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待 %s 超时", what)
}

// TestInstallToolVersion_ActivatesChosenVersion 固定「卡片上选中的版本就是之后要用的版本」
// 这条接线（AUDIT N7）：目标版本已在磁盘、当前仍激活旧版本时，点安装必须把它设为当前版本，
// 而不是下载/假装成功之后仍停在旧版本上（InstallTool 对已装版本本会早退，只有显式要求
// 激活才走幂等 SetActiveVersion）。批量更新入口已移除，卡片安装路径是该要求在 app 层的
// 唯一出口，判据也留在这里。
func TestInstallToolVersion_ActivatesChosenVersion(t *testing.T) {
	home := t.TempDir()
	dir := toolchain.InstallDir(home)
	goTool, ok := toolchain.LookupTool("go")
	if !ok {
		t.Fatal("catalog 应含 go")
	}
	recommended := goTool.LatestVersion().Version
	const old = "1.0.0"
	mkMarketVersion(t, dir, "go", old, "go")
	mkMarketVersion(t, dir, "go", recommended, "go")
	if err := toolchain.SetActiveVersion(dir, "go", old); err != nil {
		t.Fatalf("激活 %s: %v", old, err)
	}

	a := &App{home: home}
	if got := a.InstallToolVersion("go", recommended); got != "" {
		t.Fatalf("InstallToolVersion 返回 %q, want 空串", got)
	}
	waitFor(t, "安装切换到选中版本", func() bool {
		return toolchain.ActiveVersion(dir, "go") == recommended
	})
	// 再等 goroutine 走过 ConfigureChildEnv，避免用例结束回收临时目录时与它相撞。
	waitFor(t, "市场 bin 注入 PATH", func() bool {
		return strings.Contains(os.Getenv("PATH"), filepath.Join(dir, "bin"))
	})
}

// TestFinishStartupDoctor_StaleEpochDiscarded 固定自动诊断的收尾归属只认 epoch：
// 被新一轮诊断取代的旧诊断不得写入结论，也不得把 running 置回 false——那会让
// 真正的新诊断在收尾时被误判为"周期已重置"而丢弃自己的结论。
func TestFinishStartupDoctor_StaleEpochDiscarded(t *testing.T) {
	a := testApp()
	a.mu.Lock()
	a.doctorEpoch = 7
	a.startupDoctorRunning = true
	a.startupDoctorReady = false
	a.startupDoctorError = ""
	a.mu.Unlock()

	if a.finishStartupDoctor(6, "旧诊断的错误") {
		t.Fatal("过期 epoch 不应写入状态")
	}
	running, ready, _, errText := doctorFlags(a)
	if !running || ready || errText != "" {
		t.Fatalf("过期诊断不得改动状态: running=%v ready=%v err=%q", running, ready, errText)
	}
}

// TestFinishStartupDoctor_CurrentEpochWrites 当前那次的结论正常落盘，
// 并清空 cancel/done 追踪引用。
func TestFinishStartupDoctor_CurrentEpochWrites(t *testing.T) {
	a := testApp()
	a.mu.Lock()
	a.doctorEpoch = 7
	a.startupDoctorRunning = true
	a.doctorCancel = func() {}
	a.doctorDone = make(chan struct{})
	a.mu.Unlock()

	if !a.finishStartupDoctor(7, "boom") {
		t.Fatal("当前 epoch 应写入状态")
	}
	running, ready, _, errText := doctorFlags(a)
	if running || !ready || errText != "boom" {
		t.Fatalf("状态错误: running=%v ready=%v err=%q", running, ready, errText)
	}
	a.mu.Lock()
	cancel, done := a.doctorCancel, a.doctorDone
	a.mu.Unlock()
	if cancel != nil || done != nil {
		t.Fatal("收尾后应清空 cancel/done 追踪引用")
	}
}

// TestResetStartupDoctor_InvalidatesInFlightRun 退出失败态的重置必须让在途诊断过期：
// 取消它之后它仍可能收尾，而此刻下一个失败周期的诊断可能已经在跑。
func TestResetStartupDoctor_InvalidatesInFlightRun(t *testing.T) {
	a := testApp()
	cancelled := false
	a.mu.Lock()
	a.doctorEpoch = 3
	myEpoch := a.doctorEpoch
	a.startupDoctorRunning = true
	a.doctorCancel = func() { cancelled = true }
	a.doctorDone = make(chan struct{})
	a.mu.Unlock()

	a.resetStartupDoctor()
	if !cancelled {
		t.Fatal("reset 应取消在途诊断")
	}
	if a.finishStartupDoctor(myEpoch, "被取消的诊断") {
		t.Fatal("reset 之后的旧诊断不得写入状态")
	}
	running, ready, doneOnce, errText := doctorFlags(a)
	if running || ready || doneOnce || errText != "" {
		t.Fatalf("reset 后应保持清空态: running=%v ready=%v doneOnce=%v err=%q",
			running, ready, doneOnce, errText)
	}
}

// stubWindowReads 把三个窗口读取动作换成桩，返回「是否被读过」的查询入口。
//
// 读窗口是同步等 GTK 主线程的调用（Wails 的 window.Size() 内部是 invokeOnMainThread
// 加 wg.Wait），主循环退出后不会返回——真踩上去用例会挂死而不是失败，所以这条不变式
// 只能在 seam 上断言。桩返回可用几何，避免「读到越界值提前 return」把调用事实掩盖掉。
// @param t - 测试上下文，用于收尾时还原注入点。
// @returns 是否至少读过一次窗口几何。
func stubWindowReads(t *testing.T) func() bool {
	t.Helper()
	var read atomic.Bool
	prevSize, prevPos, prevMax := windowGetSize, windowGetPosition, windowIsMaximised
	windowGetSize = func(context.Context) (int, int) { read.Store(true); return 1200, 800 }
	windowGetPosition = func(context.Context) (int, int) { read.Store(true); return 10, 20 }
	windowIsMaximised = func(context.Context) bool { read.Store(true); return false }
	t.Cleanup(func() {
		windowGetSize, windowGetPosition, windowIsMaximised = prevSize, prevPos, prevMax
	})
	return read.Load
}

// shutdownTestApp 造一个收尾路径用得到的 App：harness 命令不存在（spawn 立即失败，
// Stop 快速安全），并带上 doctor 执行器与独立的临时家目录。
// @param t - 测试上下文。
// @returns 就绪的 App 与它的家目录。
func shutdownTestApp(t *testing.T) (*App, string) {
	t.Helper()
	home := t.TempDir()
	return &App{
		conn:            connector.New(),
		sup:             supervisor.NewSupervisor(supervisor.Config{Command: "dsh-no-such-bin", LogDir: t.TempDir()}, supervisor.DefaultOptions()),
		home:            home,
		preflightRunner: preflight.NewRunner("sh", writeBusyLoop(t), nil, t.TempDir()),
	}, home
}

// TestShutdown_DoesNotReadWindowGeometry 钉住「收尾路径绝不读窗口」。
//
// 窗口关闭路径上 Wails 的次序是「GTK 主循环退出 → 销毁窗口 → OnShutdown」，那时窗口
// 已经读不得了：非主线程调用会永久等一个没人再排空的 GTK dispatchq，主线程调用则直接
// 读已释放的窗口对象。这条路径曾经第一句就是 saveWindowState()，于是 harness 的停止
// 要么被挡在身后、要么随崩溃一起消失：窗口没了、启动器进程不是卡住就是已经死掉、
// harness 变成孤儿继续占着 dsh 端口与 ~/.dsh，用户看到的是「退出不干净，ll-cli ps
// 还有进程」以及下一次启动叠出第二个 harness。
func TestShutdown_DoesNotReadWindowGeometry(t *testing.T) {
	readWindow := stubWindowReads(t)
	a, _ := shutdownTestApp(t)
	defer a.sup.Stop()
	// ctx 必须非空：saveWindowState 以 a.ctx == nil 提前返回，不设它这条用例会空转
	// ——曾经就是这样漏掉了根因（反向探针把 saveWindowState 放回去仍然通过）。
	a.ctx = context.Background()

	a.Shutdown()

	if readWindow() {
		t.Fatal("Shutdown 读了窗口几何：OnShutdown 路径上 GTK 主循环已退出，该调用不会返回（见 Shutdown 的说明）")
	}
}

// TestShutdownFromSignal_SavesWindowState 钉住信号路径仍会补存窗口几何。
//
// 与 OnShutdown 相反，这条路进来时窗口通常还活着，它是进程被外部信号杀掉时唯一还能
// 存下窗口几何的时机——OnBeforeClose 只有 CloseQuits 那条路才会先存一次。
func TestShutdownFromSignal_SavesWindowState(t *testing.T) {
	readWindow := stubWindowReads(t)
	a, home := shutdownTestApp(t)
	defer a.sup.Stop()
	a.ctx = context.Background()

	a.ShutdownFromSignal()

	if !readWindow() {
		t.Fatal("ShutdownFromSignal 未读窗口：窗口仍存活时应当补存窗口几何")
	}
	st, err := LoadWindowState(home)
	if err != nil {
		t.Fatalf("读取窗口状态失败: %v", err)
	}
	if st.Width != 1200 || st.Height != 800 || st.X != 10 || st.Y != 20 {
		t.Fatalf("窗口状态未按当前窗口写入: %+v", st)
	}
}

// TestShutdownFromSignal_SkipsWindowStateAfterOnShutdown 钉住窗口已销毁时不再读窗口。
//
// OnShutdown 已置位 windowGone；此时读取要么永远等不到主线程执行、要么是在读已释放的
// 窗口对象（见 saveWindowState 的说明）。信号路径本身只负责清理，跳过这一步不影响它要
// 保证的事（harness 必须被停掉）。
func TestShutdownFromSignal_SkipsWindowStateAfterOnShutdown(t *testing.T) {
	readWindow := stubWindowReads(t)
	a, home := shutdownTestApp(t)
	defer a.sup.Stop()
	a.ctx = context.Background()
	a.windowGone.Store(true)

	a.ShutdownFromSignal()

	if readWindow() {
		t.Fatal("窗口已销毁时 ShutdownFromSignal 仍读了窗口几何")
	}
	if st, err := LoadWindowState(home); err == nil && st.Width == 1200 {
		t.Fatal("窗口已销毁时不应把当前窗口几何写进配置")
	}
}

// TestShutdownFromSignal_BoundsWindowReadWait 钉住收尾的最后一段也有界。
//
// Wails 自己也监听 SIGTERM，两条路径并发，主循环可能已被它叫停：那时窗口读取永远等不到
// 主线程执行。这次补存是可有可无的尽力而为，不能把随后的 os.Exit 拖住——否则一个停止
// 信号会变成「启动器卡死到被切换器强杀」。
func TestShutdownFromSignal_BoundsWindowReadWait(t *testing.T) {
	prevSize, prevPos, prevMax := windowGetSize, windowGetPosition, windowIsMaximised
	release := make(chan struct{})
	// saveWindowState 先无条件读三项，越界早退发生在三次读取之后。因此必须等满三次再
	// 还原注入点：只等第一次就还原，那条仍在跑的收尾 goroutine 还会拿 context.Background()
	// 去调真正的 runtime，触发 Wails 的致命守卫，把整个测试二进制带走。
	var reads sync.WaitGroup
	reads.Add(3)
	windowGetSize = func(context.Context) (int, int) { defer reads.Done(); <-release; return 0, 0 }
	windowGetPosition = func(context.Context) (int, int) { defer reads.Done(); <-release; return 0, 0 }
	windowIsMaximised = func(context.Context) bool { defer reads.Done(); <-release; return false }
	t.Cleanup(func() {
		close(release)
		reads.Wait()
		windowGetSize, windowGetPosition, windowIsMaximised = prevSize, prevPos, prevMax
	})

	a, _ := shutdownTestApp(t)
	defer a.sup.Stop()
	a.ctx = context.Background()

	done := make(chan struct{})
	go func() { a.ShutdownFromSignal(); close(done) }()

	select {
	case <-done:
	case <-time.After(windowStateSaveTimeout + 5*time.Second):
		t.Fatal("ShutdownFromSignal 未在窗口读取超时后返回：收尾又被一个等不到主线程的调用拖住了")
	}
}

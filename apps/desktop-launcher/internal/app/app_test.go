package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/connector"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/supervisor"
)

// testApp 构造一个不会真正执行 dsh 的 App：RunDoctor 命令不存在，
// exec 立即失败返回错误报告，便于在测试中同步验证自动诊断标志的转换。
func testApp() *App {
	return &App{conn: connector.New(), dshCmd: "dsh-doctor-no-such-bin"}
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
	a := &App{home: "/home/tester"}
	env := map[string]string{}
	for _, kv := range a.doctorEnv() {
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
	// dshScript 指向 mock；dshCmd 是 sh。dshScript 需要 .js 后缀判断在 New() 里，
	// 这里直接构造 App 用 exec.Command(dshCmd, dshScript, ...) 路径。
	a := &App{dshCmd: "sh", dshScript: script, home: t.TempDir()}
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
		conn:      connector.New(),
		sup:       supervisor.NewSupervisor(supervisor.Config{Command: "dsh-doctor-no-such-bin", LogDir: t.TempDir()}, supervisor.DefaultOptions()),
		dshCmd:    "sh",
		dshScript: writeBusyLoop(t),
		home:      t.TempDir(),
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
	a := &App{dshCmd: "sh", dshScript: writeBusyLoop(t), home: t.TempDir()}

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
		conn:      connector.New(),
		sup:       supervisor.NewSupervisor(supervisor.Config{Command: "dsh-doctor-no-such-bin", LogDir: t.TempDir()}, supervisor.DefaultOptions()),
		dshCmd:    "sh",
		dshScript: writeBusyLoop(t),
		home:      t.TempDir(),
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

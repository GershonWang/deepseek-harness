// 预检编排状态机的单测：用 mock doctor 脚本回放不同诊断结论，验证
// runPreflightGate 的阶段转换与放行决策。App 均不经过 New()（无 Wails ctx，
// emitStatus 静默），supervisor 用立即失败的命令构造，Stop 收尾安全。
package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/connector"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/domain"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/preflight"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/supervisor"
)

// doctorJSONOK 是全绿诊断报告；doctorJSONFatal 是含 fatal 修复项的报告。
const (
	doctorJSONOK    = `{"dshHome":"/tmp/fake","checks":[],"summary":{"total":1,"ok":1,"failed":0,"fatal":0,"fixable":0}}`
	doctorJSONFatal = `{"dshHome":"/tmp/fake","checks":[{"id":"env-bootstrap-env","name":".env","category":"env","severity":"fatal","result":{"ok":false,"message":"bad var","fixable":true,"suggestedLevel":1}}],"summary":{"total":1,"ok":0,"failed":1,"fatal":1,"fixable":1}}`
)

// writeDoctorMock 生成模拟 dsh doctor 的 sh 脚本：对任何调用回放 body 的 JSON。
func writeDoctorMock(t *testing.T, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "mock-doctor.sh")
	content := "#!/bin/sh\nprintf '%s\\n' '" + body + "'\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

// newGateTestApp 构造带门控 supervisor 与指定 doctor mock 的 App（无 Wails ctx）。
func newGateTestApp(t *testing.T, doctorScript string) *App {
	t.Helper()
	return newGateTestAppWithCLI(t, "sh", doctorScript)
}

// newGateTestAppWithCLI 与 newGateTestApp 同源，额外暴露 doctor 的 cli 入口。
// doctorCLI 为空串即"本次安装没提供 doctor"，是 resolveDoctor 解析失败时的真实取值。
//
// supervisor 按构造期门控参数构造，与 app.New 的时序契约一致：门控必须在
// NewSupervisor 之前确定，构造后补置位会被监护循环抢跑。
func newGateTestAppWithCLI(t *testing.T, cmd, doctorCLI string) *App {
	t.Helper()
	supOptions := supervisor.DefaultOptions()
	supOptions.GateFirstStart = true
	return &App{
		conn:            connector.New(),
		sup:             supervisor.NewSupervisor(supervisor.Config{Command: "dsh-gate-no-such-bin", LogDir: t.TempDir()}, supOptions),
		preflightRunner: preflight.NewRunner(cmd, doctorCLI, nil, t.TempDir()),
	}
}

// waitPreflightPhase 轮询直到预检进入指定阶段。
func waitPreflightPhase(t *testing.T, a *App, phase string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.mu.Lock()
		got := a.preflight.Phase
		a.mu.Unlock()
		if got == phase {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待预检阶段 %s 超时,当前 %s", phase, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// freshHomeState / safeModeState 带锁读取标志位。
func (a *App) freshHomeState() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.freshHome
}

func (a *App) safeModeState() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.safeMode
}

func TestPreflightGate_HealthyReportReleases(t *testing.T) {
	a := newGateTestApp(t, writeDoctorMock(t, doctorJSONOK))
	a.runPreflightGate()

	waitPreflightPhase(t, a, PreflightOK)
	// 放行后门控被消费，supervisor 应尝试 spawn（命令不存在 → 快速 Stopped
	// 并记录 start failed）。
	deadline := time.Now().Add(3 * time.Second)
	for {
		st := a.sup.Status()
		if st.LastExit != "" {
			break // start failed 已记录，证明 spawn 发生过
		}
		if time.Now().After(deadline) {
			t.Fatalf("放行后 supervisor 未尝试 spawn: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.sup.Stop()
}

func TestPreflightGate_FatalWithoutAutoFixWaitsForUser(t *testing.T) {
	// fatal + suggestedLevel 2（需确认）→ 停在 needs-confirm，不放行。
	fatalConfirm := `{"dshHome":"/tmp/fake","checks":[{"id":"plugin-dynamic-load","name":"p","category":"plugin","severity":"fatal","result":{"ok":false,"message":"bad plugin","fixable":true,"suggestedLevel":2}}],"summary":{"total":1,"ok":0,"failed":1,"fatal":1,"fixable":1}}`
	a := newGateTestApp(t, writeDoctorMock(t, fatalConfirm))
	a.runPreflightGate()

	waitPreflightPhase(t, a, PreflightNeedsConfirm)
	// 未放行：门控期 supervisor 不 spawn。
	time.Sleep(200 * time.Millisecond)
	if st := a.sup.Status(); st.PID != 0 {
		t.Fatalf("needs-confirm 阶段不应放行 spawn, pid=%d", st.PID)
	}
	a.mu.Lock()
	issues := a.preflight.Issues
	a.mu.Unlock()
	if len(issues) != 1 || issues[0].Kind != "confirm" {
		t.Fatalf("问题清单应含 1 条 confirm 项: %+v", issues)
	}
	a.sup.Stop()
}

func TestPreflightGate_DoctorFailureDoesNotBlock(t *testing.T) {
	// doctor 无输出（脚本空跑）→ 预检 error 但仍放行（尽力而为原则）。
	a := newGateTestApp(t, "/dev/null") // sh /dev/null 无输出
	a.runPreflightGate()

	waitPreflightPhase(t, a, PreflightError)
	a.sup.Stop()
}

func TestPreflightGate_DoctorNotConfiguredDoesNotBlock(t *testing.T) {
	// 本次安装未提供 doctor（cli 为空）→ 预检同样放行，但归类必须是
	// notConfigured 而非 noOutput：前者是"根本没跑起来"，后者是"跑了但没说话"，
	// 排查方向不同，合并会把用户引向错误的方向。
	a := newGateTestAppWithCLI(t, "", "")
	a.runPreflightGate()

	waitPreflightPhase(t, a, PreflightError)
	a.mu.Lock()
	got := a.preflight.Error
	a.mu.Unlock()
	if want := a.t("preflight.doctor.notConfigured"); got != want {
		t.Fatalf("doctor 缺失时应报 %q, 实得 %q", want, got)
	}
	a.sup.Stop()
}

func TestStartFreshHome_InjectsFallbackEnvAndClearsSafeMode(t *testing.T) {
	t.Setenv("DSH_SAFE_MODE", "plugins")
	home := t.TempDir()
	a := newGateTestApp(t, writeDoctorMock(t, doctorJSONOK))
	a.home = home
	a.safeMode = "plugins"

	status := a.StartFreshHome()

	if status.FreshHome != a.freshHomeState() {
		t.Fatal("freshHome 标志应与快照一致")
	}
	if !a.freshHomeState() {
		t.Fatal("StartFreshHome 后应进入 freshHome 状态")
	}
	if a.safeModeState() != "" {
		t.Fatalf("fresh home 下安全模式应清除, got %q", a.safeModeState())
	}
	want := preflight.DefaultFallbackHome(home)
	found := false
	for _, kv := range a.sup.Env() {
		if kv == "DSH_HOME="+want {
			found = true
		}
	}
	if !found {
		t.Fatalf("子进程环境应注入 DSH_HOME=%s", want)
	}
	if os.Getenv("DSH_SAFE_MODE") != "" {
		t.Fatal("StartFreshHome 应清除进程级 DSH_SAFE_MODE")
	}
	a.sup.Stop()
}

func TestStartSafeModeLevel_ConfigLevel(t *testing.T) {
	a := newGateTestApp(t, writeDoctorMock(t, doctorJSONOK))
	status := a.StartSafeModeLevel("config")
	if status.SafeMode != "config" || a.safeModeState() != "config" {
		t.Fatalf("应进入 config 安全模式: %+v", status)
	}
	if os.Getenv("DSH_SAFE_MODE") != "config" {
		t.Fatal("应设置进程级 DSH_SAFE_MODE=config")
	}
	a.sup.Stop()
}

// waitHarnessRunning 等待子进程进入运行态并返回其 PID。
func waitHarnessRunning(t *testing.T, a *App) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := a.sup.Status()
		if st.State == domain.StateRunning && st.PID != 0 {
			return st.PID
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 harness 运行态超时: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestResumeAfterPreflight_GatedReleaseStartsOnce 门控期放行只应拉起一次，且不得
// 对自己刚拉起的子进程再发终止信号：放行入口早期是无条件 Release + Restart，而
// Restart 会对存活子进程发 SIGTERM——2026-10-18 首启"服务刚起就被杀"即此。
func TestResumeAfterPreflight_GatedReleaseStartsOnce(t *testing.T) {
	logDir := t.TempDir()
	a := newGatedLongRunApp(t, logDir)

	a.resumeAfterPreflight() // 预检放行
	pid := waitHarnessRunning(t, a)

	// 稳定期：同一子进程持续运行，且日志不得出现终止/重试行（Stop 会补记终止行，
	// 因此必须在 Stop 之前检查）。
	time.Sleep(800 * time.Millisecond)
	if st := a.sup.Status(); st.State != domain.StateRunning || st.PID != pid {
		t.Fatalf("放行后应保持同一子进程: pid %d→%d state %v", pid, st.PID, st.State)
	}
	assertNoHarnessKill(t, logDir)
	a.sup.Stop()
}

// newGatedLongRunApp 构造门控 supervisor + 长跑 mock harness 的 App（无 Wails ctx），
// 用于验证放行路径对子进程的影响。
func newGatedLongRunApp(t *testing.T, logDir string) *App {
	t.Helper()
	supOptions := supervisor.DefaultOptions()
	supOptions.GateFirstStart = true
	return &App{
		conn: connector.New(),
		sup: supervisor.NewSupervisor(
			supervisor.Config{Command: "sh", Args: []string{"../supervisor/testdata/mock-dsh-web.sh"}, LogDir: logDir},
			supOptions,
		),
	}
}

// assertNoHarnessKill 断言 harness 日志中没有终止或重试行。
func assertNoHarnessKill(t *testing.T, logDir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(logDir, "harness.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"harness killed by signal", "restarting harness"} {
		if bytes.Contains(raw, []byte(bad)) {
			t.Fatalf("不应终止或重启子进程,日志出现 %q", bad)
		}
	}
}

// TestResumeAfterPreflight_KeepsRunningChild 门控被用户操作提前消费时（例如预检
// 窗口内点了重启），harness 已经在跑；此时预检收尾只是"可以启动"的结论，不得把
// 正在跑的进程杀掉重启。
func TestResumeAfterPreflight_KeepsRunningChild(t *testing.T) {
	logDir := t.TempDir()
	a := newGatedLongRunApp(t, logDir)

	// 用户操作提前消费门控并拉起子进程。
	a.sup.Restart()
	pid := waitHarnessRunning(t, a)

	a.resumeAfterPreflight() // 预检收尾
	time.Sleep(500 * time.Millisecond)
	if st := a.sup.Status(); st.State != domain.StateRunning || st.PID != pid {
		t.Fatalf("预检收尾不应重启已在运行的子进程: pid %d→%d state %v", pid, st.PID, st.State)
	}
	assertNoHarnessKill(t, logDir)
	a.sup.Stop()
}

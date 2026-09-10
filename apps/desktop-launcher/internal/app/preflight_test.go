// 预检编排状态机的单测：用 mock doctor 脚本回放不同诊断结论，验证
// runPreflightGate 的阶段转换与放行决策。App 均不经过 New()（无 Wails ctx，
// emitStatus 静默），supervisor 用立即失败的命令构造，Stop 收尾安全。
package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/connector"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/preflight"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/supervisor"
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
	return &App{
		conn:            connector.New(),
		sup:             supervisor.NewSupervisor(supervisor.Config{Command: "dsh-gate-no-such-bin", LogDir: t.TempDir()}, supervisor.DefaultOptions()),
		dshCmd:          "sh",
		dshScript:       doctorScript,
		preflightRunner: preflight.NewRunner("sh", doctorScript, t.TempDir()),
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
	a.sup.Gate()
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
	a.sup.Gate()
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
	a.sup.Gate()
	a.runPreflightGate()

	waitPreflightPhase(t, a, PreflightError)
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

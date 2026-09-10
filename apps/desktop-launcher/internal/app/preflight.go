// 预检编排：launcher 启动后在 spawn harness 之前运行 doctor 快速预检，
// 检出 fatal 问题先执行低风险自动修复并复查；仍有问题则门控等待用户决策
// （深度修复 / 忽略并启动 / 安全模式 / 全新环境）。本文件持有预检状态与
// 阶段状态机，doctor 子进程执行细节在 internal/preflight 包。
package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/preflight"
)

// 预检阶段（PreflightSummary.Phase 的取值）。
const (
	PreflightRunning      = "running"       // 快速预检进行中
	PreflightOK           = "ok"            // 预检通过，正常放行
	PreflightAutoFixed    = "autofixed"     // 低风险自动修复后复查通过
	PreflightNeedsConfirm = "needs-confirm" // 仍有 fatal 问题，等待用户决策（不放行）
	PreflightExhausted    = "exhausted"     // 深度修复后仍失败（推荐降级启动）
	PreflightSkipped      = "skipped"       // 用户选择忽略问题强行启动
	PreflightError        = "error"         // doctor 本身失败；预检尽力而为，不阻塞启动
)

// 预检各阶段的超时：快速预检实测亚秒级，给足余量即可；深度修复内含
// 全量诊断（其中真实 boot 探测最长 60s），放宽到 3 分钟。
const (
	preflightQuickTimeout   = 15 * time.Second
	preflightRepairTimeout  = 60 * time.Second
	preflightRecheckTimeout = 3 * time.Minute
)

// PreflightIssue 是预检问题清单的一条（前端渲染用）。
type PreflightIssue struct {
	ID       string
	Name     string
	Severity string
	Message  string
	Detail   string
	Fixable  bool
	Level    int    // doctor 建议修复级别（fixable 时有意义）
	Kind     string // "auto"（低风险自动修复）| "confirm"（需确认）| "none"（无方案）| "warn"（非致命）
}

// PreflightSummary 是推送给前端的预检状态摘要。
type PreflightSummary struct {
	Phase      string
	Busy       bool // 修复/复查子流程进行中
	Error      string
	Issues     []PreflightIssue
	Repairs    []string // 已应用的修复动作摘要（"checkId: message"）
	BackupDirs []string // 修复产生的备份目录
}

// equal 支持状态快照的变化检测；slice 逐条比较。
func (s PreflightSummary) equal(o PreflightSummary) bool {
	return s.Phase == o.Phase && s.Busy == o.Busy && s.Error == o.Error &&
		slices.Equal(s.Issues, o.Issues) &&
		slices.Equal(s.Repairs, o.Repairs) &&
		slices.Equal(s.BackupDirs, o.BackupDirs)
}

// issuesFrom 把一次诊断的失败项展平为前端问题清单（Classify 分桶决定 Kind）。
func issuesFrom(report *preflight.Report) []PreflightIssue {
	action := preflight.Classify(report)
	issues := make([]PreflightIssue, 0, len(report.Checks))
	appendAll := func(checks []preflight.Check, kind string) {
		for _, c := range checks {
			issues = append(issues, PreflightIssue{
				ID: c.ID, Name: c.Name, Severity: c.Severity, Message: c.Message,
				Detail: c.Detail, Fixable: c.Fixable, Level: c.SuggestedLevel, Kind: kind,
			})
		}
	}
	appendAll(action.AutoFixable, "auto")
	appendAll(action.NeedsConfirm, "confirm")
	appendAll(action.NoFix, "none")
	appendAll(action.Warnings, "warn")
	return issues
}

// setPreflight 在锁内更新预检摘要并推送状态。
func (a *App) setPreflight(mutate func(s *PreflightSummary)) {
	a.mu.Lock()
	mutate(&a.preflight)
	a.mu.Unlock()
	a.emitStatus()
}

// resumeAfterPreflight 统一的预检后放行入口：门控期由 Release 唤醒首次
// spawn；其余场景（用户在 failed/stopped 态操作）由 Restart 恢复。两个调用
// 对不适用的场景都是 no-op，组合覆盖所有放行路径。
func (a *App) resumeAfterPreflight() {
	a.sup.Release()
	a.sup.Restart()
	a.emitStatus()
}

// runPreflightGate 是预检状态机的入口：快速预检 → 低风险自动修复 → 复查，
// 每一步后决定放行或等待用户。整段登记进 doctor 追踪（shutdown 可取消）。
func (a *App) runPreflightGate() {
	ctx, cancel, myEpoch, done := a.beginDoctorRun()
	defer a.endDoctorRun(myEpoch, done, cancel)

	a.setPreflight(func(s *PreflightSummary) { s.Phase = PreflightRunning })

	quickCtx, cancelQuick := context.WithTimeout(ctx, preflightQuickTimeout)
	defer cancelQuick()
	report, err := a.preflightRunner.Diagnose(quickCtx, true)
	if err != nil {
		// 预检是尽力而为的前置检查，doctor 本身失败绝不阻塞启动；
		// 启动失败的兜底仍是现有的事后自动诊断。
		a.setPreflight(func(s *PreflightSummary) { s.Phase = PreflightError; s.Error = err.Error() })
		a.resumeAfterPreflight()
		return
	}

	action := preflight.Classify(report)
	if !action.HasFatal() {
		a.setPreflight(func(s *PreflightSummary) { s.Phase = PreflightOK })
		a.resumeAfterPreflight()
		return
	}

	// 存在 fatal：低风险项（如 .env 违规变量）自动修复——doctor 修复前均
	// 会先备份原文件，风险可控；高风险项（移除第三方插件、重置设置）等用户确认。
	if len(action.AutoFixable) > 0 {
		a.setPreflight(func(s *PreflightSummary) { s.Busy = true })
		repairCtx, cancelRepair := context.WithTimeout(ctx, preflightRepairTimeout)
		repair, repairErr := a.preflightRunner.Repair(repairCtx, 1)
		cancelRepair()
		a.setPreflight(func(s *PreflightSummary) {
			if repairErr == nil {
				s.Repairs = append(s.Repairs, repair.Applied...)
				s.BackupDirs = append(s.BackupDirs, repair.BackupDirs...)
			}
		})

		recheckCtx, cancelRecheck := context.WithTimeout(ctx, preflightQuickTimeout)
		recheck, recheckErr := a.preflightRunner.Diagnose(recheckCtx, true)
		cancelRecheck()
		if recheckErr == nil {
			report = recheck
			action = preflight.Classify(recheck)
		}
		a.setPreflight(func(s *PreflightSummary) { s.Busy = false })
		if !action.HasFatal() {
			a.setPreflight(func(s *PreflightSummary) { s.Phase = PreflightAutoFixed })
			a.resumeAfterPreflight()
			return
		}
	}

	// 仍有 fatal：进入等待用户决策态，不放行 harness。
	issues := issuesFrom(report)
	a.setPreflight(func(s *PreflightSummary) {
		s.Phase = PreflightNeedsConfirm
		s.Issues = issues
	})
}

// GetPreflight 返回当前预检状态（前端首次加载拉取，后续随状态事件推送）。
func (a *App) GetPreflight() PreflightSummary {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.preflight
}

// ConfirmDeepRepair 执行用户确认过的高风险修复（doctor --repair 2：移除
// 导致加载失败的第三方插件、重置损坏的 settings.yaml 等，均先备份），随后
// 全量复查（含真实 boot 探测）。复查通过放行启动；仍失败进入 exhausted 态，
// 前端呈现安全模式 / 全新环境两个降级选项。
func (a *App) ConfirmDeepRepair() FrontendStatus {
	a.setPreflight(func(s *PreflightSummary) { s.Busy = true })
	go func() {
		ctx, cancel, myEpoch, done := a.beginDoctorRun()
		defer a.endDoctorRun(myEpoch, done, cancel)

		repairCtx, cancelRepair := context.WithTimeout(ctx, preflightRecheckTimeout)
		repair, repairErr := a.preflightRunner.Repair(repairCtx, 2)
		cancelRepair()
		a.setPreflight(func(s *PreflightSummary) {
			if repairErr == nil {
				s.Repairs = append(s.Repairs, repair.Applied...)
				s.BackupDirs = append(s.BackupDirs, repair.BackupDirs...)
			} else {
				s.Error = repairErr.Error()
			}
		})

		fullCtx, cancelFull := context.WithTimeout(ctx, preflightRecheckTimeout)
		defer cancelFull()
		recheck, recheckErr := a.preflightRunner.Diagnose(fullCtx, false)
		if recheckErr != nil {
			a.setPreflight(func(s *PreflightSummary) { s.Busy = false; s.Error = recheckErr.Error() })
			return
		}
		if !preflight.Classify(recheck).HasFatal() {
			a.setPreflight(func(s *PreflightSummary) {
				s.Busy = false
				s.Phase = PreflightAutoFixed
				s.Issues = nil
			})
			a.resumeAfterPreflight()
			return
		}
		a.setPreflight(func(s *PreflightSummary) {
			s.Busy = false
			s.Phase = PreflightExhausted
			s.Issues = issuesFrom(recheck)
		})
	}()
	return a.snapshot()
}

// SkipPreflight 忽略预检问题直接放行启动；启动失败的兜底仍是 supervisor
// 的事后自动诊断。
func (a *App) SkipPreflight() FrontendStatus {
	a.setPreflight(func(s *PreflightSummary) { s.Phase = PreflightSkipped; s.Busy = false })
	a.resumeAfterPreflight()
	return a.snapshot()
}

// StartFreshHome 以全新用户级运行时数据目录（~/.dsh-fallback）启动 harness：
// 注入 DSH_HOME 后放行，harness 在空目录自动自举模板 profile 与默认设置。
// 原始 ~/.dsh 原样保留；凭证不迁移（含 API Key 等敏感信息，复制即扩大泄露
// 面），降级环境通过继承的环境变量或重新配置取得密钥。同时清除安全模式——
// 全新环境没有第三方插件，安全模式在其中有语义而无收益。
func (a *App) StartFreshHome() FrontendStatus {
	fallback := preflight.DefaultFallbackHome(a.home)
	a.sup.SetEnv(append(os.Environ(), "DSH_HOME="+fallback))
	os.Unsetenv("DSH_SAFE_MODE")
	a.mu.Lock()
	a.safeMode = ""
	a.freshHome = true
	a.mu.Unlock()
	a.resumeAfterPreflight()
	return a.snapshot()
}

// ExitFreshHome 退出全新环境，恢复默认 ~/.dsh 启动。
func (a *App) ExitFreshHome() FrontendStatus {
	a.sup.SetEnv(nil)
	a.mu.Lock()
	a.freshHome = false
	a.mu.Unlock()
	a.sup.Restart()
	a.emitStatus()
	return a.snapshot()
}

// preflightHomePath 返回预检与 doctor 面板指向的真实 harness home。
func preflightHomePath(home string) string {
	return filepath.Join(home, ".dsh")
}

// Package preflight 提供 harness 启动前的前置条件检测与修复执行：封装
// `dsh doctor` 子进程的调用（quick/full 诊断、分级修复）、JSON 报告解析、
// 以及面向前端呈现的动作分级。本包不依赖 GUI 与 supervisor，编排状态机由
// app 层驱动；执行器通过可注入接口与纯函数分级保证可单测。
package preflight

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Check 是诊断报告中的单条检查（doctor --json 的 checks[] 条目，result 已展平）。
type Check struct {
	ID             string
	Name           string
	Category       string
	Severity       string
	OK             bool
	Message        string
	Detail         string
	Fixable        bool
	SuggestedLevel int
}

// Report 是一次诊断的报告摘要。
type Report struct {
	DshHome string
	Checks  []Check
	Fatal   int
	Fixable int
}

// RepairReport 是一次修复的结果摘要（doctor --repair --json）。
type RepairReport struct {
	Level      int
	BackupDirs []string // 备份目录路径，供 UI 呈现回滚位置
	Applied    []string // "checkId: message"，按应用顺序
	Skipped    []string // "checkId: reason"
}

// doctorJSON 是 doctor --json 的原始文档结构。
type doctorJSON struct {
	DshHome string `json:"dshHome"`
	Checks  []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Category string `json:"category"`
		Severity string `json:"severity"`
		Result   struct {
			OK             bool   `json:"ok"`
			Message        string `json:"message"`
			Detail         string `json:"detail"`
			Fixable        bool   `json:"fixable"`
			SuggestedLevel int    `json:"suggestedLevel"`
		} `json:"result"`
	} `json:"checks"`
	Summary struct {
		Fatal   int `json:"fatal"`
		Fixable int `json:"fixable"`
	} `json:"summary"`
}

// repairJSON 是 doctor --repair --json 的原始文档结构。
type repairJSON struct {
	Level   int      `json:"level"`
	Backups []string `json:"backups"`
	Applied []struct {
		CheckID string `json:"checkId"`
		Message string `json:"message"`
	} `json:"applied"`
	Skipped []struct {
		CheckID string `json:"checkId"`
		Reason  string `json:"reason"`
	} `json:"skipped"`
}

// processRunner 抽象 doctor 子进程的执行，测试注入 fake 记录入参并回放输出。
type processRunner interface {
	// run 执行命令：env 为完整子进程环境；stdout/stderr 收集输出。与
	// dsh doctor 的约定一致，非零退出码不是错误（退出码表达诊断结论）。
	run(ctx context.Context, name string, args []string, env []string, stdout, stderr io.Writer)
}

// osProcessRunner 用 os/exec 执行真实子进程。
type osProcessRunner struct{}

func (osProcessRunner) run(ctx context.Context, name string, args []string, env []string, stdout, stderr io.Writer) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	_ = cmd.Run()
}

// Runner 执行 doctor 子进程并解析报告。子进程环境固定剥离 DSH_SAFE_MODE
// （安全模式会让诊断看不到真实安装中的第三方插件问题）并显式注入 DSH_HOME，
// 保证检测对象是 harness 实际使用的运行时目录。
type Runner struct {
	dshCmd    string // node 可执行或 dsh 直接可执行
	dshScript string // dsh 脚本路径（dshCmd 为 node 时非空）
	env       []string
	runner    processRunner
}

// NewRunner 构造 doctor 执行器：env 在 launcher 当前环境基础上剥离
// DSH_SAFE_MODE、注入 DSH_HOME=dshHome。
func NewRunner(dshCmd, dshScript, dshHome string) *Runner {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "DSH_SAFE_MODE=") {
			continue
		}
		env = append(env, kv)
	}
	return &Runner{
		dshCmd:    dshCmd,
		dshScript: dshScript,
		env:       append(env, "DSH_HOME="+dshHome),
		runner:    osProcessRunner{},
	}
}

// args 组装 doctor 子进程 argv；dshScript 非空表示 dshCmd 是 node。
func (r *Runner) args(extra ...string) []string {
	args := []string{}
	if r.dshScript != "" {
		args = append(args, r.dshScript)
	}
	return append(args, append([]string{"doctor"}, extra...)...)
}

// runDoctor 执行一次 doctor 子命令并解析 JSON 输出。
func (r *Runner) runDoctor(ctx context.Context, extra []string, out any) error {
	var stdout, stderr bytes.Buffer
	r.runner.run(ctx, r.dshCmd, r.args(extra...), r.env, &stdout, &stderr)
	output := bytes.TrimSpace(stdout.Bytes())
	if len(output) == 0 {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return fmt.Errorf("doctor 无输出")
		}
		return fmt.Errorf("doctor 无输出: %s", detail)
	}
	if err := json.Unmarshal(output, out); err != nil {
		return fmt.Errorf("doctor 输出解析失败: %w", err)
	}
	return nil
}

// Diagnose 运行一次诊断。quick=true 跳过加载探测（最长可达一分钟的子进程
// 真实 boot），适合每次启动的快速预检；quick=false 输出全量报告。
func (r *Runner) Diagnose(ctx context.Context, quick bool) (*Report, error) {
	extra := []string{"--json"}
	if quick {
		extra = append(extra, "--quick")
	}
	var raw doctorJSON
	if err := r.runDoctor(ctx, extra, &raw); err != nil {
		return nil, err
	}
	report := &Report{
		DshHome: raw.DshHome,
		Fatal:   raw.Summary.Fatal,
		Fixable: raw.Summary.Fixable,
	}
	for _, e := range raw.Checks {
		report.Checks = append(report.Checks, Check{
			ID:             e.ID,
			Name:           e.Name,
			Category:       e.Category,
			Severity:       e.Severity,
			OK:             e.Result.OK,
			Message:        e.Result.Message,
			Detail:         e.Result.Detail,
			Fixable:        e.Result.Fixable,
			SuggestedLevel: e.Result.SuggestedLevel,
		})
	}
	return report, nil
}

// Repair 运行指定级别的自动修复。level 语义与 doctor 一致：
// 1=低风险可逆（注释 .env 违规行等），2=中等（移除失效补丁/第三方插件、
// 重置 settings.yaml，均先备份），3=破坏性。修复前后的复查由调用方编排。
func (r *Runner) Repair(ctx context.Context, level int) (*RepairReport, error) {
	var raw repairJSON
	if err := r.runDoctor(ctx, []string{"--json", "--repair", fmt.Sprintf("%d", level)}, &raw); err != nil {
		return nil, err
	}
	report := &RepairReport{Level: level, BackupDirs: raw.Backups}
	for _, a := range raw.Applied {
		report.Applied = append(report.Applied, fmt.Sprintf("%s: %s", a.CheckID, a.Message))
	}
	for _, s := range raw.Skipped {
		report.Skipped = append(report.Skipped, fmt.Sprintf("%s: %s", s.CheckID, s.Reason))
	}
	return report, nil
}

// Action 把诊断报告分级为前端可呈现的动作建议。纯函数：无 IO、无时钟。
type Action struct {
	// AutoFixable 是低风险可自动修复的失败项（fixable 且 suggestedLevel<=1）。
	AutoFixable []Check
	// NeedsConfirm 是需用户确认的较高风险修复项（fixable 且 suggestedLevel>=2）：
	// 修复会移除第三方插件、重置 settings.yaml 等，虽然先备份但影响可见。
	NeedsConfirm []Check
	// NoFix 是无自动修复方案的失败项。
	NoFix []Check
	// Warnings 是非致命的失败项（仅展示，不阻塞启动）。
	Warnings []Check
}

// HasFatal 返回是否存在任一 fatal 级失败项。
func (a *Action) HasFatal() bool {
	return len(a.AutoFixable) > 0 || len(a.NeedsConfirm) > 0 || len(a.NoFix) > 0
}

// Classify 按 doctor 的严重级别与修复级别把失败项分桶。fatal 级失败决定
// 是否需要干预；error/warning 级失败只进 Warnings 供展示。
func Classify(report *Report) *Action {
	action := &Action{}
	for _, c := range report.Checks {
		if c.OK {
			continue
		}
		if c.Severity != "fatal" {
			action.Warnings = append(action.Warnings, c)
			continue
		}
		switch {
		case c.Fixable && c.SuggestedLevel <= 1:
			action.AutoFixable = append(action.AutoFixable, c)
		case c.Fixable:
			action.NeedsConfirm = append(action.NeedsConfirm, c)
		default:
			action.NoFix = append(action.NoFix, c)
		}
	}
	return action
}

// DefaultFallbackHome 返回全新环境降级启动的运行时目录（home 下 .dsh-fallback）。
// 原始 ~/.dsh 永不被删除或改写；降级环境复用同一目录以保留上一次降级期间
// 的配置，用户可自行迁移数据。
func DefaultFallbackHome(home string) string {
	return filepath.Join(home, ".dsh-fallback")
}

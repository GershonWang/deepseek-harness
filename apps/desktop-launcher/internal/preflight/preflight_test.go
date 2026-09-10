// preflight 包的单测：doctor 子进程调用的 argv 组装与报告解析用 fake
// processRunner 回放固定 JSON；动作分级为纯函数断言。真实 CLI 行为由
// dsh doctor 自身的测试与手工验证覆盖。
package preflight

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// fakeRunner 记录收到的调用入参，按序回放预设 stdout。
type fakeRunner struct {
	calls   []fakeCall
	stdouts []string
}

type fakeCall struct {
	name string
	args []string
	env  []string
}

func (f *fakeRunner) run(_ context.Context, name string, args []string, env []string, stdout, _ /*stderr*/ io.Writer) {
	f.calls = append(f.calls, fakeCall{name: name, args: args, env: env})
	if len(f.stdouts) > 0 {
		_, _ = stdout.Write([]byte(f.stdouts[0]))
		f.stdouts = f.stdouts[1:]
	}
}

// diagReportJSON 构造一份最小 doctor --json 文档。
func diagReportJSON(fatal, fixable int) string {
	return `{"dshHome":"/home/u/.dsh","checks":[{"id":"env-node-version","name":"Node.js version","category":"env","severity":"fatal","result":{"ok":true,"message":"ok","fixable":false,"suggestedLevel":1}},{"id":"cfg-settings-yaml","name":"settings.yaml","category":"config","severity":"fatal","result":{"ok":false,"message":"invalid","fixable":true,"suggestedLevel":2}}],"summary":{"total":2,"ok":1,"failed":1,"fatal":` +
		itoa(fatal) + `,"fixable":` + itoa(fixable) + `}}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func newTestRunner(t *testing.T, stdouts ...string) (*Runner, *fakeRunner) {
	t.Helper()
	fake := &fakeRunner{stdouts: stdouts}
	r := NewRunner("/usr/bin/node", "/opt/harness/lib/bin.js", "/home/u/.dsh")
	r.runner = fake
	return r, fake
}

func TestRunner_DiagnoseQuickArgvAndParse(t *testing.T) {
	r, fake := newTestRunner(t, diagReportJSON(1, 1))
	report, err := r.Diagnose(context.Background(), true)
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("应执行一次 doctor,得到 %d 次", len(fake.calls))
	}
	call := fake.calls[0]
	if call.name != "/usr/bin/node" {
		t.Errorf("命令应为 node,got %q", call.name)
	}
	want := []string{"/opt/harness/lib/bin.js", "doctor", "--json", "--quick"}
	if strings.Join(call.args, " ") != strings.Join(want, " ") {
		t.Errorf("argv 不匹配,got %v", call.args)
	}
	// 环境：剥离 DSH_SAFE_MODE、注入 DSH_HOME。
	foundHome, foundSafe := false, false
	for _, kv := range call.env {
		if kv == "DSH_HOME=/home/u/.dsh" {
			foundHome = true
		}
		if strings.HasPrefix(kv, "DSH_SAFE_MODE=") {
			foundSafe = true
		}
	}
	if !foundHome {
		t.Error("子进程环境应包含 DSH_HOME")
	}
	if foundSafe {
		t.Error("子进程环境不应携带 DSH_SAFE_MODE")
	}
	// 报告解析：result 展平到 Check。
	if report.Fatal != 1 || report.Fixable != 1 {
		t.Errorf("summary 解析错误: %+v", report)
	}
	if len(report.Checks) != 2 {
		t.Fatalf("应有 2 条检查,got %d", len(report.Checks))
	}
	bad := report.Checks[1]
	if bad.ID != "cfg-settings-yaml" || bad.OK || !bad.Fixable || bad.SuggestedLevel != 2 {
		t.Errorf("检查展平错误: %+v", bad)
	}
}

func TestRunner_DiagnoseFullOmitsQuick(t *testing.T) {
	r, fake := newTestRunner(t, diagReportJSON(0, 0))
	if _, err := r.Diagnose(context.Background(), false); err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if got := fake.calls[0].args; len(got) == 0 || got[len(got)-1] == "--quick" {
		t.Errorf("全量诊断不应携带 --quick,got %v", got)
	}
}

func TestRunner_DiagnoseEmptyOutputFails(t *testing.T) {
	r, _ := newTestRunner(t) // 无预设输出 → doctor 无输出
	if _, err := r.Diagnose(context.Background(), true); err == nil {
		t.Fatal("空输出应返回错误")
	}
}

func TestRunner_DiagnoseBadJSONFails(t *testing.T) {
	r, _ := newTestRunner(t, "not json")
	if _, err := r.Diagnose(context.Background(), true); err == nil {
		t.Fatal("坏 JSON 应返回错误")
	}
}

func TestRunner_RepairParsesReport(t *testing.T) {
	r, fake := newTestRunner(t, `{"level":2,"backups":["/home/u/.dsh/backups/doctor-1"],"applied":[{"checkId":"plugin-dynamic-load","message":"已移除元凶"}],"skipped":[{"checkId":"env-node-version","reason":"no fix available"}]}`)
	report, err := r.Repair(context.Background(), 2)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if got := fake.calls[0].args; !strings.Contains(strings.Join(got, " "), "--repair 2") {
		t.Errorf("argv 应包含 --repair 2,got %v", got)
	}
	if report.Level != 2 || len(report.BackupDirs) != 1 {
		t.Errorf("修复报告解析错误: %+v", report)
	}
	if len(report.Applied) != 1 || !strings.HasPrefix(report.Applied[0], "plugin-dynamic-load: ") {
		t.Errorf("applied 解析错误: %v", report.Applied)
	}
	if len(report.Skipped) != 1 || !strings.HasPrefix(report.Skipped[0], "env-node-version: ") {
		t.Errorf("skipped 解析错误: %v", report.Skipped)
	}
}

func TestClassify_BucketsBySeverityAndFixability(t *testing.T) {
	report := &Report{
		Checks: []Check{
			{ID: "a-auto", Severity: "fatal", OK: false, Fixable: true, SuggestedLevel: 1},
			{ID: "a-confirm", Severity: "fatal", OK: false, Fixable: true, SuggestedLevel: 2},
			{ID: "a-nofix", Severity: "fatal", OK: false, Fixable: false},
			{ID: "a-warn", Severity: "error", OK: false, Fixable: true, SuggestedLevel: 2},
			{ID: "a-ok", Severity: "fatal", OK: true},
		},
	}
	action := Classify(report)
	if len(action.AutoFixable) != 1 || action.AutoFixable[0].ID != "a-auto" {
		t.Errorf("AutoFixable 分桶错误: %+v", action.AutoFixable)
	}
	if len(action.NeedsConfirm) != 1 || action.NeedsConfirm[0].ID != "a-confirm" {
		t.Errorf("NeedsConfirm 分桶错误: %+v", action.NeedsConfirm)
	}
	if len(action.NoFix) != 1 || action.NoFix[0].ID != "a-nofix" {
		t.Errorf("NoFix 分桶错误: %+v", action.NoFix)
	}
	if len(action.Warnings) != 1 || action.Warnings[0].ID != "a-warn" {
		t.Errorf("Warnings 分桶错误: %+v", action.Warnings)
	}
	if !action.HasFatal() {
		t.Error("存在 fatal 失败项时 HasFatal 应为 true")
	}
}

func TestClassify_AllPassMeansNoFatal(t *testing.T) {
	report := &Report{Checks: []Check{{ID: "ok", Severity: "fatal", OK: true}}}
	action := Classify(report)
	if action.HasFatal() {
		t.Error("全部通过时不应有 fatal")
	}
	if len(action.Warnings) != 0 {
		t.Errorf("全部通过时不应有 Warnings: %+v", action.Warnings)
	}
}

func TestDefaultFallbackHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无用户主目录")
	}
	if got := DefaultFallbackHome(home); got == home || !strings.HasSuffix(got, ".dsh-fallback") {
		t.Errorf("fallback 目录应为 home 下的独立目录,got %q", got)
	}
	if _, err := os.Stat(DefaultFallbackHome(home)); !errors.Is(err, os.ErrNotExist) && err != nil {
		t.Errorf("stat 错误: %v", err)
	}
}

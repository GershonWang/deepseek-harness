package about

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestShowReportsMissingHelper 覆盖助手缺失：能装上却点不动的包最难查，这里必须给出
// 带路径的错误，而不是静默什么都不做。
func TestShowReportsMissingHelper(t *testing.T) {
	err := New(t.TempDir(), "1.2.3").Show(Dialog{Fields: []Field{{Label: "模式", Value: "薄壳版"}}})
	if err == nil {
		t.Fatal("助手缺失时应报错")
	}
	if !strings.Contains(err.Error(), helperName) {
		t.Fatalf("错误应带上助手路径，得到 %v", err)
	}
}

// TestShowSkipsWhileDialogIsOpen 覆盖连点：菜单可以被连点，第二次点击不应叠出第二个
// 窗口。这里直接置位运行标志，并用缺失的助手证明它提前返回、根本没去启动。
func TestShowSkipsWhileDialogIsOpen(t *testing.T) {
	p := New(t.TempDir(), "1.2.3")
	p.running = true
	if err := p.Show(Dialog{Fields: []Field{{Label: "模式", Value: "薄壳版"}}}); err != nil {
		t.Fatalf("已有对话框时 Show 应为空操作，得到 %v", err)
	}
}

// TestHarnessVersion 覆盖 DSH 版本的解析：读的是薄壳版铺在同一前缀下的 harness 树，
// 解析失败必须落到 unknown 而不是空串——空串会在对话框里留一行看不出所以然的空白。
func TestHarnessVersion(t *testing.T) {
	prefix := t.TempDir()
	if got := HarnessVersion(prefix); got != unknownVersion {
		t.Fatalf("缺失 package.json 时 = %q，期望 %q", got, unknownVersion)
	}
	harnessDir := filepath.Join(prefix, "harness")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	broken := filepath.Join(harnessDir, "package.json")
	if err := os.WriteFile(broken, []byte("not json"), 0o644); err != nil {
		t.Fatalf("准备损坏文件失败: %v", err)
	}
	if got := HarnessVersion(prefix); got != unknownVersion {
		t.Fatalf("内容损坏时 = %q，期望 %q", got, unknownVersion)
	}
	manifest, err := json.Marshal(map[string]string{"name": "dsh", "version": "0.2.0-rc.2"})
	if err != nil {
		t.Fatalf("构造 package.json 失败: %v", err)
	}
	if err := os.WriteFile(broken, manifest, 0o644); err != nil {
		t.Fatalf("写入 package.json 失败: %v", err)
	}
	if got := HarnessVersion(prefix); got != "0.2.0-rc.2" {
		t.Fatalf("HarnessVersion = %q，期望 0.2.0-rc.2", got)
	}
	if got := HarnessVersion(""); got != unknownVersion {
		t.Fatalf("空前缀 = %q，期望 %q", got, unknownVersion)
	}
}

// TestShowPassesFieldsAndPaths 覆盖与助手的接口：产品名、版本号、每个字段、复制标记、
// 关闭按钮文案、图标与打开器路径都必须出现在命令行上——助手只画窗口，任何一项漏传都会在
// 界面上表现为缺内容，而字段顺序错乱会让「DSH版本／仓库／作者」这一组重新排成别的样子。
// 两类行各标记一项需要复制的值，钉住「--copy 对居中行与事实行同样有效」这条约定。
func TestShowPassesFieldsAndPaths(t *testing.T) {
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	// 目录层级与尺寸都用打包时的真实取值（而不是常量）：常量一旦与 linglong.yaml 的安装
	// 布局漂移，这里就会失败——图标找不到不会报错，只有这条断言能挡住。
	logoDir := filepath.Join(prefix, "share", "icons", "hicolor", "256x256", "apps")
	for _, dir := range []string{binDir, logoDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("准备目录失败: %v", err)
		}
	}
	logo := filepath.Join(logoDir, "dsh-desktop.png")
	if err := os.WriteFile(logo, []byte("not a real png"), 0o644); err != nil {
		t.Fatalf("准备图标失败: %v", err)
	}
	opener := filepath.Join(binDir, "xdg-open")
	if err := os.WriteFile(opener, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("准备打开器失败: %v", err)
	}
	// 假助手：把收到的参数逐行写进文件，供断言命令行内容。
	argsPath := filepath.Join(prefix, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsPath + "\n"
	if err := os.WriteFile(filepath.Join(binDir, helperName), []byte(script), 0o755); err != nil {
		t.Fatalf("准备假助手失败: %v", err)
	}

	dialog := Dialog{
		Title: "关于",
		CenterFields: []Field{
			{Label: "当前形态", Value: "薄壳版"},
			{Label: "包 ID", Value: PackageID, Copy: true},
		},
		Fields: []Field{
			{Label: "DSH版本", Value: "0.2.0-rc.2"},
			{Label: "DSH仓库", Value: UpstreamRepo},
			{Label: "玲珑封装作者", Value: Packager, Copy: true},
			{Label: "玲珑封装仓库", Value: GithubRepo},
		},
		CloseLabel:      "关闭",
		CopyLabel:       "复制",
		CopiedLabel:     "已复制",
		OpenFailedLabel: "打不开这个链接",
	}
	if err := New(prefix, "1.2.3").Show(dialog); err != nil {
		t.Fatalf("Show 失败: %v", err)
	}
	args := waitForArgs(t, argsPath)
	want := []string{"--program-name", productName, "--version", "1.2.3", "--title", dialog.Title,
		"--copy-label", dialog.CopyLabel, "--copied-label", dialog.CopiedLabel,
		"--open-failed", dialog.OpenFailedLabel}
	for _, fields := range [][]Field{dialog.CenterFields, dialog.Fields} {
		for _, field := range fields {
			if field.Copy {
				want = append(want, "--copy", field.Value)
			}
		}
	}
	for _, field := range dialog.CenterFields {
		want = append(want, "--center-field", field.Label+"="+field.Value)
	}
	for _, field := range dialog.Fields {
		want = append(want, "--field", field.Label+"="+field.Value)
	}
	want = append(want, "--close-label", "关闭", "--logo", logo, "--uri-opener", opener)
	if len(args) != len(want) {
		t.Fatalf("助手参数 = %q，期望 %q", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("助手参数 = %q，期望 %q", args, want)
		}
	}
}

// TestShowOmitsMissingOptionalPaths 覆盖图标与打开器都缺失的安装：两者都只是增强，缺了
// 不该拦住弹框，也不该把空路径传给助手（助手会拿它当文件名去试）。
func TestShowOmitsMissingOptionalPaths(t *testing.T) {
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	argsPath := filepath.Join(prefix, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsPath + "\n"
	if err := os.WriteFile(filepath.Join(binDir, helperName), []byte(script), 0o755); err != nil {
		t.Fatalf("准备假助手失败: %v", err)
	}
	if err := New(prefix, "1.2.3").Show(Dialog{}); err != nil {
		t.Fatalf("Show 失败: %v", err)
	}
	args := waitForArgs(t, argsPath)
	for _, arg := range args {
		if arg == "--logo" || arg == "--uri-opener" {
			t.Fatalf("可选路径缺失时不应传入 %s：%q", arg, args)
		}
	}
}

// waitForArgs 等假助手把参数写进文件。
// @param t - 测试上下文。
// @param path - 参数文件路径。
// @returns 逐行的参数。
func waitForArgs(t *testing.T, path string) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil && len(raw) > 0 {
			return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("假助手未在超时前写出参数文件 %s", path)
	return nil
}

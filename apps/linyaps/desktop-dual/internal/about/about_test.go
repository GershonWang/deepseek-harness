package about

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestShowReportsMissingHelper 覆盖助手缺失：能装上却点不动的包最难查，这里必须给出
// 带路径的错误，而不是静默什么都不做。
func TestShowReportsMissingHelper(t *testing.T) {
	err := New(t.TempDir(), "1.2.3").Show("comments")
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
	if err := p.Show("comments"); err != nil {
		t.Fatalf("已有对话框时 Show 应为空操作，得到 %v", err)
	}
}

// TestShowPassesVersionCommentsAndLogo 覆盖与助手的接口：产品名、版本号、附加信息与
// 图标路径都必须出现在命令行上——助手只画窗口，任何一项漏传都会在界面上表现为缺内容。
func TestShowPassesVersionCommentsAndLogo(t *testing.T) {
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
	logo := filepath.Join(logoDir, "dsh-desktop-dual.png")
	if err := os.WriteFile(logo, []byte("not a real png"), 0o644); err != nil {
		t.Fatalf("准备图标失败: %v", err)
	}
	// 假助手：把收到的参数逐行写进文件，供断言命令行内容。
	argsPath := filepath.Join(prefix, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsPath + "\n"
	if err := os.WriteFile(filepath.Join(binDir, helperName), []byte(script), 0o755); err != nil {
		t.Fatalf("准备假助手失败: %v", err)
	}

	if err := New(prefix, "1.2.3").Show("当前形态: 薄壳版"); err != nil {
		t.Fatalf("Show 失败: %v", err)
	}
	args := waitForArgs(t, argsPath)
	want := []string{"--program-name", productName, "--version", "1.2.3", "--comments", "当前形态: 薄壳版", "--logo", logo}
	if len(args) != len(want) {
		t.Fatalf("助手参数 = %q，期望 %q", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("助手参数 = %q，期望 %q", args, want)
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

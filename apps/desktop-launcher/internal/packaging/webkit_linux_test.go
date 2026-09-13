//go:build linux

package packaging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWebkitExecPathIsEqualLengthReplacement 把打包脚本的等长替换前提固定下来。
// 脚本会因替代串过长而在构建期失败，但那时已经在容器里跑了几分钟；这里让它变成
// 一条本地就能看到的断言。
func TestWebkitExecPathIsEqualLengthReplacement(t *testing.T) {
	const original = "/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1"
	short := webkitExecPath()
	if short == "" {
		t.Fatal("短路径为空：internal/packaging/webkit-exec-path.txt 缺失或只有空白")
	}
	if !strings.HasPrefix(short, "/") {
		t.Fatalf("短路径必须是绝对路径，得到 %q", short)
	}
	if strings.ContainsAny(short, " \t\r\n\x00") {
		t.Fatalf("短路径不能含空白或 NUL，得到 %q", short)
	}
	if len(short) > len(original) {
		t.Fatalf("短路径 %q 比原路径 %q 长，等长字节替换不成立", short, original)
	}
}

// TestPatchScriptReadsSharedShortPath 守卫单源：打包脚本与 launcher 必须读同一个
// 短路径文件。脚本里再写一遍字面量的话，改一处就会产出「装得上、GUI 起不来」的包，
// 而打包与启动两个环节都不会报错。
func TestPatchScriptReadsSharedShortPath(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "linglong", "patch-webkit-exec-path.sh")
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("读取打包脚本失败: %v", err)
	}
	if !bytes.Contains(script, []byte("webkit-exec-path.txt")) {
		t.Error("打包脚本未引用 webkit-exec-path.txt（短路径必须单源）")
	}
	// 只认 Python 字节字面量，不看注释：脚本注释里说明"当前短路径是什么"是正常的，
	// 真正要挡的是把替换串写回代码里。
	if bytes.Contains(script, []byte("b'"+webkitExecPath()+"'")) {
		t.Errorf("打包脚本里又写死了短路径字面量 %q：应改为读取 webkit-exec-path.txt", webkitExecPath())
	}
}

func TestWebkitHelperLinkUsable(t *testing.T) {
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "WebKitNetworkProcess"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	if webkitHelperLinkUsable(filepath.Join(t.TempDir(), "missing"), real) {
		t.Error("链接不存在时应不可用")
	}

	mkLink := func(target string) string {
		p := filepath.Join(t.TempDir(), "dsh-webkit-4.1")
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if link := mkLink(real); !webkitHelperLinkUsable(link, real) {
		t.Error("指向真实 helper 目录的链接应可用")
	}

	stale := filepath.Join(t.TempDir(), "uninstalled-app")
	if link := mkLink(stale); webkitHelperLinkUsable(link, real) {
		t.Error("悬空链接应不可用")
	}

	other := t.TempDir()
	if link := mkLink(other); webkitHelperLinkUsable(link, real) {
		t.Error("指向非当前包目录的链接应不可用")
	}
	if err := os.WriteFile(filepath.Join(other, "WebKitNetworkProcess"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if link := mkLink(other); !webkitHelperLinkUsable(link, other) {
		t.Error("指向具备 helper 的目录应可用")
	}
}

func TestConfigureWebKitRenderingFollowsNvidiaDriver(t *testing.T) {
	savedProbe := nvidiaModulePath
	savedEnv, hadEnv := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER")
	t.Cleanup(func() {
		nvidiaModulePath = savedProbe
		if hadEnv {
			_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", savedEnv)
			return
		}
		_ = os.Unsetenv("WEBKIT_DISABLE_DMABUF_RENDERER")
	})

	probe := filepath.Join(t.TempDir(), "nvidia")
	nvidiaModulePath = probe
	_ = os.Unsetenv("WEBKIT_DISABLE_DMABUF_RENDERER")

	ConfigureWebKitRendering()
	if _, ok := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER"); ok {
		t.Error("未检测到 NVIDIA 驱动时不应关闭 DMABUF 渲染器")
	}

	if err := os.Mkdir(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	ConfigureWebKitRendering()
	if got := os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER"); got != "1" {
		t.Errorf("检测到 NVIDIA 驱动时应关闭 DMABUF 渲染器，得到 %q", got)
	}
}

func TestConfigureWebKitRenderingHonorsOptOut(t *testing.T) {
	savedProbe := nvidiaModulePath
	nvidiaModulePath = t.TempDir() // 存在的目录即可让驱动探测命中
	t.Cleanup(func() { nvidiaModulePath = savedProbe })

	t.Setenv("DSH_DESKTOP_DMABUF_RENDERER", "1")

	savedEnv, hadEnv := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER")
	t.Cleanup(func() {
		if hadEnv {
			_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", savedEnv)
			return
		}
		_ = os.Unsetenv("WEBKIT_DISABLE_DMABUF_RENDERER")
	})
	_ = os.Unsetenv("WEBKIT_DISABLE_DMABUF_RENDERER")

	ConfigureWebKitRendering()
	if _, ok := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER"); ok {
		t.Error("逃生舱开启时应保留 DMABUF 渲染器")
	}
}

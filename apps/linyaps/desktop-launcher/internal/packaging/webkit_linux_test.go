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

// clearRenderingEnv 清空本文件涉及的全部环境变量，并在用例结束后恢复原值，
// 避免设置泄漏给同包其他用例或依赖宿主机真实取值。
func clearRenderingEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"WEBKIT_DISABLE_DMABUF_RENDERER",
		"WEBKIT_DMABUF_RENDERER_FORCE_SHM",
		"WEBKIT_DISABLE_COMPOSITING_MODE",
		"DSH_DESKTOP_DMABUF_RENDERER",
		"DSH_DESKTOP_SHM_RENDERER",
		"DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING",
	} {
		saved, had := os.LookupEnv(name)
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(name, saved)
				return
			}
			_ = os.Unsetenv(name)
		})
		_ = os.Unsetenv(name)
	}
}

// fakeDRM 造出 /sys/class/drm 的等价结构：renderD<idx>/device/vendor 的内容即 PCI
// 厂商号。传空 map 得到「目录存在但没有渲染节点」的形态。
func fakeDRM(t *testing.T, vendors map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for node, vendor := range vendors {
		dir := filepath.Join(root, node, "device")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// 真实 sysfs 的 vendor 文件带结尾换行，探针必须容忍。
		if err := os.WriteFile(filepath.Join(dir, "vendor"), []byte(vendor+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// overrideDRM 把探针指向临时目录并在用例结束后复原。
func overrideDRM(t *testing.T, path string) {
	t.Helper()
	saved := drmClassPath
	drmClassPath = path
	t.Cleanup(func() { drmClassPath = saved })
}

// assertNoDMABufDisable 守住本次修复的核心：launcher 任何分支都不得再设置
// WEBKIT_DISABLE_DMABUF_RENDERER。该变量在 webkit2gtk 2.44+ 上会清空 transport mode，
// 连加速合成都一起失去（见 ConfigureWebKitRendering 的说明与上游 bug 324730）。
func assertNoDMABufDisable(t *testing.T) {
	t.Helper()
	if value, ok := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER"); ok {
		t.Errorf("launcher 不应设置 WEBKIT_DISABLE_DMABUF_RENDERER，得到 %q", value)
	}
}

// TestConfigureWebKitRenderingFallsBackOnlyOnNvidiaOnlyHosts 固定新的判据：只有渲染
// 路径只剩 NVIDIA 的机器才回退到共享内存传输；混合机（存在非 NVIDIA 渲染节点）与没有
// 渲染节点的机器都不许被覆盖。判据落在渲染节点而不是 /sys/module/nvidia，正是为了让
// Optimus 机器不再被误判。
func TestConfigureWebKitRenderingFallsBackOnlyOnNvidiaOnlyHosts(t *testing.T) {
	clearRenderingEnv(t)

	cases := []struct {
		name    string
		vendors map[string]string
		want    bool
	}{
		{"只有 NVIDIA 节点", map[string]string{"renderD128": "0x10de"}, true},
		{"Intel 主显 + NVIDIA 副卡", map[string]string{"renderD128": "0x8086", "renderD129": "0x10de"}, false},
		{"只有非 NVIDIA 节点", map[string]string{"renderD128": "0x8086"}, false},
		{"没有渲染节点", map[string]string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearRenderingEnv(t)
			overrideDRM(t, fakeDRM(t, tc.vendors))

			ConfigureWebKitRendering()
			assertNoDMABufDisable(t)
			got := os.Getenv("WEBKIT_DMABUF_RENDERER_FORCE_SHM") == "1"
			if got != tc.want {
				t.Errorf("FORCE_SHM 设置结果 = %v，期望 %v", got, tc.want)
			}
		})
	}
}

// TestNvidiaOnlyRenderStackUnreadableDRM 固定读取失败方向的语义：探针读不到（目录不存在）
// 时必须返回 false，即「不覆盖任何机器」。反过来会在没有 DRM 信息的环境里错误降级。
func TestNvidiaOnlyRenderStackUnreadableDRM(t *testing.T) {
	overrideDRM(t, filepath.Join(t.TempDir(), "absent"))
	if nvidiaOnlyRenderStack() {
		t.Error("/sys/class/drm 不可读时应判定为非 NVIDIA-only")
	}
}

// TestNvidiaOnlyRenderStackIgnoresUnknownNodes 固定另一种失配方向：与 NVIDIA 节点并存的
// 节点如果读不到厂商号（没有 device/vendor，或读失败），它既不能证明是 NVIDIA，也不能被
// 当作不存在——否则一台厂商未知的机器会被误判成 NVIDIA-only 而降级。
func TestNvidiaOnlyRenderStackIgnoresUnknownNodes(t *testing.T) {
	root := fakeDRM(t, map[string]string{"renderD128": "0x10de"})
	if err := os.MkdirAll(filepath.Join(root, "renderD129", "device"), 0o755); err != nil {
		t.Fatal(err)
	}
	overrideDRM(t, root)

	if nvidiaOnlyRenderStack() {
		t.Error("存在厂商号不明的渲染节点时应判定为非 NVIDIA-only")
	}
}

// TestConfigureWebKitRenderingHonorsOptOut 固定正向逃生舱：请求保留硬件 DMABUF 时，
// 即便判据命中也不得写入降级变量。
func TestConfigureWebKitRenderingHonorsOptOut(t *testing.T) {
	clearRenderingEnv(t)
	overrideDRM(t, fakeDRM(t, map[string]string{"renderD128": "0x10de"}))
	t.Setenv("DSH_DESKTOP_DMABUF_RENDERER", "1")

	ConfigureWebKitRendering()
	assertNoDMABufDisable(t)
	if _, ok := os.LookupEnv("WEBKIT_DMABUF_RENDERER_FORCE_SHM"); ok {
		t.Error("逃生舱开启时应保留硬件 DMABUF 路径")
	}
}

// TestConfigureWebKitRenderingShmOptIn 固定反向逃生舱：混合机上确实复现闪烁的用户可以
// 显式要求共享内存传输，而不必关掉整个加速合成。
func TestConfigureWebKitRenderingShmOptIn(t *testing.T) {
	clearRenderingEnv(t)
	overrideDRM(t, fakeDRM(t, map[string]string{"renderD128": "0x8086", "renderD129": "0x10de"}))
	t.Setenv("DSH_DESKTOP_SHM_RENDERER", "1")

	ConfigureWebKitRendering()
	assertNoDMABufDisable(t)
	if got := os.Getenv("WEBKIT_DMABUF_RENDERER_FORCE_SHM"); got != "1" {
		t.Errorf("请求共享内存传输时应设置 FORCE_SHM，得到 %q", got)
	}
}

// 关掉加速合成的逃生舱（AUDIT 17）：同一族 GPU 驱动故障里，关掉 DMABUF 仍然花屏的
// 机器可以再关掉合成。默认不开——合成是正常路径；它与渲染节点判据无关，是显式的
// 用户自救开关。
func TestConfigureWebKitRenderingDisableCompositingOptIn(t *testing.T) {
	clearRenderingEnv(t)
	overrideDRM(t, fakeDRM(t, nil)) // 不命中回退条件

	ConfigureWebKitRendering()
	if _, ok := os.LookupEnv("WEBKIT_DISABLE_COMPOSITING_MODE"); ok {
		t.Error("未请求时不应关闭加速合成")
	}

	t.Setenv("DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING", "1")
	ConfigureWebKitRendering()
	if got := os.Getenv("WEBKIT_DISABLE_COMPOSITING_MODE"); got != "1" {
		t.Errorf("请求后应关闭加速合成，得到 %q", got)
	}
}

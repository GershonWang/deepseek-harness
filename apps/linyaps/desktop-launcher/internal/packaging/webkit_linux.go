//go:build linux

package packaging

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// webkitExecPathRaw 是短路径的单一来源文件内容。打包脚本
// （linglong/patch-webkit-exec-path.sh）与 launcher 都读这一个文件：补丁把 .so 里的
// 硬编码路径替换成它，launcher 再把同名路径建为指向真实 helper 目录的符号链接。
// 两边各写一遍字面量的话，改一处就会产出「装得上、GUI 起不来」的包，而两边都不会报错。
//
//go:embed webkit-exec-path.txt
var webkitExecPathRaw string

// webkitExecPath 返回去空白后的短路径。文件按仓库约定以单个换行结尾，读取时去掉。
func webkitExecPath() string {
	return strings.TrimSpace(webkitExecPathRaw)
}

// ConfigureWebKitHelperPath 让 webkit2gtk 找到其辅助进程
// （WebKitNetworkProcess / WebKitWebProcess / WebKitGPUProcess）。
//
// 背景：Debian 正式构建的 webkit2gtk 把 helper 目录硬编码在
// libwebkit2gtk-4.1.so.0 里（/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1），
// 运行时 /usr 只读、玲珑 layer 不导出 /usr 写入。打包时已用
// patch-webkit-exec-path.sh 把该字符串字节替换为 webkitExecPath()，这里在启动时把
// 该短路径建为指向 ${PREFIX} 下真实 helper 目录的符号链接。注入 bundle 用
// WEBKIT_INJECTED_BUNDLE_PATH 覆盖。开发态（未打包）不动：helper 在系统标准路径，
// 默认即正确。
func ConfigureWebKitHelperPath() {
	prefix := HarnessPrefix()
	if prefix == "" {
		return
	}
	helperDir := filepath.Join(prefix, "lib", "x86_64-linux-gnu", "webkit2gtk-4.1")
	network := filepath.Join(helperDir, "WebKitNetworkProcess")
	if _, statErr := os.Stat(network); statErr != nil {
		return // 开发态：helper 在系统路径，无需处理
	}
	// 短路径所在目录可写，建符号链接指向真实 helper 目录。链接可能残留自旧包运行
	// （包 id 变更或卸载后成悬空），复用前必须验证目标仍指向当前包目录。
	shortPath := webkitExecPath()
	if !webkitHelperLinkUsable(shortPath, helperDir) {
		_ = os.Remove(shortPath)
		if err := os.Symlink(helperDir, shortPath); err != nil {
			fmt.Fprintf(os.Stderr, "dsh-desktop: 创建 webkit helper 符号链接失败: %v\n", err)
			return
		}
	}
	_ = os.Setenv("WEBKIT_INJECTED_BUNDLE_PATH", filepath.Join(helperDir, "injected-bundle"))
}

// ConfigureWebKitRendering 在 GTK/WebKit 初始化之前选择 webkit2gtk 的合成后端。
//
// 背景：webkit2gtk 默认启用 DMABUF 加速合成。在内核加载了 NVIDIA 专有驱动的机器
// 上，窗口被其它程序遮挡后重新暴露、合成层重建时 DMABUF buffer 协商失败，整个
// web 区域会短暂闪成纯色再自行恢复（浅色主题白、深色主题黑），而 harness 进程与
// 正在运行的任务都不受影响。
//
// 为消除该闪烁，这里一度在「/sys/module/nvidia 存在」时就设置
// WEBKIT_DISABLE_DMABUF_RENDERER=1。实测表明该覆盖的判据与旋钮都站不住：
//
//  1. 判据过宽。Optimus 这类混合机上内核同样加载 NVIDIA 模块（本机即 Intel 主显 +
//     GTX 960M），但 WebKit 在 DMABUF 路径里选的是显示 GPU——实测 WebProcess 持有
//     Intel 的 renderD128，与 NVIDIA 无关，却被一并降级。
//  2. 旋钮代价远大于「放弃一次零拷贝」。webkit2gtk 2.44+ 里
//     AcceleratedBackingStoreDMABuf::rendererBufferMode() 是在 mode.add(SharedMemory)
//     之前响应 WEBKIT_DISABLE_DMABUF_RENDERER 的，transport mode 因而被留空，
//     AcceleratedBackingStore 拿不到可用的后备存储（上游 bug 324730 与多个下游应用的
//     实测崩溃都指向这里）。同一页面实测：该变量下 WebProcess 的 CPU 约为对照臂的两倍，
//     且 WebProcess 的 GL 落到没有显示输出的 dGPU 上，形成每帧跨卡回读。
//
// 因此换成两点：
//   - 旋钮改为 WEBKIT_DMABUF_RENDERER_FORCE_SHM：它在 mode.add(SharedMemory) 之后
//     生效，跳过硬件 dmabuf 但保留合成与后备存储，才是「只去掉零拷贝呈现」的语义。
//   - 判据收窄为「这台机器只有 NVIDIA 渲染节点」：混合机存在非 NVIDIA 渲染节点时
//     WebKit 不会落到 NVIDIA 上，不该被覆盖。发行版若已带 disable-nvidia-dmabuf.patch，
//     该分支在其上通常是空操作（补丁的 isNVIDIA() 更早返回），保留它是为了没有该补丁
//     的构建。
//
// DSH_DESKTOP_DMABUF_RENDERER=1 跳过上述覆盖、保留硬件 DMABUF 路径：命中条件却并未
// 受闪烁影响的机器可以据此自救。逃生舱不主动清除已有设置，只保证 launcher 自己不写。
//
// DSH_DESKTOP_SHM_RENDERER=1 是反向逃生舱：任何机器上都强制走共享内存传输，供混合机
// 上确实复现闪烁、又不想按 DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING 关掉整个加速合成的
// 用户使用。
//
// DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1 退到更彻底的共享内存路径
// （WEBKIT_DISABLE_COMPOSITING_MODE=1）：同一族 GPU 驱动故障里，关掉 DMABUF 仍然
// 花屏的机器可以再关掉加速合成。合成是正常路径，关掉会让滚动与动画走 CPU，因此
// 默认不开。用环境变量而不是界面开关，是因为这类故障发生在窗口显示之前（起来就
// 花屏或闪退），用户进不了界面去改配置。
//
// 「单进程模式」不在可选项内：实测随包 webkit2gtk-4.1 的 .so 里没有
// WEBKIT_USE_SINGLE_WEB_PROCESS 字符串，这个构建不认该开关（AUDIT 17）。
//
// 必须在 GTK/WebKit 初始化之前调用（main 的 wails.Run 之前），之后设置不生效。
func ConfigureWebKitRendering() {
	if os.Getenv("DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING") == "1" {
		_ = os.Setenv("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
	}
	if os.Getenv("DSH_DESKTOP_DMABUF_RENDERER") == "1" {
		return
	}
	if os.Getenv("DSH_DESKTOP_SHM_RENDERER") != "1" && !nvidiaOnlyRenderStack() {
		return
	}
	_ = os.Setenv("WEBKIT_DMABUF_RENDERER_FORCE_SHM", "1")
}

// nvidiaPCIVendorID 是 NVIDIA 的 PCI 厂商号；sysfs 的 vendor 文件取值形如 "0x10de"。
const nvidiaPCIVendorID = "0x10de"

// drmClassPath 是 DRM 类目录，renderD* 渲染节点在其下；测试可覆盖。
var drmClassPath = "/sys/class/drm"

// nvidiaOnlyRenderStack 判断这台机器的渲染路径是否只剩 NVIDIA：遍历 renderD* 渲染
// 节点读其 PCI vendor，只有「至少一个 NVIDIA 节点、且没有任何非 NVIDIA 节点」才为真。
//
// 判据落在渲染节点而不是内核模块：模块存在只说明驱动装了，与 WebKit 是否用它无关，
// Optimus 机器正是反例（本机 renderD128 = Intel、renderD129 = NVIDIA，于是返回 false，
// 不再降级）。读不到 vendor 的节点按「非 NVIDIA」处理，目录也读不到时同样返回 false：
// 这个判据失配的方向必须是「不覆盖某台机器」，而不是错误地降级一台正常机器。
func nvidiaOnlyRenderStack() bool {
	entries, err := os.ReadDir(drmClassPath)
	if err != nil {
		return false
	}
	var sawNvidia, sawOther bool
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "renderD") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(drmClassPath, entry.Name(), "device", "vendor"))
		if err != nil {
			// 认不出厂商的节点既不能证明是 NVIDIA，也不能当作不存在。
			sawOther = true
			continue
		}
		if strings.EqualFold(strings.TrimSpace(string(raw)), nvidiaPCIVendorID) {
			sawNvidia = true
			continue
		}
		sawOther = true
	}
	return sawNvidia && !sawOther
}

// webkitHelperLinkUsable 判断短路径符号链接能否直接复用：存在、指向
// expected 目录、且该目录里的 WebKitNetworkProcess 可访问（未卸载）。
func webkitHelperLinkUsable(shortPath, expected string) bool {
	target, err := os.Readlink(shortPath)
	if err != nil {
		return false
	}
	if target != expected {
		return false
	}
	_, err = os.Stat(filepath.Join(expected, "WebKitNetworkProcess"))
	return err == nil
}

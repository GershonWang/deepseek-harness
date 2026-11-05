//go:build linux

package packaging

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// shortPathsRaw 是短路径清单的单一来源文件内容。打包脚本
// （linglong/patch-webkit-exec-path.sh）与 launcher 都读这一个文件：脚本据它把
// .so 里的硬编码路径等长替换，launcher 再把同名路径建为指向真实文件的符号链接。
// 两边各写一遍字面量的话，改一处就会产出「装得上、GUI 起不来」的包。
//
//go:embed short-paths.txt
var shortPathsRaw string

// shortPaths 解析短路径清单（KEY=VALUE，忽略空行与 # 注释）。
// @returns 键到短路径的映射。
func shortPaths() map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(shortPathsRaw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

// ConfigureBundledRuntime 让打包后的 WebKitGTK 6.0 找到它的辅助进程与沙箱依赖。
//
// 背景（与 v2 时代的 webkit2gtk-4.1 同源，但路径与依赖更多）：
//   - helper 目录（WebKitWebProcess/WebKitNetworkProcess/WebKitGPUProcess）以
//     PKGLIBEXECDIR 硬编码进 libwebkitgtk-6.0.so.4，运行时 /usr 只读、玲珑 layer
//     不导出 /usr 写入；
//   - WebKitGTK 6.0 默认开启 UI 进程沙箱，它按 /usr/bin/bwrap 启动
//     xdg-dbus-proxy 与 Web 进程，容器里同样没有这两个路径；
//   - 沙箱打开后 helper 跑在 bwrap 的挂载命名空间里，读不到容器级 ld.so.cache，
//     会报 libwebkitgtk-6.0.so.4: cannot open shared object file。
//
// 因此打包时把这四处路径都做等长替换（见 linglong/patch-webkit-exec-path.sh），
// 这里在启动时建好短路径链接，并把包内库目录放进 LD_LIBRARY_PATH（helper 继承该环境）。
// 开发态（未打包）直接返回：helper 与 bwrap 都在系统路径上，默认即正确。
func ConfigureBundledRuntime() {
	prefix := HarnessPrefix()
	if prefix == "" {
		return
	}
	libDir := filepath.Join(prefix, "lib", "x86_64-linux-gnu")
	helperDir := filepath.Join(libDir, "webkitgtk-6.0")
	if _, err := os.Stat(filepath.Join(helperDir, "WebKitNetworkProcess")); err != nil {
		return // 开发态：helper 在系统路径，无需处理
	}

	paths := shortPaths()
	linkIfPresent(helperDir, paths["WEBKIT_EXEC"])
	linkIfPresent(filepath.Join(prefix, "bin", "bwrap"), paths["BWARP"])
	linkIfPresent(filepath.Join(prefix, "bin", "xdg-dbus-proxy"), paths["DBUS_PROXY"])

	_ = os.Setenv("WEBKIT_INJECTED_BUNDLE_PATH", filepath.Join(helperDir, "injected-bundle"))

	// 沙箱内的 helper 读不到容器的 ld.so.cache，必须显式给出库目录。
	// 追加而不是覆盖：外层可能已有自己的 LD_LIBRARY_PATH（如宿主开发态）。
	libs := libDir + string(os.PathListSeparator) + filepath.Join(prefix, "lib")
	if existing := os.Getenv("LD_LIBRARY_PATH"); existing != "" {
		libs += string(os.PathListSeparator) + existing
	}
	_ = os.Setenv("LD_LIBRARY_PATH", libs)
}

// ConfigureInputMethod 让 GTK4 找到包内的输入法模块。
//
// 背景：宿主用 fcitx5，GTK4 通过 im module（libim-fcitx5.so）接入。GTK 的大版本换了
// 之后容器里没有 GTK4 版前端（base 只带 gtk2/gtk3），表现是「中文完全打不出来」，
// 而 GTK_IM_MODULE/XMODIFIERS 看起来都正常——因此这条依赖与搜索路径都必须随包显式处理。
//
// GTK4 没有 immodules.cache，是按目录扫描的：除了编译期 libdir，还会扫描
// $GTK_PATH/gtk-4.0/4.0.0/immodules。把包内库目录前置进 GTK_PATH 即可命中随包模块。
// 必须在 GTK 初始化之前调用（main 的 application.New 之前），之后设置不生效。
func ConfigureInputMethod() {
	prefix := HarnessPrefix()
	if prefix == "" {
		return
	}
	// 实测（GTK_DEBUG=modules）：GTK4 把 GTK_PATH 的每个条目当作「版本目录的父目录」，
	// 即它会扫描 $GTK_PATH/4.0.0/immodules；而编译期内置路径是
	// /usr/lib/x86_64-linux-gnu/gtk-4.0/4.0.0/immodules。两者对齐后，GTK_PATH 必须指向
	// .../gtk-4.0，而不是 .../x86_64-linux-gnu——写成后者时 GTK 会去找
	// .../x86_64-linux-gnu/4.0.0/immodules（不存在），模块静默不加载。
	gtkRoot := filepath.Join(prefix, "lib", "x86_64-linux-gnu", "gtk-4.0")
	if _, err := os.Stat(filepath.Join(gtkRoot, "4.0.0", "immodules", "libim-fcitx5.so")); err != nil {
		return // 开发态：模块在系统路径上，无需处理
	}
	path := gtkRoot
	if existing := os.Getenv("GTK_PATH"); existing != "" {
		path += string(os.PathListSeparator) + existing
	}
	_ = os.Setenv("GTK_PATH", path)
}

// linkIfPresent 在短路径上建一个指向 target 的符号链接；target 不存在时跳过。
//
// 链接可能残留自旧包运行（包 id 变更或卸载后成悬空），因此每次先删除再重建，
// 不做「存在即复用」——悬空链接会让 WebKit 直接报找不到 helper。
// @param target - 包内真实路径。
// @param short - 被 .so 硬编码的短路径。
func linkIfPresent(target, short string) {
	if short == "" {
		return
	}
	if _, err := os.Stat(target); err != nil {
		return
	}
	_ = os.Remove(short)
	if err := os.Symlink(target, short); err != nil {
		fmt.Fprintf(os.Stderr, "dsh-desktop: 创建 %s 符号链接失败: %v\n", short, err)
	}
}

// ConfigureWebKitRendering 在 GTK/WebKit 初始化之前选择 webkit2gtk 的合成后端。
//
// 背景：webkit2gtk 默认启用 DMABUF 加速合成。在内核加载了 NVIDIA 专有驱动的机器上，
// 窗口被其它程序遮挡后重新暴露、合成层重建时 DMABUF buffer 协商失败：轻则整个 web
// 区域短暂闪成纯色再自行恢复（浅色主题白、深色主题黑），重则大面积区域在「消失↔出现」
// 之间高频反复、页面内容不再刷新、界面点不动，只能杀掉进程重启。
//
// 旋钮用 WEBKIT_DMABUF_RENDERER_FORCE_SHM，而不是上游的 WEBKIT_DISABLE_DMABUF_RENDERER：
// webkit2gtk 2.44+ 里 AcceleratedBackingStoreDMABuf::rendererBufferMode() 是在
// mode.add(SharedMemory) 之前响应后者的，transport mode 因而被留空，
// AcceleratedBackingStore 拿不到可用的后备存储（上游 bug 324730 与多个下游应用的实测
// 崩溃都指向这里）；FORCE_SHM 在该 add 之后生效，跳过硬件 dmabuf 但保留合成与后备存储，
// 才是「只去掉零拷贝呈现」的语义。同一页面实测：DISABLE 那一臂 WebProcess 的 CPU 约为
// 对照臂的两倍，且 GL 落到没有显示输出的 dGPU 上，形成每帧跨卡回读。
//
// 触发判据是「机器的渲染节点里存在 NVIDIA」（见 hasNvidiaRenderNode）。这条判据的形态
// 改过两次，两次都不是措辞调整：
//
//   - 最初探 /sys/module/nvidia：模块存在只说明驱动装了，与「WebKit 是否踩到该缺陷」
//     之间没有被验证过的关系。
//   - 随后收窄为「只有 NVIDIA 渲染节点」，理由是混合机上 WebKit 在 DMA-BUF 路径里选的是
//     显示 GPU（实测 WebProcess 持有 Intel 的 renderD128），因而与 NVIDIA 无关。该前提
//     已被实测推翻：本机正是这个形态（Intel 主显 + GTX 960M），遮挡后重新暴露时照样
//     大面积闪烁并卡死界面；同一台机器改走 FORCE_SHM 后 22 分钟零闪零冻结。判据于是
//     回到「NVIDIA 在场」，但保留了代价更小的那个旋钮。
//
// DSH_DESKTOP_DMABUF_RENDERER=1 跳过上述覆盖、保留硬件 DMABUF 路径：命中条件却并未
// 受闪烁影响的机器可以据此自救。逃生舱不主动清除已有设置，只保证 launcher 自己不写。
//
// DSH_DESKTOP_SHM_RENDERER=1 是反向逃生舱：任何机器上都强制走共享内存传输，供没有
// NVIDIA 渲染节点、却也复现了同类闪烁（又不想关掉整个加速合成）的机器使用。
//
// DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1 退到更彻底的共享内存路径
// （WEBKIT_DISABLE_COMPOSITING_MODE=1）：同一族 GPU 驱动故障里，关掉 DMABUF 仍然
// 花屏的机器可以再关掉加速合成。合成是正常路径，关掉会让滚动与动画走 CPU，因此
// 默认不开。用环境变量而不是界面开关，是因为这类故障发生在窗口显示之前（起来就
// 花屏或闪退），用户进不了界面去改配置。
//
// 「单进程模式」不在可选项内：Linux 的 WebKitGTK 6.0 构建里没有
// WEBKIT_USE_SINGLE_WEB_PROCESS 开关（AUDIT 17）。
//
// 必须在 GTK/WebKit 初始化之前调用（main 的 wails.Run 之前），之后设置不生效。
func ConfigureWebKitRendering() {
	if os.Getenv("DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING") == "1" {
		_ = os.Setenv("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
	}
	if os.Getenv("DSH_DESKTOP_DMABUF_RENDERER") == "1" {
		return
	}
	if os.Getenv("DSH_DESKTOP_SHM_RENDERER") != "1" && !hasNvidiaRenderNode() {
		return
	}
	_ = os.Setenv("WEBKIT_DMABUF_RENDERER_FORCE_SHM", "1")
}

// nvidiaPCIVendorID 是 NVIDIA 的 PCI 厂商号；sysfs 的 vendor 文件取值形如 "0x10de"。
const nvidiaPCIVendorID = "0x10de"

// drmClassPath 是 DRM 类目录，renderD* 渲染节点在其下；测试可覆盖。
var drmClassPath = "/sys/class/drm"

// hasNvidiaRenderNode 判断这台机器的渲染节点里是否存在 NVIDIA：遍历 renderD* 节点读其
// PCI vendor，任一节点为 0x10de 即为真。
//
// 判据是「NVIDIA 在场」而不是「只有 NVIDIA」：本机这类 Optimus 混合机（Intel 驱动显示 +
// GTX 960M 已加载）同样命中 DMABUF 呈现缺陷，即使 WebKit 的 DMA-BUF 路径选中的是显示
// GPU。把混合机排除在外是上一版的判断，已被实测推翻，理由见 ConfigureWebKitRendering。
//
// 判据落在渲染节点而不是内核模块：模块存在只说明驱动装了，渲染节点才是 WebKit 真正会去
// 协商缓冲的那一侧。读不到 vendor 的节点按「非 NVIDIA」处理，目录也读不到时同样返回
// false：这个判据失配的方向必须是「不覆盖某台机器」，而不是错误地降级一台正常机器。
func hasNvidiaRenderNode() bool {
	entries, err := os.ReadDir(drmClassPath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "renderD") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(drmClassPath, entry.Name(), "device", "vendor"))
		if err != nil {
			// 认不出厂商的节点不能证明是 NVIDIA，跳过即可：它既不该让判据为真，
			// 也不影响其它 NVIDIA 节点让判据为真。
			continue
		}
		if strings.EqualFold(strings.TrimSpace(string(raw)), nvidiaPCIVendorID) {
			return true
		}
	}
	return false
}

# Agent Note: 混合 GPU 机上的 WebKitGTK DMABUF 闪烁

Status: implemented

[English](2026-10-19-desktop-launcher-hybrid-gpu-dmabuf-flicker.md) | 中文

## Problem

打包客户端把 harness GUI 跑在 webkit2gtk 的 webview 里。在这台机器上——只有 eDP-1 一个已连接输出、由 Intel HD 530 驱动，另为 GTX 960M 加载了 NVIDIA 专有驱动 580.119.02——窗口被遮挡后重新暴露时，webview 的加速合成会失败：会话区域的大片区域以很高频率在消失与重现之间反复，页面停止刷新，窗口也不再接受输入。唯一的恢复方式是从托盘退出客户端再重新启动，而这种情况一小时内发生了三次。

故障只发生在呈现环节。`launcher.log` 会记录阶段迁移、iframe 重载、挂载握手与桥接上报，而在窗口闪烁的整个 48 分钟里它一直是空的；`reload`、`reload-exhausted`、`bridge-stall`、`iframe-error` 在整个会话中都是零：页面从未重新初始化，因此 shell 与 harness 都不在嫌疑之内。页面静止不动时，`WebKitWebProcess` 持有五个 `renderD128` 描述符，并烧掉一个核心的 53%，可见合成器还活着并在空转，而不是已经消失。

[混合显卡机器上的渲染后端选择](2026-10-18-desktop-launcher-webkit-render-backend-selection.zh.md) 曾把共享内存回退收窄到渲染节点只有 NVIDIA 的机器，前提是混合机能让 WebKit 留在显示 GPU 上，因而永远不会遇到[更早记录的闪烁](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.zh.md)。这台机器恰恰是该前提的反例：它的 DMA-BUF 路径选中的是 Intel 节点，却复现了这个缺陷。

## Decision

只要 `/sys/class/drm/renderD*` 中有任意一个节点报告 NVIDIA 的 PCI 厂商号 `0x10de`，`packaging.ConfigureWebKitRendering` 就应用回退（`hasNvidiaRenderNode`），而不再要求每个节点都是 NVIDIA。混合机因此重新被覆盖，回退也仍然用更轻的 `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1`，而不是 `WEBKIT_DISABLE_DMABUF_RENDERER`：webkit2gtk 在共享内存传输加入之后才读它，所以加速合成得以保留，只丢掉零拷贝呈现这条路径。

三个逃生舱语义不变：`DSH_DESKTOP_DMABUF_RENDERER=1` 跳过回退、保留硬件 DMA-BUF 路径，`DSH_DESKTOP_SHM_RENDERER=1` 在渲染节点不匹配任何条件的机器上强制共享内存传输，`DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1` 整体关闭加速合成。

## Evidence

同一个复现步骤——会话正在执行任务，用最大化的终端遮住客户端数分钟后再切回来——在同一台机器、同一个构建上分别跑了两条传输路径，只改一个变量：

| 臂 | WebProcess 渲染节点 | dmabuf fd | nvidia fd | 复现 |
|---|---|---|---|---|
| 默认（`renderD128` DMA-BUF） | `renderD128`（Intel） | 5 | 0 | 三次尝试全部闪烁并失去输入 |
| `DSH_DESKTOP_SHM_RENDERER=1` → `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1` | `renderD129`（NVIDIA） | 0 | 38 | 22 分钟内不闪烁，包含遮挡与重新暴露 |

两条臂上 `launcher.log` 都保持为空、异常计数器都保持为零，这正是把呈现路径故障与 shell 侧故障区分开的依据。判据也在真实宿主上解过一次：`renderD128` 报告 `0x8086`，`renderD129` 报告 `0x10de`，所以回退现在在这台机器上生效。

## Alternatives considered

**保留 NVIDIA-only 触发条件，把显式打开写进文档。** 否决于第二次复现：先前笔记放着不保护的机器类别，恰恰是最早看到该缺陷的类别；而桌面项与切换器都够不到这个显式打开，所以这类机器上的用户只能等新版本。先前笔记的 Consequences 一节正是把这一点记为一项被接受的缺口。

**保留 NVIDIA-only 触发条件，改为给这个开关一个设置界面。** 作为面向渲染节点不匹配任何条件的机器的独立改进成立，但它改不了这次的结果：判据一直是命中的，只是窄到没能采取动作。

**认定失去零拷贝路径的代价太高，不能默认接受。** 依据实测否决：先前笔记自己的表格里，这台机器上两条臂的 Web/UI CPU 中位相同（2.9 / 5.8），而且 `FORCE_SHM` 是保住加速合成，而不是失去它。

**改为检测卡死并重载 webview。** 推迟而非否决：对于判据之外的机器，这是正确的安全网，但对于一个换条传输路径就能消除的缺陷，它不能充当修复；而且误判时自动重载会丢掉页面内的状态。

## Consequences

混合机在显示 GPU 上放弃零拷贝呈现路径，改为经由共享内存合成。上面的实测表明，在这类机器上 CPU 代价持平；而 `DSH_DESKTOP_DMABUF_RENDERER=1` 收窄了「命中判据却并未受影响」的机器的暴露面。

判据仍然是从「存在 NVIDIA 渲染节点」归纳出来的，而不是来自 webkit2gtk 里与驱动无关的根因，所以没有该节点的机器依旧不受保护：nouveau、AMD 与仅有核显的宿主都在判据之外，它们出现相同症状时仍然需要有人报告，并配合 `DSH_DESKTOP_SHM_RENDERER=1`。这里的任何内容都改变不了该缺陷的偶发性——复现需要持续数分钟的遮挡，因此一条只保持几分钟干净的臂，自身证据很弱。

## Testing

`CGO_ENABLED=0 go test ./internal/...` 覆盖新的触发表（只有 NVIDIA 为真；Intel 加 NVIDIA 为真；只有非 NVIDIA 为假；没有渲染节点为假）、`/sys/class/drm` 不可读返回假、厂商号文件不可读时既不把判据变成真也不掩盖可读的 NVIDIA 节点、三个逃生舱，以及「launcher 从不设置 `WEBKIT_DISABLE_DMABUF_RENDERER`」这条守卫。

未验证：从桌面项、不带任何环境变量启动打包产物的那一轮。上面的对照是从 shell 跑的，因此重新构建的 `.uab` 里那条默认路径才是收口所需的检查。

## Related

[混合显卡机器上的渲染后端选择](2026-10-18-desktop-launcher-webkit-render-backend-selection.zh.md) 拥有旋钮的选择（用 `WEBKIT_DMABUF_RENDERER_FORCE_SHM` 而不是 `WEBKIT_DISABLE_DMABUF_RENDERER`）、渲染节点探针与这几个逃生舱；它对触发条件的收窄在这里被取代。[WebKitGTK DMABUF compositing flicker on NVIDIA machines](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.zh.md) 拥有最初的复现及与 NVIDIA 驱动的相关性。[内嵌 WebView 的硬件加速策略](2026-10-18-desktop-launcher-webview-hardware-acceleration.zh.md) 拥有「合成到底发不发生」，正是它让这个缺陷变得可达。

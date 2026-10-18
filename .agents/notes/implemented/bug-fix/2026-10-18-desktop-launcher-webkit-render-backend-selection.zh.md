# Agent Note: 混合显卡机器上的 launcher 渲染后端选择

Status: implemented

[English](2026-10-18-desktop-launcher-webkit-render-backend-selection.md) | 中文

## Problem

客户反馈：打包客户端的页面没有 GPU 加速渲染，插件设置这类界面稍重的页面卡成幻灯片、CPU 占用很高；而把同一个 harness 地址放到系统浏览器里打开却很流畅。

其中前一半是已发布代码直接造成的。`packaging.ConfigureWebKitRendering` 只要 `/sys/module/nvidia` 存在就设置 `WEBKIT_DISABLE_DMABUF_RENDERER=1`（见[更早的闪烁修复](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.zh.md)）。这条回退有两点站不住：

1. **判据比缺陷更宽。** 混合显卡机器会加载 NVIDIA 模块，而 WebKit 仍在显示 GPU 上渲染。在报障机器上——只有 eDP-1 一个已连接输出、由 Intel HD 530 驱动，另有一块没有接任何输出的 GTX 960M——DMABUF 合成还开着时，运行中的 `WebKitWebProcess` 持有的是 Intel 渲染节点 `renderD128`，与 NVIDIA 无关，机器却被降级了。
2. **旋钮的代价远大于先前笔记假设的「一条零拷贝快速路径」。** webkit2gtk 2.44+ 里 `AcceleratedBackingStoreDMABuf::rendererBufferMode()` 对 `WEBKIT_DISABLE_DMABUF_RENDERER` 的反应发生在 `mode.add(SharedMemory)` **之前**，transport mode 被留空，`AcceleratedBackingStore` 根本拿不到可用的后备存储（上游 bug 324730；多个下游应用报告在同一处于 2.5x 上崩溃）。`WEBKIT_DMABUF_RENDERER_FORCE_SHM` 的返回点在这次 add **之后**，才是这条回退真正想要的「只跳过硬件 dmabuf」语义。

## Decision

`ConfigureWebKitRendering` 改为按机器的渲染节点而不是已加载的内核模块选择后端，并用更轻的旋钮降级：

- 回退的触发条件是 `nvidiaOnlyRenderStack()` 为真：`/sys/class/drm/renderD*` 中至少一个节点报告 PCI 厂商号 `0x10de`，且没有任何节点报告别的厂商号。`/sys/class/drm` 读不到时返回 false——探针看不透的机器宁可不动，也不降级。
- 回退写入 `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1`。launcher 不再在任何分支写 `WEBKIT_DISABLE_DMABUF_RENDERER`，并有测试守住这条缺席。
- `DSH_DESKTOP_DMABUF_RENDERER=1` 语义不变：跳过回退，保留硬件 DMABUF 路径。
- 新增 `DSH_DESKTOP_SHM_RENDERER=1` 反向逃生舱：任何机器上强制共享内存传输，供确实复现闪烁、又不愿整体放弃加速合成的混合机用户使用。
- `DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1` 不变。

在混合显卡机器上 launcher 现在什么都不选，WebKit 因而保持默认的、跑在显示 GPU 上的 DMA-BUF 路径。

## Evidence

在报障机器、随包 webkit2gtk 2.50.4、同一 harness 地址下测量；每次只改一个变量，读取辅助进程的 `/proc/<pid>/fd` 与 `/proc/<pid>/maps`：

| 臂 | 环境 | WebProcess 渲染节点 | dmabuf fd | WebProcess memfd | Web/UI CPU 中位 |
|---|---|---|---|---|---|
| 改动前的发布态 | `WEBKIT_DISABLE_DMABUF_RENDERER=1` | `renderD129`（NVIDIA） | 0 | 0 | 6.0 / 10.4 |
| 去掉该变量 | — | `renderD128`（Intel） | 2 | 0 | 2.9 / 5.8 |
| `WEBKIT_FORCE_DMABUF_RENDERER=1` | 强制 DMA-BUF | `renderD128`（Intel） | 2 | 0 | 5.3 / 9.0 |
| `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1` | 新回退 | `renderD129`（NVIDIA） | 0 | 2 | 2.9 / 5.8 |

`renderD128` 的 PCI ID 是 `8086:191B`（i915），`renderD129` 是 `10DE:139B`（NVIDIA）；只有 `card0-eDP-1` 处于连接状态。把 EGL 限定到 Mesa 的 vendor 目录并没有把上下文挪回 Intel——Mesa 在 NVIDIA 节点上创建 DRI screen 失败——因此 vendor 目录固定不能替代修正判据。

最后一行正是两个旋钮的分水岭：共享内存臂没有任何 dmabuf 描述符，却持有被「关掉渲染器」那一臂丢掉的两个 `memfd:WebKitSharedMemory` 描述符，也就是 `WEBKIT_DISABLE_DMABUF_RENDERER` 拿掉的后备存储。一份链式调用 `document.startViewTransition()`、同时跑 WebGL 与 CSS 动画的压力页，在旧旋钮下约四秒内以 SIGSEGV（`wait` 状态 139）杀死 UI 进程，在新旋钮下同样页面、同样构建、只改这一个变量，存活超过十六秒。

在这台混合机上，共享内存臂与关掉渲染器臂的 GL 上下文都停在 NVIDIA 节点（与两个 DMA-BUF 臂不同），因为非 DMA-BUF 路径走的是 GLVND 的默认 vendor（`__EGL_VENDOR_LIBRARY_DIRS` 里 NVIDIA 目录排第一），而不是 DMA-BUF 路径所选的显示设备。这个落点是与渲染器选择彼此独立的另一个因素，本笔记不改动它。

## Alternatives considered

**保留 `WEBKIT_DISABLE_DMABUF_RENDERER`，只收窄触发条件。** 否决：收窄解决了混合机的误判，却留下一个只要命中就会清空 transport mode 的旋钮，结果比它要防的闪烁更糟。

**去掉回退，完全交给 WebKit 默认。** 否决：只有 NVIDIA 渲染路径的机器，以及没有发行版 `disable-nvidia-dmabuf.patch` 的构建，会失去先前修复带来的保护，而闪烁在那些机器上会复现。

**无条件恢复硬件 DMA-BUF 路径。** 否决：闪烁会回来，先前笔记在这类硬件上的复现仍然成立。

**不动渲染器，改为把 GL vendor 固定到 Mesa。** 依据实测否决：把 `__EGL_VENDOR_LIBRARY_DIRS` 限定为 Mesa 目录后，web 进程仍停在 `renderD129`，并打印 `libEGL warning: egl: failed to create dri screen`。

**把回退做进设置界面而不是环境变量。** 推迟而非否决：WebKit 只在初始化时读这些变量，设置项必须承诺一次重启；在那个界面存在之前，环境变量逃生舱仍是唯一机制。

## Consequences

提交问题的混合机重新拿到 WebKit 在显示 GPU 上的零拷贝 DMA-BUF 路径，这正是先前笔记自己的实测所显示的应然状态。只有 NVIDIA 渲染路径的机器保留一条不再冒「连加速合成一起失去」风险的回退。

闪烁回退现在**不再**对混合机生效。先前的复现恰恰发生在这类机器上，所以仍然闪烁的混合机必须用 `DSH_DESKTOP_SHM_RENDERER=1` 显式打开；这个缺口是真实的，记录在此而不是默认接受。随包 webkit2gtk 里由发行版编译进去的 `disable-nvidia-dmabuf.patch` 在真正的 NVIDIA-only 机器上本来就会清空 transport mode，因此 launcher 自己这一支在那里是空操作，存在的意义是没有该补丁的构建。

两个逃生舱（`DSH_DESKTOP_DMABUF_RENDERER`、`DSH_DESKTOP_SHM_RENDERER`）对不从 shell 启动客户端的用户依然不可达，因为打包的桌面项与切换器都不写环境变量。补上这个缺口需要上面推迟的设置界面。

## Testing

`CGO_ENABLED=0 go test ./internal/...` 覆盖了新决策：触发条件表（只有 NVIDIA 为真；Intel 加 NVIDIA 为假；只有非 NVIDIA 为假；没有渲染节点为假）、`/sys/class/drm` 不可读返回假、正向逃生舱、反向逃生舱（强制共享内存）、关掉合成的逃生舱，以及「launcher 从不写 `WEBKIT_DISABLE_DMABUF_RENDERER`」这条守卫。探针也在真实宿主上解过一次：面对 Intel 与 NVIDIA 两个渲染节点返回假。

未验证：改动后的真实窗口对照——运行中的客户端被有意保持不动；以及压力页上究竟哪个进程先崩——当时拿不到 core 与内核日志。压力页与文件描述符的测量来自 WebKitGTK 自带的 MiniBrowser，而不是 Wails 的 embedding，因此绝对值不可外推；「只改一个变量就翻转崩与不崩」这个结论不依赖 embedding。

## Related

[内嵌 WebView 的硬件加速策略](2026-10-18-desktop-launcher-webview-hardware-acceleration.zh.md) 拥有「合成到底发不发生」：launcher 现在显式传按需策略，因为 Wails 默认会把加速整体关掉——这也是只改渲染器传输救不回快路径的原因。[WebKitGTK DMABUF compositing flicker on NVIDIA machines](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.zh.md) 拥有这条触发条件所要防的闪烁，以及本笔记扩展的那几个逃生舱。[Desktop launcher on Linux/Linglong](../feature/2026-08-14-desktop-launcher-linux-linglong.zh.md) 拥有这项平台适配所处的打包架构。

# Agent Note: WebKitGTK DMABUF compositing flicker on NVIDIA machines

Status: implemented

[English](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.md) | 中文

## Problem

在一台 Deepin/X11 机器上，显示由 Intel 核显（HD 530）驱动，同时内核为 GTX 960M 加载了 NVIDIA 专有驱动 580.119.02。从这个打包客户端切出去再切回来时，整个 harness 区域偶发地被刷成一块纯色，持续一瞬——浅色主题下是白、深色主题下是黑——随后自行恢复。

harness 从来不是问题所在。`dsh-desktop-launcher` 一直在运行，会话保持连接，当时正在流式输出的任务也一直在输出；短暂消失的只有像素。这排除了页面重载、WebProcess 被终止与前端重连，把问题指向合成器。

`WebKitWebProcess` 持有 `/dev/dri/renderD128`，并加载了 `libEGL.so.1.1.0` 与 `libgbm.so.1.0.0`，说明 webview 走的是 webkit2gtk 的 DMABUF 加速合成路径，而不是软件回退。进程列表里没有 `WebKitGPUProcess`，合成跑在 web 进程内。launcher 进程持有 `/dev/dri/card0`（Intel 卡），而玲珑容器同时通过扩展 `org.deepin.driver.display.nvidia.580-119-02` 暴露了 NVIDIA 驱动。

WebKitGTK 什么都没打：stderr 里只有启动那一段，而 framebuffer 相关的诊断是 `RELEASE_LOG` 调用，除非用 `WEBKIT_DEBUG` 打开对应频道，否则一直静默。因此"没有日志"两头都说明不了。

## Decision

`packaging.ConfigureWebKitRendering()` 在内核加载了 NVIDIA 专有驱动时设置 `WEBKIT_DISABLE_DMABUF_RENDERER=1`，否则什么都不做。`main` 在 `ConfigureWebKitHelperPath()` 旁边调用它，位置在 `wails.Run` 之前——webkit2gtk 只在 GTK/WebKit 初始化时读取这个开关，之后再写不生效。

探测目标是 `/sys/module/nvidia`，保存在可覆盖的 `nvidiaModulePath` 变量里，便于测试驱动两个分支。判据落在内核模块目录而不是设备节点上：它回答的正是与缺陷相关的问题——专有驱动是否已加载——而且玲珑沙箱里该条目可见，`/proc/driver/nvidia/version` 同理。

把这个覆盖限定在该条件下正是关键：其余 Linux 机器继续使用 DMABUF 加速合成，只有命中已知触发条件的机器才用较慢的呈现路径换取稳定。

## Alternatives considered

**无条件关闭 DMABUF。** 一行代码、不用探测，还能顺带覆盖我们没见过但同样受影响的显卡组合。否决：webkit2gtk 自身的说明与 [Tauri 的 Linux 图形问题页面](https://v2.tauri.app/develop/debug/linux-graphics/)都警告过，无条件下发会让本来正常的环境也失去快速路径，而已知触发条件是 NVIDIA 驱动而不是整个 Linux。

**改用 `WEBKIT_DISABLE_COMPOSITING_MODE=1`。** 它整体关闭加速合成，当然也能盖住这种情况。否决：这是两个开关里更重的那个，而且并不需要——只关掉 DMABUF 渲染器闪烁就消失了，改动更窄的胜出。

**给 harness 文档补一个显式的 `html` 背景色。** 成本低，而且 harness 根节点确实依赖 `body` 背景传播到画布。否决作为本次修复：它处理的是画布被丢弃时画什么，而已确认的机制在合成器的 buffer 路径上，因此这只会改变闪烁的颜色，故障依旧。如果将来真的观察到画布色回退，它仍是一项独立的改进。

**把变量放在玲珑包层面而不是 launcher 里下发。** `ll-cli run --env` 可以注入它，也能让 workaround 不进二进制。否决：它只对带该参数启动的场景生效，而打包应用通常从桌面项启动，缺陷会在正常路径上照旧存在。

**按 X11 判断而不是按驱动判断。** 这台机器确实跑在 X11 上。否决：Wayland 方向两边都没有证据，比证据更宽的条件会覆盖掉本不需要覆盖的机器。

## Consequences

在受影响的机器上，反复切出/切回不再闪烁。这一点在打包客户端里通过 `ll-cli run --env WEBKIT_DISABLE_DMABUF_RENDERER=1` 启动验证，并确认变量确实经由 `/proc/<pid>/environ` 到达了 launcher 进程。整个过程里 harness 进程、会话与流式任务都未被牵涉。

代价是 NVIDIA 机器上失去了零拷贝的 DMABUF 呈现路径：webkit2gtk 在那里改为经由共享内存合成。以文本为主的界面预计察觉不到，但这个取舍是真实的，也正是覆盖不无条件下发的理由。

有三处缺口记录在案而非就地关闭。nouveau、AMD 与仅有核显的机器都在当前条件之外，若它们出现相同症状，在有人报告之前不受保护。NVIDIA 这个条件是从一次单驱动分支上的复现归纳出来的，而不是来自 webkit2gtk 里与驱动无关的根因。探测读的是内核事实，因此将来若某个玲珑沙箱隐藏了 `/sys/module`，覆盖会静默失效——失效方向是缺陷回归，而绝不会是错误地覆盖了某台机器。

一旦 webkit2gtk 修掉上游的 buffer 协商问题，或受影响的驱动分支退出支持，撤掉这个覆盖就是删掉一个函数。

## Testing

`go test ./internal/packaging/` 通过可注入的探测路径覆盖两个分支：探测目标不存在时，`ConfigureWebKitRendering` 不设置 `WEBKIT_DISABLE_DMABUF_RENDERER`；该路径出现后，它设置为 `1`。测试保存并恢复探测变量与环境变量两项，因此既不依赖宿主机真的装了 NVIDIA 驱动，也不会把设置泄漏给同包的其他测试。

容器侧的前置条件是先核查、后写码，而不是假设：玲珑沙箱内 `/sys/module/nvidia` 与 `/proc/driver/nvidia/version` 均可读，后者报出的模块版本与宿主一致，同为 580.119.02。

仍未验证的部分：打包产物是经 `ll-cli run --env` 验证的，而不是重新构建的 `.uab`，因此发布前仍需跑一次 `make build` 加一轮打包。

## Related

[Desktop launcher on Linux/Linglong](../feature/2026-08-14-desktop-launcher-linux-linglong.zh.md) 拥有这项平台适配所处的打包架构。[Clipboard image bridging](2026-08-30-clipboard-paste-large-image-and-file-copy.zh.md) 是 launcher 为 harness UI 自身无法观察到的 webkit2gtk 行为做补偿的另一处。

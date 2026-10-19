# Agent Note: 桌面启动器 GUI 挂载看门狗的时间基准与重载预算

Status: implemented

[English](2026-10-19-desktop-launcher-gui-mount-watchdog.md) | 中文

## Problem

内嵌的 harness GUI 会偶发闪屏——外壳侧看到的高频闪烁，而 harness 本身一直健康。此前两次针对这类问题的"自愈 + 可诊断"尝试都没站住。

挂载看门狗按"状态快照"累计约 25 次才重载 iframe，依据是注释里写的"Go 侧每秒推一次 `harness:status`"。事实并非如此：ticker 调的是 `emitStatusIfChanged`，而 `FrontendStatus` 没有任何随时间变化的字段——界面稳定空白时根本不会产生事件。于是在看门狗最该生效的场景（harness running、target 稳定、内容不出现）计数永不前进；真正救场的是注入桥自己的 `boot-stalled` 上报与 iframe 的 `error` 通道。

重载预算按 Target 分代：任何 `Target` 变化都会清零计数器、重载计数与"已上报"标记。而每次 harness 重启都会换 token，于是在重启频繁的时段外壳反复获得新预算，"最多两次"可以被无限重复——而每次重载都是一次整页闪白。这可能是闪屏报告的一个来源；外壳当时完全没有日志，无法把它与合成层闪屏区分开。

界面侧此前完全没有落盘，闪屏发生时没有任何证据能区分"iframe 重载了"与"合成器丢帧了"。

## Decision

看门狗改由 1 秒定时器驱动，并按经过的时间（`Date.now()` 差值）判定，不再计数推送次数。既然被写进注释的前提是错的，依赖该前提的机制就整体替换，而不是打补丁。

重载预算改为时间窗（`GUI_RELOAD_WINDOW_MS` 五分钟，`GUI_RELOAD_MAX_IN_WINDOW` 两次）。Target 换代只重置本代的等待状态，不清预算、不清"已上报"；只有时间窗过期才开始新一轮。

界面侧事件写入 `launcher.log`，与 `harness.log` 同一个运行时目录、同一时间戳格式、一行一事件，经由新的 `App.LogUiEvent` 绑定（`internal/app/uievent.go`）：舞台切换、Target 换代（只记 origin 与"查询串是否变化"，绝不记查询串本身）、挂载握手、桥的 stall 上报、带原因与预算的重载、预算用尽。文件 2 MiB 轮转，写入串行化，且 Go 侧强制把 `?token=…` 替换为 `?token=<redacted>`——调用方忘了也不会把凭据写进磁盘。

## Alternatives considered

**保留快照计数，让 Go 每秒推一次心跳。** 否决：为了一个定时器废掉 `emitStatusIfChanged`（前端会每秒重渲染），而且把呈现层的需求耦合进状态通道。

**保留分代预算，另加一个全局上限。** 否决：多一层冗余复杂度；单个时间窗计数已经足够封顶，分代记账本身就是缺陷所在。

**由前端把事件写进 `localStorage`。** 否决：不在 `harness.log` 旁边、崩溃后读取方式不同，也不是任何人都能离线检查的磁盘产物。

**记录完整的 iframe target。** 以凭据红线否决：target 里带 harness token，写进日志等于把凭据落到用户会贴进问题报告的文件里。

**用 DevTools 协议侦测重载，而不是新增绑定。** 否决：打包客户端没有暴露调试端点，为诊断打开它比追加一行日志范围更大、风险更高。

## Consequences

现在可以只靠产物给闪屏报告定性：重载事件带原因与预算，换代事件带 origin 与查询串是否变化，舞台切换事件显示外壳有没有在改展示内容。这并不修复闪屏本身，但让"哪一类闪屏"变得可判定——此前两次尝试都做不到这一点。

预算语义变了：持续换 token 重启的 harness 不再按代各得两次重载；时间窗内总共两次，之后由既有失败页与诊断链路接管，直到窗口过期。这是刻意的——每次重载都让用户看一次闪白。

`launcher.log` 最多长到约 2 MiB 后轮转一次，支持包大小有界。脱敏是针对 `token=` 的正则；将来若有别的名字承载凭据，需要扩展该模式——Go 用例断言的是当前契约，而不是"将来也不会泄漏"。

## Testing

`internal/app/uievent_test.go` 固定日志契约：一事件一行且带时间戳、token 脱敏、换行压平并截断、达上限轮转、并发写入仍是一事件一行、绑定写入 `appenv.RuntimeDir()`（因此 `DSH_DESKTOP_LOG_DIR` 能重定向）。

`frontend/test-app.cjs`（80 pass）用假时钟驱动看门狗：超过阈值重载一次、挂载握手后不再重载、桥上报立即重载、外部服务模式不介入，以及针对预算缺陷的回归——时间窗内换代不得刷新重载预算。

## Related

这套仪器化要分类的闪屏本身：[NVIDIA 机器上 WebKitGTK DMABUF 合成闪烁](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.zh.md)，以及重新暴露它的加速改造：[桌面启动器内嵌 WebView 的硬件加速策略](2026-10-18-desktop-launcher-webview-hardware-acceleration.zh.md)。看门狗最初的白屏修复写在启动器 README（`apps/linyaps/desktop-launcher/README.zh.md`），新的日志文件也在那里记录。

# Agent Note: 桌面启动器预检门控与放行语义

Status: implemented

[English](2026-10-18-desktop-launcher-preflight-gate-first-spawn.md) | 中文

## Problem

安装后首次启动出现了这样的状态：harness 被拉起，0.27 秒后即被终止，随后是 13.5 秒的空白，才出现第一行插件树加载进度；外壳停在非 harness 舞台，从服务器菜单点「启动」看起来才把它唤醒。那一次启动的 `harness.log` 里恰好有一条 `harness killed by signal=terminated` 和 `restarting harness in 500ms (attempt 1)`，第二次启动则没有，而 launcher 自身的进程日志在这段空白里始终没有输出。

成因是两个缺陷。预检门控是在 `NewSupervisor` 已经启动监护循环**之后**才置位的，循环首轮就可能读到未置位的 `gated`，跳过门控直接 spawn，预检根本没跑。而预检的放行入口随后调用 `Release()` 再调用 `Restart()`，`Restart` 会给任何存活子进程发 SIGTERM：0.27 秒后预检结束，正好杀掉了被跳过的门控刚拉起的那个 harness。监护器接着走它正常的 500ms 退避重拉，因此最终活下来的进程出现得更晚。

## Decision

门控改为构造期输入：`app.New` 设置 `supervisor.Options.GateFirstStart`，`NewSupervisor` 在启动循环前把它读进 `gated` 字段，并删除构造后再置位的 `Gate()` 方法，使任何调用方都无法在循环存在之后才武装门控。

`Release()` 返回本次调用是否命中门控期。放行入口按**意图**拆分成两个，而不是按调用者区分：`resumeAfterPreflight`（预检通过、doctor 自身失败、忽略问题）只放行——健康的在跑进程不动，只唤醒停止/失败态；`resumeAfterEnvChange`（安全模式、全新运行时目录、自动修复、深度修复）还会重启在跑的进程，因为这些路径改动了子进程环境或磁盘状态，只有新进程才会带上变更。

门控等待被放行后还会重读 `manuallyStopped`：等待之前取的那份快照早于门控期发生的任何停止，不重读就会先拉起一次 harness 才去兑现停止意图。

## Alternatives considered

**只修放行入口，保留 `Gate()`。** 否决：预检仍会与已经起来的 harness 并行，它的结论来得太晚而无意义，fatal 结论还会报在一个已经在加载的 harness 上。

**新增一个 `NewGatedSupervisor` 之类的构造入口。** 否决：两个只差一个字段的构造函数更容易选错，而门控是同一对象的监护策略，不是它的变体。

**把门控开关放进 `Config`。** 否决：`Config` 描述被监护的子进程，门控属于监护时序；`Options` 本来就是监护参数的归属。

**门控默认武装（fail-closed）。** 否决：`Release()` 在未门控时的 no-op 语义已被文档化，现有测试与手动路径大多依赖「循环直接 spawn」；默认武装会让它们静默挂住，而不是暴露出缺少放行。

**只保留一个放行入口，由 `Release()` 自行判断是否重启。** 否决：安全模式与全新运行时目录必须重启在跑的进程，新环境才会生效；单入口会破坏这些路径。

**让 `Restart()` 跳过已经启动或正在运行的子进程。** 否决：重启是用户「立刻生效」的显式动作，插件重载路径依赖它终止当前进程。

**在测试里复现那次竞态。** 作为主要覆盖被否决：门控开关移入构造期后，「门控仍武装而子进程已在跑」这个造成故障的状态已不可达，因此测试改为断言可达契约——门控在放行前阻塞、`Release()` 报告是否命中门控、门控期按下停止不产生 spawn、纯放行不动在跑的子进程。

## Consequences

预检真正先于第一个 harness 进程，放行也不可能再终止它刚刚授权的那个进程。

`Release()` 返回 true 是「本次命中门控」的契约。调用方不得在可能命中门控的放行之后再接 `Restart()`；两个放行入口的存在就是为了让这个判断保持显式。

首次启动那 13.5 秒空白不再经由这条路径可达，因为先于重拉的那次终止不会再发生。它自身的原因仍未证实：候选是安装后首次运行的准备工作、以及在等待被终止进程占用的资源；launcher 侧对预检阶段与 spawn 事件的落盘日志仍然缺失，再次发生也无法只靠日志归因。

已知遗留边界：门控期按下停止会让监护器停在停止分支，而状态仍是 `starting`——首个进程从未 spawn，没有退出回调把它落到 `stopped`。外壳因此显示启动中舞台，`Start()` 又因停止/失败守卫拒绝动作，只能靠重启恢复。改动这个状态会连带替换预检决策页，因此本轮只记录、不改动。

## Testing

`TestGateFirstStart_BlocksUntilRelease`、`TestRelease_ReportsGateState`、`TestRelease_WithoutGateIsNoop`、`TestGateFirstStart_StopDuringGateDoesNotSpawn` 覆盖监护器契约；`TestResumeAfterPreflight_GatedReleaseStartsOnce` 与 `TestResumeAfterPreflight_KeepsRunningChild` 用长跑 mock harness 覆盖放行入口，断言子进程保持同一 pid、日志中不出现终止或重试行。

两处修复都做了变异校验：去掉 `manuallyStopped` 重读会让门控期停止用例失败（出现存活子进程 pid）；把放行入口改回无条件 `Release()` + `Restart()` 会让 `TestResumeAfterPreflight_KeepsRunningChild` 失败（pid 变化）。两次变异都在提交前还原。

覆盖边界：故障本身的那次交错没有被复现，只是被排除。本次改动未在真机重新启动打包客户端；本地 `apps/linyaps/desktop-launcher` 下 `go test ./internal/...` 全绿。

## Related

同一启动器的两份渲染笔记——[内嵌 WebView 的硬件加速策略](2026-10-18-desktop-launcher-webview-hardware-acceleration.zh.md)与[渲染后端选择](2026-10-18-desktop-launcher-webkit-render-backend-selection.zh.md)——负责内嵌页面如何被合成；本笔记负责 harness 进程究竟何时被拉起。

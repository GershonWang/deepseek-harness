# Agent Note: Startup progress on the desktop launcher loading page

Status: implemented

[English](2026-09-15-desktop-launcher-startup-progress.md) | 中文

## Problem

从 spawn 到 harness 打出就绪行之间，启动器只显示一个转圈的加载页，而这段就是全部等待：打包态实测 5.14–5.62 秒，其中约 4 秒花在插件挂载上。页面上没有任何东西在动，于是这段等待读起来像"第二个什么都不干的界面"——AUDIT 第 26 条记录过一次有意延期：等到 client-modules 的优化消掉主因（等待过长）之后，缺少反馈这件事被搁置了。

壳自身只能观测三个事实：进程被拉起、子进程产出了第一行输出、就绪行到达。三者之间的一切都发生在 harness 进程内部，所以没有任何壳侧信号能把这段间隔变成进度。

## Decision

启动器把一个很小的上报插件写进自己的运行时目录——与 `supervisor-overlay.yml` 同处——并通过同一个 `--patch` overlay 注入。上报插件统计已 settle 的 loader 条目数，向 stderr 写 `dsh-desktop: startup <已激活>/<总数>`。`Supervisor` 把这一行解析成 `domain.StartupProgress` 快照，`App` 再把快照映射为四个阶段加一个计数，经节流后的 `startup:progress` 事件送到加载页；1 秒一次的状态快照携带同一份视图。

每个阶段都由观测到的事实触发，绝不由流逝的时间触发：`starting`（已拉起、还没有输出）、`loading`（子进程已有输出、还没有计数）、`plugins`（有计数且 `已激活 < 总数`）、`serving`（所有计到的条目都已 settle，只剩监听端口）。没有上报插件时——在途的旧版本、或插件文件写不出来的主机——前两个阶段照常渲染，进度块保持隐藏，页面沿用改造前的文案。

这个计数可信有一个值得记录的原因：分母取 `loader.entries()` 里真正会挂载的行（排除 group、排除 disabled），它在启动期是稳定的——实测上报器激活时 127、结束时 130，多出的三行是启动后期才插入条目的插件（HMR 兜底，以及目录选择器那两个）。分子是已 settle 的 fiber，因此只增不减。开发态实测：上报器第一行落在 spawn 之后约 1.2 秒，此时已 settle 37/127（29%）；计数在 3.7 秒走到 130/130，与就绪行同时。打包态同一套上报器在 1.13 秒报 0/127、3.53 秒报 130/130。

让分子既便宜又无需依赖的是 `fiber.await()`：条目激活完成或激活失败时它都会 settle，两者都意味着这一行不再阻塞启动。上报插件因此不需要任何 import、不需要 timer 服务、也不需要数值化的 `FiberState`——这一点很关键，因为该文件是从 `~/.cache/dsh-desktop/` 加载的，那里解析不到裸包名。

## Alternatives considered

**按计时器编造阶段。** 进度条因为时间过去而前进，会在一个随档案与机器而变的等待上告诉用户不真实的事；AUDIT 第 26 条已经否决过这个方向，而这次改动的全部意义就是让加载页不再是装饰。否决。

**只上报壳侧事实（已拉起、首行输出、就绪）。** 这是不碰 harness 代码的廉价变体，并且作为降级路径保留了下来。它覆盖不了主要那一段：5.62 秒的启动里有 4.10 秒落在"首行输出"与"全部条目 settle"之间，而这段里没有任何壳可见的事件。不作为主机制。

**把上报做进上游 `app-boot`，用开关控制。** 它能服务所有 surface，包括浏览器里直接跑的 `dsh web`，也不会把启动器自有代码放进用户的插件树。本次范围不采纳：为一个桌面壳的呈现问题去改上游包、其录制输出与文档，代价过大；而注入式上报器只需一行即可移除，且今天就能端到端验证。若将来其他 surface 也需要同样的反馈，那才是该走的路。

**打开 loader 的 `enableLogs` 并注入 `logger-console`，再解析上游的 apply 日志。** 复用现成机制而不是新增文件。否决：为了设 `enableLogs` 必须整体替换根 include 的配置（与其 `path` 耦合），整棵树会开始打日志（包含稳态期的 HMR 噪音），而且行格式归上游所有，不是本仓库能写进契约的东西。

**轮询 `ctx.loader.entries()` 统计 active fiber。** 可以不必订阅 `internal/plugin`。否决：判断"active"需要在一个无法 import cordis 的文件里写死 `FiberState.ACTIVE` 的数值，而绑定在 vendored 枚举上的魔法数字正是后续同步会静默弄坏的东西；`fiber.await()` 是公开 API，不需要常量。

## Consequences

上报插件是运行在用户 harness 树里的启动器自有代码，并且在树里可见：一条 id 为 `dsh-desktop-startup-progress`、name 是绝对路径的 entry 会出现在插件树与诊断输出中。这是"不改上游代码的唯一在进程内的观察点"的代价，并由下面两条防护限定其边界。

条目抛异常会中止启动——harness 对 entry 激活是 fail-loud 的。因此上报插件不 import 任何东西，把 `apply` 整体包在一层防护里（自身出错只写一行诊断，不向外传播），并放在引用它的 overlay 旁边；当两者之一写不出来时，壳省略该行并退回粗粒度阶段，而不是让一次启动失败。

一次启动的前约 1.2 秒没有计数，因为此时树已经存在、但只有约三分之一的条目建好了 fiber。这段空窗正是 `loading` 阶段存在的理由：它是一条真实事实（"子进程已经在说话，计数还没开始"），不是填充物。

分母可能在第一次上报之后再多出几行，因此比率可能回退一个百分点。页面把进度条按单调不减渲染，同时原样显示上报的数字，于是显示不会倒退，而计数保持精确。

只有桌面启动器拉起 harness 时才有这条进度通道。浏览器里由 `dsh web` 提供的页面仍然显示 harness 自己的客户端加载页——那是另一个界面，按决策不在范围内。

## Testing

`internal/supervisor` 锁定 stderr 契约（含畸形行与无关行的模式表）、经真实子进程验证的 writer 接线（`testdata/mock-progress.sh`，同时断言 `OutputAt` 落在 spawn 窗口内），以及共用的行切分 sink 跨写入边界的行为。`internal/appenv` 锁定 overlay 行、其 YAML 转义后的绝对路径、与市场补丁共存、插件的前缀契约，以及插件源码不含任何 import。`internal/app` 锁定阶段映射表（含 `0/0`，以及所有非启动态映射到零值视图）与让进度变化能到达前端的快照比较。

`node --test frontend/test-app.cjs` 驱动 DOM 桩：计数与进度条宽度、`serving` 文案、无上报与缺字段的兜底、分母增长时的单调比率，以及离开启动态后的归零。`preview.mjs verify` 在真实 Chromium 里量加载页——50% 与 100% 的宽度必须与轨道一致、进度块必须落在舞台内、长计数必须保持单行、进度块不得与提示行重叠——并且已确认把进度条限宽到 20px 时该断言会失败。

两种 harness 启动方式都用手工跑过真实 overlay 与上报器：开发态（`apps/cli/lib/bin.js`）与打包态（`linglong/output/binary/files/harness/lib/bin.js`，其条目名经 `HostResolvedRootInclude` 解析）都成功加载了注入的绝对路径，并在就绪行之前上报了 `0/127` 到 `130/130`。

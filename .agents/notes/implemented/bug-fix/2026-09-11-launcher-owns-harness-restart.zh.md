# Agent Note: The launcher owns harness restart

Status: implemented

[English](2026-09-11-launcher-owns-harness-restart.md) | 中文

## Problem

点击插件市场的「立即重启」有时会让容器内的 harness 起不来，而 launcher 的诊断弹框随后报告一切正常。根因不是端口没释放，而是两个彼此独立的重启者在争同一个端口。

launcher 用每次应用运行只预留一次的端口拉起 `dsh web --port <port>`，其 supervisor 在任何退出之后都用同一份 argv 重启该子进程（[linglong 端口](../feature/2026-08-14-desktop-launcher-linux-linglong.zh.md)）。而 `dsh-market` 插件的 `/dsh-market/restart` 端点会 spawn 一个 detached helper，用 `process.argv.slice(2)` 重新拉起 harness——同一份 argv，也就是同一个 `--port`。两者随即绑定同一个地址：谁先绑上谁赢，另一方以 `EADDRINUSE` 退出。实测见 `/tmp/dsh-market-restart-2026-09-11T01-00-23.err.log`，替代 harness 死于 `listen EADDRINUSE: address already in use 127.0.0.1:41103`。

哪一方胜出，决定了用户看到哪一种失败。市场胜出时，supervisor 会对着已被占用的端口按指数退避反复重试，直到 `StartupTimeoutMs` 耗尽、进入 `StateFailed`，用户看到「启动失败」——而 doctor 只检查 supervisor 自己 spawn 的子进程，从不读 helper 写在 `tmpdir` 的日志，于是什么也查不到。这场竞态还会留下孤儿：helper 拉起的 harness 活得比自己的父进程更久（`ppid=1`），launcher 看不见它，它还与受监护的实例共享 `~/.dsh`。在同一台机器上实测确认，40195 与 40657 两个端口上各有一个存活的 harness 并存。

插件自带的防护覆盖不到这种部署。`restartAllowed` 会退回到 `detectedSupervisor()`，而后者只靠读取 `INVOCATION_ID`/`JOURNAL_STREAM` 并检查 `ppid === 1` 或父进程为 `systemd` 来识别 systemd、launchd 与 pm2。进程内的 Go supervisor 不属于其中任何一种：在受影响的主机上探测，`/dsh-market/status` 返回 `supervisor: null`，因此按钮始终可用。

## Decision

launcher 在每次 spawn harness 时注入自带的 patch overlay，把 `dsh-market` 行的 `allowRestart` 置为 false。`appenv.harnessArgs` 为全部四条入口分支组装 argv——覆盖二进制、打包态 `harness/lib/bin.js`、仓库内开发态 `apps/cli/lib/bin.js`，以及裸 `bin.js` 回退——并追加由 `writeSupervisorOverlay` 写入 launcher 运行时目录的 overlay。把 argv 集中到一个函数，正是为了不让任何一条分支悄悄失去这层保护。

flag 顺序是承重的，而且并不显然：`web` 子命令启用了 `passThroughOptions`，而 `--port` 属于 web app 而非 launcher，因此排在它之后的参数会被原样转发——放在那里的 `--patch` 永远不会被解析，只会以 `error: unknown option '--patch'` 失败。所以 launcher 自己的 flag 必须排在 `--port` 之前。

由于市场不再能重启 harness，launcher 补上了如今由它独占的入口：绑定的 `App.RestartServer` 调用 `supervisor.Restart`，服务器弹框在 启动/停止 旁提供 重启 按钮，由新增的 `CanRestart` 状态字段控制。插件安装后的重载有了受支持的路径，而不是被删掉的路径。

## Alternatives considered

**让 supervisor 让位于外部启动的 harness。** 子进程退出后，supervisor 探测端口，接管一个并非由它 spawn 的健康监听者并汇报该 URL。否决：这需要新增生命周期状态、健康检查，以及停止非子进程的能力；它让进程所有者去迁就第二个重启者，而不是消除冲突。既有的 `ModeExternal` 覆盖不了这个场景——该模式经 `Connector.BeginExternal` 由用户提供的 URL 进入。

**教 `detectedSupervisor()` 识别 launcher。** 这正是修复该落在的上游位置，但市场是解析进 profile `node_modules` 的第三方包；本地改动会被下一次安装覆盖，只有上游能承载它。把这个局限记录下来，胜过留下一个会静默消失的补丁。

**调整时序让某一个重启者总是胜出。** supervisor 的 Go `exec` 快过 helper 的 Node 启动加端口轮询，所以今天通常是它赢。否决：结果仍然取决于调度，败方仍然会崩溃并留下日志，而它可能留下的孤儿才是让状态无法收拾的原因。

**去掉固定端口，让每个 harness 自选。** 移除 `--port` 分开了两次绑定尝试，却没有分开两个 harness：两者都会存活，共享 `~/.dsh`。这是用可检测的崩溃换来静默的并发状态，更糟。

## Consequences

harness 的生命周期重新只有一个所有者：supervisor 是唯一 spawn harness 的进程，端口竞态无从发生，也没有任何重启路径能留下无人监护的实例。插件市场的重启按钮按设计消失——`/dsh-market/status` 现在返回 `restart: false`——插件变更通过 launcher 的 重启 按钮生效。

有两条事实对未来工作是承重的。overlay 按行 id `dsh-market` 与键 `allowRestart` 寻址一份第三方契约；上游若改名其中任何一个，patch 匹配不到行、include 只会发出警告而不会失败，保护因此静默降级，而 launcher 侧的检查才是察觉它的途径。另外，由于 `--patch` overlay 应用在 profile 的用户层之后，用户无法通过自己的配置重新启用市场的按钮；想要那份自由度的人必须改变 launcher 注入该设置的方式。

「只禁用市场、不补替代入口」的方案被否决：那会让插件安装后的重载除了退出应用之外无路可走。

## Testing

`apps/desktop-launcher` 的 `go test ./...` 钉住 argv 组装：各种入口形态下的 overlay 路径与 flag 顺序、overlay 的行 id 与 `allowRestart: false` 内容，以及 overlay 写不进去时的降级形态（不含 `--patch`）。`node --test frontend/test-app.cjs` 覆盖重启按钮——运行态可用并调用 `RestartServer`，停止态禁用。除测试套件之外，还用 `dsh web --patch <overlay> --dump-config` 转储了合成配置，它把该行标为 `patched by <overlay>` 且带有 `config: allowRestart: false`；再用该 overlay 真实启动一次，`/dsh-market/status` 返回 `restart: false` 与 `supervisor: null`。

## Related

- [Desktop launcher on Linux/linglong](../feature/2026-08-14-desktop-launcher-linux-linglong.zh.md) 拥有本 note 所约束的 supervisor spawn、就绪行与退避机制。
- [Preflight and fresh home](../feature/2026-09-10-desktop-launcher-preflight-and-fresh-home.zh.md) 拥有 supervisor 的 `Gate`/`Release`/`SetEnv` 语义。

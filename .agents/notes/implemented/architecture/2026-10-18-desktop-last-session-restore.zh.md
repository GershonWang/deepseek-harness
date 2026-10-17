# Agent Note: 桌面客户端跨启动恢复上次会话

Status: implemented

[English](2026-10-18-desktop-last-session-restore.md) | 中文

## Problem

玲珑薄壳把 harness Web GUI 嵌在 iframe 里，而该 iframe 的顶层文档来自壳自己的 `wails://` origin。GUI 把「我上次打开的会话」记在 `localStorage['dsh.sessions.current']`，而在这份跨源 iframe 中，WebKit 把该存储当作第三方存储对待：同一次运行里客户端读写都正常，但没有任何内容落盘。2026-09-28 之后，harness origin 再没有写出过 `http_127.0.0.1_*.localstorage` 文件，而壳自己 origin 的数据库仍在被打开，且当时实际在用的那个 origin 压根没有数据库。于是每次启动都没有已保存的选择，而启动策略对此的回答是开一个空白会话——用户上次工作的那段对话再也回不来。

上一处改动让监听端口跨启动保持稳定（`webPort`），消掉了一个变量：origin 不再变化。它并不能让存储持久化，症状因此依旧。

## Decision

由两条彼此独立的路径恢复上次打开的会话，各自覆盖对方覆盖不到的场景。

**客户端保留一条不依赖存储的回退。** 没有已保存的选择时，启动选择打开该工作区**最近更新的、带内容的**会话——非空白且未被归档——而不是复用或新建空白会话；工作区里没有这样的成员时保持原有的空白策略（`packages/client/ui-workspace/src/client/navigation.ts` 的 `recentEngagedSession`）。这条路径覆盖所有记不住自己的客户端：薄壳、清过存储的浏览器 profile、隐私窗口。

**壳记住确切的会话。** 随包的链接桥（`apps/linyaps/desktop-launcher/linglong/dsh-link-bridge.js`）轮询客户端的选择键，把变化以 `{ dshDesktop: true, type: "current-session", sessionId }` 上报，`null` 表示当前没有选中会话。`frontend/app.js` 转交 `App.RememberSession`，经字符集与长度校验后把 id 记进 `~/.config/dsh-desktop/config.json` 的 `lastSessionId`。下次 spawn 时，`internal/appenv/restore_session.go` 向 `--patch` overlay 插入一条插件行，把 id 作为 `config.sessionId` 带过去；插件激活时注册一条 `webserver/index-inject` 行，在服务端渲染的 `index.html` 里暴露 `__DSH_DESKTOP_RESTORE_SESSION__`。桥在客户端 module 入口执行前把该值写回选择键，且只在客户端自己没有选择时写。

## 这条事实由谁持有

会话选择是 UI 状态，因此只有 GUI 能真正观察它：壳是从页面学到它，而不是在服务端推导它，这样「用户从未打开过的会话」不会被误当成上次那个。壳提供的是持久化，不是第二个判断：它只记录页面最近上报的内容并回灌给页面，所有「要打开哪个会话」的决定仍然由客户端做出。回退之所以留在客户端也是同一个道理——它是客户端「没有记忆」时自己的策略，而不是壳的规矩。

## Alternatives considered

**把嵌入方式改对，让存储能持久化。** 让 harness 文档成为顶层文档就能恢复一方存储（2026 年 8 月之前的壳就是这样，它的按端口数据库至今还在盘上），但壳的标题栏、舞台、状态栏、引导页、弹框与失败页都是活在那份文档里的 HTML，得改成原生 chrome，或注入到一份壳并不拥有的页面里。把 GUI 代理到壳自己的 origin 能保住 UI，但必须自己承载 HTTP、WebSocket 升级与 SSE。Storage Access API 是改动最小的变体，输在 WebKitGTK 行为未经验证、且需要用户手势。

**只做客户端。** 最近带内容的会话未必就是用户打开的那个：另一个客户端或 CLI 驱动的会话可能更新。但它仍然必须存在，因为它是唯一在桥缺席时（开发态、外部 harness、旧版本包）也生效的路径。

**只做壳。** 桥缺席的地方它就失效，也帮不了普通浏览器，因此同样不能取代回退。

**用 iframe URL 传 id。** harness 对 `?token=` 的响应是 303 到 `./`，查询串会被丢掉，参数永远到不了页面。`window.name` 能跨过这次重定向，但它是无类型侧信道，壳还得在每次重载时维护它。

**把选择持久化到 Host。** 一个持久化的 Host 设置会变成所有客户端可见，还需要多客户端冲突规则；而且壳里仍在的草稿、布局与快捷键依旧是易失的。

## Consequences

- 薄壳能恢复它上次显示的那段对话，即使它的存储从不落盘；普通浏览器与 Electron 客户端继续使用它们自己那份权威记录，因为注入永不覆盖已有选择。
- 壳因此依赖一处客户端私有实现细节——键 `dsh.sessions.current` 及其 `sessionId` 成员。一旦改名，就会静默退化为客户端回退；桥的注释与启动器 README 都点明了这条耦合。
- 其他客户端状态（草稿、布局、侧栏、快捷键）在内嵌 WebView 里仍然易失，只有会话被恢复。
- 回退改变了所有记不住自己的客户端的启动表现：全新的浏览器 profile 会打开该工作区最近的对话，而不是空白会话。
- 会话 id 来自页面这条 wire 边界，因此在落盘前、以及写进 overlay 前都要经过字符集与长度校验。
- 验证：`workspaces-service.client.spec.ts` 覆盖回退的三种结果，`session_memory_test.go` 覆盖校验与往返，`test-link-bridge.cjs` 覆盖恢复、不覆盖、上报与被拒存储，`restore_session.test.mjs` 覆盖注入行与插件的「不 import」约束。打包态的端到端验证仍需重建玲珑包。

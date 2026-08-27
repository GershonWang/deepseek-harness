# Agent Note: 桌面启动器随包携带真实的 xdg-open

Status: implemented

[English](2026-08-27-bundle-xdg-open-for-host-browser-opening.md) | 中文

## Problem

从 dsh-desktop 应用打开外部链接毫无反应。两层故障叠加：Wails WebKitGTK 从不创建新的浏览上下文，`target="_blank"` 锚点的点击在到达任何打开器之前就被吞掉；而启动器唯一的打开器（`BrowserOpenURL` → `pkg/browser` → `xdg-open`）在玲珑容器里也无法工作——容器的 `/bin/xdg-open` 是个 75 字节的 `systemd-run --user` 转发壳，实测递归后立即失败。因此「关于」弹框的仓库链接（`target="_blank"`）点了没反应，任何未来把 URL 交给启动器的转交也会在同一打开步骤失败。容器内既没有浏览器二进制、也没有任何 https 默认处理程序，本地打开在设计中就不可行；必须经会话总线上的宿主 portal（`org.freedesktop.portal.Desktop`，容器内可达，后端为 `…impl.portal.desktop.dde`）到达宿主默认浏览器。

## Decision

走现有打包管线随包携带真实 xdg-utils，而不是手写 D-Bus：`buildext.apt.depends` 增加 `xdg-utils`，其合并把可用的 `xdg-open` 放进 `${PREFIX}/bin`（容器 PATH 首位，盖过基础运行时那个坏壳）。所有转交最终都经 Wails 运行时 `BrowserOpenURL`（无 `window.runtime` 的预览模式保持原生 `target="_blank"`），在 `XDG_CURRENT_DESKTOP=DDE` 下经宿主 portal 路由到本机默认浏览器。附带收益：容器内其他 xdg-open 消费方——harness 宿主侧的 `host.openPath`/`settings.openDocument`/`dsh web --open`——同样能解析到真实二进制。`tools.yaml` 登记 `xdg-open`（无 `verify` 命令：任何安全调用都无意义、真实调用会打开浏览器），`test-verify-tools.sh` 的齐全产物树夹具补上该二进制，`verify-tools.sh` 像对 git 一样门禁合并产物树。

启动器观察不到跨源 GUI iframe 内的点击，因此给打包后的 GUI 注入桥接脚本：`prepare-offline.sh` 在 `dsh deploy` 后运行 `linglong/inject-link-bridge.sh`，把 `linglong/dsh-link-bridge.js` 复制进打包的 `dsh-web-frontend/dist/assets/`（与页面同源，由 frontend-static 直接服务）并在 `dist/index.html` 追加其脚本标签（幂等）。桥仅在 `window.parent !== window` 时生效，拦截每个 `target="_blank"` 的 HTTP(S) 锚点主键点击（尊重 `defaultPrevented` 与 `download`），向 `window.parent` 投递 `{ dshDesktop: true, type: 'open-external', url }`。`frontend/app.js` 仅当消息来源是 harness 帧的 `contentWindow` 且 URL 为 http(s) 时接受该消息并打开。这只覆盖容器模式；外部 harness（别处运行的 `dsh web`）服务的是未注入的 GUI，其链接仍无反应。「关于」弹框的仓库链接走同一通道。

## Alternatives considered

**从 Go 直接调 portal（godbus 调 `org.freedesktop.portal.Desktop.OpenURI`）。** 确定性最强、不依赖 xdg-utils 的行为，但引入 Go 代码与测试面；打包层解法是用户更倾向的路线。两者并不互斥：若随包 xdg-open 实测无法经 portal 路由，Go portal 直连仍是后备设计。

**修基础运行时的转发壳。** 基础层属于 org.deepin.base，应用无权改动——这正是把真实 xdg-utils 合并进 `${PREFIX}` 的原因。

**包里携带浏览器二进制。** 没有必要——目标是宿主默认浏览器——且体积过大。

## Consequences

重建后的 `.uab` 能在本机默认浏览器中打开外部链接——「关于」弹框的仓库链接与打包 GUI 内所有 `target="_blank"` 的 HTTP(S) 链接——也让容器内所有 xdg-open 消费方可解析。代价：包体小幅增大（xdg-utils 及其 apt 依赖），且本变更依赖 deepin fork 版 xdg-open 确实经宿主 portal 路由——若重建后的端到端测试失败，后备方案是 Go portal 直连。桥只覆盖容器模式：外部 harness 服务的是未注入的 GUI，其链接仍无反应；覆盖该模式需要客户端钩子或启动器侧反代，两者均未随包。

## Testing

对改动后的 `frontend/app.js` 与 `linglong/dsh-link-bridge.js` 跑 `node --check`；`sh apps/desktop-launcher/linglong/test-verify-tools.sh` 四条路径全过（齐全产物树夹具含 `bin/xdg-open`）；`linglong.yaml` 用仓库 yaml 库解析通过；`inject-link-bridge.sh` 对夹具 dist 连跑两次证明注入与幂等；jsdom 双窗口协议冒烟证明桥恰好捕获 https 转交（javascript:/mailto: 放行）、启动器恰好经 `BrowserOpenURL` 打开该 URL、伪造来源与缺标记被忽略、独立页面保持原生行为。完整验证在重建的 `.uab` 上确认了两个表面：关于弹框的仓库链接与打包 GUI 内的 `target="_blank"` 链接都会打开宿主默认浏览器。

## Related

同一打包管线先例：[玲珑打包桌面客户端的可移植性修复](../../implemented/bug-fix/2026-08-24-linglong-git-exec-path-and-pnpm-bundling.zh.md)。harness 侧 xdg-open 消费方：[工具调用在 OS 中打开文件](../../implemented/feature/2026-07-28-tool-call-file-open-in-os.zh.md) 与 [打开就绪的 Web UI](../../implemented/feature/2026-08-12-open-ready-web-ui.zh.md)。
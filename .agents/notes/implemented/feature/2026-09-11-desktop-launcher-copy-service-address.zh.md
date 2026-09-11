# Agent Note: Copying the harness service address from the server dialog

Status: implemented

[English](2026-09-11-desktop-launcher-copy-service-address.md) | 中文

## Problem

服务器弹框把 harness 地址——`http://127.0.0.1:<port>/?token=<secret>`——当作可选中文本展示，此外什么都没有。要把它送进浏览器就得拖选一长串 token，而少选一个字符换来的是一个拒绝加载、且不说原因的页面。那个地址是写给机器读的，不是给指针读的。

## Decision

地址行在值旁边带一个复制按钮，仅在 harness 运行中显示。它原样复制 `state.status.URL`——与该行显示的完全同一个字符串，token 包含在内——走 `window.runtime.ClipboardSetText`，也就是终端已经在用的那个 Wails 注入的 GTK 剪贴板。成功后按钮图标换成对勾、tooltip 改为 已复制，持续 1.5 秒；再次点击会重置该计时器，让最后一次点击保留完整的确认。

只有运行态提供该按钮，因为其他状态在这行放的是别的东西：启动中是 `harness 正在启动…`，失败时是 `LastExit`，停止后是 `harness.log` 路径。复制它们等于复制一条错误信息。

## Alternatives considered

**复制不带 token 的地址。** 表面上更安全——token 是持有者凭据，而剪贴板是共享面。否决：Web 服务会拒绝不带它的地址，于是这次复制交给用户的是一个做不到他唯一想要之事的值，他还得回到弹框手工拼出真正的 URL。token 本就已经以明文渲染在同一行里，复制它不泄露任何屏幕尚未展示的东西。

**改用 `navigator.clipboard.writeText`。** 标准轨道 API，且无需依赖 Wails 运行时绑定。否决：WebKit 容器内剪贴板权限不可靠——终端复制当初走 `ClipboardSetText` 正是这个原因；维护两条通道会把「这里能复制、那里不能」变成反复出现的缺陷。

**新增 toast 或短暂提示条。** 比按钮变状态更醒目。否决：launcher 没有 toast 设施，为一次复制引入一套——连同短暂文字带来的行高跳动——代价高于这份确认的价值，而用户的视线本来就在他刚点过的图标上。

**所有状态都显示该按钮。** 规则最简单，还省掉状态判断。否决：它会诱导用户复制 `LastExit` 或日志路径，那读起来像是成功复制了一样他并不想要的东西。

## Consequences

只有当可复制的地址存在时，复制入口才存在。剪贴板最终会持有一个持有者 token，而这是用户点击标着 复制服务地址 的按钮主动要求的；任何把这视为暴露的评审，都应当把它与「token 本就明文显示在同一个弹框里」一并权衡。确认依赖一个 1.5 秒的计时器而非任何持久状态，因此中途移开视线的用户会看到按钮恢复正常而什么都没留下——对复制而言可以接受，剪贴板本身就是那条记录。

## Testing

`node --test frontend/test-app.cjs` 覆盖运行态（按钮可见、点击把含 token 的完整地址经由剪贴板通道写出、按钮进入已复制态）与停止态（按钮保持隐藏）。剪贴板通道在测试的 Wails runtime 中被桩替换，并记录交出的确切文本。

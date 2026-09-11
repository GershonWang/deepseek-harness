# Agent Note: xterm.js 终端重写（桌面启动器）

Status: implemented

[English](2026-09-06-desktop-launcher-xterm-terminal.md) | 中文

## 问题

内置终端最初的实现是"输出屏幕 + 底部输入框"：按键只进入输入框，按回车才写入 PTY。由此产生两个用户可见缺陷：

1. **无实时回显。** bash 的回显来自 PTY（readline 机制）；打字时 PTY 收不到字符，屏幕上什么都看不到。退格、方向键改命令等行编辑渲染错乱——自研 ANSI→HTML 解析器不理解光标移动/行擦除序列；vim、top 等全屏交互程序完全不可用。
2. **右键复制粘贴不可用。** Wails 运行时在 INPUT/TEXTAREA 之外的区域屏蔽默认右键菜单（其 contextmenu 处理逻辑在目标既非可编辑元素、也不被选区覆盖时 preventDefault），而终端区域又没有注册任何自定义处理。

## 决策

启动器前端整体迁移到 xterm.js 5.5.0，本地 vendor 到 `frontend/vendor/`（UMD 构建，离线加载，不依赖 CDN）：

- **每会话一个 xterm 实例。** `term.onData` 把按键逐字符转发到 PTY，bash readline 实时回显，与真终端一致。控制字符（Ctrl+C=`\x03`、Tab=`\t`、方向键转义序列）由 xterm 按终端协议编码，前端不再逐个拦截。
- **多标签保留状态。** 每个会话持有一个 `.terminal-holder` 包装节点；`term.open()` 只执行一次，切换标签时整体搬运 DOM 节点（`content.replaceChildren(holder)`），滚动历史与光标状态跨切换保留。
- **右键智能操作**（经典终端约定）：有选区 → 经 `window.runtime.ClipboardSetText` 复制；无选区 → 读 `ClipboardGetText` 后写入 PTY。这两个 API 由 Wails v2.15 注入的前端运行时直接提供（Linux 实现走 GTK `gtk_clipboard_*`，玲珑容器内可用），因此**没有新增任何 Go 代码**。
- **快捷键。** Ctrl+Shift+C 复制、Ctrl+Shift+V 与 Shift+Insert 粘贴，经 `attachCustomKeyEventHandler` 拦截（返回 false 阻止 xterm 再把该键编码为输入）。字号缩放与标签快捷键走同一处拦截（[终端呈现](2026-09-12-desktop-launcher-terminal-presentation.zh.md)）。
- **尺寸同步。** fit 插件按真实字符单元格计算行列，替换原先的 `clientWidth/8` 估算；`term.onResize` → `TerminalResize`（SIGWINCH）保证全屏程序正确重绘。
- **主题。** 原 `--term-*` 16 色调色板静态映射进 xterm 的 `theme` 选项（xterm 不解析 CSS 变量）；画布之外的弹框在 CSS 里对照同一组色值，两套系统主题下卡片都是同一套配色（[终端呈现](2026-09-12-desktop-launcher-terminal-presentation.zh.md)）。

## 备选方案

**把输入框按键逐个即时转发。** 改动最小，但自研 ANSI 解析器依然不理解光标移动序列——行编辑错乱、交互程序不可用的根因仍在，等于给错误架构打补丁。

**在 Go 端新增剪贴板绑定方法。** Wails v2.15 已把 `ClipboardGetText/SetText` 注入 `window.runtime`；Go 端再包一层是对既有能力的重复建设。

## 影响

实时回显与右键复制粘贴已修复；vim/top/less 等交互程序可用。底部输入框、前端命令历史（bash 自带 ↑↓ 历史）与 `parseAnsiToHtml`/`ANSI_COLORS`（约删 200 行）已移除；Ctrl+C/D/L 与 Tab 不再需要前端特判。输出上限从"500KB 字符串截断"改为 xterm 原生 `scrollback: 5000` 回滚缓冲。本次变更同时修复了 `test-app.cjs` 的存量失败：工具市场元素（`#market-search` 等）加入 `bindUI` 后未同步进 DOM stub，此前 10 个用例全红，现在 10/10 通过。Go 端 `internal/terminal` 包（PTY 会话管理）零改动。

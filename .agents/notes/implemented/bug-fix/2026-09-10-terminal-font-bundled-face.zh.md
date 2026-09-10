# Agent Note: Deliver the terminal font from the bundled frontend

Status: implemented

[English](2026-09-10-terminal-font-bundled-face.md) | 中文

## Problem

终端字体样式调整（xterm 字体栈、字重、行高）在玲珑构建上交付后没有任何可见变化。字体栈里优先的族——Cascadia Code、JetBrains Mono、Menlo、Consolas——在玲珑容器和 Deepin 25 宿主上都不存在，渲染回退到 Noto Sans Mono，与之前的 DejaVu Sans Mono 外观几乎一样。系统里没有任何带 500 字重的等宽字体，`fontWeight: 500` 只会落到 Regular。`-webkit-font-smoothing: subpixel-antialiased` 在 WebKitGTK 上是死代码，字形平滑由 fontconfig 决定。另一处关键事实：容器的 fontconfig 只扫系统目录（`/usr/share/fonts`、`/usr/local/share/fonts`、`$XDG_DATA_HOME/fonts`），复制进应用包 `share/fonts` 的字体永远不会被看见。

## Decision

终端字体随 web 前端一起交付：`frontend/vendor/fonts/jetbrains-mono/` 内置 JetBrains Mono 2.304 的 Medium、Medium Italic、Bold、Bold Italic 与 OFL.txt，经 `go:embed all:frontend` 编入二进制，由 Wails asset server 提供。`styles.css` 为四个面声明 `@font-face`（500/700 × 常规/斜体），`createTerminalSession` 在构造 `Terminal` 前等待四个面的 `document.fonts.load`——与 2 秒超时竞速——保证 xterm 用最终字体测量字符单元格。字体栈删去永远缺失的 macOS/Windows 族，`fontWeight` 为 `500`，同时移除无效的 `-webkit-font-smoothing` 声明；`font-feature-settings: "liga" 0, "calt" 0` 保留：JetBrains Mono 的连字会合并字符并破坏终端等宽格对齐。

## Alternatives considered

**通过 `FONTCONFIG_FILE` 叠加配置向 fontconfig 注册字体。** launcher 将 fontconfig 指向随包配置，追加包内字体目录。否决：fontconfig 在 WebKit 自己的进程内初始化，其环境与沙箱继承不受应用控制；为一个 webview 的需求改变整个进程树的字体解析；还需要改 Go 代码与打包脚本。`@font-face` 三者都不需要，且浏览器预览模式下同样生效。

**不打包字体，只依赖系统回退。** 渲染停留在 Noto Sans Mono/DejaVu，没有 500 字重，用户报告的视觉问题依旧存在。

**启动时把字体装入 `$XDG_DATA_HOME/fonts`。** 应用写用户主目录属于超出范围的副作用，且会与用户自己的字体管理产生竞争。

## Consequences

二进制增大约 1.1 MB 的 TTF。终端字形按真实的 500/700 字重渲染 JetBrains Mono，不受宿主字体影响；字体加载失败或超时回退系统等宽字体，不阻塞终端创建，同时也让 DOM-stub 测试环境（没有 `document.fonts`）继续可用。字体只作用于页面：GTK 窗口框架不受影响，容器内 `fc-list` 也看不到它——可观察的验证对象是渲染出来的终端，而不是 fontconfig。

# Agent Note: The desktop launcher's server dialog presentation

Status: implemented

[English](2026-09-11-desktop-launcher-server-dialog-presentation.md) | 中文

## Problem

服务器弹框是 launcher 壳里最后一个没有按工具链弹框确立的语言重建的面（分区卡、分区小标题、状态语义色、带分隔线的操作行）。三个缺陷让它格外显眼。

「连接模式」行挂着 `mode-row` 类，而 `styles.css` 里没有任何规则匹配它，于是两个平台原生 radio 直接贴在一张已经成型的卡片上。状态行是纯文本、不带任何颜色，而同一个状态在底部状态栏里是有语义色点的。地址行的值从未拿到为它写的那条规则：`.row > span:last-child` 匹配不到一个后面还跟着复制按钮的值，因此 `flex: 1 1 auto` 与 `overflow-wrap: anywhere` 对它都不生效，那串长服务 token 之所以会折行，只是因为它碰巧含一个连字符。

状态栏有另一类问题：它把完整服务地址——`http://127.0.0.1:<port>/?token=<secret>`——当作常驻可见文本打印，于是每次截图或共享屏幕都交出了一个有效的持有者凭据。

## Decision

弹框复用既有组件词汇而不是新建一套：卡片小标题用 `.section-title`，值列用 `.row-value`，按钮上方分隔线用 `.actions-bar`，状态色沿用状态栏那一套。宽度改为 560 px，与工具链弹框一致——地址在任何宽度下都会折行，520 px 只是把它折得更碎。

连接模式改为分段控件。两个 radio 仍是唯一状态源——`app.js` 依旧读 `input[name="mode"]:checked`、依旧在 `input[name="mode"]` 上绑 `change`——每个 `label` 包住自己的 input 与一个负责外观的 `span`。选中态由 `input:checked + span` 这个兄弟选择器渲染，因此不对 webkit2gtk 容器的 `:has()` 支持做任何假设。

状态行沿用状态栏语义（运行=ok、启动中=warn、失败=danger、其余中性），状态点由 `::before` 里的 `currentColor` 画出，因此没有新增 DOM 节点，`test-app.cjs` 的 DOM 桩也不需要新增元素。地址行的等宽字体与底色框跟复制按钮同一条判据——运行态——因此 `harness 正在启动…` 与 `LastExit` 不会被装扮成可复制的字段。

状态栏只显示主机与端口（`127.0.0.1:3456`）。完整地址连 token 留在弹框内，弹框负责渲染它并支持一键复制（[复制入口](2026-09-11-desktop-launcher-copy-service-address.zh.md)）。

## Alternatives considered

**只修这两个缺陷。** 这是让弹框看起来仍像另一个应用的最小改动。否决：改造与修复落在同样的行、同样的元素上，拆开只是多换一个提交，并不换来更小的改动面。

**用 `:has(input:checked)` 渲染选中段。** 这是「包含选中 radio 的那个选项」最直白的表达。否决：容器里的 webkit2gtk 版本由打包固定，兄弟选择器能达到同样结果，且不依赖 `:has()` 是否可用。

**把状态点做成真正的 `.state-dot` 元素。** 可以逐字复用一个已有类。否决：为了一个纯装饰的点，要在 `index.html` 新增 id、并在 `test-app.cjs` 的元素清单里同步补一条；`currentColor` 伪元素用同一个颜色 token 画出同一个点。

**状态栏保留完整 URL。** 保留它不会损失什么。否决：窗口可见时状态栏就可见，而这个 URL 携带持有者凭据，无需任何人去复制，它就会从截图和共享屏幕里泄露。

**只去掉查询串、保留 URL 其余部分。** 比只显示主机端口更窄。否决：URL 里唯一一眼看不出信息量的就是 token，而用户看状态栏要读的正是 `host:port`。

## Consequences

弹框现在读起来与工具链弹框同属一个壳，地址行的布局也不再取决于 token 里是否含断行字符。状态栏不再提供一个可以选中并粘进浏览器的值；这条路径改为弹框的复制按钮，它交出的是 Web 服务真正接受的那个 URL。`hostLabel` 遇到无法解析的地址时返回空标签而不是回退到原串，因此畸形地址绕不开脱敏——代价是畸形地址在状态栏上不显示任何位置。

## Testing

`node --test frontend/test-app.cjs` 跑 22 例，其中三例覆盖本次改动：运行态与启动态下状态行的语义类；地址行的 `mono`/`code` 类跟随运行态；容器模式与外部模式下状态栏文本包含主机端口且不含 token。测试构建的 vm 沙箱现在会拿到 `URL`——缺了它 `hostLabel` 会走解析失败的兜底分支，脱敏断言就会因为错误的原因通过。

渲染未在浏览器中验证：本环境没有能跑起来的引擎（缓存里的 Playwright Chromium 缺 `libnspr4` 无法启动）。主题、间距、悬停与焦点表现仍需在开发态 `make build` 运行后核对，之后才谈打包。

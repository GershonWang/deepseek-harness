# Agent Note: Theme tokens in the desktop launcher shell

Status: implemented

[English](2026-09-11-desktop-launcher-theme-tokens.md) | 中文

## Problem

launcher 壳把调色板声明了两遍——`:root` 是深色，`@media (prefers-color-scheme: light)` 是浅色——但好几处绕过了它，而缺陷都落在浅色主题上。

诊断报告用内联 `style="color:#…"` 给摘要和检查图标上色，值是从深色调色板抄来的。浅色系统上报告呈现为白底上的浅绿浅黄：✓ 1.82:1、✗ 2.46:1、可修复计数 2.31:1、info 1.99:1、兜底 1.61:1。正文要求 4.5:1。

有两个变量被引用但从未定义。`--bg-titlebar` 供 `.icon-badge` 的 2px 描边使用，于是一直吃 `#222` 兜底值，在浅色标题栏上给徽标画出一个 12.75:1 的深色环。`--mono` 供 `.doctor-detail` 与 `.doctor-actions` 使用，于是一直回落到通用 `monospace`，从未用上随包的 JetBrains Mono。

三条规则写死 `rgba(204, 167, 0, …)`——即带透明度的深色 `--warn`。浅色主题下 `--warn` 是 `#9d6d00`，于是检测中的 spinner 环出现两种黄（它的顶段用 `var(--warn)`），自动诊断提示条也保留着亮黄棕的底色与描边。

还有一类问题光靠 token 化解决不了：实色语义底上的前景色是字面量（`--brand`、`--danger` 上用 `#fff`，`--warn` 上用 `#1a1a1a`），而背景随主题变化。一套前景色不可能同时满足两套主题，因为两套主题的实色底亮度不同。

## Decision

壳里所有颜色都来自 token，这一点可以用解析 `styles.css` 的方式反复验证：颜色字面量只出现在两个主题块里，外加 Consequences 中列明的两处有意保留。

当填充值不适合当文字色时，按角色拆分 token。`--danger-text` 加入既有的 `--brand-text`：深色给 `#f48771`（在 `--bg-panel` 上 6.24:1），浅色给 `var(--danger)`（白底 4.93:1）。壳里每一处 `color: var(--danger)` 都改为 `color: var(--danger-text)`，而 `border-color`、`background` 与状态点继续用 `--danger` 作填充色。

实色底与其前景色改用 `--on-brand`、`--on-danger`、`--on-warn`，由每套主题各自给出在自己填充色上达标的前景色：两套主题的主按钮都用白字，危险色都用白字，`--warn` 上深色用 `#1a1a1a`、浅色用白字（浅色的 `--warn` 足够深，深色文字只有 3.83:1，白字达到 4.54:1）。

`:root` 声明 `color-scheme: light dark`，浏览器自己绘制的东西——滚动条、原生控件、默认画布——因此跟随与调色板相同的偏好。`--mono` 在 `:root` 定义一次，供三处等宽界面使用。`.icon-badge` 改用 `var(--bg-toolbar)`，那本来就是它描边想要对齐的颜色。三处带透明度的 warn 色调改为 `color-mix(in srgb, var(--warn) N%, transparent)`，即工具链弹框已经在用的写法。

诊断报告改为输出语义类（`sev-ok`、`sev-error`、`sev-warn`、`sev-info`、`sev-muted`）而不是内联颜色；`renderDoctorReport` 把检查项的 `OK` 与 `Severity` 映射成其中一个类名。

删除了 25 个 `--term-*` 变量，其中只有 `--term-bg` 有读取者。16 色终端调色板存放在 `app.js` 的 `TERMINAL_THEME`，因为 xterm 的 `theme` 选项接受具体色值而不是 CSS 变量，而且那套配色本就不跟随壳的主题。

## Alternatives considered

**保留内联的深色值，再为浅色补一套放在媒体查询里。** 内联样式无法被 `prefers-color-scheme` 切换。否决：那需要 JavaScript 读取配色偏好并在变化时重写 DOM，为的却是 CSS 本已负责的颜色。

**继续用 `--danger` 作为所有危险文字色，接受深色下 4.29:1。** 改动最小。否决：这是回退而不是修复——这些文字原本是 6.24:1，只因为该 token 恰好是红色就把它们改道经过一个填充 token，等于用深色缺陷换掉浅色缺陷。

**为对称于 `--danger-text` 再补 `--ok-text` 与 `--warn-text`。** 否决：`--ok` 与 `--warn` 在两套主题下作为文字本来就达标（深色 8.40:1 与 6.63:1，浅色 5.37:1 与 4.54:1），这些 token 只会是毫无收益的别名。这个不对称是测量结果，不是疏漏。

**运行时用 `getComputedStyle` 从 CSS 变量读取终端调色板。** 可以让 16 色只有一个来源。否决：壳的调色板与终端的调色板彼此独立——终端无论系统主题如何都保持深色——因此这个查询只会为两个本不需要同步的东西引入运行时依赖与重读路径。

**把深色的 `--brand` 调暗，让主按钮上的白字达到 4.5:1。** 该填充色是 `#4d6bfe`，只有 4.33:1。本次否决：它改的是品牌填充色本身，那属于产品识别决策而不是 token 化决策。[工具链市场的呈现](../feature/2026-09-12-desktop-launcher-toolchain-market-presentation.zh.md)后来接下了这个决定（见 Consequences）。

**把模态遮罩也 token 化。** 否决：45% 的黑色遮罩在浅色与深色内容上都是惯例，也没有要它随主题变化的诉求。

## Consequences

按各颜色实际所处的界面实测，改动前后如下：

| 元素 | 浅色 改前 | 浅色 改后 | 深色 改前 | 深色 改后 |
|---|---|---|---|---|
| ✓ / `sev-ok` | 1.82:1 | 5.37:1 | 8.40:1 | 8.40:1 |
| ✗ / `sev-error` | 2.46:1 | 4.93:1 | 6.24:1 | 6.24:1 |
| 可修复 / `sev-warn` | 2.31:1 | 4.54:1 | 6.63:1 | 6.63:1 |
| info / `sev-info` | 1.99:1 | 7.26:1 | 7.71:1 | 4.96:1 |
| 兜底 / `sev-muted` | 1.61:1 | 5.10:1 | 9.54:1 | 5.65:1 |
| 其它危险文字 | 4.93:1 | 4.93:1 | 4.29:1 | 6.24:1 |

徽标那个非预期的深色环在两套主题下都消失了（描边现在与标题栏同色）。spinner 环在两套主题下都是同一种颜色。诊断面板用上了随包的 JetBrains Mono。

仍有一处已知缺口，记录在此而不是就地修掉：深色主题关闭键 hover 实测 3.57:1，高于纯图标控件适用的 3:1 阈值。这里原先并列记着的另一处缺口——深色主题主按钮上的白字 4.33:1，低于 4.5:1 的文字阈值——已由[工具链市场的呈现](../feature/2026-09-12-desktop-launcher-toolchain-market-presentation.zh.md)关闭：深色 `--brand` 现在落在 `#4763f0`，白字 4.87:1，同时按钮相对卡片底保持 3.14:1。

主题块之外还剩两处颜色字面量，均为有意保留：模态遮罩 `rgba(0, 0, 0, 0.45)`，以及终端界面的 `--term-bg: #1a1a1a`。

## Testing

`node --test frontend/test-app.cjs` 跑 24 例。新增用例通过 Wails 桩驱动一份诊断报告，断言摘要与检查清单带语义类、且不含内联 `color` 声明——这条断言在改动前的代码上必然失败，因为摘要当时带着 `style="color:#89d185"`。

对比度是按调色板取值计算的，不是观测所得：本环境没有能启动的浏览器引擎（缓存里的 Playwright Chromium 缺 `libnspr4`），因此渲染结果仍需在开发态 `make build` 运行、两套主题各看一遍，之后才谈打包。

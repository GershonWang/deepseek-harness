# Agent Note: Container fonts and fontconfig injection for the desktop launcher

Status: implemented

[English](2026-09-13-desktop-launcher-container-fonts.md) | 中文

## Problem

打包态桌面客户端里，`6分32秒`、`17小时53分` 这类时间文本会出现数字与汉字互相挤压，而同一页面在 Chrome 中渲染正常。缺陷只在 WebKitGTK 下复现：玲珑容器内 CSS 字体栈的全部候选族都缺失，栈退化到一个同时覆盖拉丁与 CJK 的族（思源黑体），同一行内两种文字因而共用一套度量，字形相互挤压。

只靠打包侧注册字体与别名修不好这一点。宿主用户级 fontconfig 文件（`~/.config/fontconfig/conf.d/99-deepin.conf`）以 `prepend` 加 `binding="strong"` 把 `sans-serif` 改指思源黑体，其优先级高于任何别名 `<prefer>`，而 CSS 栈尾正是 `sans-serif`。

## Decision

随包把 Noto Sans Display（拉丁）与 JetBrains Mono（等宽，补齐 Regular 字重）装进 `${PREFIX}/share/dsh-fonts`。层内的 `usr/` 与 `etc/` 都不进容器命名空间，`usr/share/fonts` 另被宿主目录整体挂载遮蔽，因此常规字体目录不可用。

在 WebKit 初始化之前，启动器把 `FONTCONFIG_FILE` 指向 `etc/fonts/dsh-fonts.conf`，该文件由 `linglong/install-container-fonts.sh` 生成。这份配置 include 系统配置、显式声明可写的 `<cachedir>`（容器内 `/var/cache/fontconfig` 只读）、注册随包字体目录，并把 CSS 栈用到的族名别名到随包字体。`internal/packaging/fontconfig.go` 决定是否注入该变量，并在 `wails.Run` 之前调用——GTK 与 WebKit 在首次初始化时读取 `FONTCONFIG_FILE`；未打包运行时保持不设置，而不是把它指向一个相对路径。

CSS 字体栈是修复的一部分，不是它的备选。`packages/client/ui-theme/src/styles/base.css` 把 `'Noto Sans Display'` 与 `'WenQuanYi Micro Hei'` 前置到 `--dsw-font-family`，把 `'JetBrains Mono'` 前置到 `--ds-font-family-code`。`--dsw-font-family` 是全部 `--dsw-font-*` 排版 token 的基础族，33 个组件文件消费这些 token，时间文本用的 `--dsw-font-xs-13` 即由其派生。

随包配置下实测的别名覆盖范围：`sans-serif`、`BlinkMacSystemFont`、`PingFang SC`、`Hiragino Sans GB`、`Microsoft YaHei`、`Helvetica Neue`、`SF Mono`、`Fira Code`、`Menlo`、`Consolas` 都能解析到随包字体。`-apple-system` 是 fontconfig 内建兜底，任何别名都改不动（`fc-match` 返回宿主默认）。`monospace` 被系统配置压住，而 CSS 等宽栈不含裸 `monospace`，因此没有为它声明别名。

## Alternatives considered

**只在打包侧注册字体与别名，不动 CSS 字体栈。** 宿主用户级配置以 `prepend` 加 `binding="strong"` 改指 `sans-serif`，优先级高于别名；在当前宿主上实测 `-apple-system` 与 `sans-serif` 均落到思源黑体，挤压照旧。否决。

**把字体装到 `usr/share/fonts`。** 层内 `usr/` 不进容器命名空间，且 `usr/share/fonts` 被宿主目录整体挂载覆盖，随包字体永远不会被读取。否决。

**中文族取自 apt 依赖 `fonts-wqy-microhei`。** 已导出的产物层与基座层里完全没有 wqy 或微米黑实体，实际渲染的中文回退来自宿主挂载；该依赖既进不了包，也改变不了运行时。这一不符作为未修项单独跟踪。不作为机制。

## Consequences

宿主已装同名字体（思源黑体、微软雅黑）时，fontconfig 按家族名匹配会选中宿主那份，随包字体不被选中。该限制只影响本就装有该字体的机器的观感，不影响缺少该字体的机器。

`fc-match` 报告的是 fontconfig 的解析结果，而 WebKit 由自身引擎逻辑决定实际渲染字体，两者可能不同。这一缺口已人工闭环：重新打包 `0.1.2.7` 并安装后，时间文本不再挤压。

随包配置与容器内实际生效那份逐行 diff，差异只有删掉的 `monospace` 一行。用两份配置对 CSS 栈的 17 个候选族名逐条复跑 `fc-match`，结果完全一致（`sans-serif` 命中思源黑体，`BlinkMacSystemFont` 与 `PingFang SC` 命中 `NotoSansDisplay-Regular.ttf`，等宽族命中 `JetBrainsMono-*.ttf`）。

## Testing

`go test ./internal/packaging/` 覆盖 `FONTCONFIG_FILE` 的判定，含「未打包前缀必须保持不设置」。`sh -n` 检查生成脚本的语法。ui-theme 套件 81 个用例通过，另有 1 个与本次无关的既有失败。

## Related

[Deliver the terminal font from the bundled frontend](2026-09-10-terminal-font-bundled-face.zh.md) 为终端字体否决了 `FONTCONFIG_FILE` 叠加层，改为用 `@font-face` 随前端交付 JetBrains Mono。该决策仍然成立且是页面级的；本条正是同一个叠加层成为机制的场景——CSS 字体栈要为所有组件解析族名，单靠 `@font-face` 覆盖不到。两条 Note 部分重叠，均保持活动。

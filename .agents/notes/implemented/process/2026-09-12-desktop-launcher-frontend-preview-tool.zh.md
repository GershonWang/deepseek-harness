# Agent Note: Layout verification for the desktop launcher frontend

Status: implemented

[English](2026-09-12-desktop-launcher-frontend-preview-tool.md) | 中文

## Problem

启动器前端唯一的自动化测试 `frontend/test-app.cjs` 用手写的 DOM 桩跑 `app.js`。那个桩既不实现样式表也不实现布局，于是整整一类缺陷对它不可见，而这类缺陷一直在来：服务器弹框在切换连接模式时改变了约 125 px 的高度；地址行的 flex 规则因为选择器根本匹配不到而静默失效；后来又出现「已连接」提示把卡片撑高 32 px、异常高的叠放把地址输入框撑到 174 px。后两个是靠真实浏览器量出来的；而它们全都从绿灯的测试里走了过去。

## Decision

`apps/desktop-launcher/frontend/tools/preview.mjs` 把 `index.html` 放进无头 Chromium 渲染并测量。它是一个零依赖的普通 Node ESM 脚本：浏览器依次取自 `DSH_PREVIEW_BROWSER`、机器上已有的 Playwright 缓存、`PATH`，协议走 Node 内置的 `WebSocket`。Chromium 的 profile 与 `HOME`/XDG 目录被重定向到 `apps/desktop-launcher/.preview-cache` 下每次运行新建的目录——不这样做，它在文件沙箱下会因为 HOME 只读而卡住。该根目录必须留在 `frontend/` 之外：`//go:embed all:frontend` 不看 `.gitignore` 就把整个前端目录嵌进二进制，浏览器缓存写在那里面会因 Go 拒绝的嵌入文件名而让启动器构建失败。

预览页在运行时从 `index.html` 生成——剥掉脚本、改写样式路径——因此不可能与被验证的页面漂移。状态放在 `FIXTURES` 表里，一个弹框状态一条，每条写明 `app.js` 会写入的文本、类名、值、可见性与禁用标志；新增状态只是改数据。一条状态一条记录，因此夹具表就是覆盖范围。

`verify` 断言 DOM 桩看不见的几何：所有常规状态在 1 px 容差内保持同一卡片高度、两张面板始终等高、地址框保持两行预留、服务地址输入框不超过封顶。`measure` 把同一份几何按 JSON 打印，`render` 按主题与状态各写一张截图到被忽略的 `frontend/.preview`。`lefthook.yml` 在 pre-push 上以 `apps/desktop-launcher/frontend/**` 为 glob 跑 `verify`。

## Alternatives considered

**改用 Playwright 驱动**，`apps/web` 本来就依赖它。否决：`apps/desktop-launcher` 没有 `package.json`，脚本要么住进一个它不属于的包，要么为一个开发者专用工具新增依赖、改锁文件、重生成第三方声明。反正用的都是 Playwright 已经装好的那个浏览器缓存。

**截图基线比对。** 视觉回归的常见形态。否决：基线对字体、设备像素比、渲染器版本都敏感，于是每次有意的改动都变成一次基线更新，评审问题从「这条不变量是否被打破」降级成「这张 PNG 变了没有」。这里值得守住的性质是几何的，可以直接用数字断言。

**从 Go 侧用真实 webkit2gtk 引擎渲染。** 最接近生产。否决：每次运行都要 X 或 Wayland 加 CGO，等于把一次布局检查挡在它本该所在的 pre-push 路径之外。

**把 DOM 桩补足到能抓到这类问题。** 否决：那断言的是我们自己桩里的算术，而不是浏览器的布局；flex、grid、行高的每条规则都得重新实现才谈得上可信。

**放进 CI 而不是 pre-push。** 目前否决：CI runner 上没有浏览器，这么做等于新增一条要安装浏览器的 lane——比一个本地钩子重得多，而且更适合等这些不变量在本地先证明自己之后再谈。

## Consequences

触及启动器前端的推送现在要在钩子里多花约三秒（实测 2.44 s）；没碰到前端的推送会被 glob 跳过。在没有浏览器的机器上，工具会说明原因并成功退出，因此它不会堵住推送，代价是在那种机器上静默地不提供任何信号。

工具只测它夹具表里的状态。新弹框、或既有弹框的新状态，在被登记之前都是没被测到的——那张表就是覆盖范围，而没有任何机制强制新状态必须登记。截图写进被忽略的目录，因此它们是可以拿来端详的产物，而不是可评审的基线。

## Testing

`node frontend/tools/preview.mjs verify` 在当前提交的前端上通过，并打印两套主题下全部七个状态的几何：常规状态卡片 307/308，失败态带安全模式块 398，地址框 41，服务地址输入框 66，叠放异常高时 116。

这条门禁被证明会拒绝无效情形：拿掉 `.ext-state` 的预留行后，服务地址输入框在两个状态之间变化 18 px，命令以退出码 1 结束并点名该测量值。`node --test frontend/test-app.cjs`（37 例）不受影响，因为工具读的是 `index.html` 而不是那个桩。

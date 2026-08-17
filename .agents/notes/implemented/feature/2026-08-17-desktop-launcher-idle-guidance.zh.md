# Agent Note: 桌面启动器的空闲引导页

Status: implemented

[English](2026-08-17-desktop-launcher-idle-guidance.md) | 中文

## Problem

启动器窗口只在容器内 harness 就绪后才打开,并导航到其 Web GUI。当 harness 停止——手动点击「停止」或崩溃——webview 停留在最后加载的页面:服务已不在,请求持续失败,用户面对一个失效页面却没有任何如何恢复的提示。完全没有可用服务时情况相同,因为窗口只会在容器内 harness 已运行时打开。启动器在没有任何服务可用时,webview 没有任何有用的呈现状态。

## Decision

启动器自带一个自包含的引导页(`guidance.go`),以 `data:` URL 渲染的静态中文 HTML 文档,在所有服务不可用时显示。`resolveTarget` 把期望的 webview 目标归约为三种之一——`connector.Mode()` 为 `ModeExternal` 时是外部 URL,`HarnessState` 为 `StateRunning` 时是容器 URL,其余情况是引导页。1 秒状态轮询(`dshRefreshStatus`,运行在 GTK 主线程)每个 tick 解析目标,且仅当与 `webviewTarget`(记录最后加载目标的包级变量)不同时才调用 `navigateFn`;显式导航(探测成功、断开后的空闲交接)同样记录 `webviewTarget`,因此 tick 从不对同一页面重复导航。`openWindow` 在 `installDesktopUI` 执行首次刷新前就把就绪 URL 记为初始 `webviewTarget`,保证首次加载只发生一次。

引导页文案与弹框使用同一套词汇(服务器、容器内、本机/远端服务、启动、连接),并在 harness 停止与启动两种情形下都读得通——重启退避期间页面会短暂出现,所以措辞也覆盖「服务启动中」。

## Alternatives considered

**`about:blank`。** 对失效页面没有任何交代,也不提供下一步;引导页的存在是为了说明该做什么,而不是显示空白。

**随包分发 `file://` 页面。** 安装前缀只在运行时才知道(`/opt/apps/<id>/files/...`),且启动器的 CSS 已避免依赖打包资源路径。`data:` URL 让页面与文件系统和安装位置无关,且文档是静态的,不存在 origin、CSP 或更新问题。

**复用 harness Web GUI 自身的"已断开"状态。** GUI 只在服务运行时存在;空闲的启动器没有服务可供其渲染。引导必须独立于页面本身所依赖的入口循环。

## Consequences

窗口在所有服务不可用时现在显示一个有意义的引导状态,代价是每次停止/启动循环多一次页面加载(几 KB 内联 HTML),且引导内容无法展示实时状态——这部分仍由状态栏与服务器弹框承担。自动重启退避期间引导页会显示一个退避间隔,其措辞覆盖「服务启动中」,因此这一闪现读起来像有意为之,而不是页面损坏。
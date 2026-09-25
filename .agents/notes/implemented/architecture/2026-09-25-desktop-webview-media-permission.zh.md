# Agent Note: 桌面版内嵌 WebView 的媒体采集权限

Status: implemented

[English](2026-09-25-desktop-webview-media-permission.md) | 中文

## Problem

Linux 桌面版的语音输入无法录音，而同一个 Web GUI 在浏览器里录制正常。语音输入客户端把 `NotAllowedError` 映射成权限提示，于是这个失败看起来像是没授权。实际上这条路径上叠着三个彼此独立的缺陷；由于三者最终都落到同一条用户可见提示上，只修其中任何一个，可观察到的现象都不会有任何变化。

## Decision

三处都在启动器里修，各自修在拥有它的那一层。

壳把 harness UI 嵌在跨源 iframe 里，而 `microphone` 的 permissions policy 默认白名单是 `self`。缺少 `allow="microphone"` 时，请求在 WebKit 发出权限信号之前就被拒绝，因此这个缺陷会把另外两个遮住；`frontend/index.html` 的 iframe 现在带 `allow="fullscreen; microphone"`。

Wails v2 的 Linux 后端从不连接 `WebKitWebView::permission-request`，而 WebKitGTK 对该信号的默认处理是拒绝。`WebKitWebView*` 位于 `internal/` 包内，于是 `internal/webviewperm` 改用 `gtk_window_list_toplevels()` 遍历进程的顶层窗口，自行连接该信号。`OnDomReady` 提供了 GTK 主线程，因此遍历无需切换线程；遍历写成递归而非固定层级，这样 Wails 日后调整布局时只会退化成一条日志，而不会连到错误的控件上。放行判定是对 C 侧提取出的事实做纯 Go 计算，这使它脱离显示环境也能单测，同时把 C 侧限制在「读事实、调 allow 或 deny」上。只放行音频采集；摄像头、屏幕共享与非媒体请求一律拒绝，因为没有内嵌功能需要它们。

设备枚举发生在 WebKit 的 WebProcess 里，它继承启动器的环境。包内 GStreamer 插件位于 `<PREFIX>/lib/x86_64-linux-gnu/gstreamer-1.0`，而容器内的插件目录只有 `coreelements` 与 `coretracers`；没有搜索路径时 `appsink` 加载失败、枚举到 0 个音频输入，请求转而以 `OverconstrainedError` 失败。`packaging.ConfigureGStreamerPlugins()` 在 `wails.Run` 之前设置附加语义的 `GST_PLUGIN_PATH`；开发态插件本就在标准路径上，此时不做任何事。

## Alternatives considered

**给 Wails 打补丁并用 fork 分发。** 在 Wails 现有的 `load-changed` 处理旁边接上 `permission-request` 才是长期正解，但它要等上游评审与发版。把它留作独立的后续事项，可以让启动器先基于未打补丁的模块发出去。

**用 `LD_PRELOAD` 拦截 `webkit_web_view_new`。** 这能拿到同一个视图且不必改启动器的 Go 代码，但它依赖 Wails 的创建顺序，而该顺序一旦变化，编译期不会给出任何信号。

**把 `NotAllowedError` 当成枚举成功的证据。** permissions policy 的拒绝与 WebKit 的默认拒绝都会给出这个名字，而前者先发生；用它反推设备可用性，正是 GStreamer 缺陷被掩盖的原因。要区分两者，必须观察权限信号究竟有没有触发。

## Consequences

启动器因此接受了对 GTK 与 WebKit 的 CGO 依赖，`webviewperm` 成为模块里唯一的非纯 Go 包；但它的判定逻辑留在 Go 里，`policy.go` 在没有 GTK 的环境中依旧可测。启动流程从不依赖遍历成功——找不到视图时只记一行日志，壳的行为与改动前一致。验证必须在真实窗口上进行，因为遍历针对的就是 Wails 自己的控件树：本次记录的证据来自一个用启动器模块构建的一次性 Wails 探针（在打包态环境下运行），以及直接读取运行中 `WebKitWebProcess` 的环境变量，以确认发行版实际导出了哪些变量。该容器里真正采集音频还需要换一套完全不同的录制实现，见[语音输入改用 AudioWorklet 采集](2026-09-25-voice-input-audioworklet-capture.zh.md)。

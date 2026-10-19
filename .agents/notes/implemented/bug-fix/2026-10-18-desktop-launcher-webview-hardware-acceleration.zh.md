# Agent Note: 桌面启动器内嵌 WebView 的硬件加速策略

Status: implemented

[English](2026-10-18-desktop-launcher-webview-hardware-acceleration.md) | 中文

## Problem

打包客户端的页面没有 GPU 加速渲染：插件设置这类界面稍重的页面卡成幻灯片、CPU 占用很高，而把同一个 harness 地址放到系统浏览器里打开却很流畅。缺陷不在 harness、不在玲珑沙箱、也不在机器：WebKitGTK 自带的测试浏览器，从同一个容器加载同一个地址，能拿到硬件合成缓冲并使用显示 GPU。

launcher 从来没有给 Wails 传过 Linux 选项。Wails v2.15.0 在 `options.Linux` 为 nil 时一律回退到 `WebviewGpuPolicyNever`（对 wails#2977 的规避），其 Linux 后端再把它变成 `WEBKIT_HARDWARE_ACCELERATION_POLICY_NEVER`。落到这条策略上，WebKitGTK 根本不进加速合成：页面由 CPU 绘制，UI 进程从不打开 DRM 渲染节点，两类合成缓冲都不出现。现场所有观测都与这个状态吻合，也解释了[渲染后端那份笔记](2026-10-18-desktop-launcher-webkit-render-backend-selection.zh.md)里的工作为何救不回快路径——策略位于渲染器选择的上游，因此在运行中的客户端上设 `WEBKIT_FORCE_DMABUF_RENDERER=1` 毫无变化。

## Decision

`main` 把 `app.WailsLinuxOptions()` 作为 `options.App.Linux` 传给 Wails，`internal/app/window.go` 返回非 nil 的 `linux.Options`，其 `WebviewGpuPolicy` 选择 WebKitGTK 的按需加速。

取值是数值 1，而不是 Wails 文档暗示的那个常量名。Wails 的 Go 枚举次序是 `OnDemand=0`、`Always=1`、`Never=2`，而 Linux 的 C switch 是 `0 → ALWAYS`、`1 → ON_DEMAND`、`2 → NEVER`：传名为 `OnDemand` 的常量实际请求的是「始终开启」，只有数值槽位 `1` 才请求按需。`window.go` 把这个槽位写成带注释的常量，让意图不依赖于一个会误导的名字，并由 `window_test.go` 固定这个数值。

「主动关掉加速」仍然交给 `DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1`（`packaging.ConfigureWebKitRendering`），它按机器生效，不必发版。

## Alternatives considered

**按名字传 `linux.WebviewGpuPolicyAlways`。** 作为书写形式否决：它的数值恰好能到达按需，今天也能工作，但名字表达的与效果相反，日后极易被「修正」成 `OnDemand`，从而把机器无声切到无条件加速。带注释的数值常量加一条用例才能表达真实意图。

**按名字传 `linux.WebviewGpuPolicyOnDemand`。** 否决：在这个 Wails 版本里该名字映射到无条件加速，不是这里要的语义。

**升级到枚举与 C switch 一致的 Wails 版本。** 推迟而非否决：那是这处错位的最终解法，但属于独立的依赖变更、有自己的验证面；固定数值并让用例失败，已经能让错位在升级时立刻暴露。

**保持 Options 为 nil，继续靠现有 `DSH_DESKTOP_*` 开关。** 依据实测否决：那些开关作用于渲染器传输层，而 never 策略在到达那一层之前就把加速合成拿掉了；用 `WEBKIT_FORCE_DMABUF_RENDERER=1` 启动的客户端依旧没有合成缓冲、CPU 也没有改善。

**要求无条件加速。** 暂时否决：对不需要合成的页面白耗电，而按需本来就是 WebKitGTK 的默认。若真实窗口验证表明按需仍不足以合成那些重页面，切换这个槽位只是一行改动。

## Consequences

内嵌页面重新拿到 WebKitGTK 的按需加速合成；在混合显卡机器上，这意味着合成落在显示 GPU 而不是独显，与渲染后端那份笔记记录的渲染节点探针一致。

策略取值依赖 Wails 的这处枚举错位。将来修好映射的 Wails 升级会让 `TestWailsLinuxOptionsEnablesWebviewAcceleration` 失败，而不是无声改变策略；用例的报错信息点名了要重新核对的两处。

开启加速也重新暴露了此前被关闭的加速合成所掩盖的渲染风险：过去在 CPU 上绘制的机器，现在可能显现当初促成[闪烁修复](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.zh.md)的遮挡闪烁或合成花屏。那份笔记记录的渲染节点回退依然生效——覆盖所有存在 NVIDIA 渲染节点的机器，而不只是 NVIDIA-only——`DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1` 仍是按机器的逃生舱；NVIDIA-only 机器上的真实窗口表现仍未观测。

## Testing

`TestWailsLinuxOptionsEnablesWebviewAcceleration` 固定本决策依赖的两个事实：选项非 nil，且策略槽位是能到达 `WEBKIT_HARDWARE_ACCELERATION_POLICY_ON_DEMAND` 的那个数值。`CGO_ENABLED=0 go test ./internal/...` 与渲染后端的用例一起覆盖它。

随后在报障机器上验收了打包产物，启动时不带任何环境变量覆盖：UI 进程打开了 `renderD128`（Intel 显示 GPU）并持有两个 dmabuf 描述符——修复前为零；WebProcess 的 CPU 从持续 57–75% 降到六次一秒采样下的约 36%；全程没有打开任何 NVIDIA 设备节点。这就是显示 GPU 上的零拷贝硬件路径，而只改渲染器传输的规则永远到不了这一步。

本机现已观测到：上面测量的这台混合机会走那条回退，[混合 GPU 机上的 WebKitGTK DMABUF 闪烁](2026-10-19-desktop-launcher-hybrid-gpu-dmabuf-flicker.zh.md)记录了不走回退时出现的遮挡闪烁与界面失去响应。NVIDIA-only 机器上的同类表现仍未观测。

## Related

[混合显卡机器上的渲染后端选择](2026-10-18-desktop-launcher-webkit-render-backend-selection.zh.md) 拥有 launcher 请求哪一种渲染器传输、以及独显为何不再拿到合成上下文；本笔记拥有「合成到底发不发生」。 [WebKitGTK DMABUF compositing flicker on NVIDIA machines](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.zh.md) 拥有加速在受影响硬件上重新带来的闪烁。

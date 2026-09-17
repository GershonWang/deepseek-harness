# Agent Note：Wayland 会话下的剪贴板图片粘贴

Status: implemented

[English](2026-09-17-wayland-clipboard-image-paste.md) | 中文

## 问题

会话切到 Wayland 后，往打包客户端里粘贴截图毫无反应，且这次读取永不返回。三处缺陷各自独立成立。

剪贴板读取器把 X server 写死为抽象 socket `/tmp/.X11-unix/X0`、文件 socket `/tmp/.X11-unix/X0`，最后退到 TCP `127.0.0.1:6000`。X11 会话的 X server 恰好是 `:0`，缺陷因此长期不显；Wayland 会话的是 XWayland `:1`，客户端连的是不存在的 server，而唯一存在的那个从不被尝试。

认证请求漏掉了协议要求的 4 字节补齐：auth name 与 auth data 之后各需补齐到 4 字节边界。`MIT-MAGIC-COOKIE-1` 是 18 字节，请求应为 `12 + 20 + 16 = 48` 字节，实际只发 46。真实服务端对短请求不回 `Failed`，而是继续等待缺失的字节；读取器又没有超时，整个剪贴板读取因此永久阻塞——这就是「毫无反应、且永不回来」。同一个回复解析还按字节读 `Failed` 的附加数据长度（线上以 4 字节为单位），把该载荷的四分之三留在连接里。

这两处都只在服务端要求 cookie 时才发作，而这正是 XWayland 的情形：X11 主机通常允许本地连接免认证，于是那里的无认证尝试直接成功，cookie 路径从未被走到。`x11_test.go` 的假 X 服务端有同样的盲区——它按 `nameLen+dataLen` 读取，恰好接受了那份畸形的 46 字节请求——所以补齐缺陷对测试套件不可见。

Wayland 侧持有的 selection 也无法经 X11 兜底：实测 XWayland 不把 Wayland 侧的 `image/png` 桥接成 X11 selection。同一张 12420 字节 PNG 放进 Wayland 剪贴板后，`wl-paste` 读得到（12453 字节，deepin 剪贴板守护进程重编码），而 X11 的 `CLIPBOARD`/`PRIMARY` 只有 0 字节。读取器的 Wayland 通道靠外部 `wl-paste`，但 `wl-clipboard` 在包内、宿主与基础运行时三处都没有，那条通道只会静默返回空。

## 决定

按 `DISPLAY` 推导 X server 而不是写死，遵守 setup 的线格式，并把 `wl-clipboard` 随包。

`connectSocket` 改为遍历该会话 `DISPLAY` 对应的传输方式（抽象 socket、文件 socket，远端 display 才退到 TCP）；`DISPLAY` 为空或解析不出时直接放弃 X11 通道。猜 display 正是当初连错 server 的原因，因此回退链里不再保留猜测。

`setup` 用 `pad4` 补齐 auth name 与 auth data，把 `Failed` 的附加数据长度乘 4，并为握手读取设置 `readTimeout`（握手成功后撤销，使后续各请求自己的超时仍然主导各自的计时）。协议异常现在表现为超时，而不是无界阻塞。

`buildext.apt.depends` 增加 `wl-clipboard`；`tools.yaml` 登记 `wl-paste` 并以 `wl-paste --version` 作为 `verify`——该命令无需合成器即可运行且退出码为 0，无会话的构建机也能如实校验；`verify-merged-deps.sh` 以 `tool:wl-paste` 认领该依赖，使它无法在没有落点校验的情况下被声明。读取器既有的查找顺序（先 PATH，再宿主挂载与系统路径）能找到 `$PREFIX/bin/wl-paste`，因为该目录在容器 PATH 上。

## 备选方案

**在 Go 里直接实现 `wl_data_device`/`wlr-data-control`。** Wayland 通道要跨进程传文件描述符并驱动完整事件循环；`wayland.go` 已记录过，其出错面大于一次 `exec`。

**不随包 `wl-clipboard`，依赖宿主。** 宿主同样没有装它，而读取发生在容器内。

**转而让 X11 通道在 Wayland 会话下可用。** 实测排除了这条路：桥不承载图片格式，无论传输方式或认证如何，那里都没有东西可读。

## 影响

两种会话下粘贴图片都可用。X11 会话继续用自实现的 wire 客户端，并额外能在要求 cookie 的主机上工作；Wayland 侧持有的 selection 经随包的 `wl-paste` 读取。包体增加 `wl-clipboard`（24 KB 的 deb；其 `libwayland-client` 依赖已随 GTK 在包内）。由于 X11 通道先被尝试、而 Wayland 剪贴板在 X11 侧表现为空 selection，每次 Wayland 粘贴都要先付一次失败的 X11 往返——现在有界，但并非免费。

## 测试

`go build`、`go vet`、`go test ./internal/clipboard/` 通过。新增用例固定线格式：`TestSetupRequestWireFormat` 完整断言 48 字节握手（长度字段 18/16、两个 0 补齐字节、cookie 位于 `[32:48]`）以及 `Failed` 应答被完整消费；`TestPad4` 覆盖对齐与「不就地改写入参」；`TestX11Transports`、`TestConnectSocketFollowsDisplay`、`TestConnectSocketNoDisplay`、`TestReadImageSkipsX11WithoutDisplay` 覆盖 DISPLAY 推导与无 `DISPLAY` 时的短路。`authFakeServer` 现在按补齐后的长度读取请求，并在补齐位非 0 时判用例失败。

用真实组件、在容器所见的环境中（`DISPLAY=:1`、`XAUTHORITY=/run/linglong/Xauthority`、会话自己的 Wayland socket）端到端验证：宿主 `xclip` 持有 X1 的 `CLIPBOARD` 时，`ReadImage()` 读回 12420 字节且逐字节一致；真实 `wl-copy` 持有 Wayland 剪贴板时，读回 `wl-paste` 提供的 12453 字节。

打包闸门：`test-verify-tools.sh`、`test-verify-merged-deps.sh`、`test-verify-container-deps.sh` 全绿，其中包括「每个已声明依赖都必须被规则表认领」那一项。在健康产物树里删掉 `bin/wl-paste` 会让 `verify-merged-deps.sh` 报 `FAIL wl-clipboard` 并退出非零，说明这条新认领是承重的。

未做：没有真实 `ll-builder` 构建，也没有把重打的包装到机器上运行，因此「随包后的新包在 Wayland 下能粘贴截图」目前是按环节实测的结果，而非闭环的端到端结论。

## 相关

同类打包先例：[Bundle a real xdg-open in the desktop launcher package](2026-08-27-bundle-xdg-open-for-host-browser-opening.zh.md)。审计条目（`apps/desktop-launcher/AUDIT.md`）：N4（cookie 长度字节序，同一次握手上更早的一处缺陷）、S3（setup 回复未校验）、N30（本轮）。

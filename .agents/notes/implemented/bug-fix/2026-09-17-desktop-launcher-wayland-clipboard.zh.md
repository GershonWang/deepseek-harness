# Agent Note: Wayland clipboard image paste in the desktop launcher

Status: implemented

[English](2026-09-17-desktop-launcher-wayland-clipboard.md) | 中文

## Problem

切到 Wayland 会话后，往客户端里粘贴截图毫无反应且永不返回，在文管里复制图片文件同样失败。四处独立缺陷叠加：

1. `connectSocket` 把 X server 写死为抽象 socket `/tmp/.X11-unix/X0`、文件 socket `/tmp/.X11-unix/X0`，再退到 TCP `127.0.0.1:6000`。X11 会话的 X server 恰好是 `:0`，缺陷因此长期不显；Wayland 会话是 XWayland 的 `:1`，客户端连上的是另一个 X server，真正可用的那个从不被尝试。
2. `setup` 未按协议把 auth name 与 auth data 各自补齐到 4 字节边界。`MIT-MAGIC-COOKIE-1` 是 18 字节，请求应为 `12 + 20 + 16 = 48` 字节，实际只发 46。服务端在请求短于预期时不回 `Failed` 而是继续等待，而 setup 的读取没有超时，整个剪贴板读取因此永久阻塞。`Failed` 回复的附加数据长度还按字节读，而非按 4 字节单位。
3. `wl-clipboard` 未随包，`readWaylandImage` 找不到命令即静默返回。
4. Wayland 通道当时只有位图一条策略。在文管里复制图片**文件**时剪贴板上一个 `image/*` 都没有，于是每次 `wl-paste --type image/*` 探测都空手而归，URI 列表从不被查看。

## Decision

`connectSocket` 按 `DISPLAY` 推导候选（`x11Transports`：抽象 socket → 文件 socket → TCP，返回值顺序即尝试顺序），`DISPLAY` 为空或解析不出时放弃 X11 通道而不是猜测。`setup` 以 `pad4` 补齐 auth name 与 data、把 `Failed` 的附加数据长度乘 4，并给 setup 的读取加 `readTimeout`，握手成功后撤销。随包经 `buildext.apt.depends` 交付 `wl-clipboard`，`tools.yaml` 以 `wl-paste --version` 作为 `wl-paste` 的 verify 命令，`verify-merged-deps.sh` 认领该依赖。`ReadImage` 增加第 5 条策略读取 Wayland 剪贴板的 `text/uri-list`，两条通道共用同一份 URI／路径解析（`readImageFileFromURIList`），因此 X11 与 Wayland 覆盖同样的两种来源：位图与复制的文件。

`wl-paste --version` 无需连接合成器即可运行（无 `WAYLAND_DISPLAY` 时退出码 0），因此能安全用作无会话构建机上的 verify 命令。

## Measured behavior

同一张 12420 字节 PNG 放进 Wayland 剪贴板后，X11 `CLIPBOARD` 与 `PRIMARY` 读到 0 字节，`wl-paste` 读到 12453 字节（deepin 剪贴板守护进程重编码，多出的 33 字节仍在 PNG 合法范围内）；`ReadImage()` 返回后者——XWayland 不桥接图片格式，Wayland 侧持有的 selection 只能走 `wl-paste`。用真实截图工具复现同一结论：Wayland 侧提供 `image/png`（146058 字节）等十余种 `image/*`，X11 侧对 `image/png` 与 `text/uri-list` 均为 0 字节。

在 DDE 文管里复制图片文件后，剪贴板上只有 `text/uri-list`（106 字节，百分号编码）、`x-special/gnome-copied-files`（62 字节，首行 `copy` 加原始 UTF-8 路径）、`x-dfm-copied/file-icons` 与 `text/plain`，**没有任何 `image/*`**，X11 侧则完全为空。

容器把 `/home`、`/media`、`/mnt` 按宿主同路径绑定挂载，因此 URI 指向的文件在容器内可直接读取。

## Alternatives considered

**依赖 XWayland 把 Wayland 剪贴板桥接到 X11。** 实测不成立：同一张 PNG 在 X11 `CLIPBOARD` 与 `PRIMARY` 读到 0 字节。否决。

**只修 X11 通道（DISPLAY 推导与认证补齐）。** Wayland 会话下 Wayland 侧持有的 selection 只能走 `wl-paste`，而 `wl-clipboard` 当时未随包，`readWaylandImage` 只会静默返回空。不足。

**Wayland 通道保持只处理位图。** 文管复制图片文件时剪贴板上没有任何 `image/*`，探测必然空手而归。通道因此补上 `text/uri-list` 策略，使两条通道覆盖同样的两种来源。

## Consequences

X11 会话之所以长期正常，只是因为本机 X server 允许无认证连接，恰好绕过了补齐缺陷；修复后该通道不再依赖这个巧合。setup 的读取超时也让协议异常退化为报错，而不是无限阻塞。

有一处实测不可复现：X11 会话首次尝试 `PRIMARY` 用例时读到 0 字节。随后以完全相同序列重跑三轮、每轮先确认 selection 持有者确实持有 25151 字节，三轮全部通过，因此判为测试脚本竞态而非代码缺陷，机制未捕获。

## Testing

`TestSetupRequestWireFormat` 断言握手 48 字节、补齐位为 0、长度字段 18/16、`Failed` 应答被完整消费；`TestPad4`、`TestX11Transports`、`TestConnectSocketFollowsDisplay`、`TestConnectSocketNoDisplay` 覆盖传输推导。`authFakeServer` 改为按协议补齐后的长度读取并校验补齐位——它原先的形态恰好接受了那份 46 字节的畸形请求，掩盖了缺陷 2。`TestReadImageFileFromURIList`（14 例）、`TestReadWaylandUriListImage`（4 例）、`TestReadWaylandImageBitmap`（2 例）、`TestWaylandWlPaste`（2 例）与 `TestReadImageFallsBackToWaylandUriList` 覆盖 Wayland 侧各条策略。

端到端：宿主 `xclip` 持有 X1 的 `CLIPBOARD` 后 `ReadImage()` 读回 12420 字节且逐字节一致；真实 `wl-copy` 持有 Wayland 剪贴板后 `wl-paste` 与 `ReadImage()` 均读回 12453 字节；在文管复制 318520 字节 PNG 后 `ReadImage()` 读回同字节数。重构建并安装 `0.1.3.2` 后，两种会话的人工验收各四项全过：文字粘入、文字粘出、截图粘贴、文管图片文件粘贴。

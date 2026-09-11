# Agent Note: Shell clipboard reads X11 INCR image transfers

Status: implemented

[English](2026-09-11-clipboard-incr-transfer-and-event-offsets.md) | 中文

## Problem

打包壳的会话输入框里粘贴截图全程没有任何反应：粘贴板能看到那张图，同一条 X 连接上 `xclip -o -selection clipboard -t image/png` 能取回完整文件，而从粘贴板双击该条目重新复制后粘贴就正常。复制图片**文件**可以正常粘贴，复制非图片文件则同样毫无反应（那条路径属于[大图与文件复制的剪贴板粘贴](2026-08-30-clipboard-paste-large-image-and-file-copy.zh.md)）。

持有位图截图的 owner —— `deepin-screen-recorder`，以及随后从它手里接过 selection 的 `dde-clipboard-daemon` —— 用 X11 的 INCR 传输交付内容：第一次 `GetProperty` 回一个 `INCR` 属性（format 32、单个 CARD32、`bytes-after` 为 0），其值是总字节数，载荷随后由 `PropertyNotify` 通知逐块写入。壳的读取实现（`apps/desktop-launcher/internal/clipboard`）读不了这种传输，原因有四个独立的缺陷：

1. `getProperty` 只在 `bytes-after != 0` 时才进入 INCR 分支，而标记回复本身报告的 `bytes-after` 就是 0 —— 整个标记只有 4 字节，一次读就把它读尽。于是这 4 字节标记被当成载荷返回，PNG 魔数校验失败：108253 字节的 1098×699 截图变成了一个 4 字节的“图片”，读取以 `errSelectionEmpty` 结束。
2. `installWindow` 创建请求方窗口时 value-list 为空，导致没有任何 `PropertyNotify` 投递到它，分块循环只能等到超时。
3. `PropertyNotify` 的字段偏移晚读了 4 字节（window 读 8、atom 读 12，实际是 4 和 8），于是通知被拿去和 `time` 字段比较，全部被丢弃。
4. 标准 `SelectionNotify`（事件码 31）的 property 偏移晚读了 4 字节（读 24，而线上偏移是 20 —— `property` 排在 `time`、`requestor`、`selection`、`target` 之后）。容器经玲珑 X 桥访问 X，桥把事件改写成事件码 159、property 仍在偏移 20，恰好正确的那条分支让这个缺陷在打包版里一直不可见；而 `.deb` 包运行在容器外、直连普通 X 服务端，那里每一次转换都会被读成“owner 拒绝”。

## Decision

`getProperty` 只按属性类型判定：回复类型为 `INCR` 就进入分块传输，与 `bytes-after` 无关。标记里的 4 字节值是 owner 声明的总量，在任何分块被读取之前先与 `maxImageBytes` 比对；每一块都带 delete 标志读取，并重新计时 `readTimeout`；读到空块即传输结束；`maxINCRChunks`（65536）限定轮数，让每次只写 1 字节的 owner 也必须在有限轮内结束或被放弃。连接超时按块重新计时：慢但在推进的传输应当读完，而卡住的传输不能把一次粘贴拖成无限等待。

`waitPropertyNotify` 负责等待通知：一直读到属于目标属性的 `PropertyNotify`，按线上偏移 window 4、atom 8 取值，并把 X 错误作为错误上抛。`convert` 同时接受事件码 31 与 159，两者都按偏移 20 读 property —— 桥只改事件码，不改字段偏移。

`installWindow` 在 CreateWindow 的 value-mask 里发送 `CWEventMask`，其值为 `PropertyChangeMask`；这两个位号命名成常量，因为它们是两套独立编号，混用会让服务端回 BadValue。请求方窗口依旧不 map，因此读取剪贴板不会在屏幕上闪出小窗口。

## Testing

`x11_test.go` 的假服务端现在按 owner 的真实行为模拟 INCR —— 先回 `bytes-after` 为 0 的标记，之后每次读取回一块、并在每块之后发一个 `PropertyNotify`，最后回空块 —— 事件包按线上偏移构造，并断言请求方窗口选中了 `PropertyChangeMask`。它此前的 INCR 脚手架是死代码（未使用的 `incr` 字段，以及写在偏移 24、与错误读取实现互相印证的 `SelectionNotify`），所以这些缺陷能一路通过测试。新增用例覆盖三段式 INCR 图像、桥的 159 事件码、未广告的 target 列表、超限标记，以及每一块的 delete 标志。

真机验证使用了真实 owner：在 `dde-clipboard-daemon` 持有 selection 时，`ReadImage` 精确返回 108253 字节的截图（`sha256 bd7cd8a862bc803d…`、1098×699，与源文件一致），而修复前的读取实现在同一剪贴板上返回 `errSelectionEmpty`。同一 owner 上用 `xclip` 做的对照读取并不稳定 —— 它会间歇性地返回空，或只返回第一块（108253 字节中的 65432 字节）—— 因此每次对照前都重新装填 selection；修复后的读取实现每次都返回完整图像。

## Alternatives considered

**用维护中的 X11 库（如 `github.com/jezek/xgb`）替换手写 wire 客户端。** 它能消除这一类缺陷，但会给一个刻意只依赖 `xz` 与 Wails 的 Go 模块新增依赖，并重写一个其余部分正确的读取实现：四个缺陷里三个是单个偏移或单个条件写错，第四个是漏了一个属性。记为读取实现需要超出 selection 读取能力时的备选。

**用标记里的值判定传输结束，而不是空块。** 标记的 CARD32 是 owner 声明的总量，但 owner 可以写得比声明多或少；协议里唯一的结束信号是空块，而拼装结果本来就由 `maxImageBytes` 兜住。

**向 owner 索要更小的 target 以绕开 INCR。** 没有任何广告 `image/png` 的 owner 保证提供低于 INCR 阈值的格式，阈值由 owner 决定，这么做等于用手写协议修复换一个依赖 owner 的猜测。

## Consequences

截图以及任何以 INCR 交付的位图都能经壳的桥接粘贴进输入框 —— 那是打包版 WebKitGTK 渲染器唯一的位图通道。`.deb` 安装同样有了可用的读取实现：标准事件码及其偏移都能正确解析，桥不再是剪贴板可用的前提。

被中途放弃的 INCR 传输会被读成空而不是损坏的载荷，因此读取失败会报告“没有图片”，输入框回退到 WebKitGTK 自己交付的粘贴数据，而不是附上一个垃圾文件。读取实现依旧不读 selection 里的 `text/plain`，所以复制非图片文件仍然粘贴不出任何内容；这个缺口属于 owner 侧的回退策略，而不是传输协议。

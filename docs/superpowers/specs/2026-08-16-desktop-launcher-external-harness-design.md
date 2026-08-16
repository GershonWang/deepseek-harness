# 桌面启动器:外部 harness 连接(本机/远端服务)设计

## 背景与目标

`apps/desktop-launcher` 当前固定加载**玲珑容器内**的 harness 服务(Go supervisor spawn `dsh web` 子进程,webview 加载其 loopback URL)。官方仓库通过 `npx @deepseek-ai/dsh web` 在宿主机运行 web 版本并用浏览器访问。

本设计为服务器状态弹框新增**外部服务连接模式**:
1. 用户可在容器内 harness 与外部 harness 服务(本机 `npx dsh web` 或网络可达的其他机器)之间切换。
2. 切到外部模式时**先停止容器内 harness**(释放端口、暂停自动重启)。
3. 外部连接通过 webview 直接 `Navigate` 到目标 URL——已实证玲珑沙箱共享宿主网络命名空间,沙箱内可直连宿主 loopback 与任何宿主可达地址。

约束:所有改动在 `apps/desktop-launcher/`;不碰上游;supervisor.go 的容器 harness 状态机不动。

## 需求确认(已与用户对齐)

| 决策项 | 结论 |
|---|---|
| 断开外部连接后 | **自动回到容器模式**:重启容器 harness → 等就绪 → 导航回容器 URL |
| 外部 URL 记忆 | 记忆 + 打开弹框自动填充;**不自动重连** |
| 非本机地址安全确认 | 弹确认提示(同会话同 host 只弹一次,重启后重弹) |
| 弹框形态 | 方案 A:弹框内集成模式切换,整体重排布局(视觉交 @designer) |

## 整体架构

```
容器模式(默认)                外部模式
  running ──切外部──► StopHarness()(停容器+暂停自动重启)
                       → HTTP 探测 URL(3s 超时)
                       → 成功:Navigate(url) = ExternalConnected
                       → 失败:弹框内错误提示,留在当前模式
  stopped ◄──断开────   Navigate(容器URL) 前先 Start() → 等就绪
```

- **导航管理器**(新组件,纯 Go 可测):跟踪 webview 当前指向(容器/外部 URL),提供 `ConnectExternal(url)` / `DisconnectToContainer()` / `Status()`。
- **导航时机**:所有 `Navigate` 在 GTK 主线程调用(弹框按钮回调所在线程);webview_go 的 `w.Navigate` 以闭包注入 UI 层(`func(string)`),避免跨线程。
- **状态栏**:外部模式下显示「● 外部服务 http://…」;容器模式不变。
- **Supervisor 不动**:容器 harness 的 Start/StopHarness/Status 直接复用。

## 弹框布局(重设计,交 @designer)

```
┌─ 服务器状态 ──────────────────────────┐
│ 连接模式  (•) 容器内   ( ) 本机/远端服务 │
│ ────────────────────────────────── │
│ ● 运行中            (容器模式状态区)    │
│ 地址  http://127.0.0.1:40847          │
│ ────────────────────────────────── │
│ 服务地址 [http://127.0.0.1:3456____] [连接] │
│          (外部模式输入行,连接后变 [断开]) │
│ ────────────────────────────────── │
│              [启动] [重启] [停止]        │  ← 仅容器模式
└────────────────────────────────────┘
```

- 模式切换控件、分区间距、视觉层级由 designer 定稿。
- 外部模式下隐藏 启动/重启/停止,显示 连接/断开。
- 连接后输入行只读或按钮变「断开」(designer 定)。

## URL 持久化与安全确认

- **持久化**:`~/.config/dsh-desktop/config.json`,格式 `{"externalUrl": "..."}`;首次成功连接后写入;弹框打开时读取填充;文件缺失/损坏静默当空。
- **安全确认**:连接非 loopback(`127.0.0.1`/`localhost`/`::1`)地址前弹确认框,文案提示"将连接远端 harness,其命令在远端机器上执行";同会话内已确认的 host 不重复弹(内存集合),重启后重新弹。

## 错误处理

- 探测失败(超时/非 2xx/3xx)→ 弹框内红色错误提示,不导航,留在当前模式。
- 连接成功但页面加载失败 → 状态栏回退提示,可重试。
- 容器模式启动失败 → 沿用现有已修复路径(StateStopped + "start failed")。

## 测试(全部纯 Go,GTK 薄层)

| 组件 | 测试 |
|---|---|
| `probe(url)` HTTP 探测 | httptest:2xx 通过、5xx/超时失败 |
| `isLoopback(url)` | 127.0.0.1/localhost/::1 → true;局域网 IP → false |
| URL 持久化读写 | 临时目录读写、损坏 JSON 回退 |
| 连接状态机 | 容器→外部→断开回容器的状态转移(复用 mock 脚本) |

## 文件变更(全在 `apps/desktop-launcher/`)

| 文件 | 变更 |
|---|---|
| `connection.go`(新) | 探测/URL 校验/持久化/连接状态机(纯 Go) |
| `ui.go` | 弹框重设计(模式/URL/连接断开)+ 导航闭包 + 安全确认(视觉交 designer) |
| `window.go` / `main.go` | 注入 webview 的 `Navigate` 闭包 |
| `ui_state.go` | 外部模式状态文本扩展 |
| `supervisor.go` | **不动** |
| README | 更新 |

## 边界与风险

- 沙箱共享宿主网络(已实测 netns 相同、宿主 loopback 可达 HTTP 200),网络层零障碍。
- 远端 harness 需绑定非 loopback 接口才能被访问(`dsh web --host <LAN-IP>`;`--host 0.0.0.0` 被上游故意拒绝);远端浏览器信任需 `--trusted-host`。此为使用前提,文档注明,非本功能实现范围。
- 连到远端 harness 时,其命令在远端机器执行、API key 发往远端——由安全确认提示承担告知义务。
- URL 校验接受任意 http(s) 地址(不限制 loopback),非 loopback 走确认流程。

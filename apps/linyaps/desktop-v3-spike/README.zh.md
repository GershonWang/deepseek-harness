# Wails v3 技术验证 spike

[English](README.md) | 中文

`apps/linyaps/desktop-v3-spike` 是桌面客户端从 Wails v2 升级到 v3 之前的可行性验证模块。它不是一个产品包，不参与玲珑打包，也不被 `desktop-launcher` / `desktop-dual` 引用；验证通过后，它的组装方式可以直接作为升级版客户端的骨架。

## 为什么单独成一个模块

v2 与 v3 的模块路径、Linux 图形栈（GTK3/WebKit2GTK 4.1 → GTK4/WebKitGTK 6.0）和前端运行时模型（注入 `window.go` / `window.runtime` 全局 → ES module + `@wailsio/runtime`）都不同，两套栈放进同一棵 `go.mod` 无法并存，混在一起也会让「v3 能否跑通」和「真实迁移」互相干扰。

## 验证判定表

| 待验证问题 | 采集方式 | 通过标准 | 不通过意味着什么 |
| --- | --- | --- | --- |
| GTK4 + WebKitGTK 6.0 能否构建并链接 | `make build` / `verify.sh` | 构建成功，`go.stackVersion` 为 GTK 4.x + WebKitGTK 6.x | v3 迁移的第一道门槛不成立，只能退回 `-tags gtk3` 过渡或自建 runtime |
| 壳页面 origin 是否仍是 `wails://wails` | `frontend.origin` | 等于 `wails://wails` | 客户端按 origin 存的 locale / 字体 / 终端字号会在升级后静默丢失 |
| 该 origin 下 localStorage 可用 | `frontend.localStorageOK` | `true` | 需要重新设计设置持久化落点 |
| 绑定是否可用于无打包器的静态壳 | `binding.ok`（`Call.ByName`） | `true` 且 `reply` 为 `pong:spike` | 壳必须引入构建步骤或被重写 |
| 旧全局是否还存在 | `frontend.classicGlobals` | 预期 `false`，据此决定适配层写法 | 若为 `true` 可复用现有壳代码，成本大降 |
| 运行时全局是否可用 | `frontend.runtimeGlobal`（`window.wails`） | `true` | 只能走 ES module 导入 |
| 就绪前事件是否丢失 | `frontend.earlyEventSeen` + `earlyEmit` | 预期 `false`（会被丢弃） | 启动进度事件必须改成「就绪后重放」 |
| 无边框拖拽是否仍由 CSS 驱动 | `frontend.draggableValue` + 手动拖拽 | 值为 `drag` 且可拖动窗口 | 自绘标题栏需要换实现 |
| GTK4 下 GPU 策略的真实选项 | `go.gpuPolicy` + 两种取值对照运行 | 只有 `always` / `never` 生效（WebKitGTK 6.0 删除 ON_DEMAND） | 客户端现有「枚举错位」绕行代码必须重写 |
| `OnDomReady` 在 v3 是否还存在 | 源码核对（见下） | 不存在，改用 `WindowRuntimeReady` | 麦克风权限钩子的挂载时机需重新设计 |

## 源码级结论（已核对 wails v3.1.0）

- `application.Options` 没有 `OnStartup` / `OnDomReady` / `OnBeforeClose` 字段；生命周期挂在服务（`ServiceStartup` / `ServiceShutdown`）与窗口事件（`OnWindowEvent` / `RegisterHook`）上。
- `WindowRuntimeReady` 是「前端 runtime 已初始化」的唯一信号，`events.Common.WindowRuntimeReady`。
- GTK4 后端（`pkg/application/linux_cgo.go`）只处理 `WebviewGpuPolicyNever` 与 `WebviewGpuPolicyAlways`，其余取值一律落回 ALWAYS；WebKitGTK 6.0 已删除 `ON_DEMAND`。
- v3.1.0 源码里 `-tags gtk3` 后端仍然存在（`linux_cgo_gtk3.go`），与部分文档的「v3.1 移除」不一致；需要时以源码为准，但不要依赖它的长期存在。
- `BundledAssetFileServer` 会在 `/wails/runtime.js` 提供 runtime，静态壳无需打包器即可 `import` 它。
- Linux 下页面仍通过 `wails://` 自定义 URI scheme 加载，但 **v3.1.0 的 origin 实测为 `wails://localhost`，与 v2 的 `wails://wails` 不同**（见真机实测结果）。

## 真机实测结果（2026-11-03，Deepin 25 / X11 / Intel Mesa）

| 项目 | 实测值 | 结论 |
| --- | --- | --- |
| 构建与链接 | `GTK 4.18.6 · WebKitGTK 2.52.5` | 通过；只有 wails 上游 `gdk_x11_*` 弃用警告 |
| 页面 origin | `wails://localhost` | **与 v2 的 `wails://wails` 不同**：localStorage 另起一套，旧设置读不到 |
| localStorage | 可读写 | 功能正常；旧 origin 的数据无法从新 origin 访问 |
| 旧全局 | `window.go` / `window.runtime` 均不存在 | 静态壳必须加适配层 |
| runtime 全局 | `window.wails` 存在 | 适配层可基于它，或直接走 ES module |
| 绑定 | `Call.ByName` 成功，返回 `pong:spike` | 不生成 bindings、不引入打包器即可调用 Go |
| 早期事件 | 发送返回 `false`，前端未收到 | 就绪前事件会丢，启动进度需「就绪后重放」 |
| 就绪事件 | 收到，约 1023 ms（服务启动 2 ms） | `WindowRuntimeReady` 可用，是唯一可靠的「页面已就绪」信号 |
| 拖拽属性 | `--wails-draggable` 计算值为 `drag` | CSS 驱动仍成立；实际拖动需人工确认 |
| GPU 策略 | `always`，Intel Mesa 无渲染异常 | NVIDIA 机型与 `never` 对照待复核 |
| 运行期告警 | a11y bus 缺失；`signal 10`（SIGUSR1）被 JSC 接管 | 前者无害；后者说明「用 SIGUSR1 唤回窗口」在 v3 下依旧不可用，现有 Unix socket 方案仍需保留 |

**origin 变更的影响**：客户端把 locale、字体、终端字号存在壳 origin 的 localStorage 里，升级后这些值读不到，会静默回退默认值。两条路：接受一次性重置；或把壳设置迁到 Go 侧配置文件（现有 `AppConfig` 已承载窗口/主题/端口，扩展它是更稳的落点）。直接从旧 origin 读取不可行——localStorage 按 origin 隔离。

## 容器内打包验证（2026-11-03）

| 项目 | 结果 |
| --- | --- |
| 玲珑包构建 | 成功；`buildext` 把 GTK4、WebKitGTK 6.0、JSC 与 WebKit helper 进程合并进 `${PREFIX}` |
| WebKit helper 路径 | 打包时对 `libwebkitgtk-6.0.so.4` 做字节替换（`/usr/lib/x86_64-linux-gnu/webkitgtk-6.0` → `/tmp/dsh-webkit-6.0`），容器内实测 helper 确实从该路径启动 |
| 容器内功能 | 与宿主一致：origin `wails://localhost`、localStorage 可读写、`Call.ByName` 返回 `pong:spike`、就绪前事件丢失、`--wails-draggable` 为 `drag` |
| 图形栈版本 | 包内取构建容器的 apt 候选：GTK 4.18.6 + WebKitGTK **2.50.4**，而宿主验证用的是 2.52.5。两条发行线（deb12 / deb13）版本不同，正式发包需要固定目标版本 |
| WebKit UI 沙箱 | 保持开启：包内提供 `bwrap 0.10.0` 与 `xdg-dbus-proxy`，并把 `.so` 里 `/usr/bin/bwrap`、`/usr/bin/xdg-dbus-proxy` 一并做等长替换到 `/tmp` 短路径。沙箱在玲珑容器内实测可用（bwrap 创建命名空间正常） |
| 沙箱内的库解析 | 沙箱打开后 helper 跑在 bwrap 的挂载命名空间里，读不到容器级 `ld.so.cache`，报 `libwebkitgtk-6.0.so.4: cannot open shared object file`；启动脚本显式设置 `LD_LIBRARY_PATH` 指向包内 lib 目录后正常。正式客户端同样需要这一条 |
| 退出与清理 | `ll-builder run` 不会把调用者环境变量带进容器，自动退出必须在包内包装脚本里默认开启；进程被强杀会残留 `/run/user/<uid>/linglong/<hash>` 与 `box/<hash>`，阻塞后续构建与运行，需先清理 |

一句话：打包路径与沙箱策略都已验证可行，剩余的是包内 WebKit 版本固定。

## 权限模型：不需要自建钩子（2026-11-04）

客户端现有实现是自建的 GTK3 钩子（遍历控件树连接 `permission-request`）。v3 的 Linux 后端已经自己连接了该信号（`handlePermissionRequest`）：媒体采集按窗口的 `Permissions` 映射处理，**其它权限请求一律交回 WebKit 默认处理（Linux 上即拒绝）**。自建钩子在容器内实测 `installed=true`、`signalConnected=true`，但 `requests=0`——它永远排在框架之后，而框架先返回 TRUE 终止了信号。

策略映射与客户端现有 policy 等价：

```go
Permissions: map[application.PermissionType]application.Permission{
    application.PermissionMicrophone: application.PermissionAllow,
    application.PermissionCamera:     application.PermissionDeny,
}
```

A/B 实测（同一二进制、同一页面，仅改策略）：

| 策略 | `probes.mic` |
| --- | --- |
| `microphone=allow` | `ok=true` |
| `microphone=deny` | `ok=false`，`NotAllowedError` |

摄像头在容器内没有设备，`getUserMedia({video:true})` 在设备匹配阶段就以 `OverconstrainedError` 失败，因此「摄像头被拒」没有拿到硬件级证据；但映射生效已由麦克风的 A/B 证明，且屏幕共享请求同样带 video 标志，落到同一个 `PermissionCamera`。

结论：迁移时可以整块删掉 `internal/webviewperm`（GTK3 cgo 与控件树遍历），改用窗口选项里的 `Permissions`。

## 前端壳迁移验证（2026-11-04）

客户端壳是 2894 行静态脚本，只依赖 10 个 v2 全局成员。验证方式是把壳脚本写成**纯 v2 写法**（不 import 任何模块），再靠一层适配层接住：

| 项目 | 结果 |
| --- | --- |
| `window.go.app.App` 调用 | `Ping`、`ReadClipboardImage`、`RememberSession`、`ReportClientBootFailure`、`StartupState` 全部走通，绑定返回 `pong:spike` |
| `window.runtime` 调用 | `EventsOn`、`EventsEmit` 走通；`BrowserOpenURL` / `ClipboardGetText` / `ClipboardSetText` / `WindowMinimise` / `WindowToggleMaximise` / `Quit` 按同一模式映射 |
| 适配层顺序 | `legacy.shimInstalled=true`——`boot.js` 先装 shim 再动态 import 壳脚本，壳的「没有 window.go 就当浏览器预览」分支不会被误判 |
| 启动状态 | `StartupState()` 拉到 `{"phase":"service-startup","sinceStartMs":3}` |

迁移时 `index.html` 只需把 `<script src="./app.js">` 换成 `<script type="module" src="./boot.js">`，壳源码不用改。

**同时暴露了一条必须改的行为**：本轮 `announceSeen=false` —— 壳脚本晚一步订阅，就错过了 `WindowRuntimeReady` 之后立刻发出的那个事件。v3 不会为晚订阅者重放，因此客户端里所有「一次性启动进度事件」都要改成「加载后主动拉一次」；周期性状态事件（如 `harness:status`）不受影响。

## 运行方式

```sh
sh verify.sh
make check
make build
make build-gtk3
ll-builder build -f apps/linyaps/desktop-v3-spike/linglong/linglong.yaml
ll-builder run -f apps/linyaps/desktop-v3-spike/linglong/linglong.yaml
```

环境变量：`DSH_V3_SPIKE_REPORT` 指定报告路径，`DSH_V3_SPIKE_AUTOCLOSE=1` 拿到报告后自动退出，`DSH_V3_SPIKE_GPU=never|always` 切换硬件加速策略。

## 玲珑运行库现状

官方 base（`org.deepin.base/25.2.2`）与官方 runtime（dtk / webengine）都不提供 GTK4 与 WebKitGTK；deepin 25 发行版仓库里有 `libgtk-4-1`、`libgtk-4-dev`、`libwebkitgtk-6.0-4`、`libwebkitgtk-6.0-dev`，因此按官方 FAQ 的做法随包自带到 `$PREFIX/lib`。完整证据与出处见 [玲珑运行库证据](docs/linyaps-runtime-evidence.md)。

## 已完成与未完成的验证

真机实测已通过（2026-11-03，Deepin 25，GTK 4.18.6 + WebKitGTK 6.0 2.52.5）：依赖检查、cgo 构建、窗口启动、前端回传、绑定调用、报告落盘全部走通，详细结果见上一节。

已在本仓库开发沙箱完成：`go mod tidy`、`gofmt`、`GOOS=windows CGO_ENABLED=0 go build ./...` 与 `go vet ./...` 全部通过，证明对 v3.1.0 API 的用法正确。

仍未验证：标题栏实际拖动与窗口控制按钮（需人工操作确认）；`DSH_V3_SPIKE_GPU=never` 与 NVIDIA 机型下的渲染对照；摄像头拒绝的硬件级验证（容器内无摄像头）；origin 变更后的设置迁移方案；WebKit 沙箱策略（随包 bwrap vs 关闭）与包内 WebKit 版本固定。

## 升级为正式模块的退出标准

1. 判定表中「通过标准」全部成立，或每一项不成立都有明确替代方案。
2. 玲珑包里能带上 GTK4 + WebKitGTK 6.0 运行库并启动（含 WebKit 的 helper 进程路径问题，参考现有 `install-shell-runtime.sh`）。
3. 麦克风权限改由 v3 的 `Permissions` 映射承担，不再需要 GTK4 钩子（已由 A/B 实测确认映射生效）。
4. 前端适配层（事件、绑定、窗口控制、剪贴板）能以最小改动接住现有 2894 行静态壳。

四条满足后再讨论用本模块替换 `desktop-launcher` 或与 `desktop-dual` 合并。

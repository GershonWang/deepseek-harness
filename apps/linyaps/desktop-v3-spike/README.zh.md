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
- Linux 下页面仍通过 `wails://` 自定义 URI scheme 加载，origin 大概率保持 `wails://wails`；本 spike 的 `frontend.origin` 就是用来把它钉死的。

## 运行方式

```sh
sh verify.sh
make check
make build
make build-gtk3
```

环境变量：`DSH_V3_SPIKE_REPORT` 指定报告路径，`DSH_V3_SPIKE_AUTOCLOSE=1` 拿到报告后自动退出，`DSH_V3_SPIKE_GPU=never|always` 切换硬件加速策略。

## 玲珑运行库现状

官方 base（`org.deepin.base/25.2.2`）与官方 runtime（dtk / webengine）都不提供 GTK4 与 WebKitGTK；deepin 25 发行版仓库里有 `libgtk-4-1`、`libgtk-4-dev`、`libwebkitgtk-6.0-4`、`libwebkitgtk-6.0-dev`，因此按官方 FAQ 的做法随包自带到 `$PREFIX/lib`。完整证据与出处见 [玲珑运行库证据](docs/linyaps-runtime-evidence.md)。

## 已完成与未完成的验证

已在本仓库开发沙箱完成：`go mod tidy`、`gofmt`、`GOOS=windows CGO_ENABLED=0 go build ./...` 与 `go vet ./...` 全部通过，证明对 v3.1.0 API 的用法正确。

未完成（本沙箱没有 `gcc`、`pkg-config` 与 GTK/WebKit 开发包，也无法访问桌面会话）：GTK4 链接与真实运行、origin/localStorage 实测、绑定实测、事件时序实测、拖拽实测、GPU 策略对照。这些必须在装有 `libgtk-4-dev` 与 `libwebkitgtk-6.0-dev` 的 Deepin 25 桌面上执行 `sh verify.sh` 后才能下结论。

## 升级为正式模块的退出标准

1. 判定表中「通过标准」全部成立，或每一项不成立都有明确替代方案。
2. 玲珑包里能带上 GTK4 + WebKitGTK 6.0 运行库并启动（含 WebKit 的 helper 进程路径问题，参考现有 `install-shell-runtime.sh`）。
3. 麦克风权限钩子在 GTK4 下可挂载（现有实现基于 GTK3 的 `gtk_window_list_toplevels`，GTK4 需要重写）。
4. 前端适配层（事件、绑定、窗口控制、剪贴板）能以最小改动接住现有 2894 行静态壳。

四条满足后再讨论用本模块替换 `desktop-launcher` 或与 `desktop-dual` 合并。

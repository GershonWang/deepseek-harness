# Wails v3 迁移计划（desktop-launcher / desktop-dual）

本计划依据 `apps/linyaps/desktop-v3-spike` 的真机与容器实测结论制定（该模块在分支 `feat/wails-v3-upgrade` 上，随本分支一起维护）。主分支 `linglong-dev` 保持 v2 可发布状态，迁移只在本分支进行。

## 已由 spike 验证的前提

- GTK4 + WebKitGTK 6.0 可在 Deepin 25 宿主与玲珑容器内构建、运行，页面 origin 变为 `wails://localhost`。
- WebKit UI 进程沙箱默认开启，需要随包提供 `bwrap` / `xdg-dbus-proxy` 并对 `.so` 做四处路径的等长替换；沙箱内 helper 必须显式设置 `LD_LIBRARY_PATH`。
- v3 内置权限模型可替代自建的 GTK3 钩子（`PermissionMicrophone=Allow`、`PermissionCamera=Deny` 经 A/B 实测生效）。
- v2 的 `window.go` / `window.runtime` 可以用一层适配层接住，静态壳源码不需要改。
- 就绪前的事件会被丢弃，一次性启动进度必须改为加载后主动拉取。
- 壳设置的 localStorage 因 origin 变更读不到，需要迁到 Go 侧配置。

## 进度（2026-11-04）

| 阶段 | 状态 |
| --- | --- |
| 1. 依赖与入口 | ✅ 真机 `make build` 通过 |
| 4. 前端适配 | ✅ 真机运行验证：窗口出现、壳 UI 渲染、`前端 runtime 就绪`、`Run 返回 err=<nil>` |
| 5. 权限与设置 | 权限已随阶段 1 完成（v3 内置 `Permissions`）；设置迁移已改代码，单测待真机跑 |
| 2、3、6 | 阶段 3 的主体已随阶段 1 完成；阶段 2/6 待做 |

## 阶段划分与验收

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| 1. 依赖与入口 | `go.mod` 升到 `wails/v3 v3.1.0`、`go 1.26`；`main.go` 改为 `application.New` + `Window.NewWithOptions` + `app.Run`；Controller 绑定 app/window | 真机 `make build` 通过 |
| 2. 生命周期与窗口 | `OnStartup` → `ServiceStartup`、`OnShutdown` → `ServiceShutdown`、`OnBeforeClose` → `WindowClosing` 钩子；窗口几何读写改走 window 对象 | 关窗保存几何、隐藏/唤回、信号退出三条路径回归 |
| 3. 运行时 API | `runtime.EventsEmit` → `app.Event.Emit`、`runtime.Window*` → `window.*`、`ScreenGetAll` → `app.Screen.GetAll`、`MessageDialog` → `Question()` 包装 | 状态事件、终端事件、启动进度事件仍到达前端 |
| 4. 前端适配 | 新增 `v2shim.js` + `boot.js`（`index.html` 改一行）；启动进度改 `StartupState()` 拉取 | 壳页面能拿到 `window.go` / `window.runtime`，启动进度可见 |
| 5. 权限与设置 | 删除 `internal/webviewperm`，改窗口 `Permissions`；壳设置从 localStorage 迁到 `AppConfig` | 麦克风可用、摄像头/屏幕被拒；设置跨重启保留 |
| 6. 打包 | 按 spike 的 `linglong/` 改 GTK4 依赖、四路径补丁、`bwrap`、`LD_LIBRARY_PATH`、固定包内 WebKit 版本 | 容器内启动、录屏/麦克风、外链、托盘唤回回归 |

每阶段结束跑 `make check`（离线单测）并在真机做一次 `make build`；阶段 6 需要一次完整玲珑包构建与安装验证。

## 需要确认的语义变化

1. **对话框的同步性**：v3 的 `MessageDialog.Show()` 只负责把对话框交给主线程，不阻塞到用户点击；v2 的 `runtime.MessageDialog` 是同步返回的。计划用 channel 包装恢复同步语义（按钮回调里关闭 channel），但**用户点对话框的关闭按钮时是否触发 cancel 按钮的回调尚未验证**——若不触发，这条前端调用会一直挂着，需要在阶段 2 真机确认，必要时给对话框加超时兜底。
2. **屏幕信息更完整**：v3 的 `Screen` 带 `Bounds`（含原点），比 v2 的「只有尺寸」更准。为避免行为漂移，阶段 2 先保留现有保守判据（用各屏尺寸拼外接框），把"用原点精确判断"作为后续独立改进。
3. **权限范围**：Linux 上非媒体权限（通知、地理位置）由 WebKit 默认拒绝，与现状一致；`Permissions` 只覆盖麦克风/摄像头。
4. **设置迁移是一次性重置**：旧 origin 的 localStorage 无法读取，首次启动写默认值。
5. **SIGUSR1 仍被 JSC 接管**：Unix socket 唤回方案保留，不要改成信号唤醒。

## 风险与回滚

- 全部改动在本分支，`linglong-dev` 不受影响；任何阶段验证失败都可以按阶段回退。
- 包 ID 与版本号留到阶段 6 再决定，避免迁移期间影响用户升级路径。

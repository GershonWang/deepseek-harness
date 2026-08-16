# DeepSeek Harness 桌面启动器:窗口居中、状态栏与两个弹框 设计

## 背景与目标

`apps/desktop-launcher` 是 Go + webkit2gtk 的薄启动器:spawn `dsh web` 子进程,用独立窗口加载其 loopback Web GUI,打成玲珑包分发。本设计为其增加三块交互:

1. **窗口居中**:程序启动时窗口位于屏幕中央。
2. **状态栏**:窗口内容区**最底部**一行,左侧实时显示 harness 运行状态(运行中/已停止 + 端口),右侧两个按钮。底部是 GTK 惯例(`GtkStatusbar` 贴底模式),与浏览器/文件管理器一致。
3. **两个弹框**:
   - **服务器状态**:监控 harness 进程状态(状态 + 端口/URL + 上次退出原因),提供启动/重启/停止三个手动控制按钮,作为自动重启之外的协助保障。
   - **关于**:展示作者、GitHub 仓库地址(可点击打开)、harness 版本号、玲珑包版本号。

约束:所有改动集中在 `apps/desktop-launcher/`,不碰上游源码(harness Web GUI 等)。

## 需求确认(已与用户对齐)

| 决策项 | 结论 |
|---|---|
| 状态栏形态 | 保留系统标题栏(最小化/最大化/关闭不动),状态栏是窗口内容区**最底部**一行(右下角两个按钮) |
| 状态栏左侧 | 实时状态指示(运行中/已停止 + 端口),1 秒轮询刷新 |
| 服务器状态弹框 | 启动(仅停止态可用)/ 重启(仅运行态可用)/ 停止(仅运行态可用) |
| 关于弹框 | GTK 标准关于弹框:作者 GershonWang、仓库地址可点击、harness 版本、玲珑包版本 |

## 整体架构

**方案 A(选定):原生 GTK UI,全部在 desktop-launcher 内。**

备选方案 B 在 harness Web GUI(React,上游)里加状态栏——样式统一但违反适配边界,且需跨进程暴露 harness 状态给前端,已否决。

```
dsh-desktop-launcher (Go)
├── supervisor.go    harness 生命周期状态机 + Status/Start/Restart/StopHarness
├── ui.go(新)         GTK 状态栏 + 两个弹框 + 窗口居中(cgo)
├── window.go         webview 窗口,调用状态栏挂载与居中
├── version.go(新)    版本解析(纯函数)
└── prepare-offline.sh  -ldflags 注入玲珑包版本
```

GTK 代码沿用 `window.go` 现有的 cgo 模式(C 回调 + `w.Window()` 拿 GtkWindow)。

## Supervisor 状态机(核心)

现状:run() 循环 spawn → 等退出 → 总是自动退避重启(500ms→10s)。

改为四态 + 手动控制:

| 状态 | 含义 | 进入方式 |
|---|---|---|
| starting | 已 spawn、未就绪 | spawn 后 |
| running | 存活(记录端口/URL) | 就绪行匹配 |
| stopped | 未运行(记录上次退出 code/signal) | 退出/手动停止 |

- **意外退出(崩溃)** → 保持自动退避重启。
- **手动停止**(`StopHarness`)→ 杀当前进程,置 `manuallyStopped` 标记,进入"手动停止"态,**暂停自动重启**,直到手动 Start。
- **手动启动**(`Start`)→ 停止态立即 spawn,清除 `manuallyStopped` 标记,恢复崩溃自动重启。
- **重启**(`Restart`)→ 运行态或停止态均杀/清状态后立即 spawn,并清除 `manuallyStopped` 标记恢复自动重启(不等退避)。
- `Status() HarnessStatus` 返回状态、URL、上次退出原因。
- 现有 `Stop()`(应用关闭,终态)与单一 `cmd.Wait()` goroutine、退出日志机制保持不变。

## GTK UI 结构

**窗口居中**:`gtk_window_move` 按屏幕尺寸(主显示器)与窗口尺寸计算中心。环境为 kwin_x11,`gtk_window_move` 生效。

**状态栏挂载**:webview 是 GtkWindow 的直接子控件(`gtk_bin_get_child`)。用 `gtk_container_remove` 摘下,放入新建的纵向 GtkBox,底部插一行横向 GtkBox:

```
[系统标题栏:最小化/最大化/关闭]
[webview]
[状态栏:● 运行中 :40275 ...... [服务器状态] [关于]]
```

状态栏左侧 label 与服务器弹框内容均由 `gtk_timeout_add`(1 秒)轮询 `sup.Status()` 刷新。

**服务器状态弹框**(GtkDialog):状态 + 端口/URL + 上次退出原因;三按钮按状态控制 sensitive。

**关于弹框**(GtkAboutDialog):program-name、authors(GershonWang)、website(https://github.com/GershonWang/deepseek-harness,可点击)、comments、版本区显示 harness 版本与玲珑包版本。

## 版本注入

- **harness 版本**:`resolveHarnessVersion()` 读 `$PREFIX/harness/package.json`(打包态)或 `apps/cli/package.json`(开发态)的 `version` 字段。
- **玲珑包版本**:`prepare-offline.sh` 从 `linglong.yaml` 提取 `package.version`(当前 0.1.0.9),go build 加 `-ldflags "-X main.packageVersion=..."` 注入;未注入时(本地 go build)显示 "dev"。

## 测试

- **Supervisor 状态机**(mock 脚本驱动):状态转移、手动停止暂停自动重启、Start 恢复、Restart 强制重启、Status 数据。
- **版本解析**:纯函数测试。
- GTK 渲染层不做单测(headless 无显示),逻辑保持薄层,可测逻辑全部在纯 Go 侧。

## 边界与风险

- 不修改 webview_go 依赖与上游 harness 源码。
- GTK 按钮回调全部走 cgo C 回调,回调内只做状态查询与 `sup.*` 调用,避免在 GTK 主线程外触碰 GTK。
- Wayland 下 `gtk_window_move` 可能被合成器忽略(本机 kwin_x11 不受影响),文档注明。

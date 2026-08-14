# Agent Note: 通过 Go + webview_go 实现 Linux 桌面启动器

Status: implemented

English | [中文](2026-08-14-desktop-launcher-linux-linglong.zh.md)

## Problem

DeepSeek Harness 需要一个原生 Linux 桌面客户端，在独立窗口中呈现 Web UI。启动器必须启动 harness web 服务器、检测就绪状态、打开浏览器窗口，并在重启和关闭期间监管子进程。它必须在玲珑（Linglong）沙箱中工作——文件系统访问需要用户配置挂载——并且必须处理 Node.js 运行时可用性问题，而不捆绑副本。

## Decision

启动器是位于 `apps/desktop-launcher/` 的 Go 二进制文件，使用 `github.com/webview/webview_go` 创建 webkit2gtk 窗口。架构是三阶段流水线：

1. **环境解析**（`env.go`）：`resolveDesktopEnv()` 按优先级顺序发现 `dsh web` 入口点：`DSH_DESKTOP_DSH_BIN` 覆盖、玲珑打包的 `$PREFIX/harness/lib/bin.js`，或仓库相对路径 `apps/cli/lib/bin.js`。`resolveNode()` 通过 `DSH_DESKTOP_NODE` 或 `PATH` 解析 Node.js 二进制文件。

2. **进程监护**（`supervisor.go`）：`Supervisor` 以独立进程组（`Setpgid: true`）生成 `dsh web --port <port>` 子进程，将 stdout 捕获到日志文件和行扫描器，并匹配 `dsh web: http://127.0.0.1:<port>` 就绪行以发出就绪信号。失败时应用指数退避（500ms 基础，10s 上限）。`Stop()` 向进程组发送 `SIGTERM`，等待最多 5 秒，然后 `SIGKILL`。`readyScanner` 在每次生成前排空就绪通道，防止过期 URL。

3. **窗口生命周期**（`window.go`）：`openWindow()` 创建 1280×800 webkit2gtk 窗口，导航到回环 URL，并阻塞直到用户关闭窗口，然后调用 `sup.Stop()`。

`main.go` 编排：启动监护器、等待就绪（30s 超时）、打开窗口、等待进程退出。

环境变量控制行为：`DSH_DESKTOP_PORT`（默认 `0` 随机端口）、`DSH_DESKTOP_LOG_DIR`（默认 `~/.cache/dsh-desktop`）、`DSH_DESKTOP_NODE`、`DSH_DESKTOP_DSH_BIN`。

启动器选择 Go + webview_go 而非 Electron 或 Tauri，基于三个原因：二进制大小可忽略（不捆绑 Chromium 或 Node 运行时），对 webkit2gtk 的依赖已存在于大多数 Linux 桌面上，且启动器只需渲染单个 URL——不需要复杂的 Web API 表面。

## Alternatives considered

**Electron。** 被否决，因为它捆绑 Chromium 和完整的 Node.js 运行时，产生 150MB+ 的二进制文件。harness 已有自己的 Node.js 依赖；在 Electron 中添加第二个副本会使运行时占用翻倍，并使沙箱打包复杂化。

**Tauri。** 被否决，因为它需要 Rust 工具链并捆绑系统 webview，但其 Rust IPC 层为只需导航到一个 URL 的启动器增加了复杂性。Rust 编译步骤拖慢迭代，且 Tauri 的 Linux 支持无论如何都依赖 webkit2gtk——没有比直接 Go 绑定的优势。

**prepare-runtime.mjs（来自 fork）。** 原始 Electron 启动器通过 `prepare-runtime.mjs` 和 electron-builder 签名捆绑 Node.js 运行时。这被放弃，因为玲珑打包模型单独提供 Node.js；捆绑副本会产生版本冲突并使包大小翻倍。

**electron-builder 和代码签名。** fork 使用 electron-builder 进行打包和签名。这些被放弃，因为 Go 二进制文件除了 `go build` 不需要构建流水线，且玲珑无需代码签名即可处理分发和沙箱化。

## Consequences

启动器是单个静态二进制文件，运行时仅依赖 webkit2gtk-4.1。玲珑用户必须为 harness 工作目录和 API 密钥配置文件系统挂载——沙箱不继承主机路径。`dsh` web 服务器必须绑定到 `127.0.0.1` 才能使回环模型工作；`dsh web` 已默认如此。

监护器设计移植自 fork 的 `HarnessSupervisor`，适配 Go 的 goroutine 模型。dsh 闭包修复（在重新生成前排空过期就绪通道值）防止窗口打开上一个进程 URL 的竞态条件。

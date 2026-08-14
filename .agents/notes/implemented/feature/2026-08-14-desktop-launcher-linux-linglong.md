# Agent Note: Linux desktop launcher via Go + webview_go

Status: implemented

English | [中文](2026-08-14-desktop-launcher-linux-linglong.zh.md)

## Problem

DeepSeek Harness needs a native Linux desktop client that presents the web UI in a standalone window. The launcher must spawn the harness web server, detect readiness, open a browser window, and supervise the child process through restarts and shutdown. It must work in Linglong (玲珑) sandboxes where filesystem access requires user-configured mounts, and it must handle Node.js runtime availability without bundling a copy.

## Decision

The launcher is a Go binary at `apps/desktop-launcher/` that uses `github.com/webview/webview_go` to create a webkit2gtk window. The architecture is a three-stage pipeline:

1. **Environment resolution** (`env.go`): `resolveDesktopEnv()` discovers the `dsh web` entry point by checking, in priority order: the `DSH_DESKTOP_DSH_BIN` override, the Linglong-packaged `$PREFIX/harness/lib/bin.js`, or the repo-relative `apps/cli/lib/bin.js`. `resolveNode()` resolves the Node.js binary via `DSH_DESKTOP_NODE` or `PATH`.

2. **Process supervision** (`supervisor.go`): `Supervisor` spawns `dsh web --port <port>` as a child process in a separate process group (`Setpgid: true`), captures stdout into a log file and a line scanner, and matches the `dsh web: http://127.0.0.1:<port>` ready line to signal readiness. On failure, it applies exponential backoff (500ms base, 10s cap). `Stop()` sends `SIGTERM` to the process group, waits up to 5s, then `SIGKILL`. A `readyScanner` drains the ready channel before each spawn to prevent stale URLs.

3. **Window lifecycle** (`window.go`): `openWindow()` creates a 1280×800 webkit2gtk window, navigates to the loopback URL, and blocks until the user closes it, then calls `sup.Stop()`.

`main.go` orchestrates: spawn supervisor, wait for ready (30s timeout), open window, wait for process exit.

Environment variables control behavior: `DSH_DESKTOP_PORT` (default `0` for random), `DSH_DESKTOP_LOG_DIR` (default `~/.cache/dsh-desktop`), `DSH_DESKTOP_NODE`, `DSH_DESKTOP_DSH_BIN`.

The Launcher chose Go + webview_go over Electron or Tauri for three reasons: the binary size is negligible (no bundled Chromium or Node runtime), the dependency on webkit2gtk is already present on most Linux desktops, and the launcher only needs to render a single URL — no complex web API surface.

## Alternatives considered

**Electron.** Rejected because it bundles Chromium and a full Node.js runtime, producing 150MB+ binaries. The harness already has its own Node.js dependency; adding a second copy in Electron doubles the runtime footprint and complicates sandbox packaging.

**Tauri.** Rejected because it requires Rust toolchain and bundles a system webview, but its Rust IPC layer adds complexity for a launcher that only navigates to one URL. The Rust compilation step slows iteration, and Tauri's Linux support depends on webkit2gtk anyway — no advantage over the direct Go binding.

**prepare-runtime.mjs (from the fork).** The original Electron-based launcher bundled a Node.js runtime via `prepare-runtime.mjs` and electron-builder signing. This was abandoned because the Linglong packaging model provides Node.js separately; bundling a copy creates version conflicts and doubles the package size.

**electron-builder and code signing.** The fork used electron-builder for packaging and signing. These were abandoned because the Go binary needs no build pipeline beyond `go build`, and Linglong handles distribution and sandboxing without code signing.

## Consequences

The launcher is a single static binary that depends only on webkit2gtk-4.1 at runtime. Linglong users must configure filesystem mounts for the harness working directory and API keys — the sandbox does not inherit host paths. The `dsh` web server must bind to `127.0.0.1` for the loopback model to work; `dsh web` already defaults to this.

The Supervisor design is ported from the fork's `HarnessSupervisor`, adapted for Go's goroutine model. The dsh closure fix (draining stale ready-channel values before re-spawn) prevents a race where the window opens a URL from a previous process.

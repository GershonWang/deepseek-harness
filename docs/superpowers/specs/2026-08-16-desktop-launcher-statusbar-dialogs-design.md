# DeepSeek Harness desktop launcher: window centering, status bar, and two dialogs — design

English | [中文](2026-08-16-desktop-launcher-statusbar-dialogs-design.zh.md)

## Background and goals

`apps/desktop-launcher` is a thin Go + webkit2gtk launcher: it spawns a `dsh web` subprocess and loads its loopback Web GUI in a separate window, distributed as a Linglong package. This design adds three pieces of interaction to it:

1. **Window centering**: on startup the window is placed at the center of the screen.
2. **Status bar**: a single row at the **very bottom** of the window content area, showing the harness running status live on the left (running/stopped + port) and two buttons on the right. The bottom is the GTK convention (`GtkStatusbar` bottom-attach mode), consistent with browsers and file managers.
3. **Two dialogs**:
   - **Server status**: monitors the harness process status (state + port/URL + last exit reason) and provides three manual control buttons — start/restart/stop — as assistance beyond automatic restart.
   - **About**: shows the author, the GitHub repository address (clickable to open), the harness version, and the Linglong package version.

Constraints: all changes are confined to `apps/desktop-launcher/`; upstream source (the harness Web GUI and so on) is untouched.

## Confirmed requirements (aligned with the user)

| Decision | Conclusion |
|---|---|
| Status bar form | Keep the system title bar (minimize/maximize/close unchanged); the status bar is a single row at the **very bottom** of the window content area (two buttons at the bottom right) |
| Status bar left side | A live status indicator (running/stopped + port), refreshed by a 1-second poll |
| Server status dialog | Start (enabled only when stopped) / restart (enabled only when running) / stop (enabled only when running) |
| About dialog | The standard GTK about dialog: author GershonWang, clickable repository address, harness version, Linglong package version |

## Overall architecture

**Option A (chosen): native GTK UI, all inside desktop-launcher.**

Alternative B adds the status bar inside the harness Web GUI (React, upstream) — the styling would be uniform, but it violates the adaptation boundary and requires exposing harness status to the frontend across processes, so it was rejected.

```
dsh-desktop-launcher (Go)
├── supervisor.go    harness 生命周期状态机 + Status/Start/Restart/StopHarness
├── ui.go(新)         GTK 状态栏 + 两个弹框 + 窗口居中(cgo)
├── window.go         webview 窗口,调用状态栏挂载与居中
├── version.go(新)    版本解析(纯函数)
└── prepare-offline.sh  -ldflags 注入玲珑包版本
```

The GTK code follows the existing cgo pattern in `window.go` (a C callback plus `w.Window()` to obtain the GtkWindow).

## Supervisor state machine (core)

Current state: the run() loop spawns → waits for exit → always restarts with automatic backoff (500ms→10s).

Change it to four states plus manual control:

| State | Meaning | How it is entered |
|---|---|---|
| starting | spawned but not ready | after spawn |
| running | alive (records port/URL) | the readiness line matches |
| stopped | not running (records the last exit code/signal) | exit / manual stop |

- **Unexpected exit (crash)** → keep the automatic backoff restart.
- **Manual stop** (`StopHarness`) → kill the current process, set the `manuallyStopped` flag, enter the "manually stopped" state, and **pause automatic restart** until a manual Start.
- **Manual start** (`Start`) → spawn immediately from the stopped state, clear the `manuallyStopped` flag, and restore crash-triggered automatic restart.
- **Restart** (`Restart`) → from either the running or the stopped state, kill/clear the state and spawn immediately, clearing the `manuallyStopped` flag and restoring automatic restart (without waiting for the backoff).
- `Status() HarnessStatus` returns the state, URL, and last exit reason.
- The existing `Stop()` (application shutdown, terminal state) plus the single `cmd.Wait()` goroutine and the exit-log mechanism stay unchanged.

## GTK UI structure

**Window centering**: `gtk_window_move` computes the center from the screen size (primary display) and the window size. The environment is kwin_x11, where `gtk_window_move` takes effect.

**Status bar mounting**: the webview is the GtkWindow's direct child (`gtk_bin_get_child`). Remove it with `gtk_container_remove`, put it into a new vertical GtkBox, and insert one horizontal GtkBox row at the bottom:

```
[系统标题栏:最小化/最大化/关闭]
[webview]
[状态栏:● 运行中 :40275 ...... [服务器状态] [关于]]
```

The left-hand status bar label and the server dialog contents are both refreshed by `gtk_timeout_add` (1 second) polling `sup.Status()`.

**Server status dialog** (GtkDialog): state + port/URL + last exit reason; the three buttons' `sensitive` is driven by the state.

**About dialog** (GtkAboutDialog): program-name, authors (GershonWang), website (<https://github.com/GershonWang/deepseek-harness>, clickable), comments, and a version area showing the harness version and the Linglong package version.

## Version injection

- **harness version**: `resolveHarnessVersion()` reads the `version` field of `$PREFIX/harness/package.json` (packaged) or `apps/cli/package.json` (development).
- **Linglong package version**: `prepare-offline.sh` extracts `package.version` from `linglong.yaml` (currently 0.1.0.9) and injects it into go build through `-ldflags "-X main.packageVersion=..."`; when it is not injected (a local go build) it shows "dev".

## Tests

- **Supervisor state machine** (driven by a mock script): state transitions, manual stop pausing automatic restart, Start restoring it, Restart forcing a restart, and the Status data.
- **Version resolution**: pure function tests.
- The GTK rendering layer has no unit tests (headless has no display); the logic stays a thin layer, and all testable logic lives on the pure Go side.

## Boundaries and risks

- The webview_go dependency and the upstream harness source are not modified.
- Every GTK button callback goes through a cgo C callback, and inside the callback it only queries state and calls `sup.*`, avoiding touching GTK outside the GTK main thread.
- Under Wayland `gtk_window_move` may be ignored by the compositor (the local kwin_x11 is unaffected); the documentation notes this.

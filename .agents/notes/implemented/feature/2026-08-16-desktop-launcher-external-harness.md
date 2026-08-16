# Agent Note: External harness connection in the desktop launcher

Status: implemented

English | [中文](2026-08-16-desktop-launcher-external-harness.zh.md)

## Problem

The desktop launcher at `apps/desktop-launcher/` loads the web UI exclusively from the harness it supervises inside the Linglong container. Users also run the web edition on the host (`npx @deepseek-ai/dsh web`) or on a network-reachable machine, and the launcher gives no way to point the webview at that external service instead of the in-container one.

## Decision

The server-status dialog gains a connection-mode radio (容器内 / 本机或远端服务), an external service-address row with 连接/断开 buttons, and an external status area. All changes stay inside `apps/desktop-launcher/`; `supervisor.go` keeps its container state machine untouched.

Connecting to an external service follows a fixed order: validate and normalize the URL (`Connector.ValidateURL`), confirm non-loopback hosts once per session (`NeedConfirmation`/`ConfirmHost`), stop the container harness first (`StopHarness` frees the port and pauses auto-restart), then run a single HTTP probe (`Connector.BeginExternal`, 3s timeout) in a goroutine. Probe success switches `connector.Mode()` to `ModeExternal` and navigates the webview to the external URL; probe failure restores the container flow automatically — the error shows in the dialog and `Supervisor.Restart()` brings the container harness back. Disconnect runs the reverse: `EndExternal()`, restart the container, wait for its ready URL (bounded 30s), and navigate back.

`connector.Mode()` is the single authority for the current mode; the dialog radio buttons always mirror it, never the reverse. Every path that changes mode re-syncs the radio — dialog open, connect click, probe success, probe failure, disconnect — and `dshOnModeChanged` forces the radio back to external while a connection is active, so a user cannot toggle to 容器内 (and enable the container buttons) while the webview still shows the external service.

Goroutine results cross to the GTK main thread through the idle callback's `gpointer`: the goroutine packs the probe result (URL + error message) or the pending navigation URL into C memory with `C.CBytes`, the C idle callback hands the pointer to the Go handler, and frees it after the handler returns. Package-global result variables do not exist, so the handoff carries no Go data race; the `Connector` itself stays mutex-protected and unchanged.

External URLs persist to `~/.config/dsh-desktop/config.json` (`{"externalUrl": "..."}`) after a successful connection and prefill the dialog on open; the launcher never auto-reconnects.

## Alternatives considered

**Mutex-guarded package globals.** The handoff could keep `probeResultURL`/`probeResultErr`/`pendingNavURL` and guard them with a `sync.Mutex`. The gpointer route is cleaner here because the C idle callbacks already receive a `gpointer` argument — the payload rides it with no shared Go state to forget to lock.

**Probing on the GTK main thread.** A blocking probe would freeze the dialog for up to the 3s timeout. The probe runs in a goroutine and reports through `g_idle_add`, keeping the UI responsive.

**Stopping the container after a successful probe.** Rejected: the in-container harness would keep the port and keep auto-restarting while the probe runs, risking a port conflict with the external service. `StopHarness` runs before the probe.

**Remembering the URL and auto-reconnecting.** Rejected: persistence serves only to prefill the dialog; reconnecting is always an explicit user action with its own probe and confirmation.

## Consequences

The Linglong sandbox shares the host network namespace — verified empirically, the sandbox and host sit in the same netns and the host loopback answers HTTP 200 from inside the sandbox — so the webview reaches both the host loopback and any host-reachable address with no sandbox network configuration.

A remote harness must bind a non-loopback interface to be reachable (`dsh web --host <LAN-IP>`); `--host 0.0.0.0` is deliberately rejected upstream, and remote browsing needs `--trusted-host`. The security confirm dialog shows the target URL as plain text before connecting, so a typo or a stale autofilled entry is visible to the user before the API key and commands travel to that machine.

The failure path is automatic: a failed probe restarts the container harness, so the user is never stranded in external mode with a dead URL and a stopped container. The container supervisor's own restart/stop semantics are reused unchanged.

# Agent Note: Desktop launcher GUI mount watchdog time basis and reload budget

Status: implemented

English | [中文](2026-10-19-desktop-launcher-gui-mount-watchdog.zh.md)

## Problem

The embedded harness GUI occasionally flashes — an intermittent, high-frequency blink that users report against the shell while the harness keeps running. Two earlier attempts to make that class of problem self-healing and diagnosable did not hold up.

The mount watchdog counted "status snapshots" (about 25 of them) before reloading the iframe, on the stated premise that the Go side pushes `harness:status` once per second. It does not: the ticker calls `emitStatusIfChanged`, and `FrontendStatus` has no time-varying field, so a stably blank GUI produces no events at all. In exactly the scenario the watchdog was written for — harness running, target stable, content missing — the counter never advanced. What actually rescued the blank screen was the link bridge's own `boot-stalled` report and the iframe `error` channel.

The reload budget was per target generation: any `Target` change reset the counter, the reload count, and the "already reported" flag. Since every harness restart mints a new token, a restart-heavy period handed the shell a fresh budget each time, so "at most twice" could repeat without bound — and every reload is a full-page flash. This is a plausible contributor to a flicker report; the shell had no log to tell it apart from a compositing flash.

Nothing on the interface side was logged at all, so when a flash happened there was no way to tell "the iframe reloaded" from "the compositor dropped a frame".

## Decision

The watchdog is driven by a one-second timer and judges by elapsed time (`Date.now()` difference), not by counting pushes. The documented premise was wrong, so the mechanism that depended on it was replaced rather than patched.

The reload budget is scoped to a five-minute window (`GUI_RELOAD_WINDOW_MS`, `GUI_RELOAD_MAX_IN_WINDOW`). A target generation change resets only that generation's wait state; it does not refresh the budget or clear the already-reported flag. Only an expired window starts a new round.

Interface-side events go to `launcher.log` in the same runtime directory as `harness.log`, one event per line with the same timestamp format, through a new `App.LogUiEvent` binding (`internal/app/uievent.go`): stage switches, target replacements (origin plus "query changed", never the query itself), mount handshake, bridge stall reports, reloads with reason and budget, and budget exhaustion. The file rotates at 2 MiB, writes are serialized, and every `?token=…` is rewritten to `?token=<redacted>` on the Go side so a forgetful caller cannot leak credentials to disk.

## Alternatives considered

**Keep snapshot counting and make Go push a heartbeat every second.** Rejected: it defeats `emitStatusIfChanged` (the frontend would re-render once per second) to serve one timer, and it couples a presentation concern to the status channel.

**Keep the per-generation budget and add a separate global cap.** Rejected as redundant complexity: a single window-scoped counter already bounds reloads, and the generation-scoped bookkeeping was the bug.

**Write the events to `localStorage` from the frontend.** Rejected: the file would not sit next to `harness.log`, could not be read after a crash in the same way, and is not a disk artifact anyone can inspect offline.

**Log the full iframe target.** Rejected on the credential line: the target carries the harness token, and logging it would write a live credential into a file users share in bug reports.

**Detect reloads through the DevTools protocol instead of adding a binding.** Rejected: the packaged client exposes no debugging endpoint, and enabling one for diagnostics would be a larger, riskier change than an append-only log line.

## Consequences

A flicker report can now be attributed from artifacts alone: reload events carry a reason and budget, target replacements carry the origin and whether the query changed, and stage switches show whether the shell was changing what it displays. This does not fix the flicker; it makes the class of flicker decidable, which the two earlier attempts did not.

Budget semantics changed: a harness that keeps restarting (new token each time) no longer gets a fresh pair of reloads per generation. Within a window it gets two reloads total, then the existing failure page and diagnosis path takes over until the window expires. That is deliberate — repeated reloads cost the user a flash each time.

`launcher.log` grows to at most about 2 MiB before one rotation, so support bundles stay bounded. Redaction is a regular expression over `token=`; a differently named credential in a future URL would need the pattern extended, and the Go tests assert the current contract rather than the absence of any future leak.

## Testing

`internal/app/uievent_test.go` pins the log contract: one timestamped line per event, token redaction, newline flattening with truncation, rotation at the limit, concurrent writes staying one line each, and the binding writing under `appenv.RuntimeDir()` so `DSH_DESKTOP_LOG_DIR` redirects it.

`frontend/test-app.cjs` (80 pass) drives the watchdog on a fake clock: a stall past the threshold reloads once, a mount handshake stops further reloads, bridge reports reload immediately, external mode never participates, and — the regression for the budget bug — a target change inside the window does not refresh the reload budget.

## Related

The flicker this instrumentation exists to classify: [WebKitGTK DMABUF compositing flicker on NVIDIA machines](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.md) and the acceleration work that re-exposed it: [Desktop launcher WebView hardware acceleration policy](2026-10-18-desktop-launcher-webview-hardware-acceleration.md). The watchdog's original white-screen fix is described in the launcher README (`apps/linyaps/desktop-launcher/README.md`), which also documents the new log file.

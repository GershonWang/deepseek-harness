# Agent Note: Desktop launcher preflight gate and release semantics

Status: implemented

English | [中文](2026-10-18-desktop-launcher-preflight-gate-first-spawn.zh.md)

## Problem

The first launch after installing the package showed a harness that was started and terminated 0.27 s later, followed by a 13.5 s gap before the first plugin-tree progress line; the shell stayed on a non-harness stage, and starting from the server menu appeared to wake it. `harness.log` held exactly one `harness killed by signal=terminated` plus `restarting harness in 500ms (attempt 1)` for that launch and none for the second, and the launcher's own process log stayed silent across the gap.

Two defects produced that state. The preflight gate was armed *after* `NewSupervisor` had already started its guard loop, so the loop's first iteration could read the unset `gated` field, skip the gate, and spawn before preflight had run at all. The preflight release entry then called `Release()` followed by `Restart()`, and `Restart` sends SIGTERM to any live child: preflight finishing 0.27 s later killed the harness the skipped gate had just started. The supervisor then took its normal 500 ms backoff and respawned, which is why the surviving process only appeared later.

## Decision

The gate is now a construction-time input: `app.New` sets `supervisor.Options.GateFirstStart`, `NewSupervisor` reads it into the `gated` field before starting the loop, and the post-construction `Gate()` method is deleted so no caller can arm the gate after the loop exists.

`Release()` reports whether the call hit the gate. Release entries are split by intent instead of by who calls them: `resumeAfterPreflight` (preflight passed, doctor itself failed, skip) only releases, leaving a healthy running child untouched and waking only stopped or failed states; `resumeAfterEnvChange` (safe mode, fresh runtime directory, automatic repair, deep repair) also restarts a running child, because those paths changed the child environment or on-disk state and only a new process picks the change up.

The gate wait also re-reads `manuallyStopped` after it is released. The snapshot taken before the wait predates any stop that happened while the gate was held, so releasing would otherwise start a harness once before honoring the stop.

## Alternatives considered

**Fix only the release entry and leave `Gate()` in place.** Rejected: preflight would still run concurrently with an already-started harness, so its verdict would arrive too late to matter, and a fatal finding would be reported against a harness that is already loading.

**Add a second constructor such as `NewGatedSupervisor`.** Rejected: two constructors that differ by one field invite picking the wrong one, and the gate is a supervision policy of the same object rather than a variant of it.

**Put the gate flag in `Config`.** Rejected: `Config` describes the child process to guard, while the gate is a property of supervision order; `Options` already carries the supervision parameters.

**Default the gate to armed (fail-closed).** Rejected: `Release()` is documented as a no-op when ungated, and most existing tests and manual paths rely on the loop spawning without a release. Fail-closed would silently hang them instead of surfacing a missing release.

**Let `Release()` alone decide, without a second entry.** Rejected: safe mode and the fresh runtime directory must restart a running child for the new environment to take effect, so a single no-restart entry would break those paths.

**Make `Restart()` skip a child that is already starting or running.** Rejected: restart is the explicit user action for "apply now", and the plugin-reload path depends on it terminating the current process.

**Reproduce the race in a test.** Rejected as primary coverage: once the flag moves into construction, the "gate armed while a child runs" state that caused the incident is unreachable, so tests assert the reachable contracts instead — the gate blocks until release, `Release()` reports whether it hit the gate, a stop taken during the gate does not spawn, and a pure release leaves a running child alone.

## Consequences

Preflight now genuinely precedes the first harness process, and a release can no longer terminate the process it just authorized.

`Release()` returning true is the contract for "this call hit the gate". Callers must not chain `Restart()` after a release that may have hit it; the two release entries exist to keep that decision explicit.

The 13.5 s silent gap seen in that first launch is no longer reachable through this path, since the kill that preceded the respawn cannot happen. Its own cause stays unproven: the two candidates are first-run work after an install and waiting on resources held by the process that was killed, and launcher-side logging of preflight phases and spawn events is still missing, so a recurrence would not be attributable from logs alone.

Known remaining edge: a stop pressed while the gate is held leaves the supervisor waiting in its stopped branch while the state is still `starting` — the first process never spawned, so no exit callback moves it to `stopped`. The shell therefore shows a starting stage, `Start()` declines to act because of its stopped/failed guard, and only restart recovers. Changing that state would also replace the preflight decision page, so it is recorded here rather than changed with this fix.

## Testing

`TestGateFirstStart_BlocksUntilRelease`, `TestRelease_ReportsGateState`, `TestRelease_WithoutGateIsNoop` and `TestGateFirstStart_StopDuringGateDoesNotSpawn` cover the supervisor contracts; `TestResumeAfterPreflight_GatedReleaseStartsOnce` and `TestResumeAfterPreflight_KeepsRunningChild` cover the release entries against a long-running mock harness, asserting the child keeps the same pid and that no termination or restart line reaches the log.

Both fixes were mutation-checked: removing the `manuallyStopped` re-read fails the stop-during-gate test with a live child pid, and restoring the unconditional `Release()` + `Restart()` fails `TestResumeAfterPreflight_KeepsRunningChild` with a changed pid. Both mutations were reverted before committing.

Coverage limit: the incident's own interleaving is not reproduced, only excluded. The packaged client has not been relaunched on a real machine for this change; `go test ./internal/...` in `apps/linyaps/desktop-launcher` is green locally.

## Related

The two rendering notes for this launcher — [WebView hardware acceleration policy](2026-10-18-desktop-launcher-webview-hardware-acceleration.md) and [render-backend selection](2026-10-18-desktop-launcher-webkit-render-backend-selection.md) — own how the embedded page is composited. This note owns when the harness process is started at all.

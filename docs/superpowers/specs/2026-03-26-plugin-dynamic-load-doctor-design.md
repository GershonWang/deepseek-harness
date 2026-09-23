# Plugin dynamic-load check and automatic startup-failure diagnosis

English | [中文](2026-03-26-plugin-dynamic-load-doctor-design.zh.md)

## Background

A third-party plugin may import, at runtime, a dependency the current profile does not have (for example, the Linglong build lacks `@deepseek-ai/dsh-host-apiproxy`), which makes the Cordis Loader crash outright during the import phase and leaves the harness unable to start at all.

Doctor only performs static checks today (bundles resolve, patches compose), so it cannot find this kind of runtime failure. When users hit the problem they see only a blank screen or repeated restarts, with no idea of the cause and no way to fix it.

## Goals

1. Doctor can detect "third-party plugin caused a startup crash" faults and pinpoint the specific bundle
2. When startup keeps failing, the diagnosis triggers automatically, with no manual action from the user
3. During retries the frontend shows a consistent loading state and does not jump back to the guidance page

## 1. Doctor dynamic-load check (plugin-dynamic-load)

### 1.1 Check strategy

A two-step strategy of "load everything + bisect to locate":

1. **Load everything**: use the Cordis Loader to load the complete profile (base + web-app + user patch + all third-party bundles); the check passes when that succeeds
2. **Bisect to locate**: when the full load fails, exclude third-party bundles one by one by bisection until the one causing the crash is found

Why this choice:
- Most users' plugins are fine, so a single full check is fastest
- When something really is broken, log₂N subprocess runs locate it, better than loading bundles one at a time
- The existing `bisectThirdPartyBundles()` helper can be reused

### 1.2 The subprocess probe script

Add `packages/support/doctor/src/loader-probe.ts` as a standalone executable script.

**Arguments**:
- `--profile <name>` — profile name (such as `web`)
- `--dsh-home <path>` — harness home path
- `--include <bundle>` — may be given multiple times; load only these third-party bundles (used by the bisection)
- `--timeout <ms>` — timeout, 10000ms by default

**Exit codes**:
- `0` — load succeeded
- `1` — load failed (the error stack goes to stderr)
- `2` — timed out

**Load depth**: it goes through the complete Cordis Loader composition flow (compose + load the plugin tree) but starts no HTTP service and listens on no port. It disposes immediately once loading finishes (every plugin's `apply` has run), guaranteeing no side effects.

Why go through the complete Loader rather than merely importing the entry file:
- It can detect faults at the cordis.yml configuration level (referencing a nonexistent service, a malformed plugin export, and so on)
- It complements the coverage of the static checks (static checks the patch composition, dynamic checks the actual load)
- It truly verifies "can this bundle start together with the current profile"

### 1.3 Bisection implementation

Reuse the existing `bisectThirdPartyBundles()` helper framework, changing each decision to:
1. Spawn a subprocess, passing the current candidate bundle list (through `--include`)
2. Wait for the subprocess to exit or time out
3. Exit code 0 → this set is fine; non-zero → this set is broken

The candidate list comes from every third-party bundle parsed out of the user's `cordis.patch.yml`.

If several bundles are broken at once, repairing the first triggers detection again, looping until everything passes or the candidates run out.

### 1.4 Check registration

| Field | Value |
|---|---|
| id | `plugin-dynamic-load` |
| name | Plugin runtime compatibility |
| category | `plugin` |
| severity | `fatal` |
| fixable | `true` |
| suggestedLevel | `2` |

### 1.5 Repair logic (L2)

1. Locate the bundle that causes the crash (full probe + bisection)
2. Back up the profile's `package.json` (byte level, `writeFileAtomic`) into the doctor backup directory for this repair
3. Use `writeProfileManifest` to remove the culprit from `dsh.profile.bundles` — consistent with the exclusion model of `DSH_SAFE_MODE=plugins` (third-party bundles are a profile layer, not part of the user patch file)
4. Re-run the dynamic-load check to verify
5. Verification passes → the repair succeeded; still failing → restore the backup and report the failure reason

## 2. Automatic diagnosis on startup failure

### 2.1 Trigger point

When `supervisor` enters the `StateFailed` state (the 30-second startup timeout circuit breaker), it triggers one doctor diagnosis automatically.

Trigger conditions:
- Container mode (not external mode)
- The status goes from not-failed to failed
- Not a user-initiated stop
- No automatic diagnosis has run yet in this failure cycle (avoiding repeated triggers)

Why StateFailed is the trigger point:
- Exponential-backoff retries have already happened, so it is not a transient fault
- The user is looking at the startup failure screen right then and needs the diagnosis result
- It cannot misfire (a normal start or a manual stop triggers nothing)

### 2.2 The existing retry mechanism

The supervisor's current retry parameters:

| Parameter | Default |
|---|---|
| Initial restart delay | 500ms |
| Maximum restart delay | 10000ms |
| Startup timeout (circuit breaker) | 30000ms |

Backoff policy: exponential backoff `500 × 2^(n-1)` ms, capped at 10s. Once cumulative startup failure exceeds 30s it enters `StateFailed` and stops retrying.

### 2.3 Go-side changes

Add to the `App` struct:
- Startup-failure diagnosis state tracking (to avoid repeated triggers)
- Carry the diagnosis running state or result in the status event

New fields on the status event `FrontendStatus`:
- `StartupDiagnosing bool` — whether the automatic startup-failure diagnosis is running
- `StartupDoctorError string` — the doctor command error of the automatic diagnosis (non-empty means the diagnosis itself failed)

The automatic diagnosis runs in a background goroutine and pushes its result through the status event. The frontend can sense it through status changes.

### 2.4 Frontend changes

When it detects `State === "failed"` and the automatic diagnosis has finished:
1. Open the doctor diagnosis window automatically
2. Show a hint bar at the top: "startup failure detected, diagnosed automatically for you"
3. If the `plugin-dynamic-load` check failed, highlight it and emphasize the "moderate repair (L2)" button

## 3. Startup UI polish

### 3.1 Main-stage status mapping

| Status | Main stage shows |
|---|---|
| External connected | iframe (external URL) |
| Container running | iframe (container URL) |
| Container starting | Startup loading page |
| Container failed | Startup failure page |
| Container stopped (manual stop) | Guidance page |

### 3.2 Startup loading page

Centered layout, containing:
- Brand mark
- The "starting..." copy
- A spinner animation
- An optional hint at the bottom: "if there is no response for a long time, try safe mode"

### 3.3 Startup failure page

Centered layout, containing:
- A failure icon
- The "startup failed" title
- An error message summary (LastExit)
- Two primary action buttons:
  - **Diagnose the problem** — opens the doctor modal
  - **Start in safe mode** — calls StartSafeMode directly
- A small line of text at the bottom: see the full log at `~/.cache/dsh-desktop/harness.log`

### 3.4 Status debounce during retries

During the retry delay the supervisor has already let the process exit and the status is `stopped`, flipping back to `starting` only after the delay ends. That makes the main stage flicker between the guidance page and the loading page during retries.

Solution: **a 1-second frontend debounce**.

- When the status changes from `starting` to `stopped`, do not render the guidance page immediately
- Wait 1 second; if the status flips back to `starting` within that second, do not switch
- If it is still `stopped` after 1 second, render the interface for stopped

Why debounce in the frontend:
- The Go state machine needs no change and keeps clear semantics (stopped simply means the process has stopped)
- It is simple to implement, changing one place in the frontend
- It does not affect other logic that depends on the status

## 4. Edge cases

| Scenario | Handling |
|---|---|
| No third-party bundles | The check passes directly |
| Several bundles broken at once | Locate and repair them one at a time, looping until it passes |
| The subprocess load times out | Treated as a failure, with the error message marked "load timed out" |
| Linglong sandbox environment | The subprocess starts through a normal spawn; node is available inside the sandbox |
| User-initiated stop | Does not trigger the automatic diagnosis (the manuallyStopped flag) |
| External mode | Does not trigger the automatic diagnosis |
| The doctor command itself fails to run | The frontend shows "diagnosis failed" and does not pop up automatically |

## 5. List of changed files

### The doctor package
- `packages/support/doctor/src/checks/plugins.ts` — adds the plugin-dynamic-load check
- `packages/support/doctor/src/loader-probe.ts` — adds the subprocess probe script
- `packages/support/doctor/src/bisect.ts` — adapts/extends the bisection (as needed)
- `packages/support/doctor/tests/` — new tests

### desktop-launcher (Go)
- `apps/desktop-launcher/internal/app/app.go` — trigger doctor automatically on startup failure; extend the status fields
- `apps/desktop-launcher/internal/supervisor/supervisor.go` — may need minor status-event adjustments

### desktop-launcher (frontend)
- `apps/desktop-launcher/frontend/index.html` — adds the startup loading page and startup failure page structure
- `apps/desktop-launcher/frontend/app.js` — status rendering adjustments + automatic pop-up + debounce
- `apps/desktop-launcher/frontend/styles.css` — new styles

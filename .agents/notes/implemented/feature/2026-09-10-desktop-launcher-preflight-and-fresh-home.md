# Agent Note: Desktop launcher preflight gate and fresh-home fallback

Status: implemented

English | [中文](2026-09-10-desktop-launcher-preflight-and-fresh-home.zh.md)

## Problem

The desktop launcher spawns harness and only diagnoses **after** a failure: the supervisor watches for fatal-load patterns, enters `StateFailed`, and a background doctor runs once per failure cycle. By then the user has already watched several start-stop cycles. Third-party plugins that broke against a moving harness (the DSH development-phase reality) produce exactly this loop, and the doctor repair — while it can fix the underlying issue — had no path into the launch decision itself. There was also no last-resort recovery: `DSH_SAFE_MODE` requires shell-level knowledge, and nothing could start harness when the user data directory itself was broken in a way doctor cannot repair (a half-installed profile `node_modules`, corrupt `storages/*.json`, credentials).

## Decision

The launcher now gates harness startup behind a **preflight check** and owns a **fallback launch** path:

- **Gating** (`internal/supervisor`): `Config` gains `Env` (per-spawn child environment, nil inherits) and the supervisor gains `Gate()`/`Release()` — after `Gate()`, the run loop waits for the first `startCh` token, so `New()` can hold the first spawn until the preflight verdict. `SetEnv` swaps the child environment between spawns.
- **Preflight orchestration** (`internal/preflight` package + `internal/app/preflight.go`): the app gate runs `dsh doctor --json --quick` (static checks only) before any spawn, strips `DSH_SAFE_MODE` and pins `DSH_HOME` to the real home. A healthy report releases the gate. A report with fatal issues first auto-applies level-1 repairs (reversible: `.env` bootstrap-only lines) and re-checks; remaining fatal issues park the gate in `needs-confirm` with the issue list rendered in a dedicated stage page. The user then picks: deep repair (`--repair 2`, includes the live-load probe, full re-check), skip and start (existing post-mortem path stays), `config`-level safe mode, or a fresh home.
- **Fresh-home fallback** (`StartFreshHome`): injects `DSH_HOME=<home>/.dsh-fallback` for subsequent spawns. Harness bootstraps a complete default environment there (template profiles, default settings). The original `~/.dsh` is never written or deleted; the fallback directory is persistent so a downgraded session keeps its own configuration. Credentials are deliberately **not** copied — they are API keys, and duplicating them enlarges the leak surface; the fresh environment inherits ambient env vars or asks for reconfiguration.
- **Timing contracts**: quick diagnosis is bounded (15s), repairs 60s, full re-checks 3 minutes; every doctor subprocess is registered with the existing doctor tracking so shutdown cancels it. A doctor that itself fails or times out never blocks launch — the gate releases and the existing post-mortem diagnosis remains the net. The preflight is best-effort by design.

- **Convergence**: `RunDoctor`/`RunDoctorRepair` (the doctor panel) now take their argv and child environment from the same `preflight.Runner`, so the strip-`DSH_SAFE_MODE`/pin-`DSH_HOME` rules have one home.

## Alternatives considered

**Run the full doctor (with the live-load probe) on every launch.** The probe boots the whole plugin tree in a subprocess and can take up to a minute; paying it on every healthy launch is unacceptable. Quick mode (`--quick`) skips only the probe and runs sub-second; the full probe is spent where it pays — after a suspicious quick report or under the user-confirmed deep repair.

**Auto-degrade without asking (safe mode first, fresh home next).** The repairs that fix plugin incompatibility remove third-party plugins and can reset `settings.yaml`; switching to a fresh home silently makes sessions, settings, and credentials unavailable. Both cross the "user-visible consequence without consent" line, so the level-1 auto repairs are the only unattended step.

**Copy credentials into the fallback home.** Rejected on the security line above; the fallback launches with ambient `DEEPSEEK_API_KEY` when present.

**Reuse `DSH_SAFE_MODE=plugins` as the recovery recommendation instead of adding safe-mode `config` to the preflight UI.** `plugins` keeps the user patch layer, which is itself a failure source (patch targets renamed across versions); the preflight already knows whether the surviving failures sit in the config layer, so it offers `config` directly while the pre-existing failed-page button keeps `plugins`.

## Consequences

A broken user-data directory now cannot wedge the launcher: the gate times out into `error` and releases, and the fresh-home path bypasses every user-owned layer without touching them. The cost is launch latency on the happy path — one quick doctor subprocess (sub-second in practice, bounded at 15s) before the first harness spawn — and a larger launcher surface: the stage page, four new bound methods, and `Env`/gate semantics in the supervisor. The fallback home accumulates state across downgraded sessions; it is not cleaned automatically, and users migrating data back move files by hand.

## Testing

`go test` in `apps/desktop-launcher` pins the gate semantics and env swap (`internal/supervisor/gated_env_test.go` with a canary-through-ready-URL mock), doctor argv/env assembly plus JSON parsing and the severity/fixability buckets (`internal/preflight/preflight_test.go`), and the orchestration state machine with mock doctor scripts: healthy release, fatal-without-autofix parking in `needs-confirm`, doctor failure not blocking, fresh-home env injection clearing safe mode (`internal/app/preflight_test.go`). `node --test frontend/test-app.cjs` asserts the stage switch between loading and preflight pages, issue-list rendering with kind badges, the actions row visibility rules, and the fresh-home badge in the server dialog.

## Related

The post-mortem diagnosis this gate reuses and front-runs: [Doctor live-load check for third-party plugins](2026-08-28-doctor-plugin-dynamic-load.md).

# Agent Note: Startup progress on the desktop launcher loading page

Status: implemented

English | [中文](2026-09-15-desktop-launcher-startup-progress.zh.md)

## Problem

From spawn until the harness prints its ready line the launcher shows a spinner page and nothing else, and that window is the entire wait: on the packaged build it measured 5.14–5.62 s, of which roughly 4 s is plugin mounting. Nothing on the page moves, so the wait reads as a second screen that does nothing — AUDIT item 26 recorded the missing feedback as deliberately deferred once the client-modules work had removed its main cause, the length.

The shell can observe only three facts by itself: the process was spawned, the child produced its first output line, and the ready line arrived. Everything between them happens inside the harness process, so no shell-side signal can turn that gap into progress.

## Decision

The launcher writes a small reporter plugin into its runtime directory — next to `supervisor-overlay.yml` — and injects it through the same `--patch` overlay. The reporter counts loader entries whose fibers have settled and writes `dsh-desktop: startup <loaded>/<total>` to stderr. `Supervisor` parses that line into a `domain.StartupProgress` snapshot, and `App` maps the snapshot into four phases plus a count that reaches the loading page over a throttled `startup:progress` event; the 1 s status snapshot carries the same view.

Each phase is triggered by an observed fact, never by elapsed time: `starting` (spawned, no output yet), `loading` (first child output, no count yet), `plugins` (counts reported, `Loaded < Total`), `serving` (every counted entry settled, only the port listen remains). With no reporter — an older in-flight build, a host where the plugin file could not be written — the first two phases still render, the progress block stays hidden, and the page keeps its pre-change copy.

The count is trustworthy for one reason worth recording: the denominator is `loader.entries()` filtered to rows that actually mount (not groups, not disabled), and it is stable through boot. The numerator is settled fibers, so it only grows, and reporting stops at the first report that reaches the total — this channel covers the startup window, and harness keeps recomposing its tree afterwards.

Measured on the shipped client (the 16:07 run of 2026-09-15 in `~/.cache/dsh-desktop/harness.log`): the first line lands 1.24 s after spawn at `0/133`, the count first reaches the total at 5.65 s as `135/135`, and the ready line follows at 5.77 s — so the loading page spends about 4.4 s on a moving count and 0.12 s in `serving`. In the dev checkout the same reporter reported `0/127` at 1.45 s and `129/129` at 3.71 s against a ready line at 3.73 s; the packaged build reported `0/127` at 1.15 s and `129/129` at 3.27 s against 3.3 s.

`fiber.await()` is what makes the numerator cheap and dependency-free: it settles when a fiber activates or fails, both of which mean the row no longer blocks startup. The reporter needs no import, no timer service, and no numeric `FiberState`, which matters because the file is loaded from `~/.cache/dsh-desktop/` where bare specifiers do not resolve.

## Alternatives considered

**Emit invented stages on a timer.** A progress bar that moves because time passed would tell the user something untrue about a wait whose length varies with profile and machine; AUDIT item 26 already rejected the idea, and the whole point of this change is that the loading page stops being a decoration. Rejected.

**Only report the shell-side facts (spawn, first output, ready).** This is the cheap variant that touches no harness code, and it stays as the fallback path. It cannot cover the dominant segment: measured 4.10 s of a 5.62 s boot sits between "first output" and "all entries settled", and no shell-visible event lands inside it. Rejected as the primary mechanism.

**Add the reporting to upstream `app-boot` behind a flag.** It would serve every surface, including plain `dsh web` in a browser, and would not put launcher-owned code inside the user's plugin tree. Rejected for this scope: it changes an upstream package, its recorded outputs, and its docs for a desktop-shell presentation problem, while the injected reporter is removable with one row and is verified end to end today. This is the path to take if another surface ever needs the same feedback.

**Turn on the loader's `enableLogs` and inject `logger-console`, then parse the vendor's apply logs.** Reuses machinery instead of adding a file. Rejected: the root include's config would have to be replaced wholesale to set `enableLogs` (coupling to its `path`), the whole tree would start logging including steady-state HMR churn, and the line format would be vendor-owned rather than a contract this repository documents.

**Poll `ctx.loader.entries()` and count active fibers.** Avoids subscribing to `internal/plugin`. Rejected: deciding "active" needs the numeric `FiberState.ACTIVE` value inside a file that cannot import cordis, and a magic number tied to a vendored enum is exactly the kind of silent breakage a later vendor sync would cause; `fiber.await()` is public API and needs no constant.

## Consequences

The reporter is launcher-owned code executing inside the user's harness tree, and it is visible there: an entry with id `dsh-desktop-startup-progress` whose name is an absolute path appears in the plugin tree and in diagnostics. That is the cost of the only in-process vantage point that does not modify upstream code, and it is bounded by the two guards below.

An entry that throws aborts boot — the harness is fail-loud about entry activation. The reporter therefore imports nothing, wraps `apply` in a guard that reports a single diagnostic line instead of propagating, and lives beside the overlay that references it; when either file cannot be written the shell omits the row and falls back to the coarse phases rather than failing a start.

Roughly the first 1.2 s of a boot carry no counts: the reporter cannot report before it is itself mounted, and in the shipped client that first line is `0/133`. That window is why the `loading` phase exists — it is a real fact ("the child is talking, counts have not started") rather than filler.

The denominator can grow by a row or two before completion, so the ratio can dip by a percentage point; the page renders the bar monotonically and prints the reported numbers verbatim, so the display never runs backwards while the counter stays exact.

Reporting must stop at completion, and the first shipped run proved why: the reporter originally kept counting, so the ready state's own recomposition (client HMR, the user patch-layer watcher, the directory picker) added rows that were counted while the denominator was measured from the live tree, producing 26 log lines whose numerator exceeded it — the last was `161/136`. The loading page was unaffected (it is replaced by the iframe at ready, and the frontend clamps the ratio), but the rows polluted the startup-timing record this repository reads from that same log and contradicted the line's own contract. Stopping at the first count that reaches the total keeps the final line honest at 100 %, and an unusual boot whose counts never complete still reports until it does.

The progress channel exists only when the desktop launcher spawns the harness. A browser tab served by `dsh web` still shows the harness's own client-side loading page, which is a different surface and was out of scope by decision.

## Testing

`internal/supervisor` pins the stderr contract (pattern table including malformed and unrelated lines), the writer wiring through a real child (`testdata/mock-progress.sh`, which also asserts `OutputAt` lands inside the spawn window), and the shared line-splitting sink across write boundaries. `internal/appenv` pins the overlay row, its YAML-quoted absolute path, coexistence with the market patch, the plugin's prefix contract, and that the plugin source imports nothing. `node --test internal/appenv/startup_progress.test.mjs` drives the plugin against a hand-written context: seed plus per-entry settles, groups and disabled rows excluded from the denominator, one count per entry id, the stop-at-completion rule (a post-ready row must produce no line), a rejected activation counting as settled, a missing loader staying silent, and a failing reporter writing one `unavailable` line instead of throwing. `internal/app` pins the phase table (including `0/0`, and every non-starting state mapping to the zero view) and the snapshot comparison that makes progress changes reach the frontend.

`node --test frontend/test-app.cjs` drives the DOM stub: counts and bar width, the `serving` copy, the no-report and missing-field fallbacks, the monotone ratio when the denominator grows, and the reset after leaving the starting state. `preview.mjs verify` measures the loading page in real Chromium — the 50 % and 100 % widths must match the track, the block must stay inside the stage, a long counter must stay on one line, and the block must not overlap the hint — and this invariant was confirmed to fail when the bar was capped to 20 px.

Both harness launch modes were run by hand with the real overlay and reporter: the dev build (`apps/cli/lib/bin.js`) and the packaged build (`linglong/output/binary/files/harness/lib/bin.js`, which resolves entry names through `HostResolvedRootInclude`) both loaded the injected absolute path, reported `0/127` through `129/129`, and emitted no line after the ready line.

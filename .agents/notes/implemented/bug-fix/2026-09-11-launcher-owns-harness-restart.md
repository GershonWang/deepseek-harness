# Agent Note: The launcher owns harness restart

Status: implemented

English | [中文](2026-09-11-launcher-owns-harness-restart.zh.md)

## Problem

Clicking the plugin market's one-click restart sometimes left the container's harness unable to start, and the launcher's diagnosis dialog then reported no fault at all. The cause was not a port that failed to release but two independent restarters competing for the same port.

The launcher spawns `dsh web --port <port>` from a port reserved once per application run, and its supervisor restarts that child with the same argv after any exit ([linglong port](../feature/2026-08-14-desktop-launcher-linux-linglong.md)). The `dsh-market` plugin's `/dsh-market/restart` endpoint spawns a detached helper that relaunches harness from `process.argv.slice(2)` — the same argv, therefore the same `--port`. Both then bind one address: whichever wins, the other exits with `EADDRINUSE`. Observed in `/tmp/dsh-market-restart-2026-09-11T01-00-23.err.log`, where the replacement harness died on `listen EADDRINUSE: address already in use 127.0.0.1:41103`.

Which side wins decides which failure the user sees. When the market wins, the supervisor keeps retrying an occupied port with exponential backoff until `StartupTimeoutMs` expires, enters `StateFailed`, and the user gets "startup failed" — while the doctor inspects only the child the supervisor spawns and never reads the helper's `tmpdir` log, so it finds nothing wrong. The race also leaves orphans: the harness the helper started outlives its parent (`ppid=1`), is invisible to the launcher, and shares `~/.dsh` with the supervised instance. Two live harnesses on ports 40195 and 40657 were confirmed side by side on one machine.

The plugin's own guard does not cover this deployment. `restartAllowed` falls back to `detectedSupervisor()`, which recognizes only systemd, launchd, and pm2 by reading `INVOCATION_ID`/`JOURNAL_STREAM` and checking `ppid === 1` or a `systemd` parent. An in-process Go supervisor is none of those: probed on the affected host, `/dsh-market/status` reported `supervisor: null`, so the button stayed enabled.

## Decision

The launcher injects its own patch overlay on every harness spawn, setting `allowRestart: false` on the `dsh-market` row. `appenv.harnessArgs` assembles the argv for all four entry-point branches — the override binary, the packaged `harness/lib/bin.js`, the repo's dev `apps/cli/lib/bin.js`, and the bare `bin.js` fallback — and appends the overlay written by `writeSupervisorOverlay` into the launcher runtime directory. Centralizing argv in one function is what keeps a branch from silently losing the protection.

Flag order is load-bearing and not obvious: the `web` subcommand enables `passThroughOptions`, and `--port` belongs to the web app rather than the launcher, so everything after it is forwarded verbatim — a `--patch` placed there is never parsed and fails with `error: unknown option '--patch'`. The launcher's own flags therefore precede `--port`.

Because the market can no longer restart harness, the launcher gains the entry point it now owns: the `App.RestartServer` bound method calls `supervisor.Restart`, and the server dialog exposes a 重启 button beside 启动/停止 governed by a new `CanRestart` status field. Plugin-install reloads have a supported path instead of a removed one.

## Alternatives considered

**Have the supervisor yield to an externally started harness.** After its child exits, the supervisor would probe the port, adopt a healthy listener it did not spawn, and report that URL. Rejected: it requires a new lifecycle state, health checks, and the ability to stop a non-child process, and it makes the process owner adapt to a second restarter rather than removing the conflict. The existing `ModeExternal` does not cover it — that mode is entered by a user-supplied URL through `Connector.BeginExternal`.

**Teach `detectedSupervisor()` to recognize the launcher.** This is where the fix belongs upstream, but the market is a third-party package resolved into the profile's `node_modules`; a local edit is overwritten by the next install and only upstream can carry it. Recording the limitation here is preferable to a patch that disappears silently.

**Tune the timing so one restarter always wins.** The supervisor's Go `exec` beats the helper's Node startup plus port polling, so it usually wins today. Rejected: the outcome would still depend on scheduling, the losing process would still crash and log, and the orphan it can leave behind is what makes the state unmanageable.

**Drop the fixed port and let each harness choose its own.** Removing `--port` separates the two bind attempts but not the two harnesses: both would live on, sharing `~/.dsh`. That trades a detectable crash for silent concurrent state, which is worse.

## Consequences

Harness lifecycle has one owner again: the supervisor is the only process that spawns harness, so the port race cannot occur and no restart path can strand an unsupervised instance. The plugin market's restart button is gone by design — `/dsh-market/status` now reports `restart: false` — and plugin changes take effect through the launcher's 重启 button.

Two facts are load-bearing for future work. The overlay addresses a third-party contract by row id `dsh-market` and key `allowRestart`; if upstream renames either, the patch matches no row and the include emits a warning rather than failing, so the protection degrades silently and a launcher-side check is the way to notice. And because `--patch` overlays apply after the profile's user layer, a user cannot re-enable the market's button through their own configuration; anyone who wants that freedom must change how the launcher injects the setting.

The alternative of disabling the market without adding a replacement entry point was rejected: it would leave plugin-install reloads with no path at all short of quitting the application.

## Testing

`go test ./...` in `apps/desktop-launcher` pins argv assembly: the overlay path and flag order across entry-point shapes, the overlay's row id and `allowRestart: false` content, and the degraded form (no `--patch`) when the overlay cannot be written. `node --test frontend/test-app.cjs` covers the restart button — enabled and calling `RestartServer` in the running state, disabled when stopped. Beyond the suites, the composed config was dumped with `dsh web --patch <overlay> --dump-config`, which reports the row as `patched by <overlay>` with `config: allowRestart: false`, and a real boot with that overlay answered `/dsh-market/status` with `restart: false` and `supervisor: null`.

## Related

- [Desktop launcher on Linux/linglong](../feature/2026-08-14-desktop-launcher-linux-linglong.md) owns the supervisor's spawn, ready-line, and backoff mechanism this note constrains.
- [Preflight and fresh home](../feature/2026-09-10-desktop-launcher-preflight-and-fresh-home.md) owns the supervisor's `Gate`/`Release`/`SetEnv` semantics.

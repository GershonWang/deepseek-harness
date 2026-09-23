---
description: "Diagnose and repair a DeepSeek Harness installation: environment, configuration, plugin, and session-data checks that report structured results, isolate a failing plugin bundle, and back up whatever they fix."
kind: "package-library"
---

# @dsh-desktop/doctor

English | [中文](README.zh.md)

## Summary

`@dsh-desktop/doctor` inspects one harness home and reports what is broken: Node and disk prerequisites, the bootstrap environment, `settings.yaml` and user patch YAML, profile-bundle resolvability, and session or attachment data. The Desktop launcher runs this package's `cli.js` as its pre-start preflight and again from its doctor panel. A package can contribute a check with `registerCheck`. Diagnosis never writes: `runRepair(level)` re-runs the checks and applies only fixes whose suggested level the caller authorizes, after copying the affected state into `~/.dsh/backups/doctor-<timestamp>`.

## Table of Contents

- [Use this package](#use-this-package)
- [Understand the implementation](#understand-the-implementation)
- [Further Exploration](#further-exploration)
- [Known Limitations and Deferred Work](#known-limitations-and-deferred-work)
- [Dev Note](#dev-note)

-----

<a id="use-this-package"></a>
## Use this package

### When to use it

Reach for this library when a harness installation fails to start, misbehaves after a plugin install, or must be checked before it starts. The only consumer in this repository is the Desktop launcher: it runs `cli.js` before it starts the harness and strips its own `DSH_SAFE_MODE` from that child, so the preflight reports why the previous start failed instead of inheriting the shell's override. A package reaches for `registerCheck` when it owns a failure mode that the built-in checks cannot see.

Use a plugin entry point instead when the work is part of an ordinary harness run: this package is a library that a launcher, CLI, or test invokes, and it registers no profile layer.

### Entry point

```ts
import { runDiagnosis, runRepair, registerCheck } from '@dsh-desktop/doctor'

const report = await runDiagnosis()                 // reads an explicit path, else $DSH_HOME, else ~/.dsh
const failed = report.checks.filter(check => !check.result.ok)
const repair = await runRepair(1)                   // authorize fixes at level 1 (mild); 2 and 3 escalate
```

`runDiagnosis(dshHome?, { quick })` resolves every registered check concurrently and returns the report: one entry per check in registration order plus a `summary` counting `total`, `ok`, `failed`, `fatal`, and `fixable`. A check that throws is contained by its own result instead of failing the run, so one broken plugin cannot hide the other findings. The CLI exposes the same surface through `cli.js`, which the launcher spawns as `node <doctor>/lib/types/cli.js` (packaged: `<prefix>/harness/doctor`; from source: `apps/desktop-launcher/doctor`):

```sh
node <doctor>/lib/types/cli.js            # human-readable report
node <doctor>/lib/types/cli.js --json     # the same report as machine-readable JSON
node <doctor>/lib/types/cli.js --quick    # skip the live loader probe (fast static preflight)
node <doctor>/lib/types/cli.js --repair 2 # diagnose, then apply fixes whose suggestedLevel is at most 2
```

`--repair [level]` takes 1 (mild), 2 (moderate), or 3 (destructive) and defaults to 1 when the level is omitted; any other value is rejected before diagnosis runs. Failure is data, not an exception: a report with `summary.fatal > 0` means the installation cannot start until the named check passes, and `summary.fixable` counts the failures this run could repair.

-----

<a id="understand-the-implementation"></a>
## Understand the implementation

<details>
<summary>Implementation internals — click to expand</summary>

The framework keeps one process-wide check registry, runs every entry concurrently against one resolved home, and treats repair as a second pass over the same report.

### Source map

| File | Role |
|---|---|
| [`src/index.ts`](src/index.ts) | Check registry, `runDiagnosis`, `runRepair`, backup retention |
| [`src/cli.ts`](src/cli.ts) | Argument parsing, human-readable rendering, and process exit codes for the launcher's subprocess |
| [`src/types.ts`](src/types.ts) | Report, check, severity, and repair-level types |
| [`src/checks/env.ts`](src/checks/env.ts) | Node version, free disk space, bootstrap environment |
| [`src/checks/config.ts`](src/checks/config.ts) | `settings.yaml` and user patch YAML validity |
| [`src/checks/plugins.ts`](src/checks/plugins.ts) | Bundle resolvability, patch composability and targets, third-party inventory, live probe |
| [`src/checks/data.ts`](src/checks/data.ts) | Session-log integrity, corrupt-session archival, attachment storage |
| [`src/loader-probe.ts`](src/loader-probe.ts) | Standalone subprocess that really boots one profile and reports by exit code |
| [`src/auto-disabled.ts`](src/auto-disabled.ts) | Cross-process record of the bundles doctor disabled, read by the Desktop shell |
| [`src/bisect.ts`](src/bisect.ts) | Binary-search isolation of the third-party bundle that breaks a profile load |
| [`src/bisect-by.ts`](src/bisect-by.ts) | Generic subset bisection the plugin isolation builds on |
| — | No runtime invariant companion is published; the framework owns no event stream or mutable runtime data of its own, and its registration, report, and repair contracts are enforced by unit tests. |

### Check set

Eleven checks run by default, grouped by the category that names their subject. `env-node-version`, `env-disk-space`, and `env-bootstrap-env` cover the runtime precondition and reject bootstrap variables that a discovered file must not set. `cfg-settings-yaml` and `cfg-user-patch` parse the user configuration. `plugin-bundles-resolvable` proves every bundle a profile declares resolves to an installed layer, `plugin-patch-composable` and `plugin-patch-targets` compose the patch list and check its targets, `plugin-third-party-list` reports the inventory, and `plugin-dynamic-load` adds the twelfth check by booting one profile through the real Loader in a subprocess. `data-sessions-integrity` and `data-attachments` cover stored session logs and attachment files.

Severity separates a precondition from a defect: a `fatal` check blocks startup, an `error` check breaks a feature, a `warning` check degrades it, and an `info` check only reports. `--quick` removes exactly `plugin-dynamic-load`, which is the only check that spawns the probe, so a static preflight stays fast while a full run can still observe a plugin that fails only at mount time.

### Repair

`runRepair(level)` diagnoses first, creates one `backups/doctor-<timestamp>` directory for the whole run, and walks the failing entries in registration order. A failure is skipped when it is not `fixable`, when its `suggestedLevel` exceeds the requested level, or when its check declares no `fix`; otherwise the fix receives the backup directory so it can preserve pre-repair state before it writes. Repairs are therefore ordered, level-bounded, and reversible from the same report that requested them. Doctor keeps the five most recent backup directories and prunes older ones, sorting by the timestamp in the name.

</details>

-----

<a id="further-exploration"></a>
## Further Exploration

Read these pages when you need the launch path that consumes a report or the profile model that produces one.

- [Desktop launcher](../README.md) — the pre-start preflight and doctor panel that spawn `cli.js`, and the `DSH_SAFE_MODE` stripping they rely on.
- [Boot package](../../../packages/boot/app-boot/README.md) — profile resolution, patch composition, and the boot environment rules the env checks assert.
- [Plugin manager](../../../packages/boot/plugin-manager/README.md) — installing and enabling the bundles whose resolvability doctor checks.
- [Architecture](../../../docs/architecture.md) — how a profile, its bundles, and its layers compose at launch.

-----

## Known Limitations and Deferred Work

<a id="known-limitations-and-deferred-work"></a>

These limits define what one run can and cannot prove. They are current package constraints, not a task backlog.

- **The live probe boots one profile** — `plugin-dynamic-load` is the only check that observes mount-time failure, and `--quick` removes it; a static run cannot see a plugin that fails only when it mounts.
- **Repair authority belongs to the caller** — a fix runs only when its `suggestedLevel` is within the requested level, so `--repair 1` deliberately repairs less than a full run reports as fixable; level 3 fixes delete state and are never implied.
- **Backups are capped, not archived** — doctor keeps the five most recent `backups/doctor-*` directories; anything older is pruned, so a repair's preserved state must be copied elsewhere when it must outlive five further repair runs.
- **The third-party inventory reports only** — `plugin-third-party-list` names installed third-party bundles at `info` severity; deciding which of them to disable remains a human or launcher decision.

<a id="dev-note"></a>
### Dev Note

<details>
<summary>Working context for maintainers — click to expand</summary>

This package is private to the Linglong fork. It lives in `apps/desktop-launcher/doctor` instead of `packages/`, so an upstream merge never touches it, and it is not a pnpm workspace member: `node apps/desktop-launcher/tools/doctor-build.mjs` builds it and `linglong/prepare-offline.sh` stages the result into `<prefix>/harness/doctor`. The launcher spawns `cli.js` directly; no `dsh` subcommand mounts it, so no upstream CLI wiring exists to conflict.

</details>

# Agent Note: Doctor live-load check for third-party plugins

Status: implemented

English | [中文](2026-08-28-doctor-plugin-dynamic-load.zh.md)

## Problem

A third-party plugin can import a dependency the current installation no longer provides (for example the Linglong build lacks `@deepseek-ai/dsh-host-apiproxy`), which crashes the Cordis Loader during the import phase and prevents the harness from starting at all. The doctor's static checks — bundle resolvability and patch composition — cannot see this failure: the bundle resolves and the patch composes, but the plugin module import fails at runtime. Users get a white screen or endless restarts with no explanation of which plugin broke.

The doctor's plugin category inspected only static facts: whether profile bundles resolve, whether patch layers compose cleanly, and whether user patch targets exist. None of these execute plugin code.

## Decision

Add a live-load check (`plugin-dynamic-load`, category `plugin`, severity `fatal`) to `@deepseek-ai/dsh-doctor` that boots the real plugin tree and reports via a subprocess probe.

**Loader probe** (`packages/support/doctor/src/loader-probe.ts`): a standalone script that resolves the profile, heals the module fallback, writes the empty root `cordis.yml`, composes the patch stack, calls `boot()` with `provideCmdline` (including `appReady`, which `sdk-app`'s `exitOnStdinEnd` requires), disposes the tree, and exits 0 on success, 1 on load failure (reason on stderr), or 2 on timeout. The `--include` flag loads only the named third-party bundles so the binary search can test subsets.

**Bisection** (`packages/support/doctor/src/bisect-by.ts`): a generic `bisectBy<T>(items, isBad)` extracted from the profile-specific bisection, whose predicate answers "is the bad behavior present with this subset active?". The existing `bisectThirdPartyBundles` now delegates to it.

**Check and repair** (`packages/support/doctor/src/checks/plugins.ts`): the check lists third-party bundles via `loadProfile`, boots them all once; on failure it bisects to the single culprit with `bisectBy(names, subset => probe(subset).code !== 0)`. When a culprit is found, the report names it with `fixable: true`, `suggestedLevel: 2`, and the full probe output as detail. The L2 repair removes the culprit from the profile manifest's `dsh.profile.bundles` (third-party bundles are profile layers, not user patch rows), after a byte-exact backup of `package.json` to the repair backup directory; it re-boots to verify and restores the backup bytes on failure.

## Why manifest edit, not patch-file disable

Third-party bundles live in `dsh.profile.bundles` in the profile's `package.json`; they never come from the user's `cordis.patch.yml`. Commenting out lines in the patch file cannot disable a bundle layer. Removing the bundle from the manifest is also the same exclusion `DSH_SAFE_MODE=plugins` applies, and `writeProfileManifest` is the supported machine-edit API (the launcher itself rewrites the manifest on load). A `disabled: true` patch per entry would require enumerating every patch id, would collide across layers on duplicate ids, and would rewrite the user's own authoring surface (web profile reloads patches live via HMR).

## Deferred

None: the desktop launcher auto-diagnosis and startup pages landed with the check. The desktop supervisor runs a background doctor when it enters `StateFailed` (container mode), the frontend shows diagnosing progress on the failure page, and auto-opens the doctor report once the result is ready.

## Testing

`loader-probe.spec.ts` (5 tests) spawns the probe against scratch homes: fresh profile loads, a bad bundle importing a missing dep fails both full load and `--include` subset with the dep name on stderr, an `--include` subset excluding the bad bundle loads, an unknown `--include` exits 1 with a loud message, and a never-settling top-level-await load reports timeout. `plugins-dynamic-load.spec.ts` (10 tests) covers the check's registration, no-third-party pass, culprit bisection among healthy bundles, an unlocatable interaction pair, and the repair's removal + backup + verification + idempotence branches.

## Alternatives considered

- **Patch-file disable (commenting out lines)** — rejected: bundle layers are not patch rows; id-targeted `disabled` patches would need id enumeration, collide on duplicate ids, and rewrite the user's patch surface.
- **Full activation dry-run inside the doctor process** — rejected: the doctor must not mutate its own process; the subprocess probe isolates side effects and can be bound by timeout.
- **Static import scanning** — rejected: it cannot see transitive dependency loss (the real failure mode) or conditional imports.

## Consequences

The doctor now catches the most common real-world startup failure mode — a third-party plugin whose imports no longer resolve — instead of reporting all-green until the harness white-screens. The probe is slow (each real boot takes seconds), so the check runs once for the full tree and only bisects on failure. The probe inherits the dev-environment quirk of the static checks: in this repository, bundle resolution from the web-app anchor needs the pnpm virtual-store `NODE_PATH` that vitest provides; packaged installs resolve from the app anchor and do not.
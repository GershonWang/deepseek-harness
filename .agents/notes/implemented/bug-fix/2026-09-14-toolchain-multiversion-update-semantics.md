# Agent Note: Update semantics for multi-version toolchain entries

Status: implemented

English | [中文](2026-09-14-toolchain-multiversion-update-semantics.zh.md)

## Problem

The market's multi-version catalog shipped on 2026-09-14, and the user who exercised it the same day hit a dead end: after installing JDK `8u504` and being told an update existed, clicking update downloaded `21.0.12.1`, left `8u504` active, kept the update banner showing, and reported success. Three defects combined. Production code never passed `InstallOptions.Activate` (`AUDIT.md` N7). `InstallTool` returned early when the target version was already on disk, so the second click changed nothing. `HasUpdate` compared the active version to the catalog's recommended entry by string inequality, so any deliberately pinned older version reported "可更新" forever (N20). A fourth, adjacent defect made uninstalling the active version activate the alphabetically last remaining one, which with `8u504`/`17.0.20.1`/`21.0.12.1` labels selects JDK 8 (N21).

## Decision

Three changes, one set of semantics.

**Updating activates.** `UpdateAllTools` and the card's explicit install pass `Activate: true`, and `InstallTool` honours it on the already-installed early-return path, so "downloaded but not active" converges on the next click instead of never. `activate || !hadOther` still governs the default: a first install activates, and installing a second version without asking leaves the active one alone.

**Update detection compares versions.** The new `internal/toolchain/version.go` provides `compareVersions`, and `HasUpdate` becomes "the recommended version is strictly higher than the active one". A missing `current` link still reports an update, which makes the banner a repair entry point. `InstalledVersions` is sorted descending by the same comparator, so the dropdown reads newest-first instead of `17.0.20.1`/`21.0.12.1`/`8u504`.

**Uninstall falls back by version, not alphabet.** `fallbackVersion` prefers the catalog's recommended version when it is still installed, and otherwise takes the numerically highest remaining one; a tool no longer in the catalog (orphan directory) takes the same highest path.

Updating keeps the replaced version on disk — multi-version coexistence is why this catalog carries several versions — and the completion notice says so while naming the target version. The banner carries the per-tool target (`JDK (Temurin) → 21.0.12.1`) because a market card is about 170 px wide and cannot fit that sentence; the card badge keeps the short "可更新" and puts the target in its tooltip.

## Version comparison rules

Labels come from each vendor's release tag: `1.23.2` (Go), `21.0.12.1` (Temurin 21), `8u504` (Temurin 8's tag). `compareVersions` splits on `.`, `-`, `+` and `_`, takes every digit run inside a segment in order (`8u504` becomes 8 then 504, so JDK 8 build updates are orderable), and pads missing segments with zero so that `1.2` equals `1.2.0`. A label containing no digits at all falls back to byte comparison instead of inventing an order — the callers need a stable, explainable sequence, and a dictionary-order answer to "which is newer" is worse than a documented fallback. Prerelease suffixes are not modelled: `1.0.0-rc1` sorts above `1.0.0` because `rc1` contributes 1. No catalog entry uses one today, and the code states that boundary.

## Alternatives considered

**Delete the replaced version on update.** This matches the single-version intuition, but it destroys the capability the multi-version catalog exists for: a project pinned to JDK 8 would lose it to an unrelated click. Keeping it and naming the target version in the notice was chosen instead.

**Keep the string comparison and only rename the badge.** The badge becomes honest ("非推荐版本") but the dead end stays: the prompt still cannot be acted on, and the catalog's recommendation is still treated as "newer" even when it is lower than what the user runs.

**Stop prompting for a pinned version, and add an "ignore this version" preference.** That is the least intrusive option, but it needs new durable state, a settings surface, and a rule for when an ignore expires. The defect at hand — a prompt that produces no change when acted on — is fixed without any of that.

**Special-case JDK 8 and keep alphabetical fallback elsewhere.** Vendor-specific label knowledge inside the uninstall path is exactly what broke. One small comparator serves detection, display, and fallback, and is covered by one table-driven test.

## Consequences

The dead end is closed: update downloads, switches, and the banner clears. A user who deliberately runs an older JDK still sees "可更新" — now with the target version named — because that is the honest state; what changed is that acting on it produces the promised result.

`compareVersions` is the single ordering authority for detection, display, and fallback, so a future label style has one place to satisfy. Its documented limits are prerelease suffixes and labels with no digits.

Multi-version coexistence still means several full copies on disk (about 103 MB and 207 MB for the two JDK entries), and nothing removes them automatically; the card's per-version uninstall is the only cleanup path.

## Testing

`TestCompareVersions` pins the comparison rules, including the cross-style `8u504` versus `21.0.12.1` case and the byte-order fallback. `TestHasUpdate_VersionOrder` covers below, equal, and above the recommendation plus the missing link. `TestUninstall_FallbackByVersionOrder` fixes both fallback branches. `TestToolStatuses_InstalledVersionsOrderedByVersion`, `TestSortVersionsDesc` and `TestHighestVersion` pin the ordering. `TestInstallTool_AlreadyInstalledHonorsActivate` pins both activation branches. `TestUpdateAllTools_ActivatesRecommendedVersion` drives the real `UpdateAllTools` against a temporary home with both versions pre-created and no network; it fails with a 30 s timeout when `Activate` is reverted to `false`, which is how the wiring was verified rather than assumed.

## Related

- [Multi-version JDK entries in the toolchain market catalog](../feature/2026-09-14-toolchain-market-jdk-multiversion.md) added the multi-version data these defects surfaced in.
- [Dropping JDK 17 from the toolchain market catalog](../simplification/2026-09-14-toolchain-market-drop-jdk17.md) narrowed the catalog to two versions on the same day these fixes were prepared.

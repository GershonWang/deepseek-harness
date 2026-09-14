# Agent Note: Dropping JDK 17 from the toolchain market catalog

Status: implemented

English | [中文](2026-09-14-toolchain-market-drop-jdk17.zh.md)

## Problem

The catalog grew to three JDK versions (`21.0.12.1`, `17.0.20.1`, `8u504`) on 2026-09-14, the first product data the multi-version paths had ever carried. Three multi-version defects shipped with it, all still open: update-all downloads the recommended version but never activates it (`AUDIT.md` N7), `HasUpdate` compares against the recommended entry instead of version order so a deliberately pinned older version reports "可更新" forever (N20), and `Uninstall` falls back to the alphabetically last remaining version (N21).

A user exercising the market the same day hit the first two: they installed 8u504, the card and banner reported an update, they clicked update, the 21 archive downloaded — and then both versions sat on disk, 8u504 stayed active, and the update banner kept showing. Nothing the user could click changed that: the second click finds the target already installed, returns early, and still reports success.

## Decision

`jdk21` keeps its recommended entry `21.0.12.1` and `8u504`; `17.0.20.1` is removed from `internal/toolchain/tools/index.json`, and `description` now reads "可选 8 / 21". The `21.0.12.1` string stays byte-identical because it forms the install directory `jdk21-21.0.12.1` and the target of the `current/jdk21` symlink.

The motivation is to shrink the surface the three unfixed defects act on from three version labels to two, not because JDK 17 is broken. Its archive URL answered `HTTP/2 302` when checked on 2026-09-14 and its `url`/`sha256`/`size` fields were left untouched; the version is simply no longer offered.

Two versions are kept rather than one so the multi-version code paths still carry real product data while N7, N20 and N21 are fixed: a single-version catalog would put those paths back into the never-exercised state that let the defects ship.

`frontend/tools/preview.mjs` and the two launcher READMEs named the three available versions and were updated with the removal. `linglong/tools.yaml` records only each tool's recommended version, so it never carried 17 and needs no change; `verify-tools.sh` diffs tool IDs, not version lists, and `catalog_test.go` asserts nothing about 17.

## Republishing the index

The catalog clients actually read is not this file: `internal/toolchain/remote.go` pins `defaultIndexURL` to a commit hash rather than to a branch. The re-pin that `92d150b3cb` performed was repeated for this removal: `defaultIndexURL` names `ff0b924d11a2ca5cef4a908bec0ec54282ae7dc8`, whose `index.json` blob (`599f8341…`, from `git rev-parse <commit>:apps/desktop-launcher/internal/toolchain/tools/index.json`) matches the working tree and whose raw URL returned HTTP 200 with the same sha256. A launcher released before that commit keeps offering 17 until one carrying the new pin ships.

## Consequences

A machine that already installed `jdk21-17.0.20.1` keeps it: `ListVersions` scans directories rather than consulting the catalog, so the version stays switchable and uninstallable through the card's dropdown. It disappears only from the list of installable versions, and because N20 is unfixed an install still active on 17 keeps showing "可更新" — the same wording 8u504 users see.

What the removal buys is a smaller blast radius for the three open defects and a smaller diff for the fix that follows. What it costs is one version of JDK 17 that projects targeting it can no longer install from the market until 17 returns to the catalog.

## Alternatives considered

**Fix N7, N20 and N21 first and keep all three versions.** This is the real repair, and it is larger: it changes activation semantics, the update comparison, and the uninstall fallback plus their tests. Removing one version now does not block it, and the removal keeps the same day's user-visible failure from spreading to a third version label.

**Also drop `8u504`, returning to one version per tool.** That would take the multi-version surface to zero, but JDK 8 is the reason this tool has several versions at all, and a one-version catalog leaves the version dropdown, `SetActiveVersion` and the per-version uninstall with no product data — the condition that let the defects go unnoticed.

**Remove 17 from `linglong/tools.yaml` too.** Nothing to remove: that file registers one recommended version per tool, and every multi-version entry lives only in `index.json`.

**Hold the local edit until the re-pin can ship together with the N7/N20/N21 fix.** Then the catalog in git and the catalog clients read would agree at every commit. That was rejected because the user asked to shrink the version surface now; the re-pin stays a separate step and is recorded as outstanding in `AUDIT.md` N25.

## Related

- [Multi-version JDK entries in the toolchain market catalog](../feature/2026-09-14-toolchain-market-jdk-multiversion.md) added the entry this note removes; its decision stays in force for the two versions that remain.

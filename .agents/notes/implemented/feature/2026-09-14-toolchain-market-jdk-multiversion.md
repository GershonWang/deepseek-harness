# Agent Note: Multi-version JDK entries in the toolchain market catalog

Status: implemented

English | [中文](2026-09-14-toolchain-market-jdk-multiversion.zh.md)

## Problem

The catalog shipped one version per tool. `ToolVersion` is already an array, and the card already offers a version dropdown backed by `SetActiveVersion` and `Uninstall`, but no entry ever carried a second version, so that path had never carried product data. JDK is the first tool with a real need: projects target 8, 17 and 21, while the sandbox could only install 21. The tool ID (`jdk21`) also encodes "21 only", and renaming it drags in the gradle and maven dependency declarations, the packaging whitelist, test assertions, and every installed `~/.dsh-tools/jdk21-21.0.12.1` directory.

## Decision

This change is data only; the ID stays `jdk21`. In `tools/index.json`, `name` becomes `JDK (Temurin)`, `description` states the available 8 / 17 / 21, and `versions` gains 17 and 8u504 while `21.0.12.1` stays first: the first entry is the recommended version, and both update detection and update-all compare against it, so the order is semantic.

Version labels follow the release tag minus the build number: `21.0.12.1`, `17.0.20.1`, and `8u504` for Adoptium's JDK 8 tag. The `21.0.12.1` string stays byte-identical because it forms the install directory `jdk21-21.0.12.1` and the target of the `current/jdk21` symlink; changing it would turn existing installs into "not installed" and strand orphan directories.

All three versions declare `bin_rel: "bin"` and `lib_rel: "lib"`, which is where both archives put their commands and libraries. Every url, sha256 and size came from the Adoptium v3 API and was checked against the bytes actually downloaded; the 21 entry was not re-downloaded because the API's current 21 asset reports the sha256 already in the catalog.

`linglong/tools.yaml` keeps recording the recommended version only, with a comment stating that boundary: it holds one `version`/`url`/`sha256` per tool, so it cannot express the extra versions.

Renaming the ID to `jdk`, and the migration it needs (`jdk21-*` to `jdk-*` plus rebuilding the `current` links), are deferred to the next code change.

## Coverage of the added versions

`verify-tools.sh` checks each tool's single sha256 for placeholder values and diffs only the `installable` and `index.json` tool-ID sets, so a placeholder hash on the added 17 or 8u504 would still pass the build — the same gap `AUDIT.md` N16 records. This change closes it by hand instead: both archives were downloaded, their sha256 compared with the catalog, the extracted trees confirmed to hold one top-level directory with `bin/` and `lib/`, and `bin/java -version` run.

`TestE2E_CatalogInstall` installs `versions[0]` only, so the two added versions stay outside that audit. Making the audit iterate versions is a candidate for the next code change.

## Alternatives considered

**Rename the ID to `jdk` in this change.** The migration would be smallest now — one installed directory to move — but it also requires editing the gradle and maven dependency declarations, the packaging whitelist, test assertions, and new migration code. Deferring keeps this change to data, at the cost of migrating one to three directories later.

**Drop JDK 8.** That removes a 103 MB archive and one maintenance burden, but projects still target 8 and the sandbox offers no other path to it.

**Label JDK 8 as `1.8`.** It matches how the version is spoken, but breaks the release-tag style of `21.0.12.1`; Adoptium's own tag is `8u504`.

**Give `dependencies` a per-version form.** Dependencies live on `Tool`, so versions of one tool cannot declare different ones. Nothing needs that today, and the schema is not widened for a hypothetical.

**Extend `tools.yaml` to a multi-version format and upgrade the verifier.** That would put the packaging-side hash gate back over every version, but it is a code change and is deferred.

## Consequences

The market card reads "JDK (Temurin)" and lists 8 / 17 / 21: a version dropdown appears before install, switching between installed versions afterwards. 21 stays the recommended version, so an existing 21 install shows no update.

Each installed version holds a complete JDK (about 103 MB, 193 MB and 207 MB of archive respectively), and exactly one is active: `~/.dsh-tools/current/jdk21` points at it and `bin/` is rebuilt from it.

Two known gaps ship with it: the packaging-side placeholder-hash check and the end-to-end audit both cover the recommended version only.

Two adjacent findings were left unfixed. `Uninstall` activates the alphabetically last remaining version, which with these labels selects `8u504` rather than the highest version. `ToolVersion.LibRel` is written into `tool.yml` but never read: `ReconcileBinLinks` probes `root/lib` and `root/lib64` unconditionally.

The index still has to be published. Its URL is pinned to a commit hash, so this data change reaches users only through a launcher release that moves `defaultIndexURL` to the commit carrying the new index; the in-app refresh fetches that same pinned commit.

## Testing

Manual evidence: `jdk8.tar.gz` is 103542511 bytes with sha256 `9c70e102…`, `jdk17.tar.gz` is 193252603 bytes with sha256 `3808d1d1…`, and both match the size and checksum the Adoptium API reports. The extracted trees are `jdk8u504-b01/` and `jdk-17.0.20.1+1/`, each holding `bin/` and `lib/` with executable `java`, `javac`, `jdb` and `jar`, and `bin/java -version` prints `1.8.0_504` and `17.0.20.1`.

`go test ./internal/toolchain` keeps passing: its `jdk21` assertions (sha256 filled in, not installed, recommended version present) hold with three versions.

`sh apps/desktop-launcher/linglong/test-verify-tools.sh` passes, so `tools.yaml` still parses and its `installable` IDs still match `index.json`.

Not verified: nothing ran inside the Linglong container, and the launcher's own install path was not exercised for 17 or 8 — the market's only entry point is a Wails binding, and the end-to-end audit covers `versions[0]`.

## Related

- [Toolchain market catalog expansion and command exposure](2026-09-12-toolchain-market-catalog-expansion.md) owns the catalog data shape, including `bin_names` and the sha256 provenance rule this entry follows.
- [Container toolchain layers](2026-08-19-linglong-container-toolchain.md) owns the `installable` whitelist and the three-layer defense.
- [Toolchain market presentation](2026-09-12-desktop-launcher-toolchain-market-presentation.md) owns the dialog's card layout and control styling, including the version dropdown's appearance.

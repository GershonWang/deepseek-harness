# Agent Note: Toolchain market catalog expansion and command exposure

Status: implemented

English | [中文](2026-09-12-toolchain-market-catalog-expansion.zh.md)

## Problem

The market catalog held 28 tools in three categories, and the categories did not match what they named. `compiler` contained only CMake, which is a build system. `categoryLabel` in `frontend/app.js` already carried labels for `code-quality` and `debug`, but the dialog shipped three tabs, so tools in those categories could not be filtered to and would only appear under 全部.

Inside the sandbox there was no C or C++ compiler at all: `prune-gcc-toolchain.sh` strips the gcc toolchain from the packaged tree and keeps only runtime libraries. An agent facing a project that compiles C, builds a native Node addon, or installs a Python package from source had no path forward.

Two installer behaviors blocked otherwise suitable archives. `ReconcileBinLinks` names each symlink after the file inside the archive, and `provides` only feeds conflict detection, so an archive whose binary carries a platform suffix would expose that suffix as the command (`yq` ships `yq_linux_amd64`). Any file in the archive with the executable bit set is linked, including install and helper scripts that happen to be 0755 (`yq` also ships `install-man-page.sh`).

Finally, catalog entries carry fixed URLs, and mirrors rotate versions: every maven `bin.tar.gz` address on Apache dlcdn except the current release returned 404, so rot surfaces only when a user clicks install.

## Decision

The catalog now holds 43 tools across `language-sdk`, `build-tools`, `modern-cli`, and `code-quality`, and the dialog offers a tab per category.

Fifteen tools were added, each with a sha256 measured by downloading the archive and, where the project publishes one, matched against its own checksum file:

- `build-tools`: ninja 1.13.2, zig 0.16.0, gradle 9.7.1, maven 3.9.16
- `modern-cli`: yq 4.53.6, sqlite 3.53.4, duckdb 1.5.5, pandoc 3.11, just 1.58.0, grpcurl 1.9.4
- `code-quality`: ruff 0.16.7, golangci-lint 2.13.2, actionlint 1.7.12, shellcheck 0.11.0
- `language-sdk`: php 8.5.8

`compiler` is renamed `build-tools` in the catalog and in the dialog's tabs; `code-quality` and `debug` gain tabs. The dialog builds its tabs from the categories present in the loaded catalog rather than from a fixed list. The remote index is a separately published artifact, so a client and an index can disagree about the category set, and hardcoded tabs are wrong in both directions: a new client against an old index renders tabs that match nothing, and an old client against a new index hides whole categories. Known categories keep a fixed order, unknown ones follow sorted by id, a category with no tools never gets a tab, and a selection whose category disappears falls back to 全部.

Category labels live in the index, not in the client. `Index.CategoryLabels` carries an id-to-Chinese-name map that `setCatalog` installs alongside the tools, `CategoryLabels()` exposes it, and the app payload forwards it to the dialog. The client keeps a matching fallback table for indexes published before the field existed and for categories an index leaves unlabeled; an id with no label anywhere renders as-is. Together with derived tabs this makes both adding a tool and introducing a category index-only work: the label arrives with the data. The Go test asserts the shipped index labels every category its tools use, since the client no longer guarantees it, and the frontend test pins the precedence — index label first, fallback table second — by relabeling a category in the payload.

`ToolVersion.BinNames` maps archive file names to the command names they are exposed as; an empty value suppresses the file. `linkExecutables` applies the map and records the exposed name in its `seen` set, so `cleanStaleLinks` keeps renamed links and drops suppressed ones. `ReconcileBinLinks` is the only path that creates these symlinks, so the map has a single application point.

Zig is filed under `build-tools` and is the C and C++ compilation path: `zig cc` compiles and links without the gcc toolchain. Gradle and maven declare `jdk21` as a dependency, so installing them installs the JDK first. Neither needs `JAVA_HOME`: both launcher scripts fall back to `java` on PATH, which `appenv` already prepends, and both were run against the market-installed JDK with `JAVA_HOME` unset.

`TestE2E_CatalogInstall` audits the catalog end to end. It skips by default, and under `DSH_TC_E2E=1` it installs each tool in a fresh directory, which checks that the URL resolves, that the archive matches the catalog sha256, that the extracted layout agrees with `bin_rel` and `bin_names`, and that every declared command exists under `bin/` with a live target. `DSH_TC_E2E_IDS` narrows it to named tools.

Candidates checked and left out: shfmt ships only a bare binary, which the installer's tar.gz/zip/tar.xz formats cannot take; clang and LLVM are covered above; act needs a container runtime socket the sandbox does not have; `dlv` needs ptrace, whose availability inside the container has not been verified and which no product surface consumes; Ruby, Lua, and Perl publish no self-contained Linux build.

## Alternatives considered

**A separate `bin_exclude` field for suppressed files.** Cleaner reading than an empty map value, but it splits one concern across two mechanisms: the same archive file name decides both the exposed command name and whether the file is exposed at all. The empty value keeps that in one field.

**Accepting the stray `install-man-page.sh`.** It is 405 bytes and the card never names it, but it lands on PATH as an invocable command, and every future archive with a helper script would add another one.

**Keeping `compiler` and only adding zig.** Zig alone would make the name honest, but CMake, ninja, gradle, and maven are not compilers, and the category would keep implying otherwise.

**Splitting `build-tools` and `compiler`.** Two categories with one compiler in one of them. With clang excluded there is nothing to put in the second.

**Shipping clang and LLVM instead of zig.** The official `x86_64-linux` build targets Ubuntu 24.04 (glibc 2.39) while the container runs on `org.deepin.base/25.2.2`, whose glibc was not verified to satisfy it, and the archive is about 1 GB. Zig is 53 MB and needs one shared library set already present.

**Filing zig under `language-sdk`.** Zig is a language, but the gap being closed is compilation inside the sandbox, and `build-tools` is where a user looks for it.

**Trusting published checksum sidecars without downloading.** Sidecars are the upstream claim; the installer trusts the catalog value, so the catalog value is what must match the bytes. Four of the fourteen have no sidecar at all (shellcheck, ninja, and the sqlite and duckdb zips), so a download was required regardless.

**Running the audit in the default suite.** It downloads roughly 2 GB across the catalog and needs network. CI has no such budget, so it is opt-in.

## Consequences

The catalog grew from 28 to 43 entries. The dialog's grid, the `全部` tab, and the status bar all show more cards, and the remote index file grows accordingly.

Installing gradle or maven now pulls jdk21 first when the market copy is absent — about 190 MB the user did not directly ask for. A user who already has a JDK from the host mount still gets the market copy, because dependency resolution only consults the market store.

`bin_names` is now part of the catalog contract: a new tool whose archive names differ from its commands must declare the map, or the platform-suffixed name reaches PATH.

The maven URL points at `archive.apache.org` because `dlcdn.apache.org` keeps only the current release. The php URL points at the `common` channel of `dl.static-php.dev`, which is not versioned and can rotate; both were reachable when added, and only an audit run will catch a later rotation.

The remote index is still published by hand; this change reached the in-repo `tools/index.json` and `linglong/tools.yaml` only. Until the index is published to the `linglong` branch, clients keep reading the previous catalog, and the built-in copy remains the fallback. That skew is visible: an old index carries `compiler` and no `code-quality` or `debug`, so before publishing, those tabs simply do not appear, and only a published index brings them back.

The tab set now follows the index, so its width varies with the index version. Measured against the shipped stylesheet at the 560px dialog width, four tabs already wrapped the toolbar onto two rows — tabs and search, then the refresh button — and six tabs also occupy two rows, at 66px against 64px. The dialog keeps its fixed 86vh height, and switching categories does not change the tab set, so the presentation note's height invariance still holds.

## Testing

Every added sha256 was verified by download: yq, ruff, golangci-lint, actionlint, just, grpcurl, gradle, and zig matched their upstream checksum file or index; maven's official sha512 matched the measured sha512; shellcheck, ninja, and the sqlite, duckdb, and php archives publish no sidecar and carry the measured value.

`DSH_TC_E2E=1 go test ./internal/toolchain -run TestE2E_CatalogInstall` installed every added tool for real and reported each one's exposed commands; `bin/` held exactly the declared commands for yq, renamed from `yq_linux_amd64` with `install-man-page.sh` suppressed. The gradle and maven runs also exercised the jdk21 dependency path. The audit earned its place immediately: the first grpcurl run failed on a one-character transcription error in the catalog sha256, which no amount of sidecar reading would have caught.

`go test ./internal/toolchain` covers the rename and suppression behavior with `TestReconcileBinLinks_RenamesArchiveBinary`. `node --test frontend/test-app.cjs` passes 38 cases, including the category case that pins label precedence (an index-supplied label wins, an unlabeled category falls back) and the skew cases: a legacy-shaped catalog renders only its own tabs, and a selected category that disappears falls back to 全部. Replacing the derived tab list with the fixed one, or the label lookup with the fallback table alone, makes that case fail. On the Go side `TestCatalog_EveryCategoryHasLabel` fails if the shipped index leaves a category its tools use unlabeled, and `TestLoadIndex_FetchesAndCaches` asserts labels travel with a fetched index. `linglong/verify-tools.sh` reports the `installable` set and `index.json` as consistent, and `node frontend/tools/preview.mjs verify` passes, though it does not cover the market dialog.

Not verified: no run happened inside the Linglong container, so glibc compatibility for the new binaries rests on host runs.

## Related

- [Container toolchain layers](2026-08-19-linglong-container-toolchain.md) owns the build-time manifest, runtime self-check, and on-demand install layers; its `installable` enumeration was updated in place for this change.
- [Toolchain market runtime availability hint](2026-09-10-toolchain-market-runtime-availability-hint.md) owns the card row that reports commands already available on the container PATH.
- [Toolchain market presentation](2026-09-12-desktop-launcher-toolchain-market-presentation.md) owns the dialog's card layout, contrast, and empty states.

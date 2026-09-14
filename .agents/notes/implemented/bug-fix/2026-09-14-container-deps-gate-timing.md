# Agent Note: Container dependency checks run where their packages exist

Status: implemented

English | [中文](2026-09-14-container-deps-gate-timing.zh.md)

## Problem

The build's dependency gate (`linglong/verify-container-deps.sh`, called from the first line of the `build:` stage) required every package declared in `buildext.apt` — `build_depends` and `depends` alike — to be installed in the build container. The first real build carrying it failed immediately: eight `depends` packages reported "not installed" (fonts-wqy-microhei, git, git-lfs, wget, jq, xxd, zip, xdg-utils) and six base-image packages reported `dpkg --verify` mismatches (libgtk-3-0, libglib2.0-0, python3, curl, unzip, ca-certificates). No build could pass.

Both groups were false positives, and the repository already held the evidence for the first one:

1. **Timing.** ll-builder installs only `build_depends` in the build container. The generated `linglong/buildext.sh` was three lines: write an apt sandbox config, `apt update`, `apt -y install libwebkit2gtk-4.1-0`. `depends` are installed after the `build:` stage, during the preCommit merge — which `verify-tools.sh`'s header and `linglong.yaml`'s webkit comment both already state. The older build log shows the order (`[Start Build]`, then `Setting up wget/xdg-utils/git/git-lfs`, then `[Install Files]`), and the previously exported package layer carries their payloads (`bin/git`, `bin/git-lfs`, `bin/wget`, `bin/jq`, `bin/xxd`, `bin/xdg-open`, `bin/zip`).
2. **Inherited documentation.** The base layer `org.deepin.base` keeps `/usr/share/doc` and `/usr/share/man` entries in its `.list` files while the entities were pruned when the image was built (614 packages, zero `/usr/share/doc` entries; `libgtk-3-0:amd64.list` lists six doc paths, none of them present). `dpkg --verify` can therefore never be empty for a package inherited from the base.

The commit that added the gate recorded that it had never been run against a real container.

## Decision

The check is split so that each half runs where its packages exist.

`verify-container-deps.sh` stays first in `build:` and covers `build_depends` only: dpkg status, installed version equal to the apt candidate, no character-device entity, no `*.dpkg-new` residue, and a clean `dpkg --verify` except for `/usr/share/doc` and `/usr/share/man` paths, which the base image prunes. That exception is narrow and does not weaken the check: the payload a failed install would lose lives under `lib/`, `bin/` and `share/`, and the character-device branch is independent of `--verify`.

`verify-merged-deps.sh` (new, host side, called by `build-linglong.sh` before `export`) covers `depends` on the merged product tree. It scans for character devices and `*.dpkg-new` (the N19 signature as it appears in an artifact) and requires every package declared in `buildext.apt` to be claimed by a rule: `tool:<name>` (binary path resolved from `tools.yaml`, the existing single source for bundled tools), `path:<rel>` (an entity this script asserts directly), `base:<why>` (provided by the base image, by design absent from `$PREFIX`), or `none:<why>` (declared, but currently delivers nothing to the artifact). An unclaimed package fails the build, so a new dependency cannot arrive without an assertion.

Unlike `verify-tools.sh`, whose failures are advisory in `build-linglong.sh`, this check aborts the export: it exists to make silent dependency loss loud, and before it was wired in it was run against a real merged tree to measure its false-positive surface.

## Evidence behind the rules

Every `base:` and `none:` rule came from listing entities in real layers, not from package names. `/etc/ssl/certs/ca-certificates.crt` is present in the base layer and absent from the package layers, so `ca-certificates` is base-provided. `libgtk-3.so.0` and `libglib-2.0.so.0` are absent from all three package layers while the base provides them. No wqy font exists in any layer, which `AUDIT.md` N26 records as an open question. `bin/zip` is present in all three package layers but is not in `tools.yaml`, so it is asserted with `path:` — adding it to `tools.yaml` would also add it to the launcher's runtime self-check roster (`check.go` `DefaultSpecs`), a product change this fix does not need.

## Alternatives considered

**Keep one gate and check every package at `build:` time.** That is the state that failed. No single placement can see both sets: `build_depends` exist only in the build container, `depends` only in the merged tree.

**Drop the `depends` half and keep only the container check.** Fewer files, but then nothing verifies that a declared runtime dependency reached the package — the failure mode AUDIT N18 exists for — and `verify-tools.sh` covers only the tools listed in `tools.yaml`.

**Make the merged-tree check advisory, like `verify-tools.sh`.** Consistent with the older script, but this is precisely the silent-loss class the work is meant to make loud; an advisory line in a long build log is how the original problem stayed invisible for two rounds.

**Whitelist documentation paths and change nothing else.** That fixes six of the fourteen failures and leaves the eight `depends` ones, which are structural rather than cosmetic.

## Consequences

A build can pass again, and both halves of the dependency story are checked where they are observable: the container for `build_depends`, the artifact for `depends`. The cost is one more script and a rule table that must grow when a dependency is added — with an unclaimed package failing the build instead of passing unnoticed.

The documentation-path exception means a package whose documentation alone fails to install passes the container check. That is deliberate: documentation is not what the gate protects, and the base image makes the comparison meaningless for inherited packages.

`AUDIT.md` N18 and N19 now record the real mechanism and the corrected landing points; N26 records the fonts finding that the rule table surfaced.

## Testing

`test-verify-container-deps.sh` (10 cases) pins the new semantics: only `build_depends` is required, a stale apt candidate, a missing package, a character device, `*.dpkg-new` residue, a payload mismatch, documentation-only mismatches passing, documentation mismatches not masking a payload mismatch, and the real `linglong.yaml` being parsed while its `depends` packages are not required at that point.

`test-verify-merged-deps.sh` (7 cases) pins the artifact check: the real manifest plus a complete tree passes (which also proves every declared package is claimed), a missing `tools.yaml` tool, a missing `path:` entity, a missing `zip`, a character device created with `mknod`, `*.dpkg-new` residue, and an unclaimed package.

`verify-merged-deps.sh` was also run against a real merged tree from the previous successful build (`~/.cache/linglong-builder/merged/50f29c89…/files`): 16/16 OK, exit 0. Not verified: neither script has run inside a real ll-builder invocation from this machine (there is no ll-builder here), so the next build is the first end-to-end run of the corrected placement.

## Related

- [Container toolchain layers](../feature/2026-08-19-linglong-container-toolchain.md) owns the `installable` whitelist and the container's three-layer defense.

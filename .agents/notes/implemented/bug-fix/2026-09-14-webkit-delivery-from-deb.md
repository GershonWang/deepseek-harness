# Agent Note: WebKit is delivered from the apt candidate's .deb

Status: implemented

English | [中文](2026-09-14-webkit-delivery-from-deb.zh.md)

## Problem

The build container's overlay does not persist writes: `apt` reports `Unpacking`/`Setting up libwebkit2gtk-4.1-0 (2.50.4-…)`, while `/usr/lib/x86_64-linux-gnu/` keeps the 2026-04-07 entity (92,804,704 bytes, the base layer's WebKitGTK 2.48.5) and the new files appear only as `c 0,0` character devices named `*.dpkg-new` (`AUDIT.md` N19). The `build:` stage used to copy that entity out of the container into the package prefix and patch it — so every release shipped 2.48.5, the `depends` entry asked for something newer, and nothing in the build said so.

That stale entity also broke a second thing. The builder's library-dependency collection tries to copy the same file into `output/_build`, fails with `无效的参数`, downgrades it to a warning, and continues. The recent gate that turns `failed to copy` into a build failure therefore aborted every build, on a copy whose content the package takes from somewhere else.

## Decision

`build:` obtains WebKit from the apt candidate's `.deb` instead of from the container's filesystem: `apt-get download libwebkit2gtk-4.1-0`, assert exactly one `.deb`, assert the version parsed out of its filename equals `apt-cache policy`'s candidate, `dpkg-deb -x` it, assert the extracted `libwebkit2gtk-4.1.so.0.*` is a single regular file, copy it into `${PREFIX}/lib/x86_64-linux-gnu/`, run `patch-webkit-exec-path.sh`, recreate the two soname links, and print the delivered version. These are ordinary file writes that never enter the overlay, so ll-builder's merge timing cannot drop them, and the version the package carries follows the container's apt candidate rather than a number written in this repository.

The criterion for "did WebKit reach the package" moves from the builder log to the artifact. `verify-builder-log.sh` exempts exactly one line shape — a `failed to copy` whose source is `libwebkit2gtk-4.1.so.0` — reports how many lines it exempted, and still fails on every other `failed to copy`. `verify-merged-deps.sh` then asserts that the packaged `lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0` exists, is a regular file (not a character device), and contains the exec-path patch marker, whose value comes from `internal/packaging/webkit-exec-path.txt`, the same single source the launcher embeds and the patch script reads.

The exemption is safe because the failed copy is the *old* library moving into `_build`; its failure is what keeps the base version from overwriting the patched one. If a future builder succeeds at that copy and merges it, the patch marker disappears from the artifact and the build fails — the silence that N17 was about is replaced by an assertion on the thing users actually run.

## Alternatives considered

**Keep copying from the container's `/usr`.** This is the state that shipped 2.48.5 under a manifest that asked for more. The copy succeeds whenever the old entity is readable, so it looks healthy while delivering the wrong version.

**Fix the overlay itself.** It is the builder's and the kernel's business, outside this repository, and cannot be validated without a real ll-builder. Bypassing it is what remains.

**Keep the log gate and revert `failed to copy` to a warning.** One line, and it unblocks the build, but it also drops the only guard against the class N17 records: a package that quietly ships the previous layer's file.

**Extract the `.deb` into the container's `/usr`.** That is exactly the path whose writes do not land, so the extraction would appear to succeed and change nothing.

**Pin a WebKit version in the repository instead of following the candidate.** The pin would need its own update process and its own sha256, and the container's apt lists are already the authority for what can be installed; the version comparison against the candidate is the cheaper check that the download is the one that was asked for.

## Consequences

The package carries the version the container's apt candidate names, and the build log states it. The artifact assertion covers both failure directions: a missing or downgraded library, and a merge that overwrites the patched file.

Costs: one ~25 MB download per build, and a hard dependency on the candidate still being retrievable — if the mirror drops it, the build fails loudly rather than shipping an older library. The overlay defect itself is untouched; this change routes around it, so any *other* dependency whose files fail to land is still invisible unless it has its own artifact assertion.

## Testing

`test-verify-builder-log.sh` covers five paths: a clean log, a log whose only failure is the WebKit copy (passes and prints the exemption count), a log failing on a different library (still fails), a log with both (still fails, and names the unexempted count), and a missing log. The real log from the first build with the gate now reports `共 1 处，豁免 webkit 1 处` and exits 0.

`test-verify-merged-deps.sh` covers eight paths, including a packaged WebKit that exists but lacks the patch marker, which must fail.

The `build:` block was extracted from `linglong.yaml` and passed `bash -n`. Not verified: no step of this ran against a real ll-builder on this machine, so the next build is the first end-to-end run of both the extraction and the artifact assertion.

## Related

- [Container dependency checks run where their packages exist](2026-09-14-container-deps-gate-timing.md) split the dependency gate by timing and left the WebKit copy failure exempted at the log level; this note replaces that exemption's missing half with the artifact assertion.
- [Container toolchain layers](../feature/2026-08-19-linglong-container-toolchain.md) owns the `installable` whitelist and the container's three-layer defense.

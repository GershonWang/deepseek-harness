# Agent Note: The desktop launcher ships only the Linglong packaging path

Status: implemented

English | [中文](2026-09-13-remove-deb-packaging-path.zh.md)

## Problem

`apps/desktop-launcher/build-deb.sh` assembled a Linux `.deb` under `/opt/apps/com.deepseek.dsh-desktop/files`. It reused `linglong/prepare-offline.sh` for the staged build, then diverged: the bundle binds its own webkit2gtk through `linglong.yaml`, while the `.deb` declared a dependency on the host's build.

Two facts made the script both unusable and misleading.

It cannot produce a package. It creates `usr/share/icons/hicolor/256x256/apps` and then installs nine icon sizes through `install -m644`. GNU `install` requires the target directory to exist without `-D`, and under `set -eu` the first iteration (`s=16`) aborts with `No such file or directory`. `linglong.yaml` runs the same loop with `install -Dm644`, which creates each directory.

Nothing consumes it. No CI job, git hook, gate, spec, or other script references the file, and no packaging release runs it. The README nevertheless advertised "a Linglong bundle distributed on Deepin 25, plus Linux `.deb` and `.rpm`", and no `.rpm` entry point has ever existed in the tree.

## Decision

The desktop launcher has one packaging path. `apps/desktop-launcher/build-linglong.sh` runs `prepare-offline.sh` on the host, assembles in the Linglong container, prunes the gcc toolchain, verifies the merged tool tree, and exports a `.uab`.

`apps/desktop-launcher/build-deb.sh` is deleted. `.gitignore` drops its `com.deepseek.dsh-desktop_*.deb` entry. Both `apps/desktop-launcher/README.md` and `README.zh.md` describe the Linglong bundle as the distribution and claim neither `.deb` nor `.rpm`.

No Go source changes accompany the removal. The module carries no `.deb`-specific branch: `packaging.ConfigureWebKitHelperPath` returns early when the bundled webkit helper is absent, which is the unpackaged-mode and development path, not `.deb` support, and it stays.

## Alternatives considered

**Fix the missing `-D` and keep the path.** Rejected: nothing consumes the script, and it has never produced a package in this tree. Repairing it would leave a second assembly of the same `/opt/apps/<id>/files` layout to maintain beside `linglong.yaml`, with the icon install, `.desktop` install, and pnpm closure all duplicated.

**Keep the script but mark it unsupported.** Rejected: documentation would still be the only thing standing between a reader and a broken command, and a present-but-broken script is what invited the README claim in the first place.

**Keep the `.deb` recipe as a documented snippet.** Rejected: the layout logic duplicates `linglong.yaml`, so a snippet rots against the same upstream changes while offering no executable check.

**Delete the script and leave the README claims.** Rejected: that is precisely the state that makes the documentation false. The claims are the reason the removal reaches the prose as well.

## Consequences

A `.deb` install ran the harness outside the Linglong sandbox, directly against the host X server and the host webkit2gtk. That capability is given up: the launcher now has exactly one runtime environment, so behavior that only diverged outside the container — the clipboard read path in particular — loses its second, bridge-free exercise. Reintroducing a second distribution means adding a build script that shares `prepare-offline.sh` and the `linglong.yaml` icon and `.desktop` install rather than restating them.

Two implemented Agent Notes recorded the `.deb` path as live and are corrected in this change, because [implemented notes track shipped reality](../AGENTS.md). [Desktop entry needs StartupWMClass for dock icon association](../bug-fix/2026-09-12-desktop-launcher-dock-icon-association.md) no longer credits a second packaging path as a consumer of the desktop entry. [Clipboard INCR transfer and X11 event offsets](../bug-fix/2026-09-11-clipboard-incr-transfer-and-event-offsets.md) no longer presents a `.deb` install as the beneficiary of the corrected `SelectionNotify` offset.

## Testing

Absence is checked mechanically. `grep -riE 'build-deb|\.deb'` over tracked sources returns only `scripts/prepare-ci-bubblewrap.sh` and `.github/workflows/ci-master.yml`, which download unrelated `.deb` archives for bubblewrap and Wine; `grep -ri rpm` returns nothing outside this note. `git ls-files apps/desktop-launcher` no longer lists `build-deb.sh`.

The bilingual pairing record for both READMEs and for both corrected Agent Notes is re-recorded, and `verify-translation-pairing` reports the affected pairs consistent. No Go file changed, so the launcher build and its `go test ./...` result are unaffected.

## Related

[Desktop launcher on Linux/Linglong](../feature/2026-08-14-desktop-launcher-linux-linglong.md) owns the packaging path this note leaves as the only one.

# Agent Note: Host applications from inside the Linglong sandbox

Status: implemented

English | [中文](2026-09-20-host-escape-open-in-app.zh.md)

## Problem

The deepin desktop client runs inside a [Linglong sandbox](2026-08-14-desktop-launcher-linux-linglong.md). There the Open In menu offered only the file manager, while the same build on a plain host lists the user's editors and IDEs: the sandbox keeps the host's filesystem out of the XDG search path, and a process in its namespace cannot exec a program that exists only on the host. The [host toolchain mounts](2026-08-19-linglong-container-toolchain.md) make host *commands* reachable but say nothing about host *applications*, so a packaged-client user lost a capability the same catalog provides everywhere else.

## Decision

A sandbox declares one optional host-escape channel as two environment facts on the inherited process layer: `DSH_HOST_ROOTFS` (the read-only mount of the host root) and `DSH_HOST_LAUNCH` (the forwarding launcher that starts a process on the host). `apps/desktop-launcher` injects the pair in `ConfigureChildEnv` only when that mount is a readable directory and the launcher resolves on `PATH`, and never overwrites a value that is already present — an empty `DSH_HOST_ROOTFS` therefore switches the channel off. [`@deepseek-ai/dsh-launch-environment`](../../../../packages/util/launch-environment/src/index.ts) parses the pair into a `HostEscapeFact` through `hostEscapeOf()`, reading the process layer alone, exactly like `launchedThroughSsh()`: a project or user `.env` layer can never open a host channel, and a missing, empty, relative, or unknown declaration means "no channel" rather than a failure, so a host without a sandbox resolves byte-for-byte as it did before.

[`@deepseek-ai/dsh-host-open-in-app`](../../../../packages/host/open-in-app/README.md) consumes the fact only through a new `host-desktop` locator, which the Linux specs of the editors and IDEs declare. Only the desktop ids each entry documents are read — never an enumeration — from the host's `/usr/local/share`, `/usr/share`, the Linglong store's exported entries under `/var/lib/linglong/entries/share`, and the home shared with the host. A candidate is offered only when the program its `Exec` names exists on the host: a system path is verified through the host-root mount, a shared-home path directly, and a bare program name proves nothing and is skipped.

One probe per resolution pass decides whether the channel is offered at all: `systemd-run --user --collect --quiet --service-type=exec -- /bin/sh -c 'exec "$0" "$@"' /bin/true`. A failed probe withholds every host entry for that pass, because an entry that errors on click is worse than an absent one.

A host launch installs the host program as a transient unit of the host user manager: `systemd-run --user --collect --quiet --service-type=exec --expand-environment=no --unit=<unique> -- /bin/sh -c 'exec "$0" "$@"' <host program> <workspace>`. Two properties shape this argv. The forwarding launcher validates the outer executable on the sandbox side and the host then execs its own file at that path, so the bridge must be a path that exists in both namespaces — `/bin/sh` — with the host program passed as `$0`. `--service-type=exec` makes a failed host exec fail the launch immediately instead of after the watch window, `--expand-environment=no` keeps `%` and `$` in workspace paths literal, and the bridge's target is argv, never a shell string. A failed host launch is reported as a stale resolution so the open route re-resolves that one entry — the only repair available from inside the sandbox — and the Linux icon route follows the entry a host launch came from into the same host data directories, falling back to the host root for an absolute `Icon=` path.

## Alternatives considered

**Enumerate the host's applications from its data directories.** Rejected: reading arbitrary host desktop entries would offer launch protocols the catalog cannot verify, and the catalog is a deliberate whitelist. The locator reads only the ids each entry documents.

**Reuse the bundled `xdg-open` shim for host opening.** Rejected: that shim forwards to the host's own file-manager and browser opener, so it can open a directory but cannot launch a chosen host application, and the menu's contract is a named application plus a workspace.

**Let the desktop launcher resolve and launch host applications itself.** Rejected: it would duplicate the catalog and need a second channel to serve the menu. Injecting the facts keeps one owner for the catalog and lets the same resolution code run in and out of a sandbox.

**`systemd-run --user --scope`, the shape the [subprocess provider](../../implemented/architecture/2026-08-28-subprocess-native-containment.md) uses.** Rejected: a scope joins the caller's namespaces, so the target would stay in the container. Only a service reaches the host.

**Ship a host-side helper service.** Rejected as a much larger compatibility surface — a host installation, a socket protocol, and its own versioning — where the host root mount and the user manager already exist on every target system.

## Consequences

A packaged-client user now gets the host's editors and IDEs in the same menu as the file manager, with no write access to the host: detection reads two data directories plus the shared home, resolution verifies a program it will execute, and nothing is installed. The channel is a per-machine fact behind one probe, so a host user manager that becomes unreachable costs the whole pass's host entries until the next resolution; a host program that disappears between resolution and click costs one re-resolution; and an entry without an `Icon=` key keeps the generic glyph.

The declaration is a cross-language contract with no schema: Go writes the two variables and the launcher value, TypeScript reads them, and both sides name the pair in comments. Its trust boundary is the shared home — a per-user `~/.local/share/applications` is writable by the user whose applications these are, and the locator trusts it exactly that far.

Folding the browser and `xdg-open` forwarding onto this channel, and extending the catalog with host-only entries, are deferred; the current file-manager entry keeps its existing forwarding behavior.

## Verification

- Unit specs pin the locator chain (host entry through the host-root mount, per-user entry from the shared home, ids tried in order), the withholding paths (no `Exec`, bare program name, program absent on the host, failed probe), one probe per pass, the exact launch argv including the bridge and the unique unit name, and the icon lookup in the host data directories.
- The REAL-composition spec boots the WebServer and this plugin through the Loader, declares the channel on the process layer, and asserts the apps route and the `systemd-run` argv the open route produces.
- On a real Linglong client (deepin host with VS Code, Sublime Text, and two JetBrains IDEs installed) the resolver listed those four host applications, and a host program launched through the bridge created and removed a file in the host's `/tmp`.

## Related

- [Linux desktop launcher via Go + webview_go](2026-08-14-desktop-launcher-linux-linglong.md) — the sandbox this channel lives in.
- [Linglong container toolchain](2026-08-19-linglong-container-toolchain.md) — the sibling host-mount mechanism for commands.
- [Native owners contain escaped subprocess descendants](../../implemented/architecture/2026-08-28-subprocess-native-containment.md) — why harness-managed children use a scope instead.
- [Bundle xdg-open for host browser opening](../../implemented/bug-fix/2026-08-27-bundle-xdg-open-for-host-browser-opening.md) — the forwarding shim this channel does not yet replace.
- Design record: [Host-escape Open In for the Linglong container](../../../../docs/superpowers/specs/2026-09-20-host-escape-open-in-app-design.md).

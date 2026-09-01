# Agent Note: Externalize toolchains into three pluggable layers in the Linglong client

Status: implemented

English | [中文](2026-08-31-linglong-three-layer-toolchain.zh.md)

## Problem

The Linglong desktop client ships a fixed compiler/toolchain set inside the app bundle, pushing the package toward ~431 MB and freezing the tool set at build time. Users cannot add a tool, remove one, or pin a version without re-packaging the whole `.uab`. The product requirement is the reverse: toolchains live outside the app, and a user hot-plugs any tool into the running client. Availability and completeness of tools outrank package size, so the redesign must not make any tool less usable than it is on the host.

## Decision

The client resolves tools from three pluggable layers, searched lowest-to-highest priority:

- **L1 builtin** — a read-only ~150 MB minimal set inside the package, present when no user tool is installed yet.
- **L2 one-click install** — tools installed under `~/.dsh-tools` (shared across the user's HOME), with multi-version support, sha256 verification, atomic tar extraction, a download cache, and a remote catalog index with a 24-hour cache and builtin fallback.
- **L3 host import** — the user binds a host toolchain directory read-only into the sandbox via config.d; the bind takes effect after an app restart.

The catalog is a single JSON source, `internal/toolchain/tools/index.json`, embedded into the launcher with `go:embed` and overridable at runtime by a same-shape remote index fetched from a configurable URL (defaulting to the repo copy on the branch). A project-level `.dsh-toolchain.yml` pins per-tool versions and auto-switches the active version when the user opens that project. The host-import wizard scans common toolchain roots, probes each candidate's `--version`, and writes the mount config.

## Alternatives considered

**Keep bundling toolchains into the package** — rejected. It keeps the `.uab` large and couples every tool update to an app republish.

**Use Linglong `modules`/`ext_defs` as the toolchain store** — rejected. Linglong's app-layer module market is the right vehicle for toolchains, not the base/runtime module manifests; the three layers above plug into that market instead.

**Install into a version-less single directory** — rejected. Multi-version installs with an active symlink are required for project pins and rollback.

**Skip the download cache and always fetch** — rejected. A cache keyed by sha256 makes repeat and offline installs deterministic and resilient to flaky GitHub releases.

## Consequences

The package shrinks toward the L1-only footprint; every extra tool costs install-time download instead of package bytes. Integrity now depends on sha256 verification plus atomic extraction, and host imports require an app restart before the bind mount is visible. Remote-catalog outages degrade to the 24-hour cache and then the embedded catalog, so the market still renders offline. The price is a larger Go surface (catalog, install, remote, project, host discovery) and a market UI with progress, version switching, and uninstall, all of which the three layers need to stay externally pluggable.

## Related

The prior container-toolchain approach whose on-demand layer this externalization supersedes: [Linglong container toolchain availability](2026-08-19-linglong-container-toolchain.md).

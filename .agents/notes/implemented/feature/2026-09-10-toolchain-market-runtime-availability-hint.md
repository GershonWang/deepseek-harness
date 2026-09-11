# Agent Note: Toolchain market runtime-availability hint on cards

Status: implemented

English | [中文](2026-09-10-toolchain-market-runtime-availability-hint.zh.md)

## Problem

The toolchain market dialog in `apps/desktop-launcher` marks a tool "installed/installable" solely by whether the market store (container `$HOME/.dsh-tools`) holds a version directory for it. Inside the Linglong sandbox a command can exist independently of that store from three unrelated sources — bundled package tools, host-imported mounts under `/opt/host-tools`, and the base runtime — and an interactive terminal can additionally surface host commands leaked through home-directory shell rc files (for example nvm). The result looked contradictory: a terminal ran `node v24.13.1`, the bundled tool panel listed `node 24.9.0`, and the market card still said "installable", with nothing on the card explaining which node would run or where it came from.

## Decision

An uninstalled tool's card now renders a runtime hint line, `容器内已可用：command version (source)`, whenever the container PATH resolves a command the tool's catalog `Provides` list declares. The layers keep their existing responsibilities:

- `internal/domain`: `ToolCheck` records the `exec.LookPath` resolution path.
- `internal/toolchain/check.go`: `Check` fills `Path` for its fixed bundled specs; the new `ProbeCommands` probes catalog-declared commands on demand — a successful `LookPath` makes the command available, and a failing `--version` never downgrades availability, only leaves the version empty.
- `internal/app/app.go`: `annotateRuntime` merges the fixed checks with on-demand probes, matches each uninstalled tool's `Provides` in order, and `classifyRuntimeSource` buckets the resolved path by prefix — `/opt/host-tools/` is 宿主导入, the launcher-derived bundled bin directory is 随包, the market store is skipped (the `Installed` flag already owns that semantic), everything else is 系统. The function takes `home` and the bundled prefix as parameters and probes nothing beyond `ProbeCommands`, keeping the classification deterministic and testable.
- `frontend/app.js` renders the hint; installed and installing cards are untouched, and the `toolchain:status` payload only gains fields.

The hint deliberately covers only the launcher's own child-process PATH. Commands that reach an interactive terminal through shell rc files (nvm and friends) are invisible to a non-interactive probe by construction; labeling them a container source would be wrong, so the boundary is a stated limit, not an omission.

## Alternatives considered

**Pure-frontend matching against the existing builtin rows.** The builtin rows only cover the twelve fixed bundled specs, cannot classify a source, and would move UI-semantic computation into the render layer. Lost on coverage and layering.

**Renaming the badge to "not installed".** Zero structure change, but it still answers nothing about where an existing command comes from. The badge keeps describing the store; the hint line describes the runtime — the two texts together remove the ambiguity without overloading either.

**Probing every catalog command up front by extending `DefaultSpecs`.** Every refresh would run dozens of `--version` processes and the fixed list would need to track the catalog by hand. On-demand `LookPath` costs nothing for misses and probes versions only for hits.

## Consequences

A card now explains itself when the container already provides the command, and installing through the market remains meaningful — `~/.dsh-tools/bin` is injected ahead of the image PATH, so the market version takes over the launcher's subprocesses after install. Each tool refresh pays a handful of `LookPath` calls plus `--version` runs only for hits, bounded by the shared 5-second probe timeout. The status payload grew fields; older frontends ignore unknown fields, so the change is wire-compatible.

## Testing

`go test` in `apps/desktop-launcher` pins source classification (`TestClassifyRuntimeSource`), assembly semantics including the skip-market-source fallback to the next command (`TestAnnotateRuntime`), and on-demand probing including the versionless-hit case (`TestProbeCommands`). `node --test frontend/test-app.cjs` asserts the card renders the hint with version and source, omits it for installed tools and misses, and drops the version segment when the probe could not read one.

## Related

The three-layer container toolchain decision this hint surfaces in the UI: [Linglong container toolchain availability](2026-08-19-linglong-container-toolchain.md).

[The toolchain market presentation](2026-09-12-desktop-launcher-toolchain-market-presentation.md) owns the card's layout, including the separate height tier this hint's line costs.

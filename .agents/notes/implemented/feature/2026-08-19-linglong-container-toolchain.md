# Agent Note: Linglong container toolchain availability

Status: implemented

English | [中文](2026-08-19-linglong-container-toolchain.zh.md)

## Problem

`apps/desktop-launcher` runs the harness as a `dsh web` subprocess inside a Linglong sandbox container, where the harness's bash toolchain (`tool-bash`) executes. The set of available tools is therefore only the base runtime `org.deepin.base` plus whatever `buildext.apt.depends` ships into the package. Real collisions hit at runtime: git was absent so repository operations failed outright, Node had to be the bundled 24 because beige's 20 would not start the harness, and the user's project directory, credentials, and CA bundle were unreachable from the container. This note records how the plan moved those "runtime collisions" forward into three defensive layers.

## Decision

Three defensive layers, all under `apps/desktop-launcher/` except the harness manifest injection (a preset overlay, no new `packages/` package). Self-containment is the baseline for normal-user distribution; the host environment is treated as unreliable.

**Layer 1 — build-time manifest and verification.** `linglong/tools.yaml` is the single source of truth: a `tools:` section (git, git-lfs, python3, curl, wget, jq, unzip, xxd, pnpm), an `installable:` on-demand whitelist (jdk21, go, ripgrep), and `excluded:` (gcc, clang, rustc, never bundled). `linglong.yaml` ships the tools through `buildext.apt.depends` (python3, python3-pip, curl, wget, unzip, zip, jq, xxd, ca-certificates; git was added earlier). `verify-tools.sh` runs on the host after `ll-builder build` and before `export`, checking the merged tree `linglong/output/binary/files`, because buildext merges into `$PREFIX` during preCommit and the build container cannot see the merged result. Any missing binary or failing version check exits non-zero and aborts the export; `test-verify-tools.sh` pins the pass and failure paths.

**Layer 2 — runtime self-check and model-visible tool list.** The launcher status bar opens a settings dialog whose toolchain section probes `git/python3/node/curl/jq/pnpm` through `CheckTools(DefaultToolSpecs())` and lists installed and installable tools; git credentials are inherited from the host home via `~/.git-credentials`. `configurePackagedEnvForHome` prepends `$HOME/.dsh-tools/bin` to PATH and `$HOME/.dsh-tools/lib` to LD_LIBRARY_PATH when they exist, so on-demand installs take effect after a harness restart. Harness-side injection reuses the bundled `standard` preset (Phase D below).

**Layer 3 — on-demand installation of heavy or rare tools.** `toolinstall.go` downloads static or runtime-self-contained archives (which avoids glibc and postinst coupling), verifies a sha256, atomically extracts into `$HOME/.dsh-tools/<name>-<ver>` (temp directory then `mv`, with tar path-escape rejection), and updates a `current/<name>` symlink. The directory sits on host disk via the container HOME mapping, so Linglong uninstall preserves it. The whitelist (`tools.yaml` `installable`, kept in sync with the runtime catalog `internal/toolchain/catalog.go`) bounds what may be installed.

**Credentials and data reachability.** The credential GUI and the `20-host-credentials.json` template were removed: the container inherits the host `~/.git-credentials` and `~/.ssh` through HOME, so packaged sessions use the host's stored git credentials directly. Container HOME is the host home, and `ll-cli uninstall` leaves user data alone (verified in the package-manager source), so stored credentials survive reinstall; the docs recommend exporting them. `ca-certificates` ships into `$PREFIX` for git/https/python verification; private CA is a documented append-and-`update-ca-certificates` item. linyaps forwards proxy environment variables by default.

**Phase D — model-visible container tool list.** The investigation concluded Branch A: the rendered system prompt is already logged — `packages/core/agent-loop/src/agent.ts` writes it as `request/header.header.system`, and instruction content additionally lands as `user/message` events — so appending tool text to the persona satisfies "model-visible ⟺ logged" without a new `SessionEventMap` member. A preset only mounts when a session selects it and the shipped default is `standard` (set by the web-app bundle patch), so a newly added preset would never reach the model. The deployment therefore overlays `standard`: `linglong/harness-overlay/config/agent-presets/standard/agent.cordis.yml` is the repo's standard roster with a container-toolchain paragraph appended to the persona `text`, and `linglong.yaml` build copies it over `${PREFIX}/harness/config/agent-presets/standard/`. The default id and the rest of the roster stay untouched, so every packaged session carries the tool list.

## Alternatives considered

**Verifying tools inside the build container (Layer 1).** Rejected: buildext's merge happens in preCommit, after the `build:` phase, so the container never sees the merged `$PREFIX`; the host-side check against `linglong/output/binary/files` is the only point that verifies what actually ships.

**Runtime-only discovery instead of a build-time gate (Layer 1).** A runtime health panel alone would ship a package missing a mandatory tool and only report it after install. The build-time gate makes a missing tool a build failure with an apt-package hint.

**Installing on-demand tools with the package manager inside the container (Layer 3).** Rejected: requires root, postinst hooks, and per-update residue inside the container; static archives under `$HOME` keep uninstall clean and avoid glibc coupling, at the cost of no auto-update.

**Host credential read-only mount as the default (credentials).** The design keeps container-local storage (`~/.git-credentials`) as the default and the host mount as an optional advanced mode, because it is simpler and the security semantics (the harness executes code on the user's behalf regardless) are documented rather than implied.

**A new `desktop-tools` preset instead of overlaying `standard` (Phase D).** Rejected after discovery: a preset mounts only on session selection and the packaged default is `standard`, so a new preset would sit on the roster but never compose a default session without a `packages/` bundle change. Overlaying `standard`'s persona reaches every session while keeping the default id.

**A new `SessionEventMap` member for the tool list (Phase D, Branch B).** Not needed: the persona/system-prompt text is reconstructable from the session log via `request/header.header.system`, so the injection is already logged.

## Consequences

The package grows: python3 plus pip add roughly 100 MB and git drags in the perl stack (tens of MB), with the small CLI tools on top, bringing the `.uab` to an estimated 375–400 MB from the 302 MB baseline; the on-demand layer is why go and ripgrep stay out of the package entirely. Non-static artifacts (for example user code compiled by an on-demand Go install) depend on the container glibc, which is the user's responsibility and documented. Linglong's private mapping hides `~/.linglong/<appid>` and isolates `~/.ssh`; the credential panel shows the real host path, and the mount template covers `.ssh`. The `standard` overlay is a frozen copy that drifts when the source preset evolves, so it carries a re-sync comment; `tools.yaml`'s installable entries now carry pinned sha256 kept in sync with the runtime catalog, `verify-tools.sh` fails the build on placeholder hashes, and on-demand install requires network.

## Testing

`test-verify-tools.sh` pins the build gate; the launcher Go module pins behavior with `go test` (toolinstall, toolcheck, gitcred, panel-state pure functions, env injection). The verify-tools gate is exercised by deleting an entry from a fixture tree, and the real Linglong build/export on the user's machine is the runtime acceptance check the plan assigns there.

## Related

Later fixes for the same subsystem — bundled-git exec-path wrapping, bundled pnpm, git-lfs, and the pre-push hook running through npm: [Portability fixes for the Linglong-packaged desktop client](../bug-fix/2026-08-24-linglong-git-exec-path-and-pnpm-bundling.md).

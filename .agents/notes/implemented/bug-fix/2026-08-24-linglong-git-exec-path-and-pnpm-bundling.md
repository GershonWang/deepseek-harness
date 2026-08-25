# Agent Note: Portability fixes for the Linglong-packaged desktop client

Status: implemented

English | [中文](2026-08-24-linglong-git-exec-path-and-pnpm-bundling.zh.md)

## Problem

The Linglong-packaged desktop client (com.deepseek.dsh-desktop) bundles git via `buildext.apt.depends`, but the merged file tree breaks git's runtime contract: the binary's compile-time exec-path is `/usr/lib/git-core`, which does not exist inside the container, while buildext relocated the helpers to `<prefix>/lib/git-core`. Every remote operation (`git push`/`fetch`/`clone`) fails with `git: 'remote-https' is not a git command` until a user manually exports `GIT_EXEC_PATH`. A real push session spent six full-access escalations and dozens of diagnostic steps on one branch push. Repos configured for Git LFS additionally fail their post-checkout/pre-push hooks because git-lfs is not bundled, forcing `--no-verify`. The pre-push typecheck hook (`pnpm run typecheck`) cannot start inside the container: pnpm's deps-status check first fails on the store SQLite index over a read-only HOME, then — once HOME is writable — demands a destructive node_modules rebuild and aborts without a TTY. All fixes must stay portable: the built `.uab` has to behave identically on any machine, so nothing may reference the builder's identity, HOME state, or absolute package paths written literally.

## Decision

The launcher sets `GIT_EXEC_PATH` for the whole harness process tree in `ConfigureChildEnv` via `packagedGitExecPath`, which derives `<files>/lib/git-core` from `os.Executable()` (identical on any machine for a fixed package id) and only sets the variable when the directory exists, covered by unit tests in `env_test.go`. That environment injection is the shipped mechanism; every git invocation under the harness — bash tool, LFS hooks, repo pre-push hook — inherits it. The pre-push hook now runs `npm run typecheck` instead of `pnpm run typecheck`, skipping pnpm's deps-status bootstrap entirely while executing the identical commands. pnpm is bundled into the package offline: `prepare-offline.sh` downloads the packageManager-pinned pnpm tarball into `stage/node/lib/node_modules/pnpm` with a `node/bin/pnpm` shim that execs the bundled node and pnpm CLI, and the assembly in `linglong.yaml` symlinks `bin/pnpm` to it, falling back to the corepack shim only when the bundle is absent. git-lfs is added to `buildext.apt.depends` and to `tools.yaml`. `verify-tools.sh` prefers the bundled pnpm and fails loudly when the merged git is not wrapped, so `git --version` green can no longer mask broken remote help.

## Alternatives considered

**Wrap `bin/git` in the merged tree after `ll-builder build`.**

Tried first as belt-and-suspenders hardening: `wrap-git-exec-path.sh` renamed `<prefix>/bin/git` to `git.real` and installed a shell wrapper deriving `GIT_EXEC_PATH` from its own location, and `build-linglong.sh` ran it before `verify-tools.sh` plus a post-export `git.real` check. A real build proved it never ships: `ll-builder export` re-derives the exported layer from the base overlay and build layers, so the post-build edit of `linglong/output/binary/files` does not reach the `.uab` (merged tree wrapped at 18:14:01, uab exported at 18:14:27, installed `bin/git` still the raw binary). The wrapper machinery was therefore removed; the env-injection mechanism ships inside the launcher binary and is the sole fix. Byte-patching the git binary itself is additionally impossible: `/usr/lib/git-core` is 17 bytes and cannot be replaced by a longer `<prefix>/lib/git-core`.

**Bundle the developer's pnpm store / corepack cache into the package.** The store SQLite index and corepack cache are machine-specific state; snapshots are stale, oversized, and wrong on every other machine.

**Keep corepack as the pnpm source.** First use downloads pnpm and caches it under `$HOME/.cache`; in the container HOME is read-only by default, so the tool is unavailable offline and under the default sandbox.

**Keep `pnpm run typecheck` in the pre-push hook and flip its config.** pnpm 11 ignored both `verify-deps-before-run` spellings we tested and proceeded to the destructive-rebuild abort; `npm run` executes the same commands without the bootstrap.

**Widen `config.d/10-mounts.json` for writable HOME directories.** Observed read-only HOME is the harness sandbox (`workspace-write` mode), not a Linglong mount — full-access makes HOME writable without any mount change, and the shipped mount template only takes effect when a user copies it. Widening it would neither fix the default experience nor survive on machines without the listed directories. Deferred to a product-level narrow permission (writable tool directories such as `~/.cache`, `~/.local/share`, `~/.git-credentials` plus its lock, and `~/.dsh-tools`, and outbound network for git) rather than a package change.

## Consequences

Any machine running the rebuilt `.uab` gets working git remote operations out of the box, LFS hooks that pass, typecheck hooks that run under the container, and offline pnpm — with no builder-specific state in the package. The cost is git exec-path repair depends on the launcher process: a direct shell into the container (dev only, `ll-builder run --exec`) must export `GIT_EXEC_PATH` itself, and `verify-tools.sh` gates only the presence of `lib/git-core/git-remote-https` (the env-injection target). pnpm and git-lfs add a small package-size increase for pnpm and git-lfs, and a hook command that assumes npm exists — already a repository prerequisite since every package script runs via npm. Missing pieces remain product-side: writable HOME directories and outbound network are still gated behind the full-access sandbox preset until a narrow permission ships, and the mount template plus README were intentionally not updated in this change.

## Testing

`go test ./internal/appenv/` (CGO_ENABLED=0) covers `packagedGitExecPath`. `verify-tools.sh`'s git-core-helper gate is pinned by `test-verify-tools.sh` fixtures. On the packaged machine the launcher-injected `GIT_EXEC_PATH` resolves `/opt/apps/com.deepseek.dsh-desktop/files/lib/git-core` and `git ls-remote`/`git push --dry-run` succeed without manual environment Full validation requires rebuilding via `sh apps/desktop-launcher/build-linglong.sh` (host) and replaying the branch-push scenario on a clean machine under the default sandbox without escalations or `--no-verify`.

## Related

Existing record for the same toolchain mechanism: [Linglong container toolchain availability](../../implemented/feature/2026-08-19-linglong-container-toolchain.md).

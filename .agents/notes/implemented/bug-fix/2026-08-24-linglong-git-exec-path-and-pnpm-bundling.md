# Agent Note: Portability fixes for the Linglong-packaged desktop client

Status: implemented

English | [中文](2026-08-24-linglong-git-exec-path-and-pnpm-bundling.zh.md)

## Problem

The Linglong-packaged desktop client (com.deepseek.dsh-desktop) bundles git via `buildext.apt.depends`, but the merged file tree breaks git's runtime contract: the binary's compile-time exec-path is `/usr/lib/git-core`, which does not exist inside the container, while buildext relocated the helpers to `<prefix>/lib/git-core`. Every remote operation (`git push`/`fetch`/`clone`) fails with `git: 'remote-https' is not a git command` until a user manually exports `GIT_EXEC_PATH`. A real push session spent six full-access escalations and dozens of diagnostic steps on one branch push. Repos configured for Git LFS additionally fail their post-checkout/pre-push hooks because git-lfs is not bundled, forcing `--no-verify`. The pre-push typecheck hook (`pnpm run typecheck`) cannot start inside the container: pnpm's deps-status check first fails on the store SQLite index over a read-only HOME, then — once HOME is writable — demands a destructive node_modules rebuild and aborts without a TTY. All fixes must stay portable: the built `.uab` has to behave identically on any machine, so nothing may reference the builder's identity, HOME state, or absolute package paths written literally.

## Decision

The packaged git is wrapped so its exec-path derives from its own location on every machine. `wrap-git-exec-path.sh` (run by `build-linglong.sh` after `ll-builder build`, before `verify-tools.sh`) renames `<prefix>/bin/git` to `git.real` and installs a shell wrapper that exports `GIT_EXEC_PATH="${GIT_EXEC_PATH:-$PREFIX/lib/git-core}"` (PREFIX derived from `dirname $0`) before exec'ing `git.real`. Idempotence and the post-export sanity check key on the existence of `git.real`, never on grepping the file content (the real git binary contains the string `GIT_EXEC_PATH` in its usage text). The launcher sets `GIT_EXEC_PATH` for the whole harness process tree in `ConfigureChildEnv` via `packagedGitExecPath`, which derives the prefix from `os.Executable()` and only sets the variable when `<files>/lib/git-core` exists, covered by unit tests in `env_test.go`. The pre-push hook now runs `npm run typecheck` instead of `pnpm run typecheck`, skipping pnpm's deps-status bootstrap entirely while executing the identical commands. pnpm is bundled into the package offline: `prepare-offline.sh` downloads the packageManager-pinned pnpm tarball into `stage/node/lib/node_modules/pnpm` with a `node/bin/pnpm` shim that execs the bundled node and pnpm CLI, and the assembly in `linglong.yaml` symlinks `bin/pnpm` to it, falling back to the corepack shim only when the bundle is absent. git-lfs is added to `buildext.apt.depends` and to `tools.yaml`. `verify-tools.sh` prefers the bundled pnpm and fails loudly when the merged git is not wrapped, so `git --version` green can no longer mask broken remote help.

## Alternatives considered

**Byte-patch the git binary's exec-path string.** The webkit helper path was fixed this way, but `/usr/lib/git-core` (17 bytes) cannot be replaced by `<prefix>/lib/git-core` of different length, and the prefix differs on no machine only if derived at runtime; the wrapper is the only length-safe form.

**Detect "already wrapped" by grepping `GIT_EXEC_PATH` in `bin/git`.** Rejected after tests showed the real git binary contains that exact string in its usage text, which would skip wrapping a fresh merge.

**Bundle the developer's pnpm store / corepack cache into the package.** The store SQLite index and corepack cache are machine-specific state; snapshots are stale, oversized, and wrong on every other machine.

**Keep corepack as the pnpm source.** First use downloads pnpm and caches it under `$HOME/.cache`; in the container HOME is read-only by default, so the tool is unavailable offline and under the default sandbox.

**Keep `pnpm run typecheck` in the pre-push hook and flip its config.** pnpm 11 ignored both `verify-deps-before-run` spellings we tested and proceeded to the destructive-rebuild abort; `npm run` executes the same commands without the bootstrap.

**Widen `config.d/10-mounts.json` for writable HOME directories.** Observed read-only HOME is the harness sandbox (`workspace-write` mode), not a Linglong mount — full-access makes HOME writable without any mount change, and the shipped mount template only takes effect when a user copies it. Widening it would neither fix the default experience nor survive on machines without the listed directories. Deferred to a product-level narrow permission (writable tool directories such as `~/.cache`, `~/.local/share`, `~/.git-credentials` plus its lock, and `~/.dsh-tools`, and outbound network for git) rather than a package change.

## Consequences

Any machine running the rebuilt `.uab` gets working git remote operations out of the box, LFS hooks that pass, typecheck hooks that run under the container, and offline pnpm — with no builder-specific state in the package. The cost is one wrapper layer over the real git binary (update the wrapper if buildext ever stops emitting `bin/git` alongside `lib/git-core`), a small package-size increase for pnpm and git-lfs, and a hook command that assumes npm exists — already a repository prerequisite since every package script runs via npm. Missing pieces remain product-side: writable HOME directories and outbound network are still gated behind the full-access sandbox preset until a narrow permission ships, and the mount template plus README were intentionally not updated in this change.

## Testing

`go test ./internal/appenv/` (CGO_ENABLED=0) covers `packagedGitExecPath`. The wrapper was exercised end-to-end on a synthetic prefix: default exec-path repoints from `/usr/lib/git-core` to the derived `<prefix>/lib/git-core`, reruns are idempotent, and an explicit `GIT_EXEC_PATH` still wins. Full validation requires rebuilding via `sh apps/desktop-launcher/build-linglong.sh` (host) and replaying the branch-push scenario on a clean machine under the default sandbox without escalations or `--no-verify`.

## Related

Existing record for the same toolchain mechanism: [Linglong container toolchain availability](../../implemented/feature/2026-08-19-linglong-container-toolchain.md).

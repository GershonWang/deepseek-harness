# Agent Note: Terminal sessions carry their own SHELL env fallback

Status: implemented

English | [中文](2026-09-10-terminal-session-shell-env.zh.md)

## Problem

Every new terminal session printed `dircolors: no SHELL environment variable, and no shell type option given`. Linglong launches GUI apps without `SHELL` in the environment, so the launcher process — and every child it spawns via `os.Environ()`, including PTY shells — has no `SHELL`. Bash then sets `$SHELL` from `/etc/passwd` as an unexported shell variable, so `echo $SHELL` inside the terminal looks correct and masks the gap; only child processes that read the environment (dircolors in `~/.bashrc`) observe it. The same trap made the first investigation round misleading: `echo $SHELL` in an unrelated shell also reported `/bin/bash` while `env` counted zero `SHELL` entries.

## Decision

`internal/terminal/session.go` owns the fix in `ensureSessionEnv`, the same seam that already backfilled `TERM`: when the merged env lacks a non-empty `SHELL`, it appends `SHELL=<command>` — the shell this session is starting (default `/bin/bash`). Non-empty inherited values are kept untouched, and empty `SHELL=` entries are dropped because `getenv` returns the first duplicate, which would defeat the fallback. The helper is a pure function over the base env slice so the keep/replace/inject rules are pinned by table-driven tests without depending on the test process's own environment; one integration test pins exactly-one non-empty `SHELL` on the real spawn path.

## Alternatives considered

**Inject `SHELL` once in the launcher's appenv for the whole process tree.** It would also cover the harness's non-interactive bash tool, which never reads `~/.bashrc` and shows no symptom, so the blast radius buys nothing observable today and touches the credential-scrubbed env chain.

**Drop the `SHELL` requirement in the user's `~/.bashrc` (add `-b` to dircolors).** Treats the symptom, edits a user-owned file from an app concern, and leaves every other env reader (`less`, prompt themes) broken under the same gap.

## Consequences

Terminal shells get a usable `SHELL` regardless of how the container was launched, and the per-session warning disappears. The value equals the command the session starts, so a future custom-shell option inherits correct semantics for free. Outside the terminal seam nothing changes: the harness bash tool and other children still run without `SHELL` in the environment, which is harmless for non-interactive use.

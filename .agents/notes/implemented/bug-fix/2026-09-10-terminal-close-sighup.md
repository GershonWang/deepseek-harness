# Agent Note: Terminal close signals SIGHUP, not SIGTERM

Status: implemented

English | [中文](2026-09-10-terminal-close-sighup.zh.md)

## Problem

Closing a terminal tab froze the UI for exactly three seconds every time. `Session.Close` sent `SIGTERM`, closed the PTY master, waited up to three seconds for exit, then fell back to `SIGKILL`. Interactive bash ignores `SIGTERM` (the job-control convention), so the grace period always elapsed. The commit that removed `Setpgid` for the linglong sandbox (which forbids it — `fork/exec: operation not permitted`) assumed closing the PTY master would make the kernel broadcast `SIGHUP` to the child, but that broadcast only reaches the foreground process group of the *controlling terminal*; without `Setpgid`/`Setsid` the child never establishes that relationship, so no signal is ever delivered. Measured on the real path: idle interactive bash survived the full 3.002 s until `SIGKILL`, while non-interactive bash died in ~200 µs on `SIGTERM`. The frontend awaits `TerminalClose` before repainting, so every tab close absorbed the whole wait.

## Decision

`Session.Close` sends `SIGHUP` as its first signal. HUP is the semantic terminal-closure signal, interactive shells do not ignore it, and it travels via a direct `kill(2)` — no process group and no controlling-terminal relationship needed, so the linglong restriction stays satisfied. The PTY-master close, the three-second grace, and the `SIGKILL` fallback remain for pathological cases; the healthy path now measures under a millisecond (idle bash ~0.9 ms, bash with a foreground child ~0.3 ms). A regression test spawns interactive bash and fails if `Close` exceeds two seconds, and the rest of the terminal suite still passes.

## Alternatives considered

**Give the child the PTY as its controlling terminal (`Setsid` + `Setctty` in `SysProcAttr`).** Then master-close would deliver the kernel's own `SIGHUP` broadcast. Rejected: it is the same process-attribute territory linglong already forbids for `Setpgid`, so it risks reintroducing the launch failure that commit removed.

**Close the tab in the UI before awaiting the backend.** Hides the wait instead of fixing it; the zombie shell and its jobs would linger for three seconds in the backend, and a user who reopens the terminal immediately could hit the still-terminating session.

## Consequences

Tab close is now perceived as instant, and shell teardown follows terminal semantics: bash exits on HUP and forwards it to running jobs, so foreground children are cleaned up by the shell itself. Processes that ignore HUP still hit the grace period and `SIGKILL`, unchanged. Any future attempt to restore process-group attributes must re-litigate the linglong `setpgid` ban recorded here rather than rediscover it at launch-failure time.

# Agent Note: the desktop shell's container-level terminal (tray entry)

Status: implemented

English | [中文](2026-11-15-desktop-shell-terminal.zh.md)

## Problem

Desktop already ships one terminal: the [sidebar terminal](../../../../packages/client/ui-sidebar-terminal/README.md), which belongs to a Session, spawns processes over Host RPC, is constrained by the Session sandbox, and can be taken over by the Agent. It works well, but it assumes the Host is up and a Session is selected.

The tray covers the opposite situation: the window is hidden while long tasks run, the Host failed to start, or a broken plugin configuration keeps the workspace from opening — and the user wants to look inside the container while the sidebar terminal has no usable host at all. The thin-shell build ([`apps/linyaps/desktop-launcher`](../../../../apps/linyaps/desktop-launcher/README.md)) solved exactly this with its title-bar terminal: it forked a PTY inside the client process, independent of the harness. Moving to the current Electron form dropped that entry.

## Decision

Add a **container-level** terminal to the Electron shell as a tray menu item, opened only on demand.

- **Ownership**: `apps/desktop/src/terminal-{contract,session,window}.ts` with `apps/desktop/src/client/terminal.ts` and `apps/desktop/renderer/terminal.{html,css}`. The main process spawns the PTY through `node-pty` directly, so it works with no Host and no harness involvement.
- **Form**: a separate native window rather than an overlay in the main window. On Linux closing the main window only hides it, which makes an overlay invisible, and the tray is precisely the entry available while the window is hidden. The About and toolchain windows are separate for the same reason.
- **Single instance and reuse**: a repeated click only focuses the window; a re-reported size reuses the session already running there, and only the session exiting or the window/application closing ends it. A page reload must not kill the user's command.
- **Teardown**: SIGHUP first, then SIGKILL after a grace period. Interactive shells ignore SIGTERM by convention; SIGHUP is the semantic terminal-close signal, and the grace period lets background jobs finish. The timer is unref'd, so quitting the application is never held up by it.
- **Platform**: Linux only, like About. Windows keeps its shorter menu and has no precedent for this escape hatch.

### Environment

The child environment comes from the shell's existing login-shell read (`login-shell-environment.ts`) with two repairs:

- `SHELL` is filled in when missing: a container launch from the GUI carries none, and tools such as dircolors in `~/.bashrc` read it to decide their output format. bash repairs the variable from the account record but does not export it, so the child never sees it and every terminal prints `dircolors: no SHELL environment variable`.
- `TERM` is **always** overwritten with `xterm-256color`: this window's renderer is xterm.js, while an inherited value describes *who launched the application* — a script or service launch may carry `dumb`, under which vim, less, and tmux refuse to run. The repository's own development environment inherits exactly `dumb`.

An empty value equals absence only for `SHELL`; an empty string is a meaningful setting for other variables (for example `GIT_EDITOR=`), so they are left alone.

### Copy and keyboard

The page renders only the view main sends: title, screen accessible name, and status line all come from the shell locale, with five keys added across eight languages. The status line appears when the session exited or failed to start and carries a restart control; both cases share one control set, since for the user the only difference is the wording.

Keyboard ownership follows the thin shell's conclusion ([terminal presentation](2026-09-12-desktop-launcher-terminal-presentation.md)): the shell's own copy, paste, and font-size chords are not intercepted here, and everything else — **including Escape** — belongs to the PTY. Escape is how vim leaves insert mode and the prefix of readline escape sequences.

### Packaging

`node-pty` joins `apps/desktop/package.json` `dependencies`: the main-process bundle's external-package allowlist is that manifest, and `packagedImportsPlugin` fails the bundle for an unregistered bare import. The native `.node` reaches `app.asar.unpacked` through the existing `asarUnpack` rule (`**/*.node`), and Electron's asar integration resolves it there transparently. xterm gets a second Vite build into `lib/terminal/` rather than sharing the welcome bundle — the sign-in path should not pay for a terminal.

The `dsh-app://shell` route therefore gains one branch: a shell document's `../lib/...` reference arrives as `/lib/...`, a build artifact beside `renderer/` rather than a shell document. Without the branch every page asset 404s while the HTML itself still returns 200 — a blank page whose only symptom is one resource error in the console.

## Alternatives considered

**Reuse the sidebar terminal and let the tray only raise the window.** No new code, but the sidebar terminal is Session-scoped: with no Host and no selected Session there is no terminal, which is the opposite of what the tray entry must cover.

**Move the thin shell's overlay into the main window.** It is invisible while the main window is hidden, and the tray is typically used exactly then.

**Let the page call `node-pty` directly.** A sandboxed renderer has no Node builtins, and it would hand process creation to the page.

**Inherit `TERM`.** It respects the host setting, but under `dumb` the terminal is unusable, and `TERM` here describes this window's renderer rather than the launch environment.

**Ship a single terminal with no restart control.** Fewer parts, but once the shell exits the window is dead and the user must close it and go back to the tray.

## Consequences

The container now has two terminals, so the boundary must be fixed: the sidebar terminal belongs to a Session, is sandbox-constrained, and can be taken over by the Agent; the tray terminal belongs to the container, bypasses the harness, and has no sandbox or Session semantics — it only answers "run commands on this machine". That also means the tray terminal is not constrained by the Session sandbox: like the thin shell's title-bar terminal it is the human's own escape hatch, not an Agent execution channel.

`node-pty` therefore appears once more in the application (about 316 KB, only the target platform's prebuild), independent of the copy inside the dsh tree. On Linux, "no usable terminal" is no longer a state the page can reach.

## Testing

`terminal-session.spec.ts` drives a real shell over `node-pty` (skipped where the host has no native module): command echo, exit code, resize, and idempotent close, with separate coverage for shell and environment resolution. `terminal-window.spec.ts` uses an injected fake PTY for window lifecycle, session reuse, the exit and failure views, output delivered only while the window lives, sender validation, the input bound, and SIGHUP on close. `tray.spec.ts` fixes the menu position and the platform difference, and `main-startup.spec.ts` fixes the `/lib/` route.

Verified on a real machine: the `apps/desktop` build loaded `dsh-app://shell/terminal.html` in a real Electron window, the tray window started the real login shell, `echo smoke-$((6*7))` echoed `smoke-42`, and the prompt colors, bracketed paste mode (`\u001b[?2004h`), and SIGWINCH all behaved.

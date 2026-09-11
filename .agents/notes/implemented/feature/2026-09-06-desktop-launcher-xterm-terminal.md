# Agent Note: xterm.js terminal in the desktop launcher

Status: implemented

English | [中文](2026-09-06-desktop-launcher-xterm-terminal.zh.md)

## Problem

The built-in terminal shipped as an output screen plus a bottom input box: keystrokes landed only in the input box and reached the PTY when Enter was pressed. Two user-visible defects followed:

1. **No real-time echo.** Bash echoes typed characters through the PTY (readline); while keystrokes never reached the PTY, the screen showed nothing as the user typed. Line editing (backspace, arrow-key history edits) rendered wrongly because the hand-rolled ANSI→HTML parser does not understand cursor-movement or line-erase sequences, and full-screen interactive programs (vim, top) could not work at all.
2. **No right-click copy/paste.** The Wails runtime suppresses the default context menu outside INPUT/TEXTAREA elements (its contextmenu handler preventDefaults when the target is neither an editable field nor covered by a selection), and the terminal area registered no custom handler of its own.

## Decision

The launcher frontend migrated to xterm.js 5.5.0, vendored locally under `frontend/vendor/` (UMD builds, offline loading, no CDN dependency):

- **One xterm instance per session.** `term.onData` forwards every keystroke to the PTY, so bash readline echoes in real time exactly like a real terminal. Control characters (Ctrl+C=`\x03`, Tab=`\t`, arrow-key escape sequences) are encoded by xterm per the terminal protocol; the frontend no longer intercepts them one by one.
- **Multi-tab state retention.** Each session owns a `.terminal-holder` wrapper node; `term.open()` runs exactly once, and switching tabs moves the whole DOM node (`content.replaceChildren(holder)`), preserving scrollback and cursor state across switches.
- **Right-click smart action** (classic terminal convention): with a selection → copy via `window.runtime.ClipboardSetText`; without → read `ClipboardGetText` and write to the PTY. Both APIs come from the frontend runtime Wails v2.15 injects (Linux implementation goes through GTK `gtk_clipboard_*`, available inside the linglong container), so **no Go code was added**.
- **Shortcuts.** Ctrl+Shift+C copies, Ctrl+Shift+V and Shift+Insert paste, intercepted through `attachCustomKeyEventHandler` (returning false stops xterm from also encoding the key as input). Font zoom and the tab shortcuts use the same interception point ([terminal presentation](2026-09-12-desktop-launcher-terminal-presentation.md)).
- **Size sync.** The fit addon computes cols/rows from real character cells, replacing the old `clientWidth/8` estimate; `term.onResize` → `TerminalResize` (SIGWINCH) keeps full-screen programs redrawing correctly.
- **Theme.** The original `--term-*` 16-color palette maps statically into xterm's `theme` option (xterm does not resolve CSS variables), and the dialog around the canvas mirrors those values in CSS so the card stays one palette under both system themes ([terminal presentation](2026-09-12-desktop-launcher-terminal-presentation.md)).

## Alternatives considered

**Forwarding input-box keystrokes one by one.** The smallest change, but the hand-rolled ANSI parser still would not understand cursor-movement sequences — the root cause of broken line editing and unusable interactive programs would remain, patching the wrong architecture.

**New Go clipboard binding methods.** Wails v2.15 already injects `ClipboardGetText/SetText` into `window.runtime`; wrapping them again in Go would duplicate an existing capability.

## Consequences

Real-time echo and right-click copy/paste work; interactive programs (vim, top, less) are usable. The bottom input box, the frontend command history (bash provides ↑↓ history natively), and `parseAnsiToHtml`/`ANSI_COLORS` (~200 lines removed) are gone; Ctrl+C/D/L and Tab no longer need frontend special-casing. The output cap changed from a 500KB string truncation to xterm's native `scrollback: 5000` buffer. The change also fixed a pre-existing `test-app.cjs` failure: tool-market elements (`#market-search` and friends) had joined `bindUI` without being added to the DOM stub, leaving all 10 cases red before; they now pass 10/10. The Go-side `internal/terminal` package (PTY session management) is untouched.

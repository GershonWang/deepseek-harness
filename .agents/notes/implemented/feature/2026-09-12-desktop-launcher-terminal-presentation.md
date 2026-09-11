# Agent Note: The desktop launcher terminal dialog's presentation and keyboard surface

Status: implemented

English | [中文](2026-09-12-desktop-launcher-terminal-presentation.zh.md)

## Problem

[The terminal rewrite](2026-09-06-desktop-launcher-xterm-terminal.md) replaced the launcher's hand-rolled output pane with xterm.js and got the session mechanics right; its presentation and keyboard surface were never revisited. Seven user-visible gaps followed.

Only the xterm canvas was pinned to a dark palette. The dialog head and tab strip still took the shell's semantic variables, so under a light system theme `--bg`/`--bg-toolbar` resolve to `#f5f5f5`/`#e6e6e6` and a light tab strip sat directly on the `#1a1a1a` canvas: the card read as two unrelated halves.

The status dot was colored only on the active tab (`.terminal-tab.active .terminal-tab-status`), so a running background session and an exited one looked identical, and a non-zero exit code existed only as grey text inside the scrollback.

The card was fixed at `min(900px, 90vw) × min(600px, 80vh)` with no way to enlarge it. Creating a session waits up to 2 s for the bundled font before `term.open`, and the content area stayed an unlabelled black block for that time. No key closed the dialog. Tabs could only be operated with the mouse, the font size was fixed at 14 px, and the viewport scrollbar kept the WebKitGTK default — a bright edge on a black canvas. The empty state was one grey line naming the absence without naming the way out.

## Decision

**The dialog owns its palette.** `.modal-terminal` redefines the shell's semantic variables (`--bg`, `--bg-panel`, `--bg-toolbar`, `--bg-hover`, `--border`, `--border-strong`, `--fg`, `--fg-strong`, `--fg-dim`, `--ok`, `--danger`, `--danger-text`) for its own subtree, and `--term-bg` moves next to them from its duplicate declaration on `.terminal-content`. The active tab and the canvas share `#1a1a1a`, so the active tab meets the terminal without a seam. `TERMINAL_THEME` in `app.js` and this block are mirrors of one palette and each names the other, because xterm's `theme` option takes literal colors and cannot read the CSS variables.

**Tab state is derived, not inherited.** `renderTerminalTabs` computes `running` / `exited` / `failed` from `status` and `exitCode` — a `closed` session whose `exitCode` is not a number counts as a plain exit rather than a failure — colors the dot for every tab (green running, red non-zero exit, neutral otherwise), and writes the exit code into the tab's `title` alongside its name.

**The card maximizes and restores.** A head button opens/closes the state and a double-click on the head toggles it; `is-maximized` sets `calc(100vw - 24px)` by `calc(100vh - 24px)` with `max-width`/`max-height: none`, and the two icons swap through CSS so the button never moves. The state is toggled on `#terminal-card`, the element carrying `modal-terminal` that `.modal-terminal.is-maximized` selects; the backdrop `#terminal-modal` is a different element that only centers and dims, so toggling the class there changes nothing on screen. The state lives on the card, so closing and reopening the dialog keeps the size the user chose. Sizing carries no CSS transition.

**Font size is one shared preference.** `terminalState.fontSize` starts from `localStorage` (`dsh-desktop.terminal.fontSize`), is clamped to 10–20 px, applies to every live session and to sessions created later, and is followed by a re-fit because the character cell changes with it. The tab strip carries `A−` / readout / `A+`, disabled at the bounds; `Ctrl+=`, `Ctrl+-`, and `Ctrl+0` do the same through xterm's key handler.

**Startup is labelled.** `createTerminalSession` writes `正在启动终端…` into the content area before it awaits the font, and `restoreTerminalContent` is the single place that re-attaches the active session (or the empty state, hint included) — used by tab close, by a failed new tab, and by the shortcut path.

## Keyboard ownership

The dialog and the PTY both want keys, and the split is explicit.

`attachCustomKeyEventHandler` decides what reaches the PTY: `Ctrl+Shift+C`/`Ctrl+Shift+V`/`Shift+Insert` (copy and paste), the font zoom combinations, and `Ctrl+Shift+T`/`Ctrl+Shift+W`/`Ctrl+Tab` return `false` so xterm does not encode them as control characters. Everything else, **Esc included**, is the PTY's.

The dialog-level handler `onTerminalKeydown` is bound once in `initTerminal` and returns immediately while `#terminal-modal` is hidden, so no key is taken from the rest of the shell. It owns three groups:

- **Esc** closes the dialog and returns focus to `#btn-terminal`, but only when focus is outside `#terminal-content`. xterm focuses its helper textarea on open and on every tab switch, so the dialog normally has focus inside the content area, and there Esc belongs to the PTY: it leaves vim's insert mode and prefixes readline escapes. Taking it would break the interactive programs the rewrite exists to support.
- **Ctrl+Shift+T** and **Ctrl+Shift+W** create and close a session through `newTerminalSession` / `closeTerminalSession`, the same entry points the tab strip buttons use.
- **Ctrl+Tab** and **Ctrl+Shift+Tab** cycle sessions through `cycleTerminalSession`, wrapping at both ends.

These three groups sit at document level rather than in xterm's key handler because they are dialog operations, independent of which part of the dialog holds focus, while xterm only sees a key when it is some session's input channel. xterm returns `false` for the same combinations so one press cannot both switch tabs and reach the PTY; `preventDefault` suppresses the webview's own default for them.

## Alternatives considered

**Let the terminal follow the shell theme, with a light 16-color palette.** The dialog would stay the same color under both themes. Rejected: it doubles the palette that must stay in step with `TERMINAL_THEME`, and terminals conventionally carry their own colors rather than inverting with the host.

**Maximize to a true full screen (`inset: 0`).** The largest possible terminal. Rejected: it covers the frameless window's custom title bar, so the user loses minimise/close along with it; a 12 px inset keeps the window controls reachable and reads as an enlarged card rather than a second mode.

**Animate the maximize transition.** Smoother than the current snap. Rejected: xterm recomputes rows and columns from the container, so every animation frame would need its own fit; the benefit is aesthetic and the cost is a resize race.

**A draggable, freely resizable floating terminal window.** The closest thing to a real terminal, and the shape the user first described. Rejected in scope: it needs drag/resize state, edge constraints, and persistence, and it must coexist with the modal backdrop's stacking — while the maximize button already recovers most of the space.

**Show the zoom controls without a shortcut, or the shortcut without controls.** Fewer moving parts either way. Rejected: the visible `A−`/`A+` pair is what makes the feature discoverable and matches the maximize button's reasoning, and the keyboard path is what makes it usable without leaving the terminal; the readout between them is what makes the current size visible at all.

**Give the exit notice only to the tab, and drop it from the scrollback.** Cleaner output. Rejected: the line marks where a session ended inside the output the user is reading, and it copies out with the log; the tab now carries the same fact for the case where the user is looking at another tab.

**Close on Esc unconditionally.** The plainest rule for a modal. Rejected: it drives the key out of the PTY, which is where vim's insert-mode exit and readline's escape prefix live.

**Close on Esc only while the alternate screen buffer is inactive.** Would let Esc close the dialog in a plain shell while leaving full-screen programs alone. Rejected: readline in vi mode and any program using `ESC` as a prefix are not on the alternate buffer either, so the buffer is not a reliable proxy for "the user means this as input".

**Handle the tab shortcuts inside xterm's key handler only.** One handler, no document listener. Rejected: xterm sees keys only while it holds focus, so the shortcuts would die the moment focus moved to the tab strip or the head — exactly where a user reaches for them.

**Per-session font size instead of one shared preference.** Each tab could keep its own zoom. Rejected: zoom is a viewing condition of the user, not a property of a session; a shared value also means a new tab opens at the size the user just chose.

## Consequences

Under a light system theme the terminal is now the only dark surface in the launcher. That is the deliberate trade for removing the seam; the alternative was a second palette to maintain.

Esc closes the dialog less often than a modal normally would, because the terminal takes focus on open and on every tab switch. The key still works whenever focus is on the tab strip or the head, and the close button remains the primary exit; what the guard buys is that `ESC` keeps working inside vim, `less`, and readline.

Maximize is sticky across closing and reopening the dialog, and the font size survives a restart, so both are states the user sets once. Sizing is instant rather than animated, and a maximized card stays 12 px inside the window instead of covering it.

The exit line stays in the scrollback and the tab repeats it as a tooltip. `restoreTerminalContent` becomes the only writer of the empty state, and it is also what keeps a failed new tab from wiping a running session's node out of the content area — before it, the failure path only logged to the console and left the placeholder on screen.

The test stub in `test-app.cjs` grew to cover the terminal path rather than staying decorative: `El` gained `querySelector`/`querySelectorAll` (subtree only, since `innerHTML` stays a string in the stub), `replaceChildren` (which also clears that string), `closest`, and `focus` updating `document.activeElement`; the document stub records listeners and fires `keydown` with a working `preventDefault`; the vm sandbox gained `Terminal`, `FitAddon`, `requestAnimationFrame`, and a `localStorage` stub on the fake `window`; the Wails stub gained the four `Terminal*` RPCs; and the terminal subtree is built nested as `index.html` has it (`#terminal-modal` > `#terminal-card` > head and body) instead of flat. The nesting is what makes a mis-targeted class visible: with every element a child of `body`, toggling `is-maximized` on the backdrop instead of the card passed every assertion. Tab clicks stay outside the stub's reach — tags render through `innerHTML`, so assertions read that string.

## Testing

`node --test frontend/test-app.cjs` runs 37 cases, 8 of which cover this change: session creation with xterm readiness and the running tab class; session output routing plus the exit-code classes; maximize and restore through both the button and the head double-click, including the re-fit, the no-session case, and the target check that `is-maximized` lands on the element carrying `modal-terminal`; font zoom through the buttons and the key handler with clamping, inheritance by a new session, preference restore, dirty stored values, and a throwing `setItem`; the startup placeholder and its replacement by the session node; Esc passing through to the PTY while focus is inside the terminal, closing and returning focus when it is not; the tab shortcuts including the "dialog hidden takes no keys" case, the holder swap, and the combinations not reaching the PTY; and the empty state after the last tab closes.

Rendering was not verified. This environment has no `make` and no `webkit2gtk-4.1`, so the frontend was not rebuilt into the embedded binary and nothing was checked against a live window: the palette, spacing, the maximized geometry, the scrollbar (WebKit-private pseudo-elements), and whether WebKitGTK delivers `Ctrl+Tab` to the page all still need a `make build` run in the dev workspace. The suite pins behavior and DOM state, not layout — the maximize button shipped broken for exactly that reason, caught by the user rather than by the green suite.

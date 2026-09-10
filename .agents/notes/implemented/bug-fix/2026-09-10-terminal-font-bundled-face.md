# Agent Note: Deliver the terminal font from the bundled frontend

Status: implemented

English | [中文](2026-09-10-terminal-font-bundled-face.zh.md)

## Problem

The terminal font restyle (xterm font stack, weight, and line height) shipped without any visible change on the linglong build. The stack's preferred families — Cascadia Code, JetBrains Mono, Menlo, Consolas — exist neither in the linglong container nor on the Deepin 25 host, so rendering fell through to Noto Sans Mono, visually close to the previous DejaVu Sans Mono. No installed monospace face carries a 500 weight, so `fontWeight: 500` resolved to Regular. The `-webkit-font-smoothing: subpixel-antialiased` declaration is dead on WebKitGTK, which leaves glyph smoothing to fontconfig. Independently, the container's fontconfig scans only system directories (`/usr/share/fonts`, `/usr/local/share/fonts`, `$XDG_DATA_HOME/fonts`), so a font copied into the app package's `share/fonts` is never seen.

## Decision

The terminal font ships inside the web frontend: `frontend/vendor/fonts/jetbrains-mono/` holds JetBrains Mono 2.304 Medium, Medium Italic, Bold, and Bold Italic plus OFL.txt, embedded by `go:embed all:frontend` and served by the Wails asset server. `styles.css` declares `@font-face` for the four faces (500/700 × normal/italic), and `createTerminalSession` awaits `document.fonts.load` for all four faces — raced against a 2-second timeout — before constructing the `Terminal`, so xterm measures its character cells with the final font. The font stack drops the never-present macOS/Windows families, `fontWeight` is `500`, and the dead `-webkit-font-smoothing` declarations are removed while `font-feature-settings: "liga" 0, "calt" 0` stays: JetBrains Mono ligatures would merge characters and break terminal cell alignment.

## Alternatives considered

**Register the font with fontconfig via a `FONTCONFIG_FILE` overlay.** The launcher would point fontconfig at a bundled conf adding the package font directory. Rejected: fontconfig initializes inside WebKit's own processes whose environment and sandbox inheritance the app does not control, the change would alter font resolution for the whole process tree to serve one webview need, and it requires Go and packaging changes. `@font-face` needs none and also works in browser-preview mode.

**Rely on system fallbacks without bundling a font.** Rendering stays on Noto Sans Mono/DejaVu with no 500 weight, so the reported visual gap persists.

**Install fonts into `$XDG_DATA_HOME/fonts` at app startup.** Writing into the user's home directory from the app is an out-of-scope side effect and races with the user's own font management.

## Consequences

The binary grows by about 1.1 MB of TTFs. Terminal glyphs render JetBrains Mono at true 500/700 weights regardless of host fonts; a missing font load or the timeout falls back to system monospace without blocking terminal creation, which also keeps the DOM-stub test environment (no `document.fonts`) working. The font stays page-scoped: GTK window chrome does not pick it up, and `fc-list` inside the container does not show it — the observable check is the rendered terminal, not fontconfig.

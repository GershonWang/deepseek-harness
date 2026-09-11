# Agent Note: The desktop launcher's toolchain market presentation

Status: implemented

English | [中文](2026-09-12-desktop-launcher-toolchain-market-presentation.zh.md)

## Problem

The toolchain market dialog carried presentation defects that made it read as a prototype next to the rest of the shell. Measured in Chromium against the shipped 19-tool catalog:

- The `分类 · 命令 · 体积` line never rendered. `.tool-card-meta` sets `overflow: hidden`, which drops that flex child's automatic minimum size to zero, making it the only row the column may compress; with the card pinned at a 120px `min-height` the column ran 14px short, so the line measured 0px in every card.
- Buttons and form controls did not inherit the shell font stack. `button`/`input`/`select` carry a UA `font` declaration, and a declaration on the element beats a value inherited from `body`: the computed family of `.btn`, `.market-tab`, `#market-search`, `.version-select` and `#host-path` was Arial while body text used the Noto Sans CJK stack.
- The disabled install button — the state a user watches through a 1.5 GB SDK download — rendered white on brand at `opacity: .4`, measured 2.29:1.
- Only `.btn` and `.host-row input` had a `:focus-visible` ring. Category tabs fell back to the UA ring (`auto 1px rgb(16,16,16)`, invisible on `#252526`), and the search box and version select only recolored their border on focus.
- Switching category resized the dialog, because its height followed the card grid: all 19 cards reached `max-height: 86vh`, while a category holding one card was less than half that.
- The host-import block — an advanced path that only exists inside the Linglong sandbox — was permanently expanded and took the dialog's bottom sixth.
- Every installed card carried a green border. Installed is the common state here (11 of 19 cards), so the accent marked nothing, while `可更新` competed inside the same color family.
- The empty state was one left-aligned grey line with no next step, the last breakpoint was two columns at 760px, and the grid used the platform scrollbar.
- The dark brand fill `#4d6bfe` put white text at 4.33:1, under the 4.5:1 threshold for 12px text; the [theme-token audit](../bug-fix/2026-09-11-desktop-launcher-theme-tokens.md) recorded this as a known open gap.

## Decision

Card height now covers the tallest content the card can hold. Grid rows measure themselves against the card's `min-height` whenever the grid scrolls — content taller than that overflows the card instead of growing the row — so the four-row card is `141px` and a card carrying the runtime hint is `160px` through `.has-runtime`. `.tool-card-meta` and `.tool-card-runtime` refuse to shrink, and the description clamps to two lines so no input can push the meta row out.

Form controls take `font: inherit` once, after `body`. Controls that declare their own size (`.btn` 12px, `.market-tab` 12px, `.builtin-toggle` 11px, `.version-select` 11.5px) keep it; the rest follow the 13px shell size.

Disabled buttons render as a neutral fill with a weakened but still legible label (`color-mix(in srgb, var(--fg) 75%, var(--bg-input))`): 4.64:1 in dark, 5.25:1 in light. The card's progress bar carries "in progress" instead of the button's own fading.

The `:focus-visible` rule that covered `.btn` and `.host-row input` now also covers `.market-tab`, `.builtin-toggle`, `#market-search` and `.version-select`.

The market dialog is fixed at `height: 86vh`, the same value as its `max-height`. The busiest category already reached that cap, so the fix leaves the common view unchanged and keeps a short category's whitespace inside the dialog, with scrolling owned by the grid.

Host import collapses to one line: a chevron, the title, and a summary reporting `已挂载 N 项`. `aria-expanded` is derived from the body's own collapsed state rather than tracked separately in HTML and JavaScript.

The accent color is reserved for the two states that ask for action: `updatable` takes a warn border, `installing` a brand border. `installed` remains on the card as a state marker without styling.

An empty result renders a centered conclusion, a hint, and a `清空筛选` button; the button appears only when a filter is actually set, so an empty catalog cannot look self-inflicted. The status bar reports `筛选 N 个` while a filter is set, and both the tabs and the search box route through `refreshMarketView()` so that count cannot go stale.

The grid falls back to two columns at 760px and one column at 560px, and uses a 10px scrollbar whose thumb is `--border-strong` (`--fg-dim` on hover) inset by a transparent border, declared for both `scrollbar-width`/`scrollbar-color` and `::-webkit-scrollbar`. The grid also carries a 10px right padding against an equal negative right margin: the padding opens the lane the scrollbar draws in, and the margin pushes the grid — scrollbar included — into the modal body's own right padding, so the scrollbar sits 10px closer to the dialog edge while the cards stay put. Sampling the shipped screenshot column by column before that gutter put the thumb's left edge 0–1px from the cards' right edge, with 12px of unused space still left between the thumb and the dialog border. That same sampling reads the thumb as `rgb(138,138,138)`, about 46% black, where the light theme's `--border-strong` is `#b0b0b0`: webkitgtk draws its own overlay scrollbar and does not take those declarations, so the gutter, not a width the styling could pin down, is what puts the scrollbar in the right place.

The dark `--brand` moves from `#4d6bfe` to `#4763f0`: white text on it measures 4.87:1, and the button against the card base `#252526` still measures 3.14:1, the 3:1 threshold for non-text UI. Light `--brand` `#2547d0` is unchanged at 7.26:1.

## Alternatives considered

**Stack the grid the way the server dialog stacks its two panels.** That solves "content swaps without moving the frame" by measuring the taller panel. Rejected: the market's content is a variable-length list, not two mutually exclusive panels, so there is no taller twin to measure.

**Give cards `flex-shrink: 0` on the meta row alone.** One declaration, no height change. Rejected on measurement: at a 141px card the content then needed 6px more than the box, so the meta row reappeared while the card overflowed.

**Drop the card `min-height` and let content size every row.** Rejected on measurement: rows resolved to 60px and all 19 cards overflowed; the row sizing here does not follow content once the grid scrolls.

**Raise the card `min-height` to 160px for every card.** Correct without a second tier. Rejected: it spends 19px of whitespace on every card that has no runtime hint, and most do not.

**Keep `opacity` for the disabled state and raise it to 0.6.** Closest to the existing code. Rejected: opacity fades the fill and the label together, so the text still lands under 4.5:1 in dark while the button's own background washes out.

**Give the dark button its own fill token instead of moving `--brand`.** Keeps the brand fill byte-identical. Rejected: two brand fills inside one shell differ by 4% and would drift apart; the shift is imperceptible and every other `--brand` fill (brand dot, pills, tabs, safe-mode block) lands on the same measured contrast.

**Move `--brand` further down to `#4055d6`.** Clears white text at 6.02:1. Rejected: the button drops to 2.54:1 against the card base, under the 3:1 non-text threshold, so it sinks into the dark card.

**Render the install button at content width, right-aligned, instead of filling the action row.** Keeps the button small. Rejected: the flexible element in that row is the version select, so an uninstalled card would carry a lone short button while its neighbours carry a full-width select, and the column of action rows would not line up.

**Expand host import by default whenever mounts exist.** Matches "show what is configured". Rejected: the collapsed state would flip as the toolchain event arrives and the dialog would move on its own.

**Keep the green border on installed cards.** Cheapest. Rejected: with 11 of 19 cards installed, the accent marks the common state, which is what made `可更新` hard to spot.

## Consequences

Cards are 21px taller than before (141px against 120px), and a row holding a runtime-hint card is 160px, so one screen shows about half a row fewer. The dialog keeps a fixed 86vh: a category with one card now shows whitespace inside the frame instead of a smaller frame, which is what the `筛选 N 个` count and the empty state are there to explain.

The disabled install button loses the brand fill, so an in-flight install reads as "this action is unavailable now" rather than "something is happening" — the progress bar and its percentage carry that meaning.

The dark brand fill is 4% darker wherever it is used as a fill, not only on buttons. The light theme is untouched.

Both the meta row and the runtime hint now consume height, so a future addition of a fifth card row must raise the `min-height` tier or the card will clip instead of growing.

## Testing

`node --test frontend/test-app.cjs` runs 37 cases, three of them new: host import starts collapsed, expands and collapses on click with `aria-expanded` tracking it, and reports its mount count; an empty result renders the clear-filter button, which resets the search box and restores every card. The DOM stub gained `hosts-toggle`, `hosts-body` and `hosts-summary` plus the initial `hidden` state of the body. The empty-state case asserts against the grid's own children, because the stub's `querySelector` scans the element registry and would still find elements removed by `innerHTML = ""`.

Layout, theming and contrast are verified by rendering `frontend/index.html` in Chromium: the cached Playwright build under `~/.cache/ms-playwright/chromium-1234/chrome-linux64/chrome` starts in this environment, so the harness loads the real stylesheet, feeds `renderTools` a 19-tool payload, and asserts 36 conditions per run in both color schemes — dialog height identical across all four categories, no card overflowing, the meta row visible, control fonts equal to the body stack, disabled/main-button contrast, uninstall width stable across the two-click confirm, focus rings solid 2px, accent borders distinct for updatable/installing, host collapse, empty state, filtered count, and grid columns at 3/2/1. A separate geometry run measures the scrollbar lane: with the grid packed to 19 cards the cards' right edge clears the scrollport's right edge by 10px in both schemes, the grid's right edge lands 6px from the modal body's content edge, and no card overflows. It calls `setupHostsToggle()` itself because browser preview returns from `init()` before the bindings.

That harness substitutes for the manual `make build` pass earlier launcher notes had to require; it does not replace running the packaged app, which is still what confirms webkitgtk-specific rendering.

## Related

- [Toolchain market cards report in-container runtime availability](2026-09-10-toolchain-market-runtime-availability-hint.md) owns the runtime-hint line whose second height tier this change adds.
- [The desktop launcher's server dialog presentation](2026-09-11-desktop-launcher-server-dialog-presentation.md) is the earlier decision that a dialog's size should not move when its content changes.
- [Theme tokens in the desktop launcher shell](../bug-fix/2026-09-11-desktop-launcher-theme-tokens.md) recorded the dark primary-button contrast gap this change closes.

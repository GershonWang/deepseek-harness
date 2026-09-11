# Agent Note: Theme tokens in the desktop launcher shell

Status: implemented

English | [中文](2026-09-11-desktop-launcher-theme-tokens.zh.md)

## Problem

The launcher shell declares its palette twice — `:root` for dark and `@media (prefers-color-scheme: light)` for light — but several surfaces bypassed it, and the light theme carried the defects.

The doctor report colored its summary and its check icons with inline `style="color:#…"` values copied from the dark palette. On a light system the report rendered pale green and yellow on white: 1.82:1 for ✓, 2.46:1 for ✗, 2.31:1 for the fixable count, 1.99:1 for info, 1.61:1 for the fallback. Text needs 4.5:1.

Two variables were referenced but never defined. `--bg-titlebar` fed `.icon-badge`'s 2px border, which therefore kept its `#222` fallback and drew a 12.75:1 dark ring around the badge on the light titlebar. `--mono` fed `.doctor-detail` and `.doctor-actions`, which therefore fell back to the generic `monospace` and never reached the bundled JetBrains Mono.

Three rules hardcoded `rgba(204, 167, 0, …)` — the dark `--warn` with alpha. In the light theme, `--warn` is `#9d6d00`, so the diagnosing spinner drew its ring in two different yellows (its top segment used `var(--warn)`) and the auto-diagnosis hint kept a bright yellow-brown background and border.

One class of problem cannot be fixed by tokenizing alone: the foreground colors on solid semantic fills were literals (`#fff` on `--brand` and `--danger`, `#1a1a1a` on `--warn`) while their backgrounds changed per theme. A single foreground cannot satisfy both themes, because the two themes' fills have different luminances.

## Decision

Every color in the shell now comes from a token, and the audit that proves it is repeatable by parsing `styles.css`: color literals exist only in the two theme blocks, plus two deliberate exceptions named under Consequences.

Role tokens were split where a fill value is not a legal text value. `--danger-text` joins the existing `--brand-text`: the dark theme sets `#f48771` (6.24:1 on `--bg-panel`) and the light theme sets `var(--danger)` (4.93:1 on white). Every `color: var(--danger)` in the shell became `color: var(--danger-text)`, while `border-color`, `background`, and the status dots kept `--danger` as their fill.

Foreground-on-fill pairs moved to `--on-brand`, `--on-danger`, and `--on-warn`, so each theme names the foreground that passes on its own fill: white on brand in both themes, white on danger in both, `#1a1a1a` on `--warn` in dark and white on `--warn` in light (the light `--warn` is dark enough that dark text fell to 3.83:1, white reaches 4.54:1).

`:root` declares `color-scheme: light dark`, so the pieces the browser paints itself — scrollbars, native controls, the default canvas — follow the same preference the palette does. `--mono` is defined once in `:root` for the three monospace surfaces. `.icon-badge` uses `var(--bg-toolbar)`, which is what its border was always meant to match. The three alpha warn tints became `color-mix(in srgb, var(--warn) N%, transparent)`, the construction the toolchain dialog already uses.

The doctor report emits semantic classes (`sev-ok`, `sev-error`, `sev-warn`, `sev-info`, `sev-muted`) instead of inline colors; `renderDoctorReport` maps a check's `OK` and `Severity` to one of those names.

Twenty-five `--term-*` variables were deleted; only `--term-bg` had a reader. The 16-color terminal palette lives in `TERMINAL_THEME` in `app.js`, because xterm's `theme` option takes concrete values rather than CSS variables, and that palette deliberately does not follow the shell theme.

## Alternatives considered

**Keep the dark values inline and add a light set behind a media query.** Inline styles cannot be switched by `prefers-color-scheme`. Rejected: it would require JavaScript to read the scheme and rewrite the DOM on change, for colors CSS already owns.

**Leave `--danger` as the danger text color everywhere and accept 4.29:1 in the dark theme.** The smallest diff. Rejected: that is a regression, not a fix — those texts were 6.24:1 before, and routing them through a fill token because the token happens to be red would trade a light-theme defect for a dark-theme one.

**Add `--ok-text` and `--warn-text` for symmetry with `--danger-text`.** Rejected: `--ok` and `--warn` already pass as text in both themes (8.40:1 and 6.63:1 dark, 5.37:1 and 4.54:1 light), so those tokens would be aliases that buy nothing. The asymmetry is a measurement result, not an oversight.

**Read the terminal palette from the CSS variables at runtime with `getComputedStyle`.** Keeps one source for all 16 colors. Rejected: the shell palette and the terminal palette are independent — the terminal stays dark regardless of the system theme — so the lookup would add a runtime dependency and a re-read path to keep two things in sync that do not need to be.

**Darken the dark `--brand` so white text on the primary button reaches 4.5:1.** The fill is `#4d6bfe`, which yields 4.33:1. Rejected for this change: it alters the brand fill itself, which is a product-identity decision rather than a tokenization one. [The toolchain market presentation](../feature/2026-09-12-desktop-launcher-toolchain-market-presentation.md) later took that decision (see Consequences).

**Tokenize the modal scrim.** Rejected: a black scrim at 45% is conventional over both light and dark content, and no theme variation was wanted.

## Consequences

Measured before and after, on the surfaces each color actually sits on:

| Element | Light, before | Light, after | Dark, before | Dark, after |
|---|---|---|---|---|
| ✓ / `sev-ok` | 1.82:1 | 5.37:1 | 8.40:1 | 8.40:1 |
| ✗ / `sev-error` | 2.46:1 | 4.93:1 | 6.24:1 | 6.24:1 |
| fixable / `sev-warn` | 2.31:1 | 4.54:1 | 6.63:1 | 6.63:1 |
| info / `sev-info` | 1.99:1 | 7.26:1 | 7.71:1 | 4.96:1 |
| fallback / `sev-muted` | 1.61:1 | 5.10:1 | 9.54:1 | 5.65:1 |
| danger text elsewhere | 4.93:1 | 4.93:1 | 4.29:1 | 6.24:1 |

The icon badge's unintended dark ring is gone in both themes (its border now matches the titlebar). The spinner ring is one color in both themes. The doctor panes render in the bundled JetBrains Mono.

One known gap remains, recorded rather than fixed: the dark theme's close-button hover measures 3.57:1, which clears the 3:1 threshold that applies to an icon-only control. The other gap recorded here — white text on the dark theme's primary button at 4.33:1, below the 4.5:1 text threshold — is closed by [the toolchain market presentation](../feature/2026-09-12-desktop-launcher-toolchain-market-presentation.md): the dark `--brand` now sits at `#4763f0`, which puts white text at 4.87:1 while the button keeps 3.14:1 against the card base.

Two color literals remain outside the theme blocks by design: the modal scrim `rgba(0, 0, 0, 0.45)`, and `--term-bg: #1a1a1a` for the terminal surface.

## Testing

`node --test frontend/test-app.cjs` runs 24 cases. The added case drives a diagnostic report through the fake Wails runtime and asserts that the summary and the check list carry the semantic classes and contain no inline `color` declaration — the assertion that fails on the pre-change code, where the summary carried `style="color:#89d185"`.

Contrast ratios were computed from the palette values rather than observed: this environment has no browser engine that starts (the Playwright Chromium in the cache needs `libnspr4`), so the rendered result still needs a `make build` run in the dev workspace, in both themes, before packaging.

# Agent Note: Layout verification for the desktop launcher frontend

Status: implemented

English | [中文](2026-09-12-desktop-launcher-frontend-preview-tool.zh.md)

## Problem

The launcher frontend's only automated test, `frontend/test-app.cjs`, runs `app.js` against a hand-written DOM stub. That stub implements neither stylesheets nor layout, so a whole class of defect is invisible to it, and the defects kept arriving: the server dialog resized by about 125 px when the connection mode changed, the address row's flex rules silently never applied because the selector could not match, and later a connected notice grew the card by 32 px while an unusually tall stack inflated the address field to 174 px. The last two were found by measuring in a real browser; all of them passed a green suite.

## Decision

`apps/desktop-launcher/frontend/tools/preview.mjs` renders `index.html` in headless Chromium and measures it. It is a plain Node ESM script with no dependencies: the browser comes from `DSH_PREVIEW_BROWSER`, otherwise the Playwright cache already on the machine, otherwise `PATH`, and the protocol runs over Node's built-in `WebSocket`. Chromium's `HOME` and XDG directories are redirected inside the output directory, without which it stalls against a read-only home under a file sandbox.

The preview page is generated at run time from `index.html` — scripts stripped, the stylesheet path rewritten — so it cannot drift from the page it verifies. States live in a `FIXTURES` table, one entry per dialog state, each naming the text, classes, values, visibility and disabled flags that `app.js` writes; adding a state is a data edit. One entry per state means the fixture list is exactly the coverage.

`verify` asserts the geometry the DOM stub cannot see: every routine state keeps one card height within a 1 px tolerance, the two panels are always equal in height, the address box keeps its two-line reservation, and the service-address field stays within its cap. `measure` prints the same geometry as JSON, and `render` writes one screenshot per theme and state into the ignored `frontend/.preview`. `lefthook.yml` runs `verify` on pre-push with a glob on `apps/desktop-launcher/frontend/**`.

## Alternatives considered

**Drive it with Playwright**, which `apps/web` already depends on. Rejected: `apps/desktop-launcher` has no `package.json`, so the script would either live in a package it does not belong to or add a dependency, a lockfile change and a third-party-notices regeneration for a developer-only tool. The cached browser Playwright already installed is used either way.

**Baseline screenshot comparison.** The usual visual-regression shape. Rejected: baselines are sensitive to fonts, device pixel ratio and renderer version, so every intentional change becomes a baseline update and the review question becomes "did this PNG change" instead of "did this invariant break". The properties worth protecting here are geometric and can be asserted numerically.

**Render through the real webkit2gtk engine from the Go side.** Closest to production. Rejected: it needs X or Wayland and CGO on every run, which prices a layout check out of the pre-push path it belongs on.

**Teach the DOM stub enough CSS to catch this.** Rejected: it would assert our own stub's arithmetic rather than the browser's layout, and every rule of flex, grid and line-height would have to be reimplemented to be believed.

**Run it in CI rather than pre-push.** Rejected for now: CI runners have no browser, so this would add a lane that installs one — a larger commitment than a local hook, and one better made once the invariants have proven themselves locally.

## Consequences

Pushes that touch the launcher frontend now spend about three extra seconds in the hook, measured at 2.44 s; pushes that touch nothing else skip it through the glob. On a machine with no browser the tool prints why and exits successfully, so it cannot block a push, at the cost of silently contributing no signal there.

The tool only measures the states in its fixture table. A new dialog, or a new state of an existing dialog, is unmeasured until it is added — the table is the coverage, and nothing mechanically forces a new state to be registered. Screenshots are written to an ignored directory, so they are artifacts to look at, not reviewable baselines.

## Testing

`node frontend/tools/preview.mjs verify` passes on the committed frontend and prints the geometry of all seven states in both themes: card 307/308 for every routine state, 398 for the failure state with the safe-mode block, address box 41, service-address field 66, and 116 when the stack is unusually tall.

The gate was shown to reject an invalid case: removing the reserved line from `.ext-state` makes the service-address field vary by 18 px between states, and the command exits 1 naming that measurement. `node --test frontend/test-app.cjs` (37 cases) is unaffected, since the tool reads `index.html` rather than the stub.

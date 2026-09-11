# Agent Note: The desktop launcher's server dialog presentation

Status: implemented

English | [中文](2026-09-11-desktop-launcher-server-dialog-presentation.zh.md)

## Problem

The server dialog was the last surface in the launcher shell that had not been rebuilt against the language the toolchain dialog established (section cards, section headings, status colors, divided action rows). Three defects made it obvious.

The 连接模式 row carried the class `mode-row`, which no rule in `styles.css` matched, so two platform radio buttons sat against an otherwise styled card. The state row was plain text carrying no color, while the identical state had a semantic dot in the status bar. The address row's value never received the rule written for it: `.row > span:last-child` cannot match a value followed by the copy button, so neither `flex: 1 1 auto` nor `overflow-wrap: anywhere` applied to it, and the long service token wrapped only because it happened to contain a hyphen.

The status bar had a second problem of a different kind: it printed the whole service URL — `http://127.0.0.1:<port>/?token=<secret>` — as permanently visible text, so every screenshot or screen share handed over a live bearer credential.

## Decision

The dialog reuses the existing component vocabulary instead of a new one: `.section-title` for the card heading, `.row-value` for value columns, `.actions-bar` for the divider above the buttons, and the status bar's state colors. Its width becomes 560 px, matching the toolchain dialog, because the address wraps at any width and 520 px wrapped it into more fragments.

Connection mode is a segmented control. The two radio inputs remain the single source of truth — `app.js` still reads `input[name="mode"]:checked` and binds `change` on `input[name="mode"]` — and each `label` wraps its input plus a `span` that carries the appearance. Selection is styled through `input:checked + span`, a sibling selector, so no assumption is made about `:has()` support in the webkit2gtk container.

The state row takes the status bar's semantics (running = ok, starting = warn, failed = danger, otherwise neutral) and draws its dot from `currentColor` in a `::before` rule, so no DOM node was added and the DOM stub in `test-app.cjs` needed no new element. The address row's monospace font and boxed background follow the same condition as the copy button — the running state — so `harness 正在启动…` and `LastExit` are not dressed as copyable fields.

The status bar shows the host and port only (`127.0.0.1:3456`). The full address, token included, stays in the dialog, which renders it and copies it with one click ([copy affordance](2026-09-11-desktop-launcher-copy-service-address.md)).

## Alternatives considered

**Fix only the two defects.** The smallest change that leaves the dialog looking like a different application. Rejected: the restyle and the fix touch the same lines and the same elements, so splitting them buys a second commit rather than a smaller change.

**Style the selected segment with `:has(input:checked)`.** The most direct expression of "the option containing the checked radio". Rejected: the container's webkit2gtk version is fixed by the packaging, and the sibling selector reaches the same result without depending on `:has()` being available.

**Render the state dot as a real `.state-dot` element.** Reuses an existing class verbatim. Rejected: it needs a new id in `index.html` and a matching entry in the `test-app.cjs` element list, for a purely decorative mark; a `currentColor` pseudo-element produces the same dot from the same color token.

**Keep the full URL in the status bar.** Nothing is lost by leaving it. Rejected: the status bar is visible whenever the window is, and the URL carries a bearer credential, so it leaks through screenshots and screen shares without anyone copying it.

**Strip only the query string and keep the rest of the URL.** Narrower than showing the host and port. Rejected: the token is the only part of the URL that identifies nothing at a glance, and `host:port` is what the user reads the status bar for.

## Consequences

The dialog now reads as part of the same shell as the toolchain dialog, and the address row's layout no longer depends on whether the token contains a break character. The status bar no longer offers a value that can be selected and pasted into a browser; that path is the dialog's copy button, which hands over the exact URL the web server accepts. `hostLabel` returns an empty label for an address it cannot parse instead of falling back to the raw string, so a malformed address cannot defeat the masking — the cost is that a malformed address shows no location at all.

## Testing

`node --test frontend/test-app.cjs` runs 22 cases, of which three cover this change: the state row's semantic class in the running and starting states, the address row's `mono`/`code` classes following the running state, and the status bar text in container and external mode containing the host and port but not the token. The vm sandbox the test builds now receives `URL`, without which `hostLabel` fell into its parse-failure branch and the masking assertions passed for the wrong reason.

Rendering was not verified in a browser: this environment has no engine that can run (the Playwright Chromium in the cache fails to start without `libnspr4`). Theme, spacing, hover, and focus behavior still need a `make build` run in the dev workspace before packaging.

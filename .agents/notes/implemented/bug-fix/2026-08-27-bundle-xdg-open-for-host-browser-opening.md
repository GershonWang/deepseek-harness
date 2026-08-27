# Agent Note: Bundle a real xdg-open in the desktop launcher package

Status: implemented

English | [中文](2026-08-27-bundle-xdg-open-for-host-browser-opening.zh.md)

## Problem

Opening external links from the dsh-desktop app does nothing. Two independent failures stack: Wails WebKitGTK never creates a new browsing context, so a `target="_blank"` anchor click is swallowed before any opener runs; and the only opener the launcher has (`BrowserOpenURL` → `pkg/browser` → `xdg-open`) cannot work in the Linglong container, whose `/bin/xdg-open` is a 75-byte `systemd-run --user` forwarding stub — measured to recurse and fail immediately. The about dialog's repository link (`target="_blank"`) therefore does nothing, and any future handoff that hands the launcher a URL would fail at the same open step. The container has no browser binary and no https default handler at all, so a local open is impossible by design; the host default browser must be reached through the session-bus portal (`org.freedesktop.portal.Desktop`, reachable from the container, backend `…impl.portal.desktop.dde`).

## Decision

Ship the real xdg-utils through the existing packaging pipeline instead of hand-rolling D-Bus: `buildext.apt.depends` gains `xdg-utils`, whose merge puts a working `xdg-open` at `${PREFIX}/bin` (first on the container PATH, shadowing the broken base stub). Every handoff ends in the Wails runtime `BrowserOpenURL` (preview mode without `window.runtime` keeps the native `target="_blank"` behavior), which routes to the host default browser via the host portal under `XDG_CURRENT_DESKTOP=DDE`. The side gain: every other xdg-open consumer inside the container — the harness host services for `host.openPath`/`settings.openDocument`/`dsh web --open` — resolves the real binary too. `tools.yaml` registers `xdg-open` (no `verify` command: any safe invocation is meaningless and a real one opens a browser) and the `test-verify-tools.sh` pass-tree fixture gains the binary, so `verify-tools.sh` gates the merged tree as it does git.

The launcher cannot observe clicks inside the cross-origin GUI iframe, so the packaged GUI gets a bridge instead: `prepare-offline.sh` runs `linglong/inject-link-bridge.sh` after the `dsh deploy` step, which copies `linglong/dsh-link-bridge.js` into the packaged `dsh-web-frontend/dist/assets/` (served same-origin by frontend-static) and appends its script tag to `dist/index.html` (idempotent). The bridge, active only when `window.parent !== window`, intercepts each primary-button click on a `target="_blank"` HTTP(S) anchor (honoring `defaultPrevented` and `download`) and posts `{ dshDesktop: true, type: 'open-external', url }` to `window.parent`. `frontend/app.js` accepts that message only when the source is the harness frame's `contentWindow` and the URL is http(s), then opens it. This covers the container mode; an external harness (`dsh web` run elsewhere) serves an uninjected GUI whose links still do nothing. The about dialog's repository link is wired the same way.

## Alternatives considered

**Call the portal directly from Go (`org.freedesktop.portal.Desktop.OpenURI` via godbus).** Deterministic and independent of xdg-utils behavior, but adds Go code and a test surface, and the packaging-layer fix is the user-preferred route. The two are not mutually exclusive: if the bundled xdg-open empirically fails to route through the portal, the Go portal call remains the fallback design.

**Fix the base runtime's forwarding stub.** The base layer belongs to org.deepin.base; the app cannot change it, which is exactly why the real xdg-utils is merged into `${PREFIX}` instead.

**Ship a browser binary in the package.** Unnecessary — the host default browser is the goal — and oversized.

## Consequences

The rebuilt `.uab` opens external links — the about dialog's repository link and every `target="_blank"` HTTP(S) link inside the packaged GUI — in the machine's default browser, and makes xdg-open resolvable for every in-container consumer. Costs: a slightly larger package (xdg-utils and its apt dependencies), and the change depends on the deepin-forked xdg-open actually routing through the host portal — if a post-rebuild end-to-end test fails, the fallback is the Go portal call. The bridge covers the container mode only: an external harness serves an uninjected GUI, so its links still do nothing; covering that mode would require the client-side hook or a launcher-side proxy, neither shipped.

## Testing

`node --check` on the edited `frontend/app.js` and `linglong/dsh-link-bridge.js`; `sh apps/desktop-launcher/linglong/test-verify-tools.sh` passes all four paths (the pass-tree fixture includes `bin/xdg-open`); `linglong.yaml` parses with the repo's yaml library; `inject-link-bridge.sh` run twice against a fixture dist proves both injection and idempotency; a jsdom two-window protocol smoke proves the bridge captures exactly the https handoff (javascript:/mailto: pass through), the launcher opens exactly that URL through `BrowserOpenURL`, spoofed sources and missing markers are ignored, and the standalone page keeps native behavior. Full validation on a rebuilt `.uab` confirmed both surfaces: the about dialog's repository link and `target="_blank"` links inside the packaged GUI open the host's default browser.

## Related

Same packaging pipeline precedent: [Portability fixes for the Linglong-packaged desktop client](../../implemented/bug-fix/2026-08-24-linglong-git-exec-path-and-pnpm-bundling.md). Host-side xdg-open consumers in the harness: [Tool-call file open in OS](../../implemented/feature/2026-07-28-tool-call-file-open-in-os.md) and [Open the ready web UI](../../implemented/feature/2026-08-12-open-ready-web-ui.md).
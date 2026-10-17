# Agent Note: Desktop last-Session restore

Status: implemented

English | [中文](2026-10-18-desktop-last-session-restore.zh.md)

## Problem

The Linglong thin shell embeds the harness Web GUI in an iframe whose top-level document comes from the shell's own `wails://` origin. The GUI keeps "the Session I had open" in `localStorage['dsh.sessions.current']`, and inside that cross-origin iframe WebKit treats the storage as third-party: the client can read and write it during a run, but nothing reaches disk. After 2026-09-28 no `http_127.0.0.1_*.localstorage` file was written again for the harness origin, while the shell's own origin database kept being opened, and the origin actually in use had no database at all. Every launch therefore started with no saved selection, and the startup policy answered that with a blank Session — the conversation the user last worked in never came back.

A previous change made the listen port stable across launches (`webPort`), which removed one variable: the origin no longer changes. It does not make the storage persist, so the symptom stayed.

## Decision

Two independent paths restore the last-open Session, each covering where the other cannot.

**The client keeps a storage-free fallback.** With no saved selection, startup selection opens the Workspace's most recently updated content-bearing Session — non-blank and not archived — instead of reusing or creating a blank one; a Workspace with no such member keeps the existing blank policy (`recentEngagedSession` in `packages/client/ui-workspace/src/client/navigation.ts`). This covers every client that cannot remember itself: the thin shell, a cleared browser profile, a private window.

**The shell remembers the exact Session.** The packaged link bridge (`apps/linyaps/desktop-launcher/linglong/dsh-link-bridge.js`) polls the client's selection key and reports changes as `{ dshDesktop: true, type: "current-session", sessionId }`, where `null` means nothing is selected. `frontend/app.js` forwards that to `App.RememberSession`, which keeps the id in `~/.config/dsh-desktop/config.json` as `lastSessionId` after a charset and length check. On the next spawn, `internal/appenv/restore_session.go` inserts a plugin row into the `--patch` overlay carrying the id as `config.sessionId`; at activation the plugin registers a `webserver/index-inject` row that exposes `__DSH_DESKTOP_RESTORE_SESSION__` in the served `index.html`. The bridge writes that value into the client's selection key before the client module entry runs, and only when the client has no selection of its own.

## Where the fact lives

Session selection is UI state, so the GUI is the only component that observes it meaningfully; the shell learns it from the page instead of deriving it server-side, and a Session the user never opened cannot be mistaken for the last one. What the shell adds is persistence, not a second opinion: it records what the page last reported and feeds it back into the page, so the client still makes every decision about what to open. The fallback stays in the client for the same reason — it is the client's own policy for having no memory, not a shell rule.

## Alternatives considered

**Fix the embedding so the storage persists.** Making the harness document the top-level one restores first-party storage (the pre-August-2026 shell did that, and its per-port databases are still on disk), but the shell's title bar, stage, status bar, guide page, dialogs, and failure pages are HTML living in that document and would have to become native chrome or an overlay injected into a page the shell does not own. Proxying the GUI under the shell's own origin keeps the UI but must carry HTTP, WebSocket upgrades, and SSE. The Storage Access API is the smallest variant and lost to unverified WebKitGTK behavior plus a user-gesture requirement.

**Client-only.** The newest content-bearing Session is not always the one the user had open: a Session driven from another client or the CLI can be newer. It is still needed, because it is the only path that works without the bridge (development mode, an external harness, an older package).

**Shell-only.** It dies wherever the bridge is absent and cannot help a plain browser, so it cannot replace the fallback either.

**Pass the id through the iframe URL.** The harness answers `?token=` with a 303 to `./`, which drops the query string, so a parameter never reaches the page. `window.name` would survive the redirect but is an untyped side channel the shell would then have to maintain across every reload.

**Persist the selection on the Host.** A durable Host setting becomes visible to every client and needs multi-client conflict rules, and it would still leave drafts, layout, and shortcuts ephemeral inside the shell.

## Consequences

- The thin shell restores the conversation it showed last even though its storage never persists; browsers and the Electron client keep their own authoritative record, because the injection never overwrites an existing selection.
- The shell depends on one client-private detail — the key `dsh.sessions.current` and its `sessionId` member. A rename degrades silently to the client fallback; the bridge and the launcher README both name that coupling.
- Other client state (drafts, layout, sidebar, shortcuts) stays ephemeral inside the embedded WebView. Only the Session is restored.
- The fallback changes startup for every storage-less client: a fresh browser profile opens the Workspace's newest conversation instead of a blank Session.
- Session ids arrive from the page, so they are validated for charset and length before they are stored and before they reach the overlay.
- Verification: `workspaces-service.client.spec.ts` covers the three fallback outcomes, `session_memory_test.go` covers validation and round-tripping, `test-link-bridge.cjs` covers restore, no-overwrite, reporting, and denied storage, `restore_session.test.mjs` covers the injected row and the plugin's no-import constraint. A packaged end-to-end run still needs a rebuilt Linglong bundle.

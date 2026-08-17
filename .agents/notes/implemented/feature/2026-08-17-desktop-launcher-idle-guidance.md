# Agent Note: Idle guidance page in the desktop launcher

Status: implemented

English | [中文](2026-08-17-desktop-launcher-idle-guidance.zh.md)

## Problem

The launcher window opens only after the container harness is ready and navigates to its web GUI. When the harness stops — a manual 停止 click or a crash — the webview stays on that last loaded page: the service is gone, requests fail, and the user sees a dead page with no hint how to get back to work. The same happens while no service is available at all, because the window never opens unless the container harness is already running. The launcher had no state where the webview showed anything useful while no service was available.

## Decision

The launcher owns a self-contained guidance page (`guidance.go`), a static Chinese HTML document rendered from a `data:` URL, shown whenever no service is available. `resolveTarget` reduces the desired webview destination to one of three values — the external URL while `connector.Mode()` is `ModeExternal`, the container URL while `HarnessState` is `StateRunning`, and the guidance page otherwise. The 1s status tick (`dshRefreshStatus`, running on the GTK main thread) resolves the target every tick and calls `navigateFn` only when it differs from `webviewTarget`, the package variable that records the last loaded target. Explicit navigations (probe success, idle handoff after disconnect) record `webviewTarget` too, so the tick never re-navigates to the same page. `openWindow` records the ready URL as the initial `webviewTarget` before `installDesktopUI` runs its first refresh, keeping the initial load single.

The guidance copy mirrors the dialog vocabulary (服务器, 容器内, 本机/远端服务, 启动, 连接) and reads correctly both when the harness is stopped and while it is starting — restart backoff shows the page for the backoff interval, so the wording also covers 服务启动中.

## Alternatives considered

**`about:blank`.** Hides nothing about the dead page and offers no next step; the guidance page exists to say what to do, not to look empty.

**A `file://` page shipped with the package.** The install prefix is only known at runtime (`/opt/apps/<id>/files/...`) and the launcher already avoids depending on packaged asset paths for its CSS. A `data:` URL makes the page independent of filesystem and install location, and the document is static, so no origin, CSP, or update concerns apply.

**Reusing the harness web GUI's own "disconnected" state.** The GUI only exists while its server runs; an idle launcher has no server to render it. Guidance must live outside the entrance cycle the page itself depends on.

## Consequences

The window now shows a purposeful state whenever no service is available, at the cost of one extra page load on every stop/start cycle (a few KB of inline HTML) and guidance content that cannot show live status — the status bar and server dialog continue to carry that. During automatic restart backoff the guidance page shows for the backoff interval, and its wording covers 服务启动中, so the flash reads as intentional rather than broken.
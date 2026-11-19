# Agent Note: Immediate loading window on desktop startup

Status: implemented

English | [中文](2026-11-19-desktop-immediate-loading-window.zh.md)

## Problem

A cold Desktop launch created the main window hidden and showed it only after the Host answered ready. Profile preparation, the login-shell probe, Host module loading, plugin activation, and the welcome-state RPCs therefore ran with nothing on screen but the tray: with a 215-entry profile the Host alone needed 3.1–4.6 s on the reporting machine, and the renderer document load was awaited before the Host even spawned. The shared Web loading page already drew progress for this window, but it stayed offscreen.

## Decision

[`reconcileBackend`](../../../../apps/desktop/src/main.ts) starts the loading document and the Host concurrently instead of awaiting the document before spawning, and reveals the main window as soon as the document finishes loading. The window is still created hidden because sign-in state decides what may appear; when the Host requires credentials, `showWelcome` hides the revealed document before the welcome window opens, so the workspace never becomes visible. A Host failure leaves the document visible and recovery presents its actions in that window instead of raising a new one.

The tray entry and `dsh://open` now focus a revealed loading window instead of returning early. A hidden startup window still waits for startup to choose, which is what keeps a late activation from revealing the workspace before the credential check. Revealing happens only in the startup reconciliation; later Host restarts and update recovery already have a visible window.

## Alternatives considered

**A separate native splash window.** A dedicated small window can show brand progress without loading the Web document, but it adds a second startup surface, its own document, and a handoff that must agree with the tray, single-instance, and close behavior. The Web loading page already renders honest progress and becomes the workspace document.

**Keep the window hidden until the Host is ready.** This avoids showing a loading document that unauthenticated users do not need, but leaves the whole boot with no feedback and makes tray activation inert, which reads as a hang.

**Reveal only after the Host theme preference is known.** The palette would switch at most once instead of at reveal, at the cost of hiding the document for the entire boot that the page exists to cover. The accepted flash is smaller than the wait.

## Consequences

A cold launch shows the loading page within about a second, and unauthenticated launches see it until the Host is ready before the welcome window replaces it. The loading page uses the system palette until the Host theme preference arrives, so an explicit light or dark choice can repaint once. Closing the loading window still hides the window, and the chosen surface appears when startup completes.

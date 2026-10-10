# Agent Note: A tray switcher as its own module for the dual-client bundle

Status: implemented

English | [中文](2026-10-10-dual-client-tray-switcher.zh.md)

## Problem

The dual-client Linglong bundle lets a user choose which of two clients runs and keeps at most one of them running. The first implementation put that control in the launcher binary as `--mode=bubble`: an always-on-top transparent 72×72 Wails window that started clients and killed their process groups. Five fix commits in one day all addressed window-compositor behavior — a transparent frame that rendered black, a shadow that became a black edge, a single click that did nothing, a switch that blocked for the stop grace period, and a dragged position that had to be remembered.

The persisted screen position is an unrecoverable failure mode. A monitor change can leave the ball outside the visible area, and the ball is the only way to reach a client that is still running. The switcher also lived inside the launcher's process, embed tree, and locale dictionary, so no part of it could be owned or tested on its own.

## Decision

The switcher is `apps/linyaps/desktop-dual`: its own Go module and its own binary, `dsh-desktop-switcher`, built with `CGO_ENABLED=0` and depending only on `github.com/godbus/dbus/v5`. It registers a StatusNotifierItem with a dbusmenu menu and starts or stops clients as child processes.

`internal/switchboard` moved unchanged because it imports only the standard library. It resolves the thin-shell client as the sibling binary `dsh-desktop-launcher` under `<PREFIX>/bin` instead of re-executing itself, so the launcher's `--mode=bubble` mode, its `bubble.*` frontend assets, and its eight `bubble.*` dictionary keys are gone.

The item reports its mode through three surfaces: a corner badge on the icon, a localized tooltip, and the first menu entry, which always names the other mode. A tray icon cannot carry text, so the mode marker string that the floating ball displayed is replaced by these. The menu also sets the default mode, greying out the entry that is already the default, and quits the switcher, which stops the client with it. A failed switch raises a desktop notification and sets the item's status to `NeedsAttention`.

One switcher per session is enforced by a well-known bus name. When the session has no `org.kde.StatusNotifierWatcher`, the switcher starts the default client, logs the reason, and retries registration every five seconds, so the entry point returns when the desktop shell does.

The bundle exposes only the switcher's desktop entry: `install-shell-runtime.sh` also installs the thin-shell bundle's entry, which would launch a client outside the switcher and break exclusive switching.

## Alternatives considered

**Keep the floating ball and extract the switcher into a module that still links Wails.** Rejected: the switcher would need the launcher's container adaptation (WebKit helper path, rendering configuration, fontconfig), which forces a shared module plus `go.work` or path replacements across modules, and the window failure modes remain.

**Use DDE's own tray plugin interface.** Rejected: `PluginsItemInterfaceV2` is an in-process C++/QML plugin loaded by dde-tray-loader from host plugin directories. A Linglong package is read-only and unprivileged, so it cannot install or register one.

**Move the switcher sources into `desktop-dual` while keeping one binary.** Rejected: a Go module cannot contain packages outside its directory tree, and `go:embed` cannot reach outside its package directory, so file placement alone would not move ownership.

**Keep polling in the frontend instead of Go.** Rejected: there is no frontend. State changes still poll once per second, which is what the floating ball did; the state is small and a client can exit outside the switcher.

## Consequences

Switching stays destructive: the current client's process group is stopped and the other client starts, so a running session is interrupted. That is unchanged from the floating ball and remains a product decision.

The tray item cannot show a mode name as text; mode identity is a badge colour plus the tooltip and the first menu entry. The badge colours and the two geometric fallbacks are unit-tested, and the icon falls back to a drawn circle when the installed icon files are absent.

`desktop-dual` and `desktop-launcher` share no Go package, so the launcher's Wails and WebKit dependencies and its container adaptation stay in its own module.

Verified on the host session bus from inside the Linglong container: the item registers and appears in the watcher's list, the tooltip carries the localized mode name, `GetLayout` returns the expected entries, a menu `Event` writes the default mode, quitting unregisters the item, and a second launch exits on the single-instance name. Mutual exclusion was verified with fake clients under a simulated install layout: the first client starts, a switch terminates it and starts the other, and quitting terminates the remaining client.

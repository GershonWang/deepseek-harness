# Agent Note: A tray switcher as its own module for the dual-client bundle

Status: implemented

English | [中文](2026-10-10-dual-client-tray-switcher.zh.md)

## Problem

The dual-client Linglong bundle lets a user choose which of two clients runs and keeps at most one of them running. The first implementation put that control in the launcher binary as `--mode=bubble`: an always-on-top transparent 72×72 Wails window that started clients and killed their process groups. Five fix commits in one day all addressed window-compositor behavior — a transparent frame that rendered black, a shadow that became a black edge, a single click that did nothing, a switch that blocked for the stop grace period, and a dragged position that had to be remembered.

The persisted screen position is an unrecoverable failure mode. A monitor change can leave the ball outside the visible area, and the ball is the only way to reach a client that is still running. The switcher also lived inside the launcher's process, embed tree, and locale dictionary, so no part of it could be owned or tested on its own.

## Decision

The switcher is `apps/linyaps/desktop-dual`: its own Go module and its own binary, `dsh-desktop-switcher`, built with `CGO_ENABLED=0` and depending only on `github.com/godbus/dbus/v5`. It registers a StatusNotifierItem with a dbusmenu menu and starts or stops clients as child processes.

`internal/switchboard` moved unchanged because it imports only the standard library. It resolves the thin-shell client as the sibling binary `dsh-desktop-launcher` under `<PREFIX>/bin` instead of re-executing itself, so the launcher's `--mode=bubble` mode, its `bubble.*` frontend assets, and its eight `bubble.*` dictionary keys are gone.

The item reports its mode through three surfaces: a corner badge on the icon, a localized tooltip, and the first menu entry, which always names the other mode. A tray icon cannot carry text, so the mode marker string that the floating ball displayed is replaced by these.

A left click shows the client rather than switching it: switching is destructive, so it belongs behind an explicit menu entry, while a client closed by mistake must be one click away. The two clients close differently and the click handles both — the thin-shell client quits when its window closes, so a click starts it again; the official client only hides its window, so a click launches its binary again and Electron's single-instance lock turns that into a focus request. `ItemIsMenu` is false, which is what makes the host deliver the left click as `Activate` instead of opening the menu. The right-click menu carries the switch entry, a Settings submenu whose two entries are dbusmenu radio items marking the current default mode, About, and Quit, which stops the client with it. A failed action raises a desktop notification naming the step that failed and sets the item's status to `NeedsAttention`.

The default-mode entries are radio items rather than a greyed-out current entry: greying out reads as "this entry is unavailable" when it only reports a state, and the radio mark keeps both entries clickable. A changed default is reported through `ItemsPropertiesUpdated`, which updates the affected entries in place; `LayoutUpdated` is reserved for a changed menu shape. Whole-tree updates are avoided on purpose: rebuilding the popup while the user is interacting with it put DDE's tray popup into a state where `SurfacePopup.qml` reads `x` and `y` of a null object, after which no tray menu of any application could be opened until `dde-shell@DDE.service` was restarted. DDE renders dbusmenu through `libdbusmenu-qt5`, which honours both `toggle-state` and `ItemsPropertiesUpdated`.

About is drawn by `dsh-desktop-about`, a small GTK3 program that `prepare-offline.sh` compiles and installs next to the switcher. The switcher is a `CGO_ENABLED=0` binary with no window toolkit, and the container ships no zenity, kdialog, or Qt; GTK3 is already there because the thin-shell client uses it, so a helper that only draws a window costs less than adding a dialog runtime. All user-facing text stays in the switcher's locale dictionary and reaches the helper as arguments, so the helper holds no copy of it. The helper centers the dialog on the monitor work area itself, on every size allocation: GTK's position hint is only a suggestion that DDE's window manager ignores, and the size read during the allocation callback is still the previous one.

One switcher per session is enforced by a well-known bus name. When the session has no `org.kde.StatusNotifierWatcher`, the switcher starts the default client, logs the reason, and checks again every five seconds, so the entry point returns when the desktop shell does. The check also compares the watcher's unique bus name with the one recorded at registration, so a shell that crashes and is replaced by a new process gets the item re-registered instead of leaving a resident switcher with no visible entry point.

The bundle exposes only the switcher's desktop entry: `install-shell-runtime.sh` also installs the thin-shell bundle's entry, which would launch a client outside the switcher and break exclusive switching.

## Alternatives considered

**Keep the floating ball and extract the switcher into a module that still links Wails.** Rejected: the switcher would need the launcher's container adaptation (WebKit helper path, rendering configuration, fontconfig), which forces a shared module plus `go.work` or path replacements across modules, and the window failure modes remain.

**Use DDE's own tray plugin interface.** Rejected: `PluginsItemInterfaceV2` is an in-process C++/QML plugin loaded by dde-tray-loader from host plugin directories. A Linglong package is read-only and unprivileged, so it cannot install or register one.

**Move the switcher sources into `desktop-dual` while keeping one binary.** Rejected: a Go module cannot contain packages outside its directory tree, and `go:embed` cannot reach outside its package directory, so file placement alone would not move ownership.

**Keep polling in the frontend instead of Go.** Rejected: there is no frontend. State changes still poll once per second, which is what the floating ball did; the state is small and a client can exit outside the switcher.

**Report About through a desktop notification.** Rejected: notifications are the one channel the switcher already had, but a notification is not a dialog — it lands in the notification centre instead of the screen the user is looking at, and carries neither a product icon nor the version fields an About box is opened for.

**Use the container's `xmessage`.** Rejected: it is already installed, but it draws a Motif-era X11 window through the core font path, so Chinese text renders as missing glyphs, which is exactly the case an About box must get right.

## Consequences

Switching stays destructive: the current client's process group is stopped and the other client starts, so a running session is interrupted. That is unchanged from the floating ball and remains a product decision.

The tray item cannot show a mode name as text; mode identity is a badge colour plus the tooltip and the first menu entry. The badge colours and the two geometric fallbacks are unit-tested, and the icon falls back to a drawn circle when the installed icon files are absent.

`desktop-dual` and `desktop-launcher` share no Go package, so the launcher's Wails and WebKit dependencies and its container adaptation stay in its own module.

Verified on the host session bus from inside the Linglong container: the item registers and appears in the watcher's list, the tooltip carries the localized mode name, `GetLayout` returns the expected entries, a menu `Event` writes the default mode, quitting unregisters the item, and a second launch exits on the single-instance name. The left/right click split was verified against the real host before the switcher changed: a probe item with `ItemIsMenu` false received `Activate` on a left click and served its layout on a right click, and a menu containing a submenu rendered as one. Toggle rendering was measured the same way, with one probe carrying a radio pair and one carrying radio and checkmark items at both states: `toggle-state` 1 draws a check mark and 0 draws nothing, so a radio pair shows exactly one mark. The notification split was verified before the code changed too: a probe that flips the pair and reports it only through `ItemsPropertiesUpdated` moves the mark to the clicked entry and clears the other one. The About dialog's centering is measured with `xwininfo`: the outer frame's center lands within one pixel of the screen center, where the pre-fix build was off by (−175, −106). Re-registration is covered by a package test on a private bus that replaces the watcher's connection, and by running the built binary against a stub watcher: with the pre-fix binary the replacement host receives nothing, with the fix it receives the registration on the next check. `Present` is unit-tested for its three branches, the menu tree for the submenu protocol surface, and the About helper for the arguments the switcher passes it. Mutual exclusion was verified with fake clients under a simulated install layout: the first client starts, a switch terminates it and starts the other, and quitting terminates the remaining client.

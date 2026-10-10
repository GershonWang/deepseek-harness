# DeepSeek Harness dual-client Linglong bundle

English | [中文](README.zh.md)

`apps/linyaps/desktop-dual` packages both desktop clients of this repository into one Linglong bundle and adds a resident switcher that keeps exactly one of them running. The Electron client comes from [`apps/linyaps/desktop-linglong`](../desktop-linglong); the Go + Wails thin shell comes from [`apps/linyaps/desktop-launcher`](../desktop-launcher), and the switcher is the only entry point the bundle installs.

## What this module produces

One `.uab` whose command entry is `/opt/apps/com.deepseek.dsh-desktop-dual/files/bin/dsh-desktop-switcher`, a tray process with no window. It starts the client selected as default, exposes switching on the system tray, and terminates the client's whole process group when it exits, so no client is left running without a visible control point.

The bundle carries no third runtime: the thin shell's harness tree and Node come from the launcher stage, the Electron client brings its own Electron runtime and dsh tree.

## Why the switcher is its own module

The switcher was a `--mode=bubble` mode of the launcher binary, drawn as an always-on-top floating ball. It is now a separate Go module and a separate binary for two reasons.

A floating window has failure modes a tray icon does not: it overlaps content, fights fullscreen applications, and its persisted screen position can leave it off-screen after a monitor change, which removes the only way to reach the background client. Tray items are hosted by the desktop panel and cannot be lost that way.

A tray process needs no window toolkit. `desktop-dual` therefore builds with `CGO_ENABLED=0` and depends only on `github.com/godbus/dbus/v5`; the launcher keeps its Wails and WebKit dependencies to itself. The two modules share no Go package.

## How switching behaves

Switching is mutually exclusive and destructive by design: the switcher stops the current client's process group, waits for it to exit, then starts the other one. Any session running in the stopped client is interrupted. That is a product decision, not a defect.

The tray item reports the running mode through a corner badge, a tooltip, and the first menu entry, which always names the other mode. The menu also sets the default mode (shown greyed out when it is already the default) and quits the switcher, which stops the client with it.

A switch failure raises a desktop notification and marks the item as needing attention; the error also reaches the tooltip, so the reason is never silent.

## Building

Run the host-side preparation first, then assemble in the Linglong container.

```sh
sh apps/linyaps/desktop-dual/prepare-offline.sh
ll-builder build -f apps/linyaps/desktop-dual/linglong.yaml
```

`prepare-offline.sh` calls the two client modules' own preparation scripts and then builds the switcher into `stage/bin/`. The launcher stage requires GTK and WebKit development files on the host, exactly as the thin-shell bundle does.

The aggregate entry point runs the same script: `sh apps/linyaps/build-linglong.sh dual`.

## Known limits

The switcher registers as a StatusNotifierItem. When the session has no `org.kde.StatusNotifierWatcher`, it starts the default client, logs the reason, and checks again every five seconds, so the entry point returns when the desktop shell does. The check also compares the watcher's unique bus name with the one recorded at registration, so a shell that crashes and is replaced gets the entry point back within five seconds instead of leaving the clients unreachable. Until a host is present, the clients cannot be switched.

Switching is only available through the tray while a switcher process runs. A second launch exits immediately instead of starting a second client.

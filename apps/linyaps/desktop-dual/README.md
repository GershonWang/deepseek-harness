# DeepSeek Harness dual-client Linglong bundle (the thin shell's upgrade)

English | [中文](README.zh.md)

`apps/linyaps/desktop-dual` packages both desktop clients of this repository into one Linglong bundle and adds a resident switcher that keeps exactly one of them running. The bundle **keeps the thin shell's package ID `com.deepseek.dsh-desktop`** and only raises the version, so the Linglong store treats it as an upgrade of the thin shell rather than a second application on the shelf. The Electron client comes from [`apps/linyaps/desktop-linglong`](../desktop-linglong); the Go + Wails thin shell comes from [`apps/linyaps/desktop-launcher`](../desktop-launcher), and the switcher is the only entry point the bundle installs.

## What this module produces

One `.uab` whose command entry is `/opt/apps/com.deepseek.dsh-desktop/files/bin/dsh-desktop-switcher`, a tray process with no window. It starts the client selected as default, exposes switching on the system tray, and terminates the client's whole process group when it exits, so no client is left running without a visible control point.

The bundle carries no third runtime: the thin shell's harness tree and Node come from the launcher stage, the Electron client brings its own Electron runtime and dsh tree. Together the two runtimes take about 1.8 GB, which is the price of switching inside one package. The one extra binary the switcher needs is `dsh-desktop-about`, a small GTK3 helper that draws the About dialog, because the switcher itself carries no window toolkit.

## Why the bundle keeps the thin shell's package ID

The package ID is the identity on the store: the same ID at a higher version is an upgrade, so the user keeps the same application, its desktop icon, its pinned launcher and taskbar entries, and the toolchains and settings under the home directory. A different ID becomes a second application that has to be installed again, with none of that state carried over.

The bundle therefore also reuses the thin shell's desktop entry filename (`com.deepseek.dsh-desktop.desktop`), `Name` and `Icon` verbatim; only `Exec` changes, pointing at the switcher, which in turn starts the client selected as default. The package directory is a read-only layer inside the container (measured mount options: `ro`), so nothing can be written into it at runtime; both clients ship inside the package, and there is no step that pulls the other one in afterwards.

## Why the switcher is its own module

The switcher was a `--mode=bubble` mode of the launcher binary, drawn as an always-on-top floating ball. It is now a separate Go module and a separate binary for two reasons.

A floating window has failure modes a tray icon does not: it overlaps content, fights fullscreen applications, and its persisted screen position can leave it off-screen after a monitor change, which removes the only way to reach the background client. Tray items are hosted by the desktop panel and cannot be lost that way.

A tray process needs no window toolkit. `desktop-dual` therefore builds with `CGO_ENABLED=0` and depends only on `github.com/godbus/dbus/v5`; the launcher keeps its Wails and WebKit dependencies to itself. The two modules share no Go package.

## How switching behaves

Switching is mutually exclusive and destructive by design: the switcher stops the current client's process group, waits for it to exit, then starts the other one. Any session running in the stopped client is interrupted. That is a product decision, not a defect.

The tray item reports the running mode through a corner badge, a tooltip, and the first menu entry, which always names the other mode. A left click shows the client instead of switching: it starts the default client when none runs, and asks the official client to bring back its hidden window when one is already running, so a client closed by mistake is one click away. The right-click menu carries the switch entry, a Settings submenu whose two entries are radio items marking the current default (both stay clickable), About, and Quit; quitting stops the client with it. About is drawn by the bundled GTK3 helper, centered on the monitor work area.

A failed action raises a desktop notification naming what failed and marks the item as needing attention; the error also reaches the tooltip, so the reason is never silent.

## Building

Run the host-side preparation first, then assemble in the Linglong container.

```sh
sh apps/linyaps/desktop-dual/prepare-offline.sh
ll-builder build -f apps/linyaps/desktop-dual/linglong.yaml
```

`prepare-offline.sh` calls the two client modules' own preparation scripts, then builds the switcher and the About dialog helper into `stage/bin/`. The launcher stage requires GTK and WebKit development files on the host, exactly as the thin-shell bundle does; the helper is compiled against the same GTK development files, so it adds no new host requirement.

The aggregate entry point runs the same script: `sh apps/linyaps/build-linglong.sh dual`.

## Known limits

The switcher registers as a StatusNotifierItem. When the session has no `org.kde.StatusNotifierWatcher`, it starts the default client, logs the reason, and checks again every five seconds, so the entry point returns when the desktop shell does. The check also compares the watcher's unique bus name with the one recorded at registration, so a shell that crashes and is replaced gets the entry point back within five seconds instead of leaving the clients unreachable. Until a host is present, the clients cannot be switched.

Switching is only available through the tray while a switcher process runs. A second launch exits immediately instead of starting a second client.

The dual-client package from before the unified identity (`com.deepseek.dsh-desktop-dual`) can live on the same machine as this bundle, but only one of them should: they install the same switcher, and the single-instance name is deliberately kept identical so the two exclude each other. Without that, both switchers would start a client and fight over the same dsh port and `~/.dsh` state. The visible effect is that only whichever started first owns the tray entry. On a test machine, run `ll-cli uninstall com.deepseek.dsh-desktop-dual` first.

A left click can only show a client that still runs: the thin-shell client quits when its window closes, so a click restarts it, while the official client hides its window and a click brings that window back. A thin-shell window that is merely minimized is not raised.

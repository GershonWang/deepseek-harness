# DeepSeek Harness dual-client Linglong bundle (the thin shell's upgrade)

English | [中文](README.zh.md)

`apps/linyaps/desktop-dual` packages both desktop clients of this repository into one Linglong bundle and adds a resident switcher that keeps exactly one of them running. The bundle **keeps the thin shell's package ID `com.deepseek.dsh-desktop`** and only raises the version, so the Linglong store treats it as an upgrade of the thin shell rather than a second application on the shelf. The Electron client comes from [`apps/linyaps/desktop-linglong`](../desktop-linglong); the Go + Wails thin shell comes from [`apps/linyaps/desktop-launcher`](../desktop-launcher), and the switcher is the only entry point the bundle installs.

## What this module produces

The module builds two variants that share one switcher. `dual` packages the thin shell and the official Electron client; `tray` packages the thin shell alone, so it carries no Electron runtime, its image is correspondingly smaller, and its tray menu narrows to Show, About, and Quit. Both keep the thin shell's package ID `com.deepseek.dsh-desktop`.

Which tray entries the switcher offers follows from what the package actually contains: at startup it probes for `<PREFIX>/bin/dsh-desktop-launcher` and `<PREFIX>/electron/dsh-desktop-official`, so a variant needs no build-time switch to select its menu.

Each variant builds one `.uab` whose command entry is `/opt/apps/com.deepseek.dsh-desktop/files/bin/dsh-desktop-switcher`, a tray process with no window. It starts a client and gives it a tray item, then terminates the client's whole process group when it exits, so no client is left running without a visible control point.

The bundle carries no third runtime: the thin shell's harness tree and Node come from the launcher stage, and in `dual` the Electron client brings its own Electron runtime and dsh tree. Together those two runtimes take about 1.8 GB, which is the price of switching inside one package; `tray` drops the Electron half. The one extra binary the switcher needs is `dsh-desktop-about`, a small GTK3 helper that draws the About dialog, because the switcher itself carries no window toolkit.

## Why the bundle keeps the thin shell's package ID

The package ID is the identity on the store: the same ID at a higher version is an upgrade, so the user keeps the same application, its desktop icon, its pinned launcher and taskbar entries, and the toolchains and settings under the home directory. A different ID becomes a second application that has to be installed again, with none of that state carried over.

The bundle therefore also reuses the thin shell's desktop entry filename (`com.deepseek.dsh-desktop.desktop`), `Name` and `Icon` verbatim; only `Exec` changes, pointing at the switcher, which in turn starts the client selected as default. The package directory is a read-only layer inside the container (measured mount options: `ro`), so nothing can be written into it at runtime; both clients ship inside the package, and there is no step that pulls the other one in afterwards.

## Why the switcher is its own module

The switcher was a `--mode=bubble` mode of the launcher binary, drawn as an always-on-top floating ball. It is now a separate Go module and a separate binary for two reasons.

A floating window has failure modes a tray icon does not: it overlaps content, fights fullscreen applications, and its persisted screen position can leave it off-screen after a monitor change, which removes the only way to reach the background client. Tray items are hosted by the desktop panel and cannot be lost that way.

A tray process needs no window toolkit. `desktop-dual` therefore builds with `CGO_ENABLED=0` and depends only on `github.com/godbus/dbus/v5`; the launcher keeps its Wails and WebKit dependencies to itself. The two modules share no Go package.

## How switching behaves

Switching is mutually exclusive and destructive by design: the switcher stops the current client's process group, waits for it to exit, then starts the other one. Any session running in the stopped client is interrupted. That is a product decision, not a defect.

The tray item reports the running mode through a corner badge, a tooltip, and the first menu entry, which always names the other mode. A left click shows the client instead of switching: it starts the default client when none runs, and brings back a running client's hidden window — the thin shell's as much as the official client's — so a client closed by mistake is one click away. The right-click menu carries the switch entry, a Settings submenu whose two entries are radio items marking the current default (both stay clickable), About, and Quit; quitting stops the client with it.

The menu follows what the package carries, decided by the same startup probe. `dual` holds both clients, so the switch entry and the Settings submenu are available. `tray` holds the thin shell alone: there is no other client to switch to and no default form to choose, so neither entry appears.

About is drawn by the bundled GTK3 helper, whose window title is "About", centered on the monitor work area. Above the separator sit the package version line and two centered facts: current client and package ID; below it are four rows: DSH version, DSH repository, Linglong packager, and Linglong packaging repo. Those facts used to live in the thin shell's title-bar about dialog, which is gone; the DSH version is resolved from the `${PREFIX}/harness/package.json` this bundle reuses (an unreadable file shows `unknown`, never a blank row). Every value, the two repository URLs included, is rendered by the same kind of GtkLabel and is deliberately not selectable: a selectable label selects its whole line, so the dialog would open on a highlighted row, which is exactly what had to go, and drawing the links with the same widget as the other values is what keeps the fact grid strictly aligned with a uniform row height. The package ID, the one value users have to quote verbatim, therefore carries a one-click copy button that briefly reads "Copied". There is no Close button and the dialog has no default action: the title bar's ✕ and Esc are the ways out, and no control takes focus when it opens. A click on either repository row tries the host portal, the bundled `xdg-open`, then GTK's default handler in that order: the Linglong container has no local URI handler at all (the bundled `xdg-open` lacks `dde-open` and every candidate browser, exiting 3 with "no method available", and GIO's default handler returns `Operation not supported`), whereas the host portal sits on the session bus and was measured opening the machine's default browser, so it goes first. As soon as one of the three opens the link the dialog closes, since handing the link to the browser is all it was opened for; when all three fail it stays open and writes the reason in place instead of leaving the click with no visible effect.

A failed action raises a desktop notification naming what failed and marks the item as needing attention; the error also reaches the tooltip, so the reason is never silent.

## Building

Run the host-side preparation first, then assemble in the Linglong container.

```sh
sh apps/linyaps/desktop-dual/prepare-offline.sh
ll-builder build -f apps/linyaps/desktop-dual/linglong.yaml
```

The `tray` variant skips the official Electron client: `--without-official` leaves it out of `stage/`, and `linglong-tray.yaml` assembles the thin shell and the switcher alone.

```sh
sh apps/linyaps/desktop-dual/prepare-offline.sh --without-official
ll-builder build -f apps/linyaps/desktop-dual/linglong-tray.yaml
```

`prepare-offline.sh` calls the packaged clients' own preparation scripts, then builds the switcher and the About dialog helper into `stage/bin/`. The launcher stage requires GTK and WebKit development files on the host, exactly as the thin-shell bundle does; the helper is compiled against the same GTK development files, so it adds no new host requirement.

The aggregate entry point runs the same script: `sh apps/linyaps/build-linglong.sh dual` for the dual-client package, and `sh apps/linyaps/build-linglong.sh tray` for the thin-shell-only variant.

Calling `ll-builder` directly from a VS Code/Electron integrated terminal cannot map user namespaces because of `NoNewPrivs`; the one-click scripts switch to the host passthrough wrapper automatically. See the launcher README's [packaging notes](../desktop-launcher/README.md) for the cause and the full commands.

## Known limits

The switcher registers as a StatusNotifierItem. When the session has no `org.kde.StatusNotifierWatcher`, it starts the default client, logs the reason, and checks again every five seconds, so the entry point returns when the desktop shell does. The check also compares the watcher's unique bus name with the one recorded at registration, so a shell that crashes and is replaced gets the entry point back within five seconds instead of leaving the clients unreachable. Until a host is present, the clients cannot be switched.

Switching is only available through the tray while a switcher process runs. A second launch exits immediately instead of starting a second client.

`dual` and `tray` share the package ID but not a version line: `tray` (0.1.6.2) continues the thin shell's line (0.1.5.6), while `dual` (0.2.0.6) stays the higher version under the same ID. `tray` is therefore only an upgrade of the thin shell: on a machine that already has `dual` it is a lower version, which the store does not accept as an upgrade, so it cannot be installed over `dual`. Moving a machine from `dual` to `tray` means uninstalling `dual` first.

`dual` and `tray` share one host staging directory, `apps/linyaps/desktop-dual/stage/`, and the two variants require different contents in it. After a `tray` build with `--without-official`, that directory has no `stage/electron/`; running `dual` with `--no-prepare` on that staging therefore stops with an explicit report that the official client's files are missing, instead of producing a package that lacks the official client. Re-run `prepare-offline.sh` in full, without `--no-prepare`, to restore it.

The dual-client package from before the unified identity (`com.deepseek.dsh-desktop-dual`) can live on the same machine as this bundle, but only one of them should: they install the same switcher, and the single-instance name is deliberately kept identical so the two exclude each other. Without that, both switchers would start a client and fight over the same dsh port and `~/.dsh` state. The visible effect is that only whichever started first owns the tray entry. On a test machine, run `ll-cli uninstall com.deepseek.dsh-desktop-dual` first.

In the packages that install the switcher (`tray` and `dual`), closing the thin-shell window hides it and leaves the harness running; a left click or the Show entry unminimizes and shows it again. Only those packages behave that way: the launcher hides on close only when it starts with `DSH_DESKTOP_HIDE_ON_CLOSE=1`, and only the switcher injects that variable, so the standalone `web` package still ends when its window is closed.

# Agent Note: Desktop entry needs StartupWMClass for dock icon association

Status: implemented

English | [中文](2026-09-12-desktop-launcher-dock-icon-association.zh.md)

## Problem

Started as `ll-cli run com.deepseek.dsh-desktop` from a terminal, the launcher window carried a generic placeholder icon in the Deepin dock. Started from the application launcher, the same window carried the bundled DeepSeek icon.

Two measurements on the running window explain the difference. Its `WM_CLASS` is `"dsh-desktop-launcher", "Dsh-desktop-launcher"`, and `_NET_WM_ICON` is absent — the window publishes no icon of its own. The installed entry is `com.deepseek.dsh-desktop.desktop` and declared no `StartupWMClass`.

A dock has two routes from a window to a desktop entry. The application launcher adds a hint naming the entry it started, and the dock follows that hint directly. A terminal `ll-cli run` adds no hint, leaving the dock to match `WM_CLASS` against `StartupWMClass` — unset — or against the entry's own filename, `com.deepseek.dsh-desktop`, which matches neither half of the window class. Association fails, and the fallback is unavailable because the window sets no icon.

That is why the defect only appeared on the command line: the launcher path never needed the field.

## Decision

The desktop entry declares `StartupWMClass=dsh-desktop-launcher`, the window class instance field measured with `xprop`. One source file serves both packaging paths — `linglong/linglong.yaml` installs it into the bundle and `build-deb.sh` into the `.deb` — so both distributions gain the association from one line.

The value is the instance rather than the class field (`Dsh-desktop-launcher`): dock implementations in practice match the lowercased instance, which is also what comparable third-party entries do for windows whose class is capitalised (Sublime Text runs `"sublime_text", "Sublime_text"`).

## Alternatives considered

**Have the launcher set the window icon, so the fallback works.** It would fix every entry that fails to associate, not just this one, and the launcher already knows where its icon lives (`packaging.AboutIconPath`). Rejected: Wails v2's Linux frontend exposes no window-icon API — `pkg/runtime/window.go` has title, size, position, background colour and theme, but nothing for the icon, and the `gtk_window_set_icon` call inside `window.c` is not reachable from Go. Making it reachable means patching Wails or adding cgo of our own, which is a large dependency for a cosmetic fallback.

**Rename the binary to match the entry, or the entry to match the binary.** Rejected: `dsh-desktop-launcher` is the build output named by the Makefile, the Linglong entry script and the `.deb` packaging, while `com.deepseek.dsh-desktop` is the Linglong application id that the sandbox, the desktop entry and the published bundle all key on. Either rename reaches far past the dock.

**Set `WM_CLASS` from application code instead.** Rejected: GTK derives the class from the program name, and Wails owns window creation, so overriding it means cgo calls against a window this program does not create — more moving parts than one declarative field.

**Leave it and document the workaround.** Rejected: `ll-cli run` is how the launcher is started during development and debugging, which is exactly when a developer needs to tell the window apart in the dock.

## Consequences

A launcher window now associates with its desktop entry however it was started, so the dock shows `Icon=dsh-desktop` instead of a placeholder. The application-launcher path is unchanged — it still associates through the hint, and now has a second route that agrees with it.

The constraint this introduces is a coupling: `StartupWMClass` must equal the window's `WM_CLASS` instance, which GTK derives from the executable name. Renaming the built binary without updating the field silently brings the placeholder icon back for command-line launches. The field carries a comment saying so, because nothing else would catch it.

## Testing

The diagnosis is reproducible with the window open:

```sh
xprop -name "DeepSeek Harness" WM_CLASS        # "dsh-desktop-launcher", "Dsh-desktop-launcher"
xprop -name "DeepSeek Harness" _NET_WM_ICON    # not found
```

The field is readable by a standard parser (`configparser` reads `StartupWMClass` back as `dsh-desktop-launcher`), so the entry is not malformed. `desktop-file-validate` is not installed in this environment, so the entry was not checked against the freedesktop schema validator.

Verifying the fix in a running system does not require a rebuild: copying the entry into `~/.local/share/applications/` and restarting the client from the terminal exercises the same lookup the dock performs, because the dock reads installed entries rather than the packaged source. Confirmation on a freshly installed bundle is still outstanding — the change was not exercised through a rebuilt `.uab` here.

## Related

[Desktop launcher on Linux/Linglong](../feature/2026-08-14-desktop-launcher-linux-linglong.md) owns the packaging and desktop integration this entry belongs to.

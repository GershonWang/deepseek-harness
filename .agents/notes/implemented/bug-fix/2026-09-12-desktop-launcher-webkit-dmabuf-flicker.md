# Agent Note: WebKitGTK DMABUF compositing flicker on NVIDIA machines

Status: implemented

English | [中文](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.zh.md)

## Problem

On a Deepin/X11 machine whose display is driven by an Intel iGPU (HD 530) while the NVIDIA proprietary driver 580.119.02 is loaded for a GTX 960M, switching away from the packaged launcher and back intermittently repainted the whole harness area a solid color for a moment — white under a light theme, black under a dark one — and then recovered on its own.

The harness was never at fault. `dsh-desktop-launcher` kept running, the session stayed connected, and a task streaming at the time kept streaming; only the pixels were briefly gone. That rules out a page reload, a terminated web process, and a frontend reconnect, and points at the compositor.

`WebKitWebProcess` held `/dev/dri/renderD128` open with `libEGL.so.1.1.0` and `libgbm.so.1.0.0` loaded, so the webview was on webkit2gtk's DMABUF accelerated-compositing path rather than a software fallback. No `WebKitGPUProcess` existed; compositing ran inside the web process. The launcher process held `/dev/dri/card0` (the Intel card) open while the Linglong container also exposed the NVIDIA driver through the extension `org.deepin.driver.display.nvidia.580-119-02`.

WebKitGTK logged nothing: stderr carried only the startup block, and the framebuffer diagnostics are `RELEASE_LOG` calls that stay silent unless `WEBKIT_DEBUG` enables that channel. Their absence therefore says nothing either way.

## Decision

`packaging.ConfigureWebKitRendering()` sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` when the kernel has the NVIDIA proprietary driver loaded, and does nothing otherwise. `main` calls it beside `ConfigureWebKitHelperPath()`, before `wails.Run`, because webkit2gtk reads that switch while GTK/WebKit initialize and a later write has no effect.

The probe is `/sys/module/nvidia`, kept in the overridable `nvidiaModulePath` variable so tests can drive both branches. It targets the kernel's module directory rather than device nodes because it answers the question the defect correlates with — is the proprietary driver loaded — and because the Linglong sandbox exposes it, as it does `/proc/driver/nvidia/version`.

Restricting the override to that condition is the point: every other Linux machine keeps DMABUF accelerated compositing, and only machines that match the documented trigger pay for stability with the slower presentation path.

## Alternatives considered

**Disable DMABUF unconditionally.** One line, no probe, and it would also cover affected machines whose GPU we have not seen. Rejected: webkit2gtk's own guidance and [Tauri's Linux graphics page](https://v2.tauri.app/develop/debug/linux-graphics/) both warn that shipping this unconditionally disables a faster path for users on working setups, and the known trigger is the NVIDIA driver rather than Linux at large.

**Set `WEBKIT_DISABLE_COMPOSITING_MODE=1` instead.** It disables accelerated compositing wholesale, which certainly covers this case. Rejected: it is the heavier of the two switches and was not needed — turning off the DMABUF renderer alone made the flicker stop, so the narrower change wins.

**Give the harness document an explicit `html` background.** Cheap, and the harness root does rely on `body`’s background propagating to the canvas. Rejected as the fix: it addresses what a discarded canvas paints, but the confirmed mechanism is the compositor’s buffer path, so this would leave the failure in place and merely change its color. It remains an independent improvement if a canvas-color fallback is ever observed.

**Ship the variable through the Linglong package instead of the launcher.** `ll-cli run --env` can inject it, and it kept the workaround out of the binary. Rejected: it only applies to launches that pass that flag, while the packaged application is normally started from the desktop entry, so the defect would survive the normal path.

**Gate on X11 rather than on the driver.** The host session is X11. Rejected: no Wayland evidence exists in either direction, and being broader than the evidence would override the compositor on machines that may not need it.

## Consequences

On the affected machine the flicker is gone across repeated switch-away/switch-back cycles, verified in the packaged client by launching with `ll-cli run --env WEBKIT_DISABLE_DMABUF_RENDERER=1` and confirming the variable reached the launcher process through `/proc/<pid>/environ`. The harness process, session, and streaming task were never implicated at any point.

The cost is the zero-copy DMABUF presentation path on NVIDIA machines: webkit2gtk composites through shared memory there instead. Text-heavy UI is not expected to notice, but the trade is real and is why the override is not unconditional.

Three gaps are recorded rather than closed. nouveau, AMD, and iGPU-only machines are outside the current condition, so a matching symptom there would still be unprotected until reported. The NVIDIA condition is inferred from one reproduction on one driver branch, not from a driver-independent root cause in webkit2gtk. And the probe reads a kernel fact, so a future Linglong sandbox that hides `/sys/module` would silently stop applying the override — the failure mode is the bug returning, never a machine wrongly overridden.

Removing the override is a one-function deletion once webkit2gtk fixes the upstream buffer negotiation, or once the affected driver branch is out of support.

## Testing

`go test ./internal/packaging/` covers both branches through the injectable probe path: with the probe absent, `ConfigureWebKitRendering` leaves `WEBKIT_DISABLE_DMABUF_RENDERER` unset; once the path exists, it sets `1`. The test saves and restores both the probe variable and the environment entry, so it neither depends on the host having an NVIDIA driver nor leaks the setting into sibling tests.

The container-side preconditions were checked before the probe was written, not assumed: inside the Linglong sandbox `/sys/module/nvidia` and `/proc/driver/nvidia/version` are both readable, the latter reporting the same 580.119.02 module version as the host.

Still unverified: the packaged artifact was validated through `ll-cli run --env` rather than through a rebuilt `.uab`, so a `make build` plus one packaging run remains the check before release.

## Related

[Desktop launcher on Linux/Linglong](../feature/2026-08-14-desktop-launcher-linux-linglong.md) owns the packaging architecture this platform adaptation sits in. [Clipboard image bridging](2026-08-30-clipboard-paste-large-image-and-file-copy.md) is the other place where the launcher compensates for a webkit2gtk behavior the harness UI cannot observe itself.

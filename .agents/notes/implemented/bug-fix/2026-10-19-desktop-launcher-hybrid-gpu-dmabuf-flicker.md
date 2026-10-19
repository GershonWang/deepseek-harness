# Agent Note: WebKitGTK DMABUF flicker on hybrid GPU machines

Status: implemented

English | [中文](2026-10-19-desktop-launcher-hybrid-gpu-dmabuf-flicker.zh.md)

## Problem

The packaged launcher runs the harness GUI in a webkit2gtk webview. On this machine — Intel HD 530 driving the only connected output (eDP-1), with the NVIDIA proprietary driver 580.119.02 loaded for a GTX 960M — the webview's accelerated compositing fails as the window is re-exposed after being occluded: large regions of the session area alternate between disappearing and reappearing at high frequency, the page stops refreshing, and the window stops accepting input. The only recovery is quitting the launcher from the tray and starting it again, and it happened three times in one hour.

The failure is in presentation only. `launcher.log`, which records stage transitions, iframe reloads, mount handshakes and bridge reports, stayed empty for the whole 48-minute window in which the window was flickering, and `reload`, `reload-exhausted`, `bridge-stall` and `iframe-error` were zero for the entire session: the page never re-initialized, so neither the shell nor the harness was implicated. The `WebKitWebProcess` held five `renderD128` descriptors and burned 53% of one core while the page was static, so the compositor was alive and spinning rather than gone.

[Render-backend selection on hybrid GPU machines](2026-10-18-desktop-launcher-webkit-render-backend-selection.md) had narrowed the shared-memory fallback to machines whose only render node is NVIDIA, on the premise that a hybrid machine keeps WebKit on the display GPU and therefore never meets the [flicker recorded earlier](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.md). This machine is that premise's counterexample: its DMA-BUF path selects the Intel node, and it reproduces the defect.

## Decision

`packaging.ConfigureWebKitRendering` applies the fallback when any `/sys/class/drm/renderD*` node reports NVIDIA's PCI vendor id `0x10de` (`hasNvidiaRenderNode`), not only when every node does. Hybrid machines are therefore covered again, and the fallback stays the lighter `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1` rather than `WEBKIT_DISABLE_DMABUF_RENDERER`: webkit2gtk reads it after the shared-memory transport is added, so accelerated compositing survives and only the zero-copy present path is dropped.

The three escape hatches keep their meanings: `DSH_DESKTOP_DMABUF_RENDERER=1` skips the fallback and keeps the hardware DMA-BUF path, `DSH_DESKTOP_SHM_RENDERER=1` forces shared-memory transport on a machine whose render nodes match nothing, and `DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1` turns accelerated compositing off entirely.

## Evidence

The same reproduction — a session executing a task, a maximized terminal covering the launcher for several minutes, then switching back — was run against both transport paths on the same machine and the same build, one variable:

| Arm | WebProcess render node | dmabuf fds | nvidia fds | Reproduction |
|---|---|---|---|---|
| default (`renderD128` DMA-BUF) | `renderD128` (Intel) | 5 | 0 | flicker and input loss in three of three attempts |
| `DSH_DESKTOP_SHM_RENDERER=1` → `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1` | `renderD129` (NVIDIA) | 0 | 38 | no flicker over 22 minutes, occlusion and re-exposure included |

`launcher.log` stayed empty and the anomaly counters stayed zero on both arms, which is what separates a presentation-path failure from a shell-side one. The predicate was also resolved against the live host, where `renderD128` reports `0x8086` and `renderD129` reports `0x10de`, so the fallback now applies here.

## Alternatives considered

**Keep the NVIDIA-only trigger and document the opt-in.** Rejected on the second reproduction: the machine class the earlier note left unprotected is the class the defect was first seen on, and the opt-in is unreachable from the desktop entry or the switcher, so a user on such a machine can only wait for a release. That note's Consequences section recorded exactly this as an accepted gap.

**Keep the NVIDIA-only trigger and give the switch a settings surface.** Holds as a separate improvement for machines whose render nodes match nothing, but it would not have changed this outcome: the trigger matched all along and was merely too narrow to act on.

**Treat the missing zero-copy path as too expensive to take by default.** Rejected on measurement: the earlier note's own table has both arms at the same Web/UI CPU median on this machine (2.9 / 5.8), and `FORCE_SHM` keeps accelerated compositing rather than losing it.

**Detect the stall and reload the webview instead.** Deferred, not rejected: it is the right safety net for machines outside the trigger, but it cannot be the fix for a defect a transport switch removes, and an automatic reload discards in-page state on a false positive.

## Consequences

Hybrid machines give up the zero-copy present path on their display GPU and composite through shared memory instead. The measurement above puts the CPU cost at parity on this machine class, and `DSH_DESKTOP_DMABUF_RENDERER=1` bounds the exposure of a machine that matches the trigger without being affected.

The trigger is still inferred from an NVIDIA render node being present rather than from a driver-independent root cause in webkit2gtk, so machines without one remain unprotected: nouveau, AMD and iGPU-only hosts sit outside it, and a matching symptom there still needs a report plus `DSH_DESKTOP_SHM_RENDERER=1`. Nothing here changes the defect's intermittent nature — the reproduction needs an occlusion lasting minutes, so an arm that stays clean for a few minutes is weak evidence on its own.

## Testing

`CGO_ENABLED=0 go test ./internal/...` covers the new trigger table (NVIDIA only true; Intel plus NVIDIA true; non-NVIDIA only false; no render nodes false), an unreadable `/sys/class/drm` returning false, an unreadable vendor file neither making the predicate true nor hiding a readable NVIDIA node, the three escape hatches, and the guard that the launcher never sets `WEBKIT_DISABLE_DMABUF_RENDERER`.

Not verified: a packaged-build run from the desktop entry with no environment variable. The A/B above ran from a shell, so the default path in a rebuilt `.uab` is the check that closes this.

## Related

[Render-backend selection on hybrid GPU machines](2026-10-18-desktop-launcher-webkit-render-backend-selection.md) owns the switch choice (`WEBKIT_DMABUF_RENDERER_FORCE_SHM` over `WEBKIT_DISABLE_DMABUF_RENDERER`), the render-node probe and the escape hatches; its trigger narrowing is superseded here. [WebKitGTK DMABUF compositing flicker on NVIDIA machines](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.md) owns the original reproduction and the correlation with the NVIDIA driver. [WebView hardware acceleration](2026-10-18-desktop-launcher-webview-hardware-acceleration.md) owns whether compositing happens at all, which is what made the defect reachable.

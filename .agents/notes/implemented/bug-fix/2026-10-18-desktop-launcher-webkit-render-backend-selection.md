# Agent Note: Desktop launcher render-backend selection on hybrid GPU machines

Status: implemented

English | [中文](2026-10-18-desktop-launcher-webkit-render-backend-selection.zh.md)

## Problem

Customers reported that the packaged launcher's page had no GPU-accelerated rendering, that plugin settings screens with heavier UI degraded into a slideshow, and that CPU use was very high — while opening the same harness URL in a system browser stayed smooth.

The shipped code caused the first half of that directly. `packaging.ConfigureWebKitRendering` set `WEBKIT_DISABLE_DMABUF_RENDERER=1` whenever `/sys/module/nvidia` existed (the [earlier flicker fix](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.md)). Two things about that fallback do not hold:

1. The trigger is wider than the defect. A hybrid machine loads the NVIDIA module while WebKit still renders on the display GPU. On the reporting machine — Intel HD 530 driving the only connected output (eDP-1) plus a GTX 960M with no connected output — the live `WebKitWebProcess` held the Intel render node `renderD128` while DMABUF compositing was on, so NVIDIA had nothing to do with it, yet the machine was downgraded anyway.
2. The knob costs far more than the "one zero-copy fast path" the earlier note assumed. In webkit2gtk 2.44+ `AcceleratedBackingStoreDMABuf::rendererBufferMode()` returns on `WEBKIT_DISABLE_DMABUF_RENDERER` **before** `mode.add(SharedMemory)`, leaving the transport mode empty so `AcceleratedBackingStore` has no usable backing store at all (upstream bug 324730; several downstream applications report crashes from the same site on 2.5x). `WEBKIT_DMABUF_RENDERER_FORCE_SHM` returns **after** that add, which is the "skip only the hardware dmabuf" semantic the fallback actually wanted.

## Decision

`ConfigureWebKitRendering` selects the backend from the machine's render nodes instead of the loaded kernel module, and downgrades with the lighter knob:

- The fallback applies when `nvidiaOnlyRenderStack()` is true: at least one `/sys/class/drm/renderD*` node reports PCI vendor `0x10de` and no node reports anything else. An unreadable `/sys/class/drm` returns false, so a machine the probe cannot inspect is left alone rather than downgraded.
- The fallback sets `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1`. The launcher no longer writes `WEBKIT_DISABLE_DMABUF_RENDERER` anywhere, and a test asserts that absence.
- `DSH_DESKTOP_DMABUF_RENDERER=1` keeps its meaning: skip the fallback and keep the hardware DMABUF path.
- `DSH_DESKTOP_SHM_RENDERER=1` is the new reverse escape hatch: force the shared-memory transfer on any machine, for a hybrid host that does reproduce the flicker without giving up accelerated compositing entirely.
- `DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1` is unchanged.

On a hybrid machine the launcher now selects nothing, so WebKit keeps its default DMA-BUF path on the display GPU.

## Evidence

Measured on the reporting machine with the packaged webkit2gtk 2.50.4 and the same harness URL, one variable per arm, reading `/proc/<pid>/fd` and `/proc/<pid>/maps` of the helper processes:

| Arm | Environment | WebProcess render node | dmabuf fds | WebProcess memfd fds | Web / UI CPU median |
|---|---|---|---|---|---|
| shipped before the change | `WEBKIT_DISABLE_DMABUF_RENDERER=1` | `renderD129` (NVIDIA) | 0 | 0 | 6.0 / 10.4 |
| variable removed | — | `renderD128` (Intel) | 2 | 0 | 2.9 / 5.8 |
| `WEBKIT_FORCE_DMABUF_RENDERER=1` | forced DMA-BUF | `renderD128` (Intel) | 2 | 0 | 5.3 / 9.0 |
| `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1` | the new fallback | `renderD129` (NVIDIA) | 0 | 2 | 2.9 / 5.8 |

`renderD128` is `8086:191B` (i915) and `renderD129` is `10DE:139B` (NVIDIA); only `card0-eDP-1` is connected. Pinning EGL to the Mesa vendor directory did not move the context back to Intel — Mesa failed to create a DRI screen on the NVIDIA node — so vendor-directory pinning is not a substitute for fixing the trigger.

The last row is what separates the two knobs: the shared-memory arm has no dmabuf descriptor but does hold the two `memfd:WebKitSharedMemory` descriptors the disabled-renderer arm leaves absent, which is the backing store `WEBKIT_DISABLE_DMABUF_RENDERER` takes away. A stress page that chains `document.startViewTransition()` while animating WebGL and CSS killed the UI process with SIGSEGV (`wait` status 139) within about four seconds under the old knob and survived beyond sixteen seconds under the new one — same page, same build, one variable.

Both the shared-memory and the disabled-renderer arms keep their GL context on the NVIDIA node here, unlike the DMA-BUF arms, because the non-DMA-BUF path resolves through GLVND's default vendor (NVIDIA's directory first in `__EGL_VENDOR_LIBRARY_DIRS`) rather than the display device the DMA-BUF path selects. That placement is a separate factor from the renderer selection and is not changed here.

## Alternatives considered

**Keep `WEBKIT_DISABLE_DMABUF_RENDERER` and only narrow the trigger.** Rejected: narrowing fixes the hybrid misjudgement but keeps a knob that empties the transport mode on every machine it does hit, which is a worse outcome than the flicker it prevents.

**Drop the fallback and always leave WebKit at its default.** Rejected: NVIDIA-only machines and builds without the distribution's `disable-nvidia-dmabuf.patch` would lose the protection the earlier fix added, and the flicker reproduces there.

**Re-enable the hardware DMA-BUF path unconditionally.** Rejected: the flicker returns, and the earlier note's reproduction on this hardware class still stands.

**Force the GL vendor to Mesa instead of touching the renderer.** Rejected on measurement: restricting `__EGL_VENDOR_LIBRARY_DIRS` to the Mesa directory left the web process on `renderD129` with `libEGL warning: egl: failed to create dri screen`.

**Reach the fallback from a settings dialog instead of environment variables.** Deferred, not rejected: WebKit reads these variables only during initialization, so a setting would have to promise a restart; the environment escape hatches stay the mechanism until that surface exists.

## Consequences

The hybrid machine that filed the report keeps WebKit's zero-copy DMA-BUF path on its display GPU, which is the state the earlier note's own measurement showed to be the intended one. Machines whose only render path is NVIDIA keep a fallback that no longer risks losing accelerated compositing.

The flicker workaround now does **not** apply to hybrid machines. The earlier reproduction happened on exactly this machine class, so a hybrid host that still flickers must opt in with `DSH_DESKTOP_SHM_RENDERER=1`; that gap is real and is recorded here rather than silently accepted. The distribution-compiled `disable-nvidia-dmabuf.patch` in the bundled webkit2gtk already empties the transport mode on genuinely NVIDIA-only machines, so the launcher's own branch is a no-op there and exists for builds without the patch.

Two escape hatches (`DSH_DESKTOP_DMABUF_RENDERER`, `DSH_DESKTOP_SHM_RENDERER`) still cannot be set by a user who does not launch the client from a shell, because neither the packaged desktop entry nor the switcher writes environment variables. Closing that gap needs the settings surface that was deferred above.

## Testing

`CGO_ENABLED=0 go test ./internal/...` covers the new decision: the trigger table (NVIDIA-only true; Intel plus NVIDIA false; non-NVIDIA only false; no render nodes false), an unreadable `/sys/class/drm` returning false, the opt-out escape hatch, the shared-memory opt-in, the compositing opt-out, and the guard that the launcher never sets `WEBKIT_DISABLE_DMABUF_RENDERER`. The probe was also resolved against the live host, where it returns false for the Intel plus NVIDIA render-node pair.

Not verified: a real-window comparison after the change, since the running client was deliberately left untouched, and which process faults first on the stress page, which no core dump or kernel log was available to establish. The stress-page and file-descriptor measurements come from WebKitGTK's own MiniBrowser rather than the Wails embedding, so their absolute figures do not transfer; the one-variable crash flip does not depend on the embedding.

## Related

[WebKitGTK DMABUF compositing flicker on NVIDIA machines](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.md) owns the flicker this trigger protects against and the escape hatches this note extends. [Desktop launcher on Linux/Linglong](../feature/2026-08-14-desktop-launcher-linux-linglong.md) owns the packaging architecture this platform adaptation lives in.

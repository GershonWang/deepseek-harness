# Agent Note: Desktop launcher WebView hardware acceleration policy

Status: implemented

English | [中文](2026-10-18-desktop-launcher-webview-hardware-acceleration.zh.md)

## Problem

The packaged launcher's page had no GPU-accelerated rendering: plugin settings screens with heavier UI degraded into a slideshow and CPU use was very high, while opening the same harness URL in a system browser stayed smooth. The defect was not in the harness, the Linglong sandbox, or the machine: WebKitGTK's own test browser, loading the same URL from the same container, held hardware compositing buffers and used the display GPU.

The launcher never passed Wails a Linux options value. Wails v2.15.0 falls back to `WebviewGpuPolicyNever` whenever `options.Linux` is nil — a workaround for wails#2977 — which its Linux backend turns into `WEBKIT_HARDWARE_ACCELERATION_POLICY_NEVER`. With that policy WebKitGTK does not enter accelerated compositing at all: the page is painted on the CPU, the UI process never opens a DRM render node, and no compositing buffers of either kind appear. Every live measurement matched that state, and it also explains why the renderer-selection work in [the DMABUF note](2026-10-18-desktop-launcher-webkit-render-backend-selection.md) could not recover the fast path: the policy sits upstream of the renderer choice, so setting `WEBKIT_FORCE_DMABUF_RENDERER=1` on the running client changed nothing.

## Decision

`main` passes `app.WailsLinuxOptions()` as `options.App.Linux`, and `internal/app/window.go` returns a non-nil `linux.Options` whose `WebviewGpuPolicy` selects WebKitGTK's on-demand acceleration.

The numeric value is one, not the constant Wails' documentation suggests. Wails' Go enumeration orders `OnDemand=0`, `Always=1`, `Never=2`, while its Linux C switch maps `0 → ALWAYS`, `1 → ON_DEMAND`, `2 → NEVER`: passing the constant named `OnDemand` therefore requests always-on acceleration, and only the numeric slot `1` requests on-demand. `window.go` spells the slot as a documented constant so the intent does not depend on a misleading name, and `window_test.go` pins the number.

Turning acceleration off deliberately stays with `DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1` (`packaging.ConfigureWebKitRendering`), which is per-machine and needs no release.

## Alternatives considered

**Pass `linux.WebviewGpuPolicyAlways` by name.** Rejected as the written form: its numeric value is the one that reaches on-demand, so it would work today, but the name says the opposite of the effect and invites a later "fix" that silently switches the machine to unconditional acceleration. A documented numeric constant with a test says what it means.

**Pass `linux.WebviewGpuPolicyOnDemand` by name.** Rejected: that name maps to unconditional acceleration in this Wails release, which is not the semantics chosen here.

**Upgrade Wails to a version whose enumeration matches the C switch.** Deferred, not rejected: it is the durable fix for the mismatch, but it is a separate dependency change with its own verification, and pinning the value plus a failing test already makes the mismatch loud.

**Leave the options nil and rely on the existing `DSH_DESKTOP_*` switches.** Rejected on measurement: those switches act on the renderer transport, and the never policy removes accelerated compositing before that layer is reached, so a client started with `WEBKIT_FORCE_DMABUF_RENDERER=1` still produced no compositing buffers and no CPU improvement.

**Ask for always-on acceleration.** Rejected for now: it costs power on pages that do not need compositing, and on-demand is WebKitGTK's own default. If a real-window check shows on-demand still leaving the heavy screens uncomposited, switching that one slot is a one-line change.

## Consequences

The embedded page regains WebKitGTK's on-demand accelerated compositing, which on a hybrid machine means compositing on the display GPU rather than the discrete GPU, matching the render-node probe recorded in the DMABUF note.

The policy value depends on a Wails enumeration quirk. A Wails upgrade that fixes the mapping fails `TestWailsLinuxOptionsEnablesWebviewAcceleration` instead of silently changing the policy, and the test message names the two places to re-check.

Enabling acceleration also re-exposes the rendering hazards the acceleration work was hiding: a machine that previously painted on the CPU can now show the occlusion flicker or compositing artifacts that motivated [the earlier flicker fix](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.md). The render-node fallback recorded there still applies — to every machine with an NVIDIA render node, not only NVIDIA-only ones — and `DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1` remains the per-machine escape hatch; the real-window behavior on an NVIDIA-only machine is still unobserved.

## Testing

`TestWailsLinuxOptionsEnablesWebviewAcceleration` pins both facts the decision depends on: the options value is non-nil, and its policy slot is the numeric one that reaches `WEBKIT_HARDWARE_ACCELERATION_POLICY_ON_DEMAND`. `CGO_ENABLED=0 go test ./internal/...` covers it alongside the render-backend tests.

The packaged client was then verified on the reporting machine, launched with no environment overrides. Its UI process opened `renderD128` (the Intel display GPU) and held two dmabuf descriptors where the pre-fix client held none, the web process CPU fell from the sustained 57–75% to about 36% over six one-second samples, and no NVIDIA device node was opened at all. That is the zero-copy hardware path on the display GPU, which the renderer-transport rules alone never reached.

Now observed on this host: the hybrid machine measured above does take the fallback, and [WebKitGTK DMABUF flicker on hybrid GPU machines](2026-10-19-desktop-launcher-hybrid-gpu-dmabuf-flicker.md) records the occlusion flicker and input loss that follow without it. An NVIDIA-only machine's real-window behavior is still unobserved.

## Related

[Render-backend selection on hybrid GPU machines](2026-10-18-desktop-launcher-webkit-render-backend-selection.md) owns which renderer transport the launcher requests and why the discrete GPU no longer receives the compositing context; this note owns whether compositing happens at all. [WebKitGTK DMABUF compositing flicker on NVIDIA machines](2026-09-12-desktop-launcher-webkit-dmabuf-flicker.md) owns the flicker that acceleration reintroduces on the affected hardware class.

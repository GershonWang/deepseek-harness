# Wails v3 Feasibility Spike

English | [中文](README.zh.md)

`apps/linyaps/desktop-v3-spike` validates whether the desktop client can move from Wails v2 to v3 before any production code is touched. It is not a product package: it is not built into a Linglong bundle and is not referenced by `desktop-launcher` or `desktop-dual`. Once the checks pass, its assembly is meant to become the skeleton of the upgraded client.

## Why a Separate Module

Wails v2 and v3 differ in module path, Linux graphics stack (GTK3/WebKit2GTK 4.1 to GTK4/WebKitGTK 6.0), and frontend runtime model (injected `window.go` / `window.runtime` globals versus ES modules plus `@wailsio/runtime`). Both stacks cannot share one `go.mod`, and mixing them would blur "does v3 work here" with "how large is the migration".

## Verification Matrix

| Question | Evidence collected | Pass criterion | What failure means |
| --- | --- | --- | --- |
| Can GTK4 + WebKitGTK 6.0 build and link? | `make build` / `verify.sh` | Build succeeds and `go.stackVersion` reports GTK 4.x with WebKitGTK 6.x | The first prerequisite of the migration fails; only the `-tags gtk3` stopgap or a custom runtime remains |
| Is the shell page origin still `wails://wails`? | `frontend.origin` | Equals `wails://wails` | Locale, fonts, and terminal font size stored per origin are silently lost on upgrade |
| Is localStorage usable on that origin? | `frontend.localStorageOK` | `true` | Settings persistence needs a different home |
| Do bindings work from a bundler-free static shell? | `binding.ok` (`Call.ByName`) | `true` with reply `pong:spike` | The shell must gain a build step or be rewritten |
| Do the old globals still exist? | `frontend.classicGlobals` | Expected `false`; decides the shim design | If `true`, the existing shell code can be reused and the cost drops sharply |
| Is the runtime global available? | `frontend.runtimeGlobal` (`window.wails`) | `true` | Only ES module imports are available |
| Are pre-ready events dropped? | `frontend.earlyEventSeen` plus `earlyEmit` | Expected `false` (dropped) | Startup progress events must replay after ready |
| Is frameless dragging still CSS driven? | `frontend.draggableValue` plus a manual drag | Value is `drag` and the window moves | The custom title bar needs a different implementation |
| Which GPU policies really work on GTK4? | `go.gpuPolicy` plus runs with both values | Only `always` / `never` apply; WebKitGTK 6.0 removed ON_DEMAND | The client's existing enum-mismatch workaround must be rewritten |
| Does `OnDomReady` still exist in v3? | Source inspection (below) | It does not; use `WindowRuntimeReady` | The microphone permission hook needs a new mount point |

## Findings From the v3.1.0 Source

- `application.Options` has no `OnStartup`, `OnDomReady`, or `OnBeforeClose`; lifecycle lives on services (`ServiceStartup` / `ServiceShutdown`) and window events (`OnWindowEvent` / `RegisterHook`).
- `events.Common.WindowRuntimeReady` is the only signal that the frontend runtime is initialized.
- The GTK4 backend (`pkg/application/linux_cgo.go`) handles only `WebviewGpuPolicyNever` and `WebviewGpuPolicyAlways`; every other value falls back to ALWAYS, because WebKitGTK 6.0 removed `ON_DEMAND`.
- The `-tags gtk3` backend is still present in the v3.1.0 source (`linux_cgo_gtk3.go`), which contradicts documentation claiming removal in v3.1; treat the source as authoritative, but do not rely on the path surviving.
- `BundledAssetFileServer` serves the runtime at `/wails/runtime.js`, so a static shell can `import` it without a bundler.
- On Linux the page is still loaded through the `wails://` custom URI scheme, but **v3.1.0 measures as `wails://localhost`, not v2's `wails://wails`** (see the measured results).

## Measured Results (2026-11-03, Deepin 25 / X11 / Intel Mesa)

| Item | Measured | Conclusion |
| --- | --- | --- |
| Build and link | `GTK 4.18.6 · WebKitGTK 2.52.5` | Passes; only upstream `gdk_x11_*` deprecation warnings |
| Page origin | `wails://localhost` | **Differs from v2's `wails://wails`**: localStorage starts empty, so old settings are unreadable |
| localStorage | Readable and writable | Works; data from the old origin cannot be reached from the new one |
| Legacy globals | `window.go` and `window.runtime` are gone | The static shell needs a shim |
| Runtime global | `window.wails` exists | A shim can build on it, or the shell can use ES modules directly |
| Bindings | `Call.ByName` succeeds with `pong:spike` | Go is callable without generated bindings or a bundler |
| Early events | Emit returned `false` and the frontend never saw it | Pre-ready events are dropped; startup progress must replay after ready |
| Ready event | Received at about 1023 ms, service startup at 2 ms | `WindowRuntimeReady` works and is the only reliable readiness signal |
| Drag property | `--wails-draggable` computes to `drag` | CSS-driven dragging still holds; an actual drag needs manual confirmation |
| GPU policy | `always`, no rendering problems on Intel Mesa | NVIDIA machines and the `never` comparison still need checking |
| Runtime warnings | Missing a11y bus; `signal 10` (SIGUSR1) taken over by JSC | The first is harmless; the second means waking the window with SIGUSR1 still fails under v3, so the existing Unix socket path stays |

**Impact of the origin change**: the client stores locale, fonts, and terminal font size in the shell origin's localStorage, so an upgrade cannot read them and falls back to defaults silently. Two options: accept a one-time reset, or move shell settings into the Go-side configuration file (the existing `AppConfig` already carries window, theme, and port, and extending it is the sturdier home). Reading the old origin directly is impossible because localStorage is isolated per origin.

## Packaging Verified In A Linglong Container (2026-11-03)

| Item | Result |
| --- | --- |
| Linglong package build | Succeeds; `buildext` merges GTK4, WebKitGTK 6.0, JSC, and the WebKit helper processes into `${PREFIX}` |
| WebKit helper path | The build byte-patches `libwebkitgtk-6.0.so.4` (`/usr/lib/x86_64-linux-gnu/webkitgtk-6.0` to `/tmp/dsh-webkit-6.0`), and the helpers measurably launch from that path inside the container |
| Behaviour inside the container | Same as the host: origin `wails://localhost`, localStorage works, `Call.ByName` returns `pong:spike`, pre-ready events are dropped, and `--wails-draggable` is `drag` |
| Graphics stack version | The package takes the build container's apt candidate: GTK 4.18.6 with **WebKitGTK 2.50.4**, while the host run used 2.52.5. The two distribution lines (deb12 and deb13) differ, so a real release must pin the target version |
| WebKit UI process sandbox | Kept enabled: the package ships `bwrap 0.10.0` and `xdg-dbus-proxy`, and the build byte-patches `/usr/bin/bwrap` and `/usr/bin/xdg-dbus-proxy` in the `.so` to short `/tmp` paths as well. The sandbox measurably works inside the Linglong container (bwrap creates its namespace normally) |
| Library resolution inside the sandbox | With the sandbox on, helpers run in bwrap's mount namespace and cannot read the container-level `ld.so.cache`, failing with `libwebkitgtk-6.0.so.4: cannot open shared object file`; the startup script sets `LD_LIBRARY_PATH` to the packaged lib directory and everything works. A real client needs the same |
| Exit and cleanup | `ll-builder run` does not pass the caller's environment into the container, so automatic exit must be the default inside the package's wrapper script; a hard kill leaves `/run/user/<uid>/linglong/<hash>` and `box/<hash>` behind, which blocks later builds and runs until removed |

In one line: both packaging and the sandbox policy are verified to work; what remains is pinning the packaged WebKit version.

## Permission Model: No Custom Hook Needed (2026-11-04)

The existing client ships a custom GTK3 hook that walks the widget tree and connects `permission-request`. The v3 Linux backend connects that signal itself (`handlePermissionRequest`): media capture follows the window's `Permissions` map, and **every other request goes back to WebKit's default handling, which denies it on Linux**. Inside the container the custom hook measured `installed=true` and `signalConnected=true` yet `requests=0`: it always runs after the framework, and the framework returns TRUE first, which ends the signal emission.

The equivalent mapping is:

```go
Permissions: map[application.PermissionType]application.Permission{
    application.PermissionMicrophone: application.PermissionAllow,
    application.PermissionCamera:     application.PermissionDeny,
}
```

A/B measurement on one binary and one page, changing only the policy:

| Policy | `probes.mic` |
| --- | --- |
| `microphone=allow` | `ok=true` |
| `microphone=deny` | `ok=false` with `NotAllowedError` |

The container has no camera, so `getUserMedia({video:true})` fails at device matching with `OverconstrainedError`; camera denial therefore has no hardware-level evidence, but the microphone A/B proves the map is honoured, and screen sharing carries the video flag and maps to the same `PermissionCamera`.

Conclusion: the migration can delete `internal/webviewperm` (GTK3 cgo plus widget-tree walking) and configure `Permissions` on the window instead.

## Frontend Shell Migration Check (2026-11-04)

The client shell is a 2894-line static script that depends on 10 v2 globals. The check writes the shell in **pure v2 style** (no module imports) and adapts v3 back to those globals:

| Item | Result |
| --- | --- |
| `window.go.app.App` calls | `Ping`, `ReadClipboardImage`, `RememberSession`, `ReportClientBootFailure`, and `StartupState` all work; the binding returns `pong:spike` |
| `window.runtime` calls | `EventsOn` and `EventsEmit` work; `BrowserOpenURL`, `ClipboardGetText`, `ClipboardSetText`, `WindowMinimise`, `WindowToggleMaximise`, and `Quit` map the same way |
| Shim ordering | `legacy.shimInstalled=true`: `boot.js` installs the shim before dynamically importing the shell, so the shell's "no window.go means browser preview" branch never misfires |
| Startup state | `StartupState()` returns `{"phase":"service-startup","sinceStartMs":3}` |

A migration only needs `index.html` to replace `<script src="./app.js">` with `<script type="module" src="./boot.js">`; the shell source stays untouched.

**One behaviour must change**: this round measured `announceSeen=false`, because the shell subscribed one step late and missed the event emitted right after `WindowRuntimeReady`. v3 does not replay events for late subscribers, so every one-shot startup-progress event in the client must become "pull once after load". Periodic status events such as `harness:status` are unaffected.

## Shell Settings Migration Check (2026-11-04)

| Item | Result |
| --- | --- |
| First run | `settings.source=default`, writing defaults to `/tmp/dsh-v3-spike-settings.json` |
| Second run | `settings.source=file`, reading the same content back |
| Stored content | `{"locale":"zh-CN","terminalFontSize":13,"theme":"dark"}` |

Conclusion: moving settings to the Go side survives runs. The old origin (`wails://wails`) localStorage cannot be read at all, so the migration action is to move those keys into the client's existing `AppConfig` and write defaults on first start; users see a one-time reset and are then insulated from origin changes.

## Running

```sh
sh verify.sh
make check
make build
make build-gtk3
ll-builder build -f apps/linyaps/desktop-v3-spike/linglong/linglong.yaml
ll-builder run -f apps/linyaps/desktop-v3-spike/linglong/linglong.yaml
```

Environment variables: `DSH_V3_SPIKE_REPORT` sets the report path, `DSH_V3_SPIKE_AUTOCLOSE=1` exits after the report is written, and `DSH_V3_SPIKE_GPU=never|always` switches the hardware acceleration policy.

## Linglong Runtime Availability

Neither the official base (`org.deepin.base/25.2.2`) nor the official runtimes (dtk, webengine) ship GTK4 or WebKitGTK. The Deepin 25 distribution repository does provide `libgtk-4-1`, `libgtk-4-dev`, `libwebkitgtk-6.0-4`, and `libwebkitgtk-6.0-dev`, so the libraries are bundled with the package under `$PREFIX/lib`, as the official FAQ prescribes. Full evidence and sources: [Linglong runtime evidence](docs/linyaps-runtime-evidence.md).

## Verification Done and Not Done

The real machine ran end to end on 2026-11-03 (Deepin 25, GTK 4.18.6 + WebKitGTK 6.0 2.52.5): dependency check, cgo build, window startup, frontend reporting, binding call, and report writing all succeeded. The results are in the previous section.

Done in this repository's development sandbox: `go mod tidy`, `gofmt`, `GOOS=windows CGO_ENABLED=0 go build ./...`, and `go vet ./...` all pass, which shows the v3.1.0 API usage is correct.

Still unverified: an actual title bar drag and the window control buttons (manual); the `DSH_V3_SPIKE_GPU=never` and NVIDIA rendering comparison; hardware-level confirmation that the camera is denied (the container has no camera); a settings migration plan for the origin change; and the WebKit sandbox policy (bundled bwrap versus disabled) together with pinning the packaged WebKit version.

## Exit Criteria for Promoting the Module

1. Every pass criterion in the matrix holds, or each failure has a concrete alternative.
2. A Linglong package can ship the GTK4 + WebKitGTK 6.0 libraries and start, including the WebKit helper process paths handled today by `install-shell-runtime.sh`.
3. The microphone permission moves to v3's `Permissions` map, so the GTK4 hook is no longer needed (the A/B measurement confirms the map takes effect).
4. A frontend shim (events, bindings, window controls, clipboard) can cover the existing 2894-line static shell with minimal edits.

Only after all four hold does replacing `desktop-launcher` or merging with `desktop-dual` make sense.

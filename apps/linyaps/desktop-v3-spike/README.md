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
- On Linux the page is still loaded through the `wails://` custom URI scheme, so the origin very likely stays `wails://wails`; `frontend.origin` exists to settle it.

## Running

```sh
sh verify.sh
make check
make build
make build-gtk3
```

Environment variables: `DSH_V3_SPIKE_REPORT` sets the report path, `DSH_V3_SPIKE_AUTOCLOSE=1` exits after the report is written, and `DSH_V3_SPIKE_GPU=never|always` switches the hardware acceleration policy.

## Linglong Runtime Availability

Neither the official base (`org.deepin.base/25.2.2`) nor the official runtimes (dtk, webengine) ship GTK4 or WebKitGTK. The Deepin 25 distribution repository does provide `libgtk-4-1`, `libgtk-4-dev`, `libwebkitgtk-6.0-4`, and `libwebkitgtk-6.0-dev`, so the libraries are bundled with the package under `$PREFIX/lib`, as the official FAQ prescribes. Full evidence and sources: [Linglong runtime evidence](docs/linyaps-runtime-evidence.md).

## Verification Done and Not Done

Done in this repository's development sandbox: `go mod tidy`, `gofmt`, `GOOS=windows CGO_ENABLED=0 go build ./...`, and `go vet ./...` all pass, which shows the v3.1.0 API usage is correct.

Not done, because the sandbox has no `gcc`, no `pkg-config`, no GTK/WebKit development packages, and no desktop session: GTK4 linking and real execution, the origin and localStorage measurements, the binding call, event timing, dragging, and the GPU policy comparison. Those require `sh verify.sh` on a Deepin 25 desktop with `libgtk-4-dev` and `libwebkitgtk-6.0-dev` installed.

## Exit Criteria for Promoting the Module

1. Every pass criterion in the matrix holds, or each failure has a concrete alternative.
2. A Linglong package can ship the GTK4 + WebKitGTK 6.0 libraries and start, including the WebKit helper process paths handled today by `install-shell-runtime.sh`.
3. The microphone permission hook mounts under GTK4; the current implementation walks the widget tree with GTK3's `gtk_window_list_toplevels` and must be rewritten.
4. A frontend shim (events, bindings, window controls, clipboard) can cover the existing 2894-line static shell with minimal edits.

Only after all four hold does replacing `desktop-launcher` or merging with `desktop-dual` make sense.

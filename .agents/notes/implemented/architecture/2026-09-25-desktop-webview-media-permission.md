# Agent Note: Desktop WebView media capture permissions

Status: implemented

English | [中文](2026-09-25-desktop-webview-media-permission.zh.md)

## Problem

Voice input could not record in the Linux desktop build, while the same Web GUI recorded normally in a browser. The voice-input client maps `NotAllowedError` to a permissions message, so the failure read as a missing grant. Three independent defects actually sat on that path, and because all three end in the same user-visible message, fixing any one of them alone changed nothing observable.

## Decision

Fix all three in the launcher, each at the layer that owns it.

The shell embeds the harness UI in a cross-origin iframe, and `microphone`'s permissions policy allowlist defaults to `self`. Without `allow="microphone"` the request is rejected before WebKit emits a permission signal, so this defect masks the other two; the iframe in `frontend/index.html` now carries `allow="fullscreen; microphone"`.

Wails v2's Linux backend never connects `WebKitWebView::permission-request`, and WebKitGTK's default for that signal is deny. The `WebKitWebView*` lives in an `internal/` package, so `internal/webviewperm` instead walks the process's top-level windows with `gtk_window_list_toplevels()` and connects the signal itself. `OnDomReady` supplies the GTK main thread, so the walk needs no thread hop, and the walk is recursive rather than depth-bound so a future Wails layout change degrades to a logged miss instead of attaching to the wrong widget. The grant decision is a pure Go function over facts extracted in C, which keeps it unit-testable without a display and confines C to reading facts and calling allow or deny. Only audio capture is granted; camera, display, and non-media requests stay denied because no embedded feature needs them.

Device enumeration runs in WebKit's WebProcess, which inherits the launcher's environment. The bundle ships its GStreamer plugins under `<PREFIX>/lib/x86_64-linux-gnu/gstreamer-1.0` while the container's plugin directory holds only `coreelements` and `coretracers`, so with no search path `appsink` fails to load, enumeration reports zero audio inputs, and the request fails with `OverconstrainedError` instead. `packaging.ConfigureGStreamerPlugins()` sets the additive `GST_PLUGIN_PATH` before `wails.Run` and does nothing in dev mode, where the plugins are already on the standard path.

## Alternatives considered

**Patch Wails and consume a fork.** Connecting `permission-request` beside Wails' existing `load-changed` handler is the correct long-term fix, but it waits on upstream review and release. Keeping it a separate follow-up lets the launcher ship now against the unpatched module.

**Intercept `webkit_web_view_new` through `LD_PRELOAD`.** This reaches the same view without launcher Go code, but depends on Wails' creation order and leaves no compile-time signal when that order changes.

**Treat `NotAllowedError` as proof that enumeration succeeded.** Both a permissions-policy rejection and WebKit's default deny produce that name, and the policy rejection happens first; inferring device availability from it hid the GStreamer defect. Separating the two requires observing whether the permission signal fired at all.

## Consequences

The launcher accepts a CGO dependency on GTK and WebKit, making `webviewperm` the only non-pure-Go package in the module; its decision logic stays in Go so `policy.go` still tests where GTK is absent. Startup never depends on the walk succeeding — a miss logs one line and the shell behaves as before. Verification needs a real window because the traversal targets Wails' own widget tree: the recorded evidence came from a throwaway Wails probe built from the launcher module and run against the packaged environment, plus reading the live `WebKitWebProcess` environment to confirm which variables the shipped app actually exports.

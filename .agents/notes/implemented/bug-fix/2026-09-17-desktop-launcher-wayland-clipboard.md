# Agent Note: Wayland clipboard image paste in the desktop launcher

Status: implemented

English | [中文](2026-09-17-desktop-launcher-wayland-clipboard.zh.md)

## Problem

After switching to a Wayland session, pasting a screenshot into the client did nothing and never returned, and copying an image file in the file manager failed the same way. Four independent defects stacked up:

1. `connectSocket` hardcoded the X server as abstract socket `/tmp/.X11-unix/X0`, file socket `/tmp/.X11-unix/X0`, then TCP `127.0.0.1:6000`. An X11 session's server is exactly `:0`, so the defect stayed invisible; a Wayland session runs XWayland on `:1`, so the client connected to a different X server and never tried the one that exists.
2. `setup` did not pad the auth name and auth data to 4-byte boundaries. `MIT-MAGIC-COOKIE-1` is 18 bytes, so the request must be `12 + 20 + 16 = 48` bytes but only 46 were sent. A server that receives a short request waits instead of replying `Failed`, and the setup read had no timeout, so the clipboard read blocked forever. The `Failed` reply's extra-data length was also read in bytes rather than 4-byte units.
3. `wl-clipboard` was not shipped, so `readWaylandImage` found no command and returned silently.
4. The Wayland channel had only a bitmap strategy. Copying an image file in the file manager puts no `image/*` on the clipboard at all, so every `wl-paste --type image/*` probe came back empty and the URI list was never consulted.

## Decision

`connectSocket` derives candidates from `DISPLAY` (`x11Transports`: abstract socket, file socket, TCP, in that order) and gives up on the X11 channel when `DISPLAY` is unset or unparseable instead of guessing. `setup` pads auth name and data with `pad4`, multiplies the `Failed` extra-data length by 4, and gives the setup read a `readTimeout` that is cleared once the handshake succeeds. The package ships `wl-clipboard` through `buildext.apt.depends`, `tools.yaml` declares `wl-paste` with `wl-paste --version` as its verify command, and `verify-merged-deps.sh` claims that dependency. `ReadImage` gains a fifth strategy that reads the Wayland clipboard's `text/uri-list`, and both channels share one URI/path parser (`readImageFileFromURIList`), so X11 and Wayland cover the same two sources: bitmaps and copied files.

`wl-paste --version` runs without a compositor connection (exit code 0 with no `WAYLAND_DISPLAY`), which is why it is safe as the build-machine verify command.

## Measured behavior

A 12420-byte PNG placed on the Wayland clipboard read back as 0 bytes from X11 `CLIPBOARD` and `PRIMARY`, and as 12453 bytes from `wl-paste` (the deepin clipboard manager re-encodes it; the extra 33 bytes stay inside a valid PNG). `ReadImage()` returned the latter: XWayland does not bridge image formats, so a Wayland-held selection is reachable only through `wl-paste`. A real screenshot tool reproduced the same result: the Wayland side offered `image/png` (146058 bytes) among a dozen `image/*` types, while the X11 side read 0 bytes for both `image/png` and `text/uri-list`.

Copying an image file in the DDE file manager puts only `text/uri-list` (106 bytes, percent-encoded), `x-special/gnome-copied-files` (62 bytes, first line `copy` plus the raw UTF-8 path), `x-dfm-copied/file-icons`, and `text/plain` on the clipboard — no `image/*` at all — and the X11 side is completely empty.

The container binds `/home`, `/media`, and `/mnt` at the same paths as the host, so files referenced by those URIs are readable inside the container.

## Alternatives considered

**Rely on XWayland to bridge the Wayland clipboard to X11.** Measured false: the same PNG read 0 bytes from X11 `CLIPBOARD` and `PRIMARY`. Rejected.

**Fix only the X11 channel (DISPLAY derivation and auth padding).** In a Wayland session the Wayland-held selection is reachable only through `wl-paste`, and `wl-clipboard` was not shipped, so `readWaylandImage` silently returned nothing. Insufficient.

**Keep the Wayland channel bitmap-only.** The file manager puts no `image/*` on the clipboard when copying an image file, so the probe always came back empty. The channel therefore gained the `text/uri-list` strategy, so both channels cover both sources.

## Consequences

The X11 session worked for as long as it did only because the local X server accepts unauthenticated connections, which happened to bypass the padding defect; after the fix the channel no longer depends on that coincidence. The setup read timeout also turns a protocol anomaly into an error instead of an unbounded block.

One measurement was not reproducible: the first `PRIMARY` attempt in the X11 session read 0 bytes. Re-running the identical sequence three times, each time confirming the selection owner held 25151 bytes first, passed all three times, so it is treated as a race in the test script rather than a code defect, and no mechanism was captured for it.

## Testing

`TestSetupRequestWireFormat` asserts the 48-byte handshake, zero padding bits, the 18/16 length fields, and that the `Failed` reply is fully consumed; `TestPad4`, `TestX11Transports`, `TestConnectSocketFollowsDisplay`, and `TestConnectSocketNoDisplay` cover transport derivation. `authFakeServer` reads the protocol-padded length and checks the padding bits, because its earlier form accepted the malformed 46-byte request and masked defect 2. `TestReadImageFileFromURIList` (14 cases), `TestReadWaylandUriListImage` (4), `TestReadWaylandImageBitmap` (2), `TestWaylandWlPaste` (2), and `TestReadImageFallsBackToWaylandUriList` cover the Wayland strategies.

End to end: with the host `xclip` holding X1's `CLIPBOARD`, `ReadImage()` read back 12420 bytes byte-identical; with a real `wl-copy` holding the Wayland clipboard, `wl-paste` and `ReadImage()` both read 12453 bytes; after copying a 318520-byte PNG in the file manager, `ReadImage()` read back the same byte count. After rebuilding and installing `0.1.3.2`, manual acceptance passed all four cases in both sessions: paste text in, copy text out, paste a screenshot, paste an image file.

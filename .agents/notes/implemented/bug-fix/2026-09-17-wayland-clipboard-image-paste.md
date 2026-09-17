# Agent Note: Clipboard image paste in a Wayland session

Status: implemented

English | [中文](2026-09-17-wayland-clipboard-image-paste.zh.md)

## Problem

Pasting a screenshot into the packaged client did nothing once the session ran on Wayland, and the read never returned. Four independent defects stacked.

The clipboard reader hard-coded its X server: abstract socket `/tmp/.X11-unix/X0`, file socket `/tmp/.X11-unix/X0`, then TCP `127.0.0.1:6000`. An X11 session's X server is `:0`, which is why this survived; a Wayland session's is XWayland on `:1`, so the client talked to a server that was not there and never tried the one that was.

The authentication request omitted the protocol's 4-byte padding after auth name and auth data. `MIT-MAGIC-COOKIE-1` is 18 bytes, so the request must be `12 + 20 + 16 = 48` bytes and went out as 46. A real server does not answer a short request with `Failed`; it keeps waiting for the missing bytes, and the reader had no deadline, so the whole clipboard read blocked forever — the "nothing happens and it never comes back" symptom. The same reply parser read the `Failed` reply's additional-data length as bytes where the wire format counts 4-byte units, leaving three quarters of that payload in the connection.

Both defects only bite when the server demands a cookie, which is exactly the XWayland case: X11 hosts commonly accept the local connection without authentication, so the no-auth attempt succeeded there and the cookie path was never exercised. The fake X server in `x11_test.go` had the same blind spot — it read `nameLen+dataLen` bytes, which accepted the malformed 46-byte request — so the padding defect was invisible to the suite.

A selection held by a Wayland client also cannot fall back through X11: measured, XWayland does not bridge Wayland's `image/png` into an X11 selection. The same 12420-byte PNG placed on the Wayland clipboard came back from `wl-paste` (12453 bytes; deepin's clipboard daemon re-encodes it) while X11 `CLIPBOARD`/`PRIMARY` yielded 0 bytes. A selection held by an XWayland client still reaches the X11 channel. The reader's Wayland channel shells out to `wl-paste`, but `wl-clipboard` was absent from the package, the host and the base runtime alike, so that channel silently returned nothing.

That channel then only had a bitmap strategy. Copying an image *file* in the file manager advertises no `image/*` type at all — measured against DDE's file manager, the offered types were `text/uri-list`, `x-special/gnome-copied-files`, `x-dfm-copied/file-icons` and `text/plain` — so every `wl-paste --type image/*` probe came back empty and the URI list was never consulted. Since the selection is not bridged to X11 either, that paste failed on both channels at once. The X11 channel covered both sources and the Wayland channel only one, and that asymmetry was the defect.

## Decision

Derive the X server from `DISPLAY` instead of hard-coding it, honour the setup wire format, and bundle `wl-clipboard`.

`connectSocket` walks the transports for the session's `DISPLAY` (abstract socket, file socket, then TCP for remote displays) and gives up on the X11 channel when `DISPLAY` is unset or unparsable. Guessing a display is what produced the wrong-server connection in the first place, so the fallback chain no longer includes a guess.

`setup` pads auth name and data through `pad4`, multiplies the `Failed` additional-data length by 4, and sets `readTimeout` for the handshake read, cleared once the handshake succeeds so the per-request deadlines that follow still own their own timing. A protocol fault now surfaces as a timeout instead of an unbounded block.

`buildext.apt.depends` gains `wl-clipboard`; `tools.yaml` registers `wl-paste` with `verify: wl-paste --version`, which runs without a compositor and exits 0, so a headless build host can check it honestly; and `verify-merged-deps.sh` claims the dependency as `tool:wl-paste`, so it cannot be declared without a landing check. The reader's existing lookup order (PATH first, then the host-mount and system paths) finds `$PREFIX/bin/wl-paste` because that directory is on the container PATH.

Both channels cover both sources. `ReadImage` gains a fifth strategy reading the Wayland clipboard's `text/uri-list`, and the URI-list parsing the X11 channel already had moves into `readImageFileFromURIList`, which both channels share rather than each growing its own copy. The Wayland side tries `text/uri-list` before `x-special/gnome-copied-files` (the GNOME convention, whose first line is `copy`/`cut`): DDE's file manager offers both and their line format is identical, so one parser serves them and `uriToPath` skips the `copy` line by rejecting it as a non-`file://` entry. Paths resolve without translation because the container bind-mounts `/home`, `/media` and `/mnt` at the host's paths.

## Alternatives considered

**Implement `wl_data_device`/`wlr-data-control` directly in Go.** The Wayland channel transfers file descriptors across processes and needs a full event loop; `wayland.go` already records that the error surface outweighs one `exec`.

**Keep the package free of `wl-clipboard` and rely on the host.** The host does not ship it either, and the read happens inside the container.

**Make the X11 channel carry Wayland-owned selections instead.** Measurement rules this out: the bridge carries no image format, so a selection held by a Wayland client cannot be read through X11 regardless of transport or authentication.

**Treat `text/plain` as a file path too.** Some file managers put the bare path there. Rejected on both channels: pasting text that happens to mention `photo.png` would then paste an image instead, which is content the clipboard owner never offered as an image.

## Consequences

Clipboard image paste works in both session types and from both sources: a bitmap on the clipboard, or an image file copied in the file manager. X11 sessions keep the self-implemented wire client and now also work on hosts that demand a cookie; Wayland sessions read through the bundled `wl-paste`. The package grows by `wl-clipboard` (a 24 KB deb; its `libwayland-client` dependency is already present through GTK). Because the X11 channel is attempted first and a Wayland clipboard yields an empty X11 selection, every Wayland paste pays a failed X11 round trip before falling through — bounded now, not free.

## Testing

`go build`, `go vet` and `go test ./internal/clipboard/` pass. New tests pin the wire format: `TestSetupRequestWireFormat` asserts the 48-byte handshake in full (length fields 18/16, two zero pad bytes, cookie at `[32:48]`) and that the `Failed` reply is consumed completely; `TestPad4` covers alignment and that the helper does not mutate its input; `TestX11Transports`, `TestConnectSocketFollowsDisplay`, `TestConnectSocketNoDisplay` and `TestReadImageSkipsX11WithoutDisplay` cover display derivation and the no-`DISPLAY` short circuit. `authFakeServer` now reads requests at their padded length and fails the test when the pad bytes are not zero.

End to end with real components, in the environment the container sees (`DISPLAY=:1`, `XAUTHORITY=/run/linglong/Xauthority`, the session's Wayland socket): with the host's `xclip` owning X1's `CLIPBOARD`, `ReadImage()` returned the 12420-byte PNG byte for byte; with a real `wl-copy` owning the Wayland clipboard, it returned the 12453 bytes `wl-paste` serves; with an image file copied in DDE's file manager, it returned the file's 318520 bytes as a valid, plausible PNG where it previously returned 0 bytes and `errSelectionEmpty`.

The bridge measurement was repeated with the real screenshot tool (region capture, "copy to clipboard"), not only a synthetic `wl-copy` bitmap: the Wayland side offered `image/png` at 146058 bytes among a dozen `image/*` types, the X11 side returned 0 bytes for `image/png` and `text/uri-list` alike, and `ReadImage()` returned the PNG. A real screenshot therefore depends on the bundled `wl-paste` for the same reason the synthetic case did, which is what makes bundling `wl-clipboard` a requirement rather than an optimization.

`TestReadImageFileFromURIList` covers the shared URI parser (percent-encoded spaces, a `copy` first line, `#` comments, a non-image extension, a `.png` whose contents are not an image, an oversize file, a directory, a remote host) and `TestReadWaylandUriListImage` covers the type fallback order against a stub `wl-paste` on `PATH`, which is what lets the Wayland channel be exercised without a compositor or a clipboard owner. Reverse proof: making `readWaylandUriListImage` return nil, dropping `x-special/gnome-copied-files`, or dropping the magic-byte check each fails its corresponding test.

Packaging gates: `test-verify-tools.sh`, `test-verify-merged-deps.sh` and `test-verify-container-deps.sh` all pass, including the case that requires every declared dependency to be claimed by the rule table. Removing `bin/wl-paste` from a healthy tree makes `verify-merged-deps.sh` report `FAIL wl-clipboard` and exit non-zero, which shows the new claim is load-bearing.

A real `ll-builder` build then closed the packaging question (`.uab` sha256 `0cee3d5520be6b9470512c56fc10ed3a53efeb7bd13029df69a67e71b70f31c9`): `verify-merged-deps` reported `OK wl-clipboard (tools.yaml: wl-paste → bin/wl-paste)`, `verify-tools` reported `OK wl-paste`, the builder log had no unexempted `failed to copy`, and the export produced a 347 MiB `.uab`. The installed launcher binary contains `readWaylandUriListImage` and `readImageFileFromURIList`, symbols that exist only after this fix, so the shipped package carries it. Executed from the running container's own rootfs, the packaged `wl-paste` reached the live compositor and listed the current clipboard types, which means the binary the client actually runs works inside the container. Manual acceptance on the machine then passed all four Wayland paths: pasting text into the client, copying text out of it, pasting a screenshot, and pasting an image file copied in the file manager.

Still unverified: the X11 session at runtime. No session switch was run after the install — the last X11 session traces on the machine predate it. The X11 code path was exercised with `DISPLAY=:1` and `WAYLAND_DISPLAY` unset, reading 25151 bytes from both a bitmap and a `text/uri-list`; that differs from a real X11 session only in the display number and the `XAUTHORITY` value, both covered by unit tests.

## Related

Packaging precedent: [Bundle a real xdg-open in the desktop launcher package](2026-08-27-bundle-xdg-open-for-host-browser-opening.md). Audit entries in `apps/desktop-launcher/AUDIT.md`: N4 (cookie length byte order, the previous defect in this same handshake), S3 (unvalidated setup reply) and N30 (this round).

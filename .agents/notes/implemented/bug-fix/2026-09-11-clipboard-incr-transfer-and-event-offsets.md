# Agent Note: Shell clipboard reads X11 INCR image transfers

Status: implemented

English | [中文](2026-09-11-clipboard-incr-transfer-and-event-offsets.zh.md)

## Problem

Pasting a screenshot into the packaged shell's conversation composer did nothing, for every capture: the clipboard-history panel showed the image, `xclip -o -selection clipboard -t image/png` over the same X connection returned the whole file, and re-copying the entry from the history panel made the paste work. Copied image *files* pasted correctly, and a copied non-image file pasted nothing at all (that path owns [clipboard paste for large images and copied files](2026-08-30-clipboard-paste-large-image-and-file-copy.md)).

The owners of a raster capture — `deepin-screen-recorder` and the `dde-clipboard-daemon` that takes the selection over from it — deliver it as an X11 INCR transfer: the first `GetProperty` answers with an `INCR` property (format 32, one CARD32, `bytes-after` 0) whose value is the total byte count, and the payload follows in property segments announced by `PropertyNotify`. The shell reader in `apps/desktop-launcher/internal/clipboard` could not read that transfer, for four independent reasons:

1. `getProperty` entered its INCR branch only when `bytes-after != 0`, but the marker reply itself reports `bytes-after` 0 — the whole marker was 4 bytes and one read consumed it. The 4-byte marker was therefore returned as the payload and failed the PNG magic check, so a 108253-byte 1098×699 capture became a 4-byte "image" and the read ended as `errSelectionEmpty`.
2. `installWindow` created the requestor window with an empty value-list, so no `PropertyNotify` reached it and the chunk loop could only wait for its deadline.
3. The `PropertyNotify` field offsets were read four bytes late (window at 8, atom at 12 instead of 4 and 8), so a notification was compared against the `time` field and discarded.
4. The standard `SelectionNotify` (event code 31) property offset was read four bytes late (24 instead of the wire offset 20, where `property` sits after `time`, `requestor`, `selection`, and `target`). The container reaches X through the Linglong bridge, which rewrites the event to code 159 with the property at offset 20, so the branch that happened to be correct kept the defect invisible in the packaged build; a `.deb` install runs outside the container against a plain X server, where every conversion would have been read as refused.

## Decision

`getProperty` decides on the property type alone: a reply typed `INCR` starts a chunked transfer regardless of `bytes-after`. The marker's 4-byte value is the declared total, checked against `maxImageBytes` before any segment is read; each segment is read with the delete flag set and re-armed with `readTimeout`; the transfer ends when a segment reads back empty, and `maxINCRChunks` (65536) bounds the round count so an owner writing one-byte segments must still finish or be abandoned. The connection deadline is re-armed per segment because a slow but progressing transfer should complete, while a stalled one must not extend the paste indefinitely.

`waitPropertyNotify` owns the notification wait: it reads packets until a `PropertyNotify` for the requested property arrives, using the wire offsets window 4 and atom 8, and surfaces X errors as errors. `convert` accepts event codes 31 and 159 with the property read at offset 20 in both, since the bridge changes only the event code.

`installWindow` sends `CWEventMask` in the CreateWindow value-mask with `PropertyChangeMask` as its value; the two bit numbers are named constants because they are separate numbering schemes and mixing them makes the server answer BadValue. The requestor window stays unmapped, so no window flashes on screen during a read.

## Testing

`x11_test.go`'s fake server now emulates INCR as an owner does — marker first with `bytes-after` 0, then one segment per read with a `PropertyNotify` after each, then an empty segment — emits real 32-byte event packets at the wire offsets, and asserts the requestor window selected `PropertyChangeMask`. Its previous INCR scaffolding was dead code (an unused `incr` field, and a `SelectionNotify` written at offset 24 that agreed with the wrong reader), which is why the defects passed the suite. New cases cover a three-segment INCR image, the bridge's 159 event code, an unadvertised target list, an oversize marker, and the delete flag on every segment.

Live verification used the real owner: with `dde-clipboard-daemon` holding the selection, `ReadImage` returns the 108253-byte capture exactly (`sha256 bd7cd8a862bc803d…`, 1098×699, equal to the source file) while the pre-fix reader returns `errSelectionEmpty` on the same clipboard. Control reads with `xclip` on that owner were inconsistent — it intermittently returned nothing or only the first segment (65432 of 108253 bytes) — so each comparison re-armed the selection first; the fixed reader returned the complete image on every run.

## Alternatives considered

**Replace the hand-written wire client with a maintained X11 library such as `github.com/jezek/xgb`.** It would have removed this class of defect, but it adds a dependency to an application whose Go module deliberately carries only `xz` and Wails, and it rewrites a reader that is otherwise correct: three of the four defects are single-offset or single-condition mistakes, and the fourth is one missing attribute. Recorded as the fallback if the reader needs capabilities beyond selection reading.

**Detect transfer completion by the marker value instead of the empty segment.** The marker's CARD32 is the declared total, but an owner may write more or fewer bytes than it declares; only the empty segment is the protocol's end-of-transfer signal, and `maxImageBytes` already bounds the assembled result.

**Ask the owner for a smaller target to avoid INCR.** No owner advertising `image/png` guarantees a format below the INCR threshold, and the threshold is the owner's choice, so this would trade a protocol fix for an owner-dependent guess.

## Consequences

Screenshots and any other INCR-served capture paste into the composer through the shell bridge, which is the only bitmap channel the packaged WebKitGTK renderer has. A `.deb` install now has a working reader as well: the standard event code and its offsets are read correctly, so the bridge is no longer a precondition for the clipboard to work.

An abandoned INCR transfer is read as empty rather than as a corrupt payload, so a failed read reports "no image" and the composer falls back to WebKitGTK's own paste data instead of attaching a garbage file. The reader still never reads `text/plain` from the selection, so a copied non-image file continues to paste nothing; that gap belongs to the owner's fallback policy rather than to the transfer protocol.

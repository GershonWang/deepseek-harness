# Agent Note: Shell clipboard paste handles large images and file copies

Status: implemented

English | [中文](2026-08-30-clipboard-paste-large-image-and-file-copy.zh.md)

## Problem

Two clipboard paste paths in the packaged dsh-desktop-launcher produced wrong output:

1. **Large images pasted blank.** An image copied from a chat app (e.g. WeChat, right-click "copy image") reached the composer as a blank grey placeholder. The X11 clipboard bridge read the selection into a corrupt, truncated buffer: a 537,485-byte PNG came back as 13,200 bytes with no IEND chunk, so the browser could not decode it.

2. **Copying an image file did nothing.** Copying an image file in the file manager (right-click copy or Ctrl+C, which puts a `text/uri-list` on the X11 CLIPBOARD) and pasting into the composer had no effect: no attachment, no text, no error.

The clipboard bridge is the only channel for pasted images in the WebKitGTK shell — the engine never surfaces clipboard bitmaps to the page — so both defects were in that path.

## Decision

**Read the full reply payload.** The X11 wire reply header carries its length in a CARD32 at bytes 4-7 (in 4-byte words). The Go client read only the low 16 bits (`binary.LittleEndian.Uint16(hdr[4:6])`), so every reply larger than 65,535 words (262,140 bytes) was silently cut to `(length & 0xFFFF) * 4` bytes. `readReply` now reads the length as `binary.LittleEndian.Uint32(hdr[4:8])`. Large clipboard images (screenshots, chat images) transfer completely again.

**Consult the shell bridge before file items.** The web composer's capture-phase paste handler short-circuited whenever the paste event carried a `kind: 'file'` item with an `image/*` type, letting Lexical handle it natively. WebKitGTK exposes file items for a file-copy paste (a `text/uri-list` selection), but the container-side engine cannot provide the file bytes, so Lexical inserted nothing. The handler now always asks the shell bridge first — the bridge reads both a bitmap selection and a `text/uri-list` pointing at a readable image file — and only falls back to the delivered file items or plain text when the bridge has no image. This makes file-manager copies land as attachments.

## Testing

The Go change is verified against the live X11 server: reading a 537,485-byte PNG from the real clipboard returns the complete buffer ending in IEND, where it previously returned 13,200 truncated bytes. The existing protocol tests (fake server) pass. The web change is covered by the existing ui-conversation suite (349 tests, including `desktop-clipboard` and the composer paste path) plus a clean typecheck.

## Alternatives considered

**Map the requestor window.** Considered while diagnosing the blank image (a prior fix had stopped mapping the 8×8 requestor window to remove a top-left flash). A mapped-vs-unmapped comparison on the real server reads the same truncated 13,200 bytes both ways, proving the truncation is the reply-length bug, not a window-state effect. No mapping change is needed for large reads.

**Keep the file-item early return and only widen its condition.** Rejected because the container cannot rely on WebKitGTK to deliver usable file bytes from a selection it withholds; the shell bridge is the only reliable reader for both bitmap and uri-list clipboard contents.

## Consequences

Pasting large images (screenshots, chat images over 262 KB) works and renders fully. Copying an image file in the file manager and pasting it lands as an attachment when the container can read the file path (typically the mounted home directory). Pastes whose file path the container cannot read fall back to the delivered file items, matching the previous no-op rather than regressing text paste. Text-only pastes are unchanged.

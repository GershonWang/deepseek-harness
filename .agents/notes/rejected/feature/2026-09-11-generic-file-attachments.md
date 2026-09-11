# Agent Note: Generic file attachments in the composer

Status: rejected — every part of it lands in the harness core, and the format step changes the durable session format for every deployment, which does not belong on the Linglong launcher branch.

English | [中文](2026-09-11-generic-file-attachments.zh.md)

## Problem

The composer accepts text and raster images only. Copying a non-image file — from the file manager, or from any application that writes `text/uri-list` to the X11 clipboard — and pasting it into the composer does nothing at all: the packaged shell's clipboard bridge reads the selection, rejects a path whose extension is not a raster one, and returns an empty result, so neither an attachment nor a message appears. [The durable attachment decision](../../implemented/feature/2026-07-22-web-multimodal-image-input-and-durable-attachments.md) recorded generic files, file picking, and PDF as explicit follow-ups.

## Proposal

Add a second attachment kind beside images: an opaque file object stored byte for byte in the existing content-addressed attachment store, a file part on the wire, a `FileBlock` in the merge-extensible content map, a model-facing text block naming a read-only path with media type, byte length, and display name (the model reads the file with its own filesystem tools), a composer intake that stops filtering to images, and a shell that hands copied files over the clipboard bridge. Deployment policy would bound size and count rather than media type.

Because a file block carries model-visible content, the plan also carried a format step: `SESSION_FORMAT_VERSION` 0→1, a near-identity v0→v1 upgrade step, the adjacent-version upgrade chain the [version mechanism](../../implemented/architecture/2026-08-10-session-log-version-mechanism.md) had deferred, and a read-side guard that refuses a content block type the build does not know instead of dropping it.

## Why rejected

Six layers assume raster images end to end — the attachment service definition and its local provider, wire admission, the SDK protocol type, `ContentBlockMap` and the llm projection, the client's media-type check with its whole attachment rail, and the shell's clipboard bridge. A launcher-only change cannot deliver the capability: bytes for a PDF handed to the composer would be mislabeled by the client's magic-number fallback, refused by admission, or stored as garbage.

The `linglong-dev` branch carries the Linglong desktop launcher. Changing the harness's durable session format from it would move every deployment's log format, rewrite 144 recorded session fixtures, regenerate a catalog, and require its own snapshot and two-SDK evidence — a blast radius well beyond that branch's purpose. The capability belongs in a standalone core proposal against the harness, where that evidence is owned.

## Alternatives considered

**Shell-only delivery: hand the copied file's path to the composer as text.** The shell already reads `text/uri-list`, so a copied file could paste as its path, and the file would still reach the model through the agent's own read tools — with no attachment capability, no wire change, and no format change. This is the viable launcher-scoped option if the goal is only that a copied file stops pasting nothing; it was not taken because the request was for real attachments rather than path text.

**Represent files as durable text blocks instead of a new block type.** No format step at all, but history loses the structured attachment: no chip, no re-attach, only a line of path text. Rejected as a half-capability, since the shell-side path option above is strictly cheaper.

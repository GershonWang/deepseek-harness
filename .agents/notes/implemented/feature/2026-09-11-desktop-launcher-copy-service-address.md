# Agent Note: Copying the harness service address from the server dialog

Status: implemented

English | [中文](2026-09-11-desktop-launcher-copy-service-address.zh.md)

## Problem

The server dialog showed the harness address — `http://127.0.0.1:<port>/?token=<secret>` — as selectable text, and nothing else. Getting that value into a browser meant dragging a selection across a long token, and one missed character produced a page that refuses to load with no hint about why. The address is written to be read by a machine, not by a pointer.

## Decision

The address row carries a copy button beside the value, shown only while the harness is running. It copies `state.status.URL` verbatim — the same string the row displays, token included — through `window.runtime.ClipboardSetText`, the Wails-injected GTK clipboard the terminal already uses. Success replaces the button's icon with a check mark and its tooltip with 已复制 for 1.5 seconds; a second click restarts that timer so the last click keeps a complete confirmation.

Only the running state offers the button because every other state puts something else in that row: `harness 正在启动…` while starting, `LastExit` when failed, and the `harness.log` path once stopped. Copying those is copying an error message.

## Alternatives considered

**Copy the address without its token.** Safer on its face — the token is a bearer credential and the clipboard is a shared surface. Rejected: the web server rejects an address without it, so the copy would hand the user a value that cannot do the one thing they asked for, and they would have to return to the dialog to assemble the real URL by hand. The token is already rendered in plain text in the same row, so copying it discloses nothing the screen has not.

**Use `navigator.clipboard.writeText`.** The standards-track API, and it would avoid depending on a Wails runtime binding. Rejected: clipboard permission is unreliable inside the WebKit container, which is why terminal copy goes through `ClipboardSetText` in the first place; maintaining two channels would turn "copy works here but not there" into a recurring defect.

**Add a toast or transient banner.** More noticeable than a button changing state. Rejected: the launcher has no toast facility, and introducing one for a single copy — with the row-height shift a transient line brings — costs more than the confirmation is worth, while the user's gaze is already on the icon they just clicked.

**Show the button in every state.** Simplest rule, and it removes the state check. Rejected: it invites copying `LastExit` or a log path, which reads as a successful copy of something the user did not want.

## Consequences

A copy affordance exists only where a copyable address exists. The clipboard ends up holding a bearer token, which the user asked for by clicking a button labeled 复制服务地址; anyone reviewing that as an exposure should weigh it against the token already being on screen in the same dialog. The confirmation depends on a 1.5-second timer rather than on any durable state, so a user who looks away mid-flash sees the button return to normal with nothing recorded — acceptable for a copy, where the clipboard itself is the record.

## Testing

`node --test frontend/test-app.cjs` covers the running state (the button is visible, clicking writes the complete address including its token through the clipboard channel, and the button enters its copied state) and the stopped state (the button stays hidden). The clipboard channel is stubbed in the test's Wails runtime, which records the exact text handed over.

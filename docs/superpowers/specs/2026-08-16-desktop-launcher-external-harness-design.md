# Desktop launcher: external harness connection (local/remote service) design

English | [中文](2026-08-16-desktop-launcher-external-harness-design.zh.md)

## Background and goals

`apps/desktop-launcher` currently always loads the harness service **inside the Linglong container** (the Go supervisor spawns a `dsh web` subprocess and the webview loads its loopback URL). The official repository runs the web build on the host machine through `npx @deepseek-ai/dsh web` and accesses it with a browser.

This design adds an **external service connection mode** to the server status dialog:
1. The user can switch between the container harness and an external harness service (a local `npx dsh web`, or another machine reachable over the network).
2. Switching to external mode **stops the container harness first** (releasing the port and pausing automatic restart).
3. The external connection makes the webview `Navigate` straight to the target URL — it is already proven that the Linglong sandbox shares the host network namespace, so from inside the sandbox the host loopback and any host-reachable address can be reached directly.

Constraints: every change stays in `apps/desktop-launcher/`; upstream is untouched; the container harness state machine in supervisor.go does not change.

## Confirmed requirements (aligned with the user)

| Decision | Conclusion |
|---|---|
| After disconnecting the external connection | **Return to container mode automatically**: restart the container harness → wait until ready → navigate back to the container URL |
| Remembering the external URL | Remembered and filled in automatically when the dialog opens; **no automatic reconnect** |
| Safety confirmation for non-local addresses | Show a confirmation prompt (once per host per session, shown again after a restart) |
| Dialog form | Option A: integrate the mode switch inside the dialog and rearrange the whole layout (visuals to @designer) |

## Overall architecture

```
容器模式(默认)                外部模式
  running ──切外部──► StopHarness()(停容器+暂停自动重启)
                       → HTTP 探测 URL(3s 超时)
                       → 成功:Navigate(url) = ExternalConnected
                       → 失败:弹框内错误提示,留在当前模式
  stopped ◄──断开────   Navigate(容器URL) 前先 Start() → 等就绪
```

- **Navigation manager** (new component, pure Go and testable): tracks where the webview currently points (container/external URL) and provides `ConnectExternal(url)` / `DisconnectToContainer()` / `Status()`.
- **Navigation timing**: every `Navigate` is called on the GTK main thread (the thread the dialog button callbacks run on); the `w.Navigate` of webview_go is injected into the UI layer as a closure (`func(string)`), avoiding cross-thread calls.
- **Status bar**: in external mode it shows "● external service http://…"; container mode is unchanged.
- **Supervisor untouched**: the container harness's Start/StopHarness/Status are reused directly.

## Dialog layout (redesign, to @designer)

```
┌─ 服务器状态 ──────────────────────────┐
│ 连接模式  (•) 容器内   ( ) 本机/远端服务 │
│ ────────────────────────────────── │
│ ● 运行中            (容器模式状态区)    │
│ 地址  http://127.0.0.1:40847          │
│ ────────────────────────────────── │
│ 服务地址 [http://127.0.0.1:3456____] [连接] │
│          (外部模式输入行,连接后变 [断开]) │
│ ────────────────────────────────── │
│              [启动] [重启] [停止]        │  ← 仅容器模式
└────────────────────────────────────┘
```

- The mode switch control, the spacing between sections, and the visual hierarchy are finalized by the designer.
- In external mode, start/restart/stop are hidden and connect/disconnect are shown.
- After connecting, the input row is read-only or the button becomes "Disconnect" (the designer decides).

## URL persistence and safety confirmation

- **Persistence**: `~/.config/dsh-desktop/config.json`, format `{"externalUrl": "..."}`; written after the first successful connection; read and filled in when the dialog opens; a missing or corrupt file is silently treated as empty.
- **Safety confirmation**: before connecting to a non-loopback address (`127.0.0.1`/`localhost`/`::1`), show a confirmation box whose text warns "this connects to a remote harness and its commands run on the remote machine"; a host already confirmed in this session is not asked again (an in-memory set), and the prompt returns after a restart.

## Error handling

- Probe failure (timeout / non-2xx / non-3xx) → a red error hint inside the dialog, no navigation, and the current mode is kept.
- The connection succeeds but the page fails to load → the status bar falls back to a hint and the user can retry.
- Container-mode startup failure → reuse the existing repaired path (StateStopped + "start failed").

## Tests (all pure Go, GTK stays a thin layer)

| Component | Test |
|---|---|
| `probe(url)` HTTP probe | httptest: 2xx passes, 5xx/timeout fails |
| `isLoopback(url)` | 127.0.0.1/localhost/::1 → true; a LAN IP → false |
| URL persistence read/write | read and write in a temporary directory, corrupt JSON falls back |
| Connection state machine | the state transition container → external → disconnect back to container (reusing the mock script) |

## File changes (all under `apps/desktop-launcher/`)

| File | Change |
|---|---|
| `connection.go` (new) | probe / URL validation / persistence / connection state machine (pure Go) |
| `ui.go` | dialog redesign (mode/URL/connect-disconnect) + navigation closure + safety confirmation (visuals to the designer) |
| `window.go` / `main.go` | inject the webview `Navigate` closure |
| `ui_state.go` | extend the external-mode status text |
| `supervisor.go` | **unchanged** |
| README | updated |

## Boundaries and risks

- The sandbox shares the host network (measured: the same netns, and the host loopback is reachable with HTTP 200), so the network layer is no obstacle.
- A remote harness must bind a non-loopback interface to be reachable (`dsh web --host <LAN-IP>`; `--host 0.0.0.0` is deliberately rejected upstream); browser trust on the remote side needs `--trusted-host`. This is a precondition for use, noted in the documentation, not part of this feature's implementation scope.
- When connected to a remote harness its commands run on the remote machine and the API key goes to the remote one — the safety confirmation prompt carries that disclosure duty.
- URL validation accepts any http(s) address (loopback is not required); non-loopback goes through the confirmation flow.

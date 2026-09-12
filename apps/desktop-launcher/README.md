# DeepSeek Harness Linux desktop launcher

English | [中文](README.zh.md)

`apps/desktop-launcher` is the desktop client for deepseek-harness. It is a Go launcher built with [Wails v2](https://wails.io): it spawns `dsh web` as a supervised child process and embeds the harness Web GUI in an iframe inside a thin launcher shell (status bar, server/settings dialogs, guide page). It is packaged as a Linglong bundle distributed on Deepin 25, plus Linux `.deb` and `.rpm`.

> Independent Go module, not part of the pnpm workspace. The launcher shell is a static HTML/CSS/JS page (no Node toolchain) embedded into the binary via `go:embed`.

## Architecture

```
┌─ Wails 窗口（webkit2gtk）───────────────────────────────────┐
│  launcher 壳（frontend/，静态 HTML/CSS/JS，go:embed）          │
│   ├── 自定义标题栏(无边框)：品牌 + 图标动作 + 窗口控制        │
│   ├── 舞台：<iframe src=http://127.0.0.1:<port>> 嵌 harness UI │
│   │         （未运行/未连接时显示内置引导页）                   │
│   └── 底部状态栏：● 运行中 http://127.0.0.1:<port>             │
└───────────────▲───────────────────────────────────────────┘
                │ window.go.app.App.*（Wails 绑定）+ 1s 状态事件
┌───────────────┴───────────────────────────────────────────┐
│ internal/app —— 绑定层：状态快照、控制方法、事件推送          │
│   supervisor  监护 harness 子进程（spawn/杀进程组/退避重启）  │
│   connector   外部服务连接状态机（探测/确认记忆/持久化）       │
│   toolchain   工具链自检 + 按需安装                          │
│   appenv      环境解析（bin/端口/日志目录/子进程环境变量）     │
│   packaging   打包态路径、版本、webkit 平台适配               │
│   domain      共享领域模型（纯类型）                          │
└───────────────────────────────────────────────────────────┘
```

Layering rules: `domain` has zero dependencies; `supervisor`/`connector`/`toolchain`/`appenv`/`packaging` are pure Go (stdlib only) and unit-testable; `app` orchestrates them and talks to the frontend; `main` only assembles.

The rendering tier is Chromium/WebKit loading the loopback origin served by `dsh web`, fully reusing the existing Web GUI without rewriting any UI. Because the harness page now lives in an iframe with a real `http://127.0.0.1` origin, the legacy opaque-`location.origin` webkit quirk no longer applies.

Harness has exactly one lifecycle owner: the supervisor. `appenv` writes a patch overlay into the launcher runtime directory on every spawn and passes it as `--patch`, setting `allowRestart: false` on the `dsh-market` row. Without it the plugin market's one-click restart relaunches harness from the same argv — hence the same stable `--port` — while the supervisor is restarting it too, so one of the two dies with `EADDRINUSE`; a market-side win additionally leaves a harness the launcher cannot see or manage. Launcher flags precede `--port`, because the `web` subcommand forwards everything after it verbatim. Plugin changes reload through the server dialog's restart (重启) button, `App.RestartServer`.

## File layout

```
main.go                 Wails 入口：环境 → 控制器 → wails.Run（内嵌 frontend）
frontend/               壳 UI：index.html / styles.css / app.js（无 Node 构建链）
internal/domain/        纯领域模型（HarnessStatus/ToolCheck/Mode）
internal/supervisor/    harness 进程监护（含 process_unix.go / process_windows.go）
internal/appenv/        环境解析（bin/端口/日志目录/子进程环境变量）
internal/connector/     外部服务连接（探测/校验/确认记忆/持久化）
internal/toolchain/     工具链自检 + 按需安装（tar.gz 校验解包）
internal/packaging/     打包态路径、版本、webkit 平台适配（webkit_linux.go）
linglong/               Linglong 构建清单 + 宿主预备脚本
icons/hicolor/*/apps/dsh-desktop.png   hicolor icon set (16–512 RGBA rounded)
icons/dsh-desktop.png   dev-mode fallback (256×256)
```

## Environment resolution (three-level fallback)

`appenv.Resolve()` resolves the child command by priority:

| Priority | Trigger | command | args |
|---|---|---|---|
| 1 | `DSH_DESKTOP_DSH_BIN` set | `$DSH_DESKTOP_DSH_BIN` | `web --port $PORT` |
| 2 | `$PREFIX/harness/lib/bin.js` exists (packaged) | `$PREFIX/node/bin/node` (bundled Node 24, falls back to PATH node when missing) | `$PREFIX/harness/lib/bin.js web --port $PORT` |
| 3 | `../cli/lib/bin.js` exists (development, relative to CWD) | `node` | `../cli/lib/bin.js web --port $PORT` |

Development infers the repo root from CWD rather than the executable path, because under `go run .` the binary lives in `/tmp/go-build...`. When none of the three levels hit, it falls back to `node bin.js web --port $PORT` (bin.js relative to CWD).

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `DSH_DESKTOP_DSH_BIN` | unset | Directly names the dsh bin path, skipping other resolution |
| `DSH_DESKTOP_PORT` | unset | By default reserves a free loopback port (reused across harness restarts so the GUI can reconnect); an explicit value is respected, `0` lets the system pick a free port |
| `DSH_DESKTOP_LOG_DIR` | `~/.cache/dsh-desktop` | Directory where `harness.log` is written |
| `DSH_DESKTOP_NODE` | unset | Overrides the node executable path |
| `DSH_DESKTOP_DMABUF_RENDERER` | unset | `1` keeps webkit2gtk's DMABUF accelerated compositing on a machine with the NVIDIA driver, where the launcher otherwise disables it (see Known issues) |

## Connecting to an external service

The server dialog supports two connection modes:

- **Container-native** (default): starts and supervises the bundled harness inside the Linglong container.
- **Local/remote service**: connect to an external harness service (a local `npx @deepseek-ai/dsh web` or any other reachable machine). Switching to external mode stops the container harness first; disconnecting restarts the container harness and navigates back.
- **Idle guide page**: while the container harness is stopped and no external service is connected, the stage shows the built-in guide page (start-in-container, local npx service, and remote connection walkthroughs) instead of a stale service page; it switches back to the matching address once the service is ready or connected.

Connecting to an external service requires the target harness to bind a reachable interface (`dsh web --host <LAN-IP>`; `--host 0.0.0.0` is deliberately rejected upstream, see its CLI hint), reachable on the LAN or via port forwarding/tunnel. The first connection to an external address (non-127.0.0.1/localhost) shows a native security confirmation; the last address is remembered in `~/.config/dsh-desktop/config.json`, auto-filled when the dialog opens, and never auto-reconnects.

## Building and running

### Development

```sh
# 1. 先构建 harness（生成 apps/cli/lib/bin.js 和前端 dist）
pnpm run build

# 2. 构建启动器（Wails；Linux 用 -tags "production webkit2_41" 显式选 webkit2gtk-4.1）
cd apps/desktop-launcher
make build          # 等价: go build -tags "production webkit2_41" -o dsh-desktop-launcher .

# 3. 运行（命中环境解析优先级 3）
./dsh-desktop-launcher
```

> The root `pnpm run dev:desktop` / `pnpm run build:desktop` still call the plain `go run`/`go build` (no `-tags "production webkit2_41"`); for the Wails stack use `make build` or `linglong/prepare-offline.sh` instead. The root scripts are upstream-synced and intentionally left untouched.

### Testing

```sh
cd apps/desktop-launcher
go test ./...        # 单元 + mock 子进程集成测试
node --test frontend/test-app.cjs        # 前端 DOM 桩测试
node frontend/tools/preview.mjs verify   # 前端布局不变量（无头 Chromium）
DSH_TC_E2E=1 go test ./internal/toolchain -run TestE2E_CatalogInstall   # 市场清单审计（需外网）
```

The frontend has no build step: `index.html`, `styles.css`, and `app.js` are embedded as-is. `test-app.cjs` runs `app.js` against a hand-written DOM stub, so it observes the behavior those files produce but not the layout. `frontend/tools/preview.mjs` covers what the stub cannot: it renders `index.html` in headless Chromium, replays each dialog state the way `app.js` writes it, and asserts the layout invariants — the card keeps one height across connection modes and running states, the address box keeps its two-line reservation, and the service-address field stays within its cap. It also opens the toolchain market and checks, with cards whose meta rows carry a long command list, that the grid does not overflow horizontally and that a row's cards stay equal in width (`1fr` fails here: the cards' min-content width pushes the tracks past the container). `render` writes one screenshot per theme and state into `frontend/.preview`; `measure` prints the raw geometry instead. Chromium's profile and `HOME`/XDG directories live in `apps/desktop-launcher/.preview-cache`, created per run and deleted when it finishes: `//go:embed all:frontend` embeds the whole frontend directory regardless of `.gitignore`, so a browser cache written there makes the launcher build fail on filenames Go's embed rules reject. The browser comes from `DSH_PREVIEW_BROWSER`, otherwise the Playwright cache, otherwise `PATH`; with none available the tool states why and exits without failing, so a machine without a browser can still push.

The market catalog has a comparable opt-in audit: `DSH_TC_E2E=1 go test ./internal/toolchain -run TestE2E_CatalogInstall` skips by default and, when enabled, installs each catalog tool for real to verify the URL resolves, the archive sha256 matches the catalog, the extracted layout matches the `bin_rel`/`bin_names` declaration, and every declared command is actually exposed under `bin/`. Mirror sites rotate versions (Apache dlcdn keeps only the current release, so older pinned URLs 404 silently), and that rot surfaces only under such an audit or when a user clicks install; `DSH_TC_E2E_IDS` narrows the run by ID.

## Linglong packaging

**One-click scripts**: `build-linglong.sh` (Linglong `.uab`, assembled in a container) and `build-deb.sh` (a Linux `.deb` installed to `/opt/apps/<id>/files`, webkit uses the system build) both run from the repo root; by default both fully rerun `prepare-offline.sh`, and adding `--no-prepare` reuses the existing `stage/` to only repackage.

**Two-step assemble build**: heavy toolchains (pnpm/tsc/tsdown/go) all run on the host; the container only copies and assembles. This avoids the build container's environment problems (Debian npm proxy bug, no HOME, no Node 22 on beige, tsdown failing to load config under Node 22), and the container no longer touches the repo's node_modules.

```sh
# 1. 宿主机构建全部产物（lib + web + dsh 闭包 + Go 启动器 -> stage/）
#    源码改动后需重新运行
sh apps/desktop-launcher/linglong/prepare-offline.sh

# 2. 玲珑组装打包（仓库根运行，秒级）
ll-builder build -f apps/desktop-launcher/linglong/linglong.yaml
ll-builder export --ref main:com.deepseek.dsh-desktop/0.1.0.9/x86_64
```

Packaging notes:

- `base: org.deepin.base/25.2.2` (3-segment fuzzy match resolves the stable repository's 25.2.2.6; base rejects a full 4-segment version)
- Runtime dependencies (webkit2gtk-4.1/gtk3/libsoup3) are pulled from beige via `buildext.apt.depends`; `git` is also shipped through `buildext.apt.depends` (harness runs containerized and the bash toolchain executes in the caplet, so repository git operations depend on it and the base runtime does not include git). They are merged into `${PREFIX}/bin` (on the container PATH) and `${PREFIX}/lib` (in the ld search path). Node 24.9.0 is downloaded to `stage/node` (npmmirror) by prepare-offline and assembled into `${PREFIX}/node` by linglong.yaml. harness needs Node >=24; beige's Debian nodejs 20 externalizes cjs-module-lexer to the absolute path `/usr/share/nodejs/`, which does not exist in the sandbox and crashes at startup, so bundling is required
- The closure fix (`scripts/fix-deploy-closure.mjs`) runs in the host prepare phase (peer deps, materialized symlinks, legacy hoists)
- The Go launcher is built with Wails using build tags `production webkit2_41` (`production` enables the real Wails runtime, `webkit2_41` selects webkit2gtk-4.1; the old webkit2gtk-4.0 pkg-config shim is no longer needed)
- The sandbox does not authorize the user's project directory by default: mount rules take effect only after you copy the templates (`linglong/config.d/*.json`) to `~/.config/linglong/apps/com.deepseek.dsh-desktop/config.d/` and edit paths. The toolchain dialog's host-path mount writes the same per-app drop-in itself (read-only rbind, effective after restart); sources outside the home directory are unreliable on some systems, so prefer on-demand install or home-directory paths.
- The Linglong package version is extracted from linglong.yaml by prepare-offline and injected into the launcher (`-ldflags -X github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging.Version=...`), shown in the about dialog

External links cannot open through WebKit's new-window path in the Wails webview (`target="_blank"` does nothing), and the base runtime's `xdg-open` is a broken forwarding stub, so the package bundles the real xdg-utils and every handoff ends in the Wails runtime `BrowserOpenURL` (xdg-open → host portal → the machine's default browser). Links inside the embedded harness GUI are covered because the launcher cannot observe clicks in the cross-origin iframe: `prepare-offline.sh` injects `linglong/dsh-link-bridge.js` into the packaged GUI dist (`inject-link-bridge.sh`), the bridge hands each `target="_blank"` HTTP(S) click to the shell via `postMessage`, and `frontend/app.js` opens it. This covers the container mode only — an external harness (`dsh web` run elsewhere) serves an uninjected GUI, so its links still do nothing. The about dialog's repository link is wired the same way.

## Container usability (toolchain/mounts)

- Self-contained toolchain: `buildext.apt.depends` ships git/python3/curl/wget/unzip/zip/jq/xxd/ca-certificates/xdg-utils; the manifest and verification live in `linglong/tools.yaml` and `verify-tools.sh` (host-side verification of the merged product tree before export). The official `dsh` CLI (`harness/lib/bin.js`) is exposed on the container PATH through a thin `$PREFIX/bin/dsh` wrapper alongside the bundled node/pnpm, so `dsh plugin` and every other subcommand work inside the sandbox (including shells spawned by node-pty).
- On-demand install: heavy/rare tools (jdk21, go, ripgrep, uv) install to `$HOME/.dsh-tools` (in the container, on host disk, preserved across uninstall by default) after sha256 verification, and the launcher injects PATH/LD_LIBRARY_PATH automatically; the self-check panel shows the installable list. The whitelist is `linglong/tools.yaml`'s `installable`, kept in sync with the runtime catalog (`internal/toolchain/catalog.go`); `verify-tools.sh` fails the build on placeholder hashes. When an archive's file names differ from the commands they should expose, the catalog's `bin_names` renames them, or suppresses them with an empty value — keeping platform-suffixed names and bundled helper scripts out of PATH. The market's category tabs and their Chinese labels also come from the index: tabs are built from the categories present in the catalog, and labels come from the index's `category_labels` (the client keeps a matching fallback table for older indexes), so adding or removing tools and introducing a category needs no client release as long as the tool relies on no new field or archive format.
- Card state semantics: the market dialog's "installed/installable" only describes version directories in the market store (`$HOME/.dsh-tools`), independently of command availability inside the container. When a tool is not in the store but the container PATH already provides a command it declares (bundled / host-imported / system), the card shows "容器内已可用：command version (source)"; the source is classified by the resolved binary path prefix (`classifyRuntimeSource` in `internal/app/app.go`), with probing and assembly in `internal/toolchain/check.go` (`ProbeCommands`) and `annotateRuntime`.
- Proxy: linyaps forwards the host's `http_proxy/https_proxy/all_proxy` by default; the company's private CA is appended to the container's writable area and `update-ca-certificates` is run.

## Clipboard image bridging

The packaged shell renders the harness UI inside a WebKitGTK iframe. Unlike
Chromium, WebKitGTK never surfaces clipboard bitmaps to the page: pasting a
screenshot produces no `clipboardData` image item, and dragging an image file
navigates the frame. Text (including pasted paths) works; images do not.

The workaround keeps the WebKitGTK renderer: the shell process — which shares
the host X11 display — reads the `CLIPBOARD` selection itself and hands the
page a base64 PNG over the existing `{ dshDesktop: true }` postMessage protocol.

- Go: `internal/clipboard` is a zero-dependency X11 wire client
  (no cgo/no external tools) that reads `image/png` with INCR support,
  timeouts and payload caps; `App.ReadClipboardImage()` (Wails binding)
  returns base64 or `""`.
- Shell frontend (`frontend/app.js`): answers `clipboard-read-image`
  messages from the harness iframe with `clipboard-image-result`.
- Harness UI (`packages/client/ui-conversation`): `desktop-clipboard.ts`
  wraps the request; `InputBar` asks the shell on paste when no image item
  arrived and shows a "paste image from clipboard" toolbar button. Both only
  activate inside the shell iframe — a plain browser tab keeps the native
  Chromium path untouched.

## Known issues

- **Shared `~/.dsh` across harness versions**: an external harness (e.g. `npx @deepseek-ai/dsh web`, a published release) shares the same `~/.dsh` home as the launcher's bundled harness. A different version may write `~/.dsh/.credentials.yaml` in a schema this version rejects (a `version` key whose value is not a string), crashing the harness at boot into a restart loop. If the harness enters a restart loop after using an external harness, check `~/.cache/dsh-desktop/harness.log` for `credentials-local` errors; back up and remove `~/.dsh/.credentials.yaml` so the harness rebuilds an empty store (stored credentials are lost).
- **WebKitGTK DMABUF compositing on NVIDIA**: when the kernel has the NVIDIA proprietary driver loaded, WebKitGTK's default DMABUF accelerated compositing can fail to build a framebuffer as a window is re-exposed after being occluded, painting the whole web area a solid color for a moment (white under a light theme, black under a dark one) before it recovers. The failure is intermittent, and the harness process and any running task are unaffected. The launcher therefore sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` whenever `/sys/module/nvidia` exists (`packaging.ConfigureWebKitRendering`, called before `wails.Run`), giving up the zero-copy compositing path on those machines only; every other machine keeps the default. Setting `DSH_DESKTOP_DMABUF_RENDERER=1` opts back into the DMABUF path on such a machine, for a machine that matches the driver condition but was never affected.
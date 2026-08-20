# DeepSeek Harness Linux desktop launcher

English | [中文](README.zh.md)

`apps/desktop-launcher` is the Linux desktop client for deepseek-harness. It is a thin Go launcher: it spawns `dsh web` as a child process and, once ready, opens an independent window with the system webkit2gtk that loads its loopback Web GUI. It is packaged as a Linglong bundle distributed on Deepin 25.

> Independent Go module, not part of the pnpm workspace. Linux only.

## Architecture

```
用户点击应用图标
   │
   ▼
dsh-desktop-launcher (Go 二进制)
   │  1. resolveDesktopEnv() 解析子进程命令
   │  2. NewSupervisor 起监护循环并 spawn `dsh web`(Start() 仅手动停止后恢复)
   │     ├── 逐行扫 stdout，等就绪行: "dsh web: http://127.0.0.1:<port>"
   │     ├── 崩溃后指数退避重启 (500ms→10s)
   │     └── 优雅停止 (SIGTERM → 5s → SIGKILL，按进程组广播)
   │  3. 就绪后 webkit2gtk 开独立窗口加载 loopback URL
   │  4. 窗口关闭 → sup.Stop() 停止子进程
   ▼
dsh web 子进程（node + apps/cli/lib/bin.js web）
   └── Cordis web 服务器 + React Web GUI
```

The rendering tier is only Chromium/WebKit loading the loopback origin served by `dsh web`, fully reusing the existing Web GUI without rewriting any UI. Configuration such as the API key and workspace is inherited from the launcher's child environment and parsed by harness's own config resolution.

## File layout

| File | Purpose |
|---|---|
| `main.go` | Entry: resolve environment → start supervisor → open window → block on the event loop |
| `env.go` | `resolveDesktopEnv()`: three-level resolution of the child command/arguments |
| `supervisor.go` | `HarnessSupervisor`: spawn, readiness detection, exponential-backoff restart, graceful stop |
| `window.go` | `webview/webview_go` wrapper: Navigate, title, size, close callback |
| `ui.go` | Bottom status bar, server-status/about dialogs, window centering (GTK cgo) |
| `connection.go` | External-service connection: probe, URL validation, persistence, connection state (pure Go) |
| `version.go` | harness/Linglong version parsing (`packageVersion` injected by prepare-offline) |
| `linglong/linglong.yaml` | Linglong build manifest |
| `linglong/tools.yaml` | Container tool manifest (binary/verify/on-demand whitelist) |
| `linglong/config.d/10-mounts.json` | Filesystem mount config template (reference for users) |
| `linglong/prepare-pkgconfig.sh` | Generates the webkit2gtk-4.0.pc shim (pointing at 4.1) |
| `icons/dsh-desktop.png` | Application icon |
| `testdata/mock-dsh-web.sh` | Mock child process for integration tests |

## Environment resolution (three-level fallback)

`resolveDesktopEnv()` resolves the child command by priority:

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

## Connecting to an external service

The server status dialog supports two connection modes:

- **Container-native** (default): starts and supervises the bundled harness inside the Linglong container.
- **Local/remote service**: connect to an external harness service (a local `npx @deepseek-ai/dsh web` or any other reachable machine). Switching to external mode stops the container harness first; disconnecting restarts the container harness and navigates back.
- **Idle guide page**: while the container harness is stopped and no external service is connected, the main view shows the built-in guide page (start-in-container, local npx service, and remote connection walkthroughs) instead of a stale service page; it switches back to the matching address once the service is ready or connected.

Connecting to an external service requires the target harness to bind a reachable interface (`dsh web --host <LAN-IP>`; `--host 0.0.0.0` is deliberately rejected upstream, see its CLI hint), reachable on the LAN or via port forwarding/tunnel. The first connection to an external address (non-127.0.0.1/localhost) shows a security confirmation; the last address is remembered in `~/.config/dsh-desktop/config.json`, auto-filled when the dialog opens, and never auto-reconnects.

## Building and running

### Development

```sh
# 1. 先构建 harness（生成 apps/cli/lib/bin.js 和前端 dist）
pnpm run build
pnpm run build:web

# 2. 构建启动器（webkit2gtk-4.0 shim：webview_go 编译期硬编码 4.0，
#    deepin 25 只有 4.1，需先用 linglong/prepare-pkgconfig.sh 生成 shim）
cd apps/desktop-launcher
sh linglong/prepare-pkgconfig.sh /tmp/dsh-pkgconfig
PKG_CONFIG_PATH=/tmp/dsh-pkgconfig go build -o dsh-desktop-launcher .

# 3. 运行（命中环境解析优先级 3）
./dsh-desktop-launcher
```

From the repo root you can also use `pnpm run dev:desktop` / `pnpm run build:desktop` (delegating to `go run` / `go build`).

### Testing

```sh
go test -v ./...        # 单元 + mock 子进程集成测试
```

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
- Runtime dependencies (webkit2gtk-4.1/gtk3) are pulled from beige via `buildext.apt.depends`; `git` is also shipped through `buildext.apt.depends` (harness runs containerized and the bash toolchain executes in the caplet, so repository git operations depend on it and the base runtime does not include git). They are merged into `${PREFIX}/bin` (on the container PATH) and `${PREFIX}/lib` (in the ld search path). Node 24.9.0 is downloaded to `stage/node` (npmmirror) by prepare-offline and assembled into `${PREFIX}/node` by linglong.yaml (downloaded from the same source when missing in the container). harness needs Node >=24; also, beige's Debian nodejs 20 externalizes cjs-module-lexer to the absolute path `/usr/share/nodejs/`, which does not exist in the sandbox and crashes at startup, so bundling is required
- The closure fix (`scripts/fix-deploy-closure.mjs`) runs in the host prepare phase (peer deps, materialized symlinks, legacy hoists)
- Building the Go launcher on the host first requires `linglong/prepare-pkgconfig.sh` to generate the webkit2gtk-4.0 shim (webview_go hardcodes 4.0 at compile time; deepin 25 only has 4.1)
- The sandbox does not authorize the user's project directory by default; the user must configure a mount: copy `linglong/config.d/10-mounts.json` to `~/.config/linglong/apps/com.deepseek.dsh-desktop/config.d/` and edit the path
- The Linglong package version is extracted from linglong.yaml by prepare-offline and injected into the launcher (`-ldflags -X main.packageVersion=...`), shown in the about dialog

## Container usability (toolchain/credentials/mounts)

- Self-contained toolchain: `buildext.apt.depends` ships git/python3/curl/wget/unzip/zip/jq/xxd/ca-certificates; the manifest and verification live in `linglong/tools.yaml` and `verify-tools.sh` (host-side verification of the merged product tree before export).
- On-demand install: heavy/rare tools (go, ripgrep) install to `$HOME/.dsh-tools` (in the container, on host disk, preserved across uninstall by default), and the launcher injects PATH/LD_LIBRARY_PATH automatically; the self-check panel shows the installable list and install directory, and the whitelist is `linglong/tools.yaml`'s `installable` (installable only once url/sha256 are filled in).
- git credentials: the GUI "设置 → Git 凭据" area writes `~/.git-credentials` (container HOME = host home directory; `ll-cli uninstall` does not clear user data); optionally bind-mount the host's same-named file read-only (template at `linglong/config.d/20-host-credentials.json`, copied to `~/.config/linglong/apps/com.deepseek.dsh-desktop/config.d/` and edit `<USER>`).
- Proxy: linyaps forwards the host's `http_proxy/https_proxy/all_proxy` by default; the company's private CA is appended to the container's writable area and `update-ca-certificates` is run.

## Known webkit2gtk compatibility

- **`location.origin` may return `"null"`** (opaque origin). harness's `resolveBase()` is fixed: when origin is `"null"` it extracts the real origin from `location.href` (`packages/client/connection/src/client/rpc.ts` and `packages/host/apiproxy/src/fetch/client.ts`). After changes rebuild with `pnpm run build:lib:host`, `pnpm run build:lib:client`, `pnpm run build:web`.
- **Modern CSS features**: Deepin 25's webkit2gtk 2.50.4 natively supports `color-mix()`/`:has()`/`@container`. Older distros (e.g. Ubuntu 22.04's 2.36) need CSS `@supports` gating hardening.
- **`AbortSignal.any()`**: supported from webkit2gtk 2.44+. Fine on 2.50.4 locally; on older distros `postJson()` in `packages/host/apiproxy/src/fetch/client.ts` errors and needs a feature-detect fallback.

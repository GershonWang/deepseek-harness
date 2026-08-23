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
│   gitcred     git store 凭据读写（~/.git-credentials）       │
│   appenv      环境解析（bin/端口/日志目录/子进程环境变量）     │
│   packaging   打包态路径、版本、webkit helper 打点            │
│   domain      共享领域模型（纯类型）                          │
└───────────────────────────────────────────────────────────┘
```

Layering rules: `domain` has zero dependencies; `supervisor`/`connector`/`toolchain`/`gitcred`/`appenv`/`packaging` are pure Go (stdlib only) and unit-testable; `app` orchestrates them and talks to the frontend; `main` only assembles.

The rendering tier is Chromium/WebKit loading the loopback origin served by `dsh web`, fully reusing the existing Web GUI without rewriting any UI. Because the harness page now lives in an iframe with a real `http://127.0.0.1` origin, the legacy opaque-`location.origin` webkit quirk no longer applies.

## File layout

```
main.go                 Wails 入口：环境 → 控制器 → wails.Run（内嵌 frontend）
frontend/               壳 UI：index.html / styles.css / app.js（无 Node 构建链）
internal/domain/        纯领域模型（HarnessStatus/ToolCheck/CredentialInfo/Mode）
internal/supervisor/    harness 进程监护（含 process_unix.go / process_windows.go）
internal/appenv/        环境解析（bin/端口/日志目录/子进程环境变量）
internal/connector/     外部服务连接（探测/校验/确认记忆/持久化）
internal/toolchain/     工具链自检 + 按需安装（tar.gz 校验解包）
internal/gitcred/       git store 凭据读写
internal/packaging/     打包态路径、版本、webkit helper 打点（webkit_linux.go）
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
- Runtime dependencies (webkit2gtk-4.1/gtk3/libsoup3) are pulled from beige via `buildext.apt.depends`; `git` is also shipped through `buildext.apt.depends` (harness runs containerized and the bash toolchain executes in the caplet, so repository git operations depend on it and the base runtime does not include git). They are merged into `${PREFIX}/bin` (on the container PATH) and `${PREFIX}/lib` (in the ld search path). Node 24.9.0 is downloaded to `stage/node` (npmmirror) by prepare-offline and assembled into `${PREFIX}/node` by linglong.yaml. harness needs Node >=24; beige's Debian nodejs 20 externalizes cjs-module-lexer to the absolute path `/usr/share/nodejs/`, which does not exist in the sandbox and crashes at startup, so bundling is required
- The closure fix (`scripts/fix-deploy-closure.mjs`) runs in the host prepare phase (peer deps, materialized symlinks, legacy hoists)
- The Go launcher is built with Wails using build tags `production webkit2_41` (`production` enables the real Wails runtime, `webkit2_41` selects webkit2gtk-4.1; the old webkit2gtk-4.0 pkg-config shim is no longer needed)
- The sandbox does not authorize the user's project directory by default; the user must configure a mount: copy `linglong/config.d/10-mounts.json` to `~/.config/linglong/apps/com.deepseek.dsh-desktop/config.d/` and edit the path
- The Linglong package version is extracted from linglong.yaml by prepare-offline and injected into the launcher (`-ldflags -X github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging.Version=...`), shown in the about dialog

## Container usability (toolchain/credentials/mounts)

- Self-contained toolchain: `buildext.apt.depends` ships git/python3/curl/wget/unzip/zip/jq/xxd/ca-certificates; the manifest and verification live in `linglong/tools.yaml` and `verify-tools.sh` (host-side verification of the merged product tree before export).
- On-demand install: heavy/rare tools (go, ripgrep) install to `$HOME/.dsh-tools` (in the container, on host disk, preserved across uninstall by default), and the launcher injects PATH/LD_LIBRARY_PATH automatically; the self-check panel shows the installable list and install directory, and the whitelist is `linglong/tools.yaml`'s `installable` (installable only once url/sha256 are filled in).
- git credentials: the GUI "设置 → Git 凭据" area writes `~/.git-credentials` (container HOME = host home directory; `ll-cli uninstall` does not clear user data); optionally bind-mount the host's same-named file read-only (template at `linglong/config.d/20-host-credentials.json`, copied to `~/.config/linglong/apps/com.deepseek.dsh-desktop/config.d/` and edit `<USER>`).
- Proxy: linyaps forwards the host's `http_proxy/https_proxy/all_proxy` by default; the company's private CA is appended to the container's writable area and `update-ca-certificates` is run.

## Known issues

- **Shared `~/.dsh` across harness versions**: an external harness (e.g. `npx @deepseek-ai/dsh web`, a published release) shares the same `~/.dsh` home as the launcher's bundled harness. A different version may write `~/.dsh/.credentials.yaml` in a schema this version rejects (a `version` key whose value is not a string), crashing the harness at boot into a restart loop. If the harness enters a restart loop after using an external harness, check `~/.cache/dsh-desktop/harness.log` for `credentials-local` errors; back up and remove `~/.dsh/.credentials.yaml` so the harness rebuilds an empty store (stored credentials are lost).
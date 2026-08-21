# DeepSeek Harness Linux 桌面启动器

[English](README.md) | 中文

`apps/desktop-launcher` 是 deepseek-harness 的桌面客户端。它基于 [Wails v2](https://wails.io) 用 Go 编写：spawn `dsh web` 子进程并监护，把 harness Web GUI 以 iframe 嵌进一层薄的启动器壳（状态栏、服务器/设置弹框、引导页）。最终打成如意玲珑（Linglong）包在 Deepin 25 上分发，另支持 Linux `.deb` 与 `.rpm`。

> 独立 Go module，不纳入 pnpm workspace。壳 UI 是静态 HTML/CSS/JS（无 Node 构建链），经 `go:embed` 打进二进制。

## 架构

```
┌─ Wails 窗口（webkit2gtk）───────────────────────────────────┐
│  launcher 壳（frontend/，静态 HTML/CSS/JS，go:embed）          │
│   ├── 顶栏：状态 + 服务器/设置/关于                            │
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

分层规则：`domain` 零依赖；`supervisor`/`connector`/`toolchain`/`gitcred`/`appenv`/`packaging` 为纯 Go（仅标准库）、可单测；`app` 编排它们并面向前端；`main` 只做组装。

渲染层只是 Chromium/WebKit 加载 `dsh web` 服务的 loopback origin，完全复用现有 Web GUI，不重写任何 UI。由于 harness 页面现在以 iframe 方式加载、拥有真实的 `http://127.0.0.1` origin，旧的 opaque `location.origin` webkit 兼容问题不再适用。

## 文件结构

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
icons/dsh-desktop.png   应用图标
```

## 环境解析（三级回退）

`appenv.Resolve()` 按优先级解析子进程命令：

| 优先级 | 触发条件 | command | args |
|---|---|---|---|
| 1 | `DSH_DESKTOP_DSH_BIN` 已设 | `$DSH_DESKTOP_DSH_BIN` | `web --port $PORT` |
| 2 | `$PREFIX/harness/lib/bin.js` 存在（打包态） | `$PREFIX/node/bin/node`（捆绑 Node 24，缺失时回退 PATH node） | `$PREFIX/harness/lib/bin.js web --port $PORT` |
| 3 | `../cli/lib/bin.js` 存在（开发态，相对 CWD） | `node` | `../cli/lib/bin.js web --port $PORT` |

开发态用 CWD 而非可执行文件路径推算 repo 根，因为 `go run .` 时二进制在 `/tmp/go-build...`。三级均未命中时回退为 `node bin.js web --port $PORT`（bin.js 相对 CWD）。

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `DSH_DESKTOP_DSH_BIN` | 未设 | 直接指定 dsh bin 路径，跳过其他解析 |
| `DSH_DESKTOP_PORT` | 未设 | 默认保留一个空闲 loopback 端口（harness 重启复用，GUI 可重连）；显式指定则尊重，`0` 让系统选空闲端口 |
| `DSH_DESKTOP_LOG_DIR` | `~/.cache/dsh-desktop` | `harness.log` 写入目录 |
| `DSH_DESKTOP_NODE` | 未设 | 覆盖 node 可执行文件路径 |

## 连接外部服务

服务器弹框支持两种连接模式：

- **容器内**（默认）：启动并监护玲珑容器内捆绑的 harness。
- **本机/远端服务**：连接外部 harness 服务（本机 `npx @deepseek-ai/dsh web` 或网络可达的其他机器）。切到外部模式会先停止容器内 harness；断开后自动重启容器 harness 并导航回。
- **空闲引导页**：容器内 harness 停止且外部服务未连接时，舞台默认显示内置引导页（容器内启动、本机 npx 服务、远端连接三种方案的开箱步骤），不再停留在已失效的服务页面；服务就绪或连接成功后自动切回对应地址。

连接外部服务的前提：目标 harness 需绑定可访问的接口（`dsh web --host <LAN-IP>`；`--host 0.0.0.0` 被上游有意拒绝，原因见其 CLI 提示），局域网可达或经端口转发/隧道。外部地址（非 127.0.0.1/localhost）首次连接会弹原生安全确认；上次地址记忆在 `~/.config/dsh-desktop/config.json`，打开弹框自动填充、不自动重连。

## 构建与运行

### 开发态

```sh
# 1. 先构建 harness（生成 apps/cli/lib/bin.js 和前端 dist）
pnpm run build

# 2. 构建启动器（Wails；Linux 用 -tags "production webkit2_41" 显式选 webkit2gtk-4.1）
cd apps/desktop-launcher
make build          # 等价: go build -tags "production webkit2_41" -o dsh-desktop-launcher .

# 3. 运行（命中环境解析优先级 3）
./dsh-desktop-launcher
```

> 仓库根的 `pnpm run dev:desktop` / `pnpm run build:desktop` 仍调用裸 `go run`/`go build`（不带 `-tags "production webkit2_41"`）；Wails 栈请用 `make build` 或 `linglong/prepare-offline.sh`。根脚本与上游同步，刻意不动。

### 测试

```sh
cd apps/desktop-launcher
go test ./...        # 单元 + mock 子进程集成测试
```

## 玲珑打包

**一键脚本**：`build-linglong.sh`（玲珑 .uab，经容器组装）与 `build-deb.sh`（linux .deb，安装到 `/opt/apps/<id>/files`、webkit 用系统版）都在仓库根直接运行；默认全量重跑 `prepare-offline.sh`，加 `--no-prepare` 可复用现有 `stage/` 只重打包。

**组装式两步构建**：重工具链（pnpm/tsc/tsdown/go）全部在宿主机跑，容器只复制组装。规避了构建容器的环境问题（Debian npm 代理 bug、无 HOME、beige 无 Node 22、tsdown 在 Node 22 下加载配置失败），且容器不再碰仓库的 node_modules。

```sh
# 1. 宿主机构建全部产物（lib + web + dsh 闭包 + Go 启动器 -> stage/）
#    源码改动后需重新运行
sh apps/desktop-launcher/linglong/prepare-offline.sh

# 2. 玲珑组装打包（仓库根运行，秒级）
ll-builder build -f apps/desktop-launcher/linglong/linglong.yaml
ll-builder export --ref main:com.deepseek.dsh-desktop/0.1.0.9/x86_64
```

打包要点：

- `base: org.deepin.base/25.2.2`（3 段式模糊匹配 stable 仓库的 25.2.2.6；base 不接受 4 段完整版本号）
- 运行时依赖（webkit2gtk-4.1/gtk3/libsoup3）由 `buildext.apt.depends` 从 beige 拉入；`git` 也经 `buildext.apt.depends` 带入（harness 容器化运行、bash 工具链在胶囊内执行，仓库 git 操作依赖它，基础运行时不含 git）。合并进 `${PREFIX}/bin`（在容器 PATH 上）与 `${PREFIX}/lib`（在 ld 搜索目录）。Node 24.9.0 由 prepare-offline 下载到 `stage/node`（npmmirror），linglong.yaml 组装进 `${PREFIX}/node`。harness 需要 Node >=24；且 beige 的 Debian 版 nodejs 20 把 cjs-module-lexer 外部化到绝对路径 `/usr/share/nodejs/`，沙箱内不存在导致启动即崩，故必须捆绑
- 闭包修复（`scripts/fix-deploy-closure.mjs`）在宿主机 prepare 阶段执行（peer deps、符号链接实体化、legacy hoists）
- Go 启动器用 Wails 构建，须带 `-tags "production webkit2_41"`（wails 在该标签下选用 webkit2gtk-4.1）；旧的 webkit2gtk-4.0 pkg-config shim 不再需要
- 沙箱默认不授权用户项目目录，用户需配置挂载：复制 `linglong/config.d/10-mounts.json` 到 `~/.config/linglong/apps/com.deepseek.dsh-desktop/config.d/` 并改路径
- 玲珑包版本由 prepare-offline 从 linglong.yaml 提取并注入 launcher（`-ldflags -X github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging.Version=...`），关于弹框展示

## 容器可用性（工具链/凭据/挂载）

- 工具链自包含：`buildext.apt.depends` 随包带入 git/python3/curl/wget/unzip/zip/jq/xxd/ca-certificates；清单与校验见 `linglong/tools.yaml` 与 `verify-tools.sh`（宿主侧在 export 前校验合并产物树）。
- 按需安装：重/罕见工具（go、ripgrep）装到 `$HOME/.dsh-tools`（容器内、宿主磁盘、卸载默认保留），launcher 自动注入 PATH/LD_LIBRARY_PATH；自检面板展示可安装清单与安装目录，白名单见 `linglong/tools.yaml` 的 `installable`（url/sha256 填实后才可安装）。
- git 凭据：GUI「设置 → Git 凭据」区写入 `~/.git-credentials`（容器 HOME = 宿主主目录；`ll-cli uninstall` 不清理用户数据）；可选只读挂载宿主同名文件（模板见 `linglong/config.d/20-host-credentials.json`，复制到 `~/.config/linglong/apps/com.deepseek.dsh-desktop/config.d/` 并改 `<USER>`）。
- 代理：linyaps 默认转发宿主 `http_proxy/https_proxy/all_proxy`；公司私有 CA 追加到容器可写区并 `update-ca-certificates`。
# DeepSeek Harness Linux 桌面启动器

`apps/desktop-launcher` 是 deepseek-harness 的 Linux 桌面客户端。它是一个 Go 编写的薄启动器：spawn `dsh web` 子进程，检测就绪后，用系统 webkit2gtk 打开独立窗口加载其 loopback Web GUI。最终打成如意玲珑（Linglong）包在 Deepin 25 上分发。

> 独立 Go module，不纳入 pnpm workspace。仅支持 Linux。

## 架构

```
用户点击应用图标
   │
   ▼
dsh-desktop-launcher (Go 二进制)
   │  1. resolveDesktopEnv() 解析子进程命令
   │  2. HarnessSupervisor.Start() —— spawn `dsh web` 子进程
   │     ├── 逐行扫 stdout，等就绪行: "dsh web: http://127.0.0.1:<port>"
   │     ├── 崩溃后指数退避重启 (500ms→10s)
   │     └── 优雅停止 (SIGTERM → 5s → SIGKILL，按进程组广播)
   │  3. 就绪后 webkit2gtk 开独立窗口加载 loopback URL
   │  4. 窗口关闭 → sup.Stop() 停止子进程
   ▼
dsh web 子进程（node + apps/cli/lib/bin.js web）
   └── Cordis web 服务器 + React Web GUI
```

渲染层只是 Chromium/WebKit 加载 `dsh web` 服务的 loopback origin，完全复用现有 Web GUI，不重写任何 UI。API key / workspace 等配置继承启动器子进程环境，由 harness 自己的配置解析。

## 文件结构

| 文件 | 职责 |
|---|---|
| `main.go` | 入口：解析环境 → 起监护器 → 开窗口 → 阻塞事件循环 |
| `env.go` | `resolveDesktopEnv()`：三级解析子进程命令/参数 |
| `supervisor.go` | `HarnessSupervisor`：spawn、就绪检测、退避重启、优雅停止 |
| `window.go` | `webview/webview_go` 封装：Navigate、标题、尺寸、关闭回调 |
| `linglong/linglong.yaml` | 玲珑构建清单 |
| `linglong/config.d/10-mounts.json` | 文件系统挂载配置模板（用户参考） |
| `linglong/prepare-pkgconfig.sh` | 生成 webkit2gtk-4.0.pc shim（指向 4.1） |
| `icons/dsh-desktop.png` | 应用图标 |
| `testdata/mock-dsh-web.sh` | 集成测试用的 mock 子进程 |

## 环境解析（三级回退）

`resolveDesktopEnv()` 按优先级解析子进程命令：

| 优先级 | 触发条件 | command | args |
|---|---|---|---|
| 1 | `DSH_DESKTOP_DSH_BIN` 已设 | `$DSH_DESKTOP_DSH_BIN` | `web --port $PORT` |
| 2 | `$PREFIX/harness/lib/bin.js` 存在（打包态） | `node`（PATH 中的 beige nodejs） | `$PREFIX/harness/lib/bin.js web --port $PORT` |
| 3 | `../cli/lib/bin.js` 存在（开发态，相对 CWD） | `node` | `../cli/lib/bin.js web --port $PORT` |

开发态用 CWD 而非可执行文件路径推算 repo 根，因为 `go run .` 时二进制在 `/tmp/go-build...`。

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `DSH_DESKTOP_DSH_BIN` | 未设 | 直接指定 dsh bin 路径，跳过其他解析 |
| `DSH_DESKTOP_PORT` | `0` | 传给 `dsh web --port`，0 让系统选空闲端口 |
| `DSH_DESKTOP_LOG_DIR` | `~/.cache/dsh-desktop` | `harness.log` 写入目录 |
| `DSH_DESKTOP_NODE` | 未设 | 覆盖 node 可执行文件路径 |

## 构建与运行

### 开发态

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

从仓库根也可用 `pnpm run dev:desktop` / `pnpm run build:desktop`（委托给 `go run` / `go build`）。

### 测试

```sh
go test -v ./...        # 单元 + mock 子进程集成测试
```

## 玲珑打包

**组装式两步构建**：重工具链（pnpm/tsc/tsdown/go）全部在宿主机跑，容器只复制组装。规避了构建容器的环境问题（Debian npm 代理 bug、无 HOME、beige 无 Node 22、tsdown 在 Node 22 下加载配置失败），且容器不再碰仓库的 node_modules。

```sh
# 1. 宿主机构建全部产物（lib + web + dsh 闭包 + Go 启动器 -> stage/）
#    源码改动后需重新运行
sh apps/desktop-launcher/linglong/prepare-offline.sh

# 2. 玲珑组装打包（仓库根运行，秒级）
ll-builder build -f apps/desktop-launcher/linglong/linglong.yaml
ll-builder export --ref main:org.deepseek.dsh-desktop/0.1.0.0/x86_64
```

打包要点：

- `base: org.deepin.base/25.2.2`（3 段式模糊匹配 stable 仓库的 25.2.2.6；base 不接受 4 段完整版本号）
- 运行时依赖（webkit2gtk-4.1/gtk3/nodejs 20.15.1）由 `buildext.apt.depends` 从 beige 拉入；Node 20 足够运行 harness（已验证无 Node 22+ API）
- 闭包修复（`scripts/fix-deploy-closure.mjs`）在宿主机 prepare 阶段执行（peer deps、符号链接实体化、legacy hoists）
- 宿主机构建 Go 启动器需先运行 `linglong/prepare-pkgconfig.sh` 生成 webkit2gtk-4.0 shim（webview_go 编译期硬编码 4.0，deepin 25 只有 4.1）
- 沙箱默认不授权用户项目目录，用户需配置挂载：复制 `linglong/config.d/10-mounts.json` 到 `~/.config/linglong/apps/org.deepseek.dsh-desktop/config.d/` 并改路径

## 已知 webkit2gtk 兼容性

- **`location.origin` 可能返回 `"null"`**（opaque origin）。harness 的 `resolveBase()` 已修复：origin 为 `"null"` 时从 `location.href` 提取真实 origin（`packages/client/connection/src/client/rpc.ts` 和 `packages/host/apiproxy/src/fetch/client.ts`）。修改后需重建 `pnpm run build:lib:host`、`pnpm run build:lib:client`、`pnpm run build:web`。
- **CSS 现代特性**：Deepin 25 的 webkit2gtk 2.50.4 原生支持 `color-mix()`/`:has()`/`@container`。旧发行版（如 Ubuntu 22.04 的 2.36）需 CSS `@supports` 门控加固。
- **`AbortSignal.any()`**：webkit2gtk 2.44+ 才支持。本机 2.50.4 无碍；旧发行版在 `packages/host/apiproxy/src/fetch/client.ts` 的 `postJson()` 会报错，需 feature-detect 回退。

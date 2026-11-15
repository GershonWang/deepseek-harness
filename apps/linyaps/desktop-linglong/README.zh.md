# 以玲珑包分发的 DeepSeek Harness 桌面客户端（Electron）

[English](README.md) | 中文

`apps/linyaps/desktop-linglong` 把上游 Electron Desktop 客户端（[`apps/desktop`](../../desktop)）打包成面向 Deepin 25 的玲珑包。它沿用 [`apps/linyaps/desktop-launcher`](../desktop-launcher)（内嵌 `dsh web` 的 Go + Wails 薄壳）的应用 ID `com.deepseek.dsh-desktop`，因此是它的**升级**而不是并列安装：用户的桌面图标、启动器登记与数据目录都保持不变。

版本号必须高于薄壳版最后发布的 `0.1.7.2`，玲珑按版本号判定升级，低于已安装版本会被当作版本倒退拒绝。当前为 `0.2.0.1`。

## 本模块产出什么

一个 `.uab`，提供两个命令：`/opt/apps/com.deepseek.dsh-desktop/files/bin/dsh-desktop` 用 `--no-sandbox` 启动 Electron 应用；`/opt/apps/com.deepseek.dsh-desktop/files/bin/dsh` 是应用自带的 CLI，托盘「终端」和容器里的其它终端都能直接执行它——`$PREFIX/bin` 在容器 PATH 首位。

Electron Desktop 是自包含的：它自带 Electron 运行时，也自带 `resources/app.asar/dsh` 树，因此本包不再随包第二份 Node 运行时或 harness 树。Linux x64 上解包后的应用实测约 1.1 GB（Electron 227 MB、主运行时 496 MB、dsh 树 331 MB），与薄壳包同一量级：打包 Electron 应用并不会让负载变小。

## 为什么没有复用上游的打包入口

[`package-target.ts`](../../desktop/scripts/package-target.ts) 把目标类型绑在**发布目标**上（`DesktopPackageTargetName` 只有 mac-arm64、mac-x64、win-x64）：代码签名、公证、`electron-updater`、COS 上传。Linux 不是发布目标，因为分发由玲珑包自己负责。把 `linux-x64` 加进那个类型会牵动整条上传与更新类型链，实测一次性引发 12 处类型错误，因此 `prepare-offline.sh` 直接调用底层打包步骤，顺序与 `package-target.ts` 一致。

## 构建

先在宿主机准备产物，再在玲珑容器内组装。

```sh
sh apps/linyaps/desktop-linglong/prepare-offline.sh
ll-builder build -f apps/linyaps/desktop-linglong/linglong.yaml
ll-builder export --ref com.deepseek.dsh-desktop
```

`prepare-offline.sh` 会构建仓库、打包 dsh 与 vendor 包集、准备 Electron 运行时与主运行时、准备 dsh 树，并用 `electron-builder --linux --x64 --dir` 产出到 `apps/linyaps/desktop-linglong/stage/`。容器构建只复制该目录树、安装图标与桌面入口、写入启动包装脚本。暂存完成后，[write-package-version.sh](write-package-version.sh) 把 `linglong.yaml` 的 `package.version` 写进 `stage/resources/linglong-version.json`，应用启动后读回该文件，「关于」窗口据此显示玲珑包版本；复用旧暂存目录时 `build-linglong.sh --no-prepare` 会重写该文件。

### 随包工具链

容器内没有 `git`、`wget`、`jq` 这类工具，而 harness 自己要调用 `git`（`workspace-changes` 算工作区变更，`worktree` 插件的 `gitCommand` 也指向它），模型在 bash 工具里也需要通用 shell 工具。这些依赖声明在 [linglong.yaml](linglong.yaml) 的 `buildext.apt.depends`，由玲珑在 build 段之后合并进 `${PREFIX}`（`$PREFIX/bin` 已在容器 PATH 首位）。

Chromium 的输入法与剪贴板由 Electron 自己处理，因此薄壳版随包的 `libwebkitgtk-6.0-4`、`fcitx5-frontend-gtk4`、`bubblewrap` 这些 WebKitGTK 专属依赖在这里不需要。

依赖是否真的进了包由 [verify-merged-deps.sh](verify-merged-deps.sh) 在合并产物树上断言：`ll-builder` 会把「拷不进去」降级成警告，而 `depends` 又在 `build` 段之后合并，日志里看不到落点。新增依赖必须同时在它的规则表里认领，否则构建失败。它自己的通过/失败路径由 [test-verify-merged-deps.sh](test-verify-merged-deps.sh) 覆盖。

`prepare-offline.sh` 会经 `@electron/get` 从 GitHub 下载 Electron 二进制，而它不读 `http_proxy`/`https_proxy`（Node 的 `fetch` 不认这两个变量）。在到不了 GitHub 的网络里，先设 `ELECTRON_MIRROR=https://npmmirror.com/mirrors/electron/`，或在已导出代理时设 `NODE_USE_ENV_PROXY=1`。

## 本模块依赖的上游改动

同一次改动给 `apps/desktop` 补上了 Linux 平台路径。

| 文件 | 改动 |
|---|---|
| [`desktop-build-paths.mjs`](../../desktop/scripts/desktop-build-paths.mjs) | `linux-x64` 进入 `SUPPORTED_TARGETS`；`desktopTargetPlatform` 返回 `linux` |
| [`desktop-build-paths.d.mts`](../../desktop/scripts/desktop-build-paths.d.mts) | `DesktopBuildTarget` = 发布目标 + `linux-x64` |
| [`prepare-runtime.ts`](../../desktop/scripts/prepare-runtime.ts) | 平台推导与 Electron 可执行路径认识 `linux` |
| [`prepare-dsh.ts`](../../desktop/scripts/prepare-dsh.ts) | Electron 可执行路径按打包目标解析，不再用构建宿主平台 |
| [`prepare-cli.ts`](../../desktop/scripts/prepare-cli.ts) | POSIX 的 `dsh` 启动脚本在 Linux 上被赋予可执行权限；它的启动路径针对 macOS 应用包，Linux 容器用的是 `linglong.yaml` 生成的 `$PREFIX/bin/dsh` |
| [`electron-builder-config.mjs`](../../desktop/scripts/electron-builder-config.mjs) | Linux 目标不生成更新源 |

## 已知边界

- 本包没有代码签名、公证与应用内更新，因为玲珑容器禁止提权。Electron 沙箱因此用 `--no-sandbox` 关闭。也因为没有 `app-update.yml` 进入安装树，应用不再显示检查更新命令、托盘入口、更新渠道行与左下角状态，也不再轮询更新源：这些控件只在带更新源的构建里出现。
- Linux 上 Office 转换选用 WASM 引擎，因为锁定的 `libreoffice-kit` 只为 macOS 与 Windows 声明了原生包。
- 运行期库由基础层提供：[`linglong.yaml`](linglong.yaml) 的 `depends` 只保证构建期可解析。已在 Deepin 25 上用 0.1.0.2 验证：包可安装并启动。

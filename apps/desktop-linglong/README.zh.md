# 以玲珑包分发的 DeepSeek Harness 官方桌面客户端

[English](README.md) | 中文

`apps/desktop-linglong` 把上游 Electron Desktop 客户端（[`apps/desktop`](../desktop)）打包成面向 Deepin 25 的玲珑包。它是官方客户端一侧的实现，与 [`apps/desktop-launcher`](../desktop-launcher)（把内嵌 `dsh web` 的 Go + Wails 薄壳打包）并列。两个包相互独立，以不同的应用 ID 并存安装。

## 本模块产出什么

一个 `.uab`，唯一入口是 `/opt/apps/com.deepseek.dsh-desktop-official/files/bin/dsh-desktop-official`——一个带 `--no-sandbox` 启动 Electron 应用的包装脚本。

Electron Desktop 是自包含的：它自带 Electron 运行时，也自带 `resources/app.asar/dsh` 树，因此本包不再随包第二份 Node 运行时或 harness 树。Linux x64 上解包后的应用实测约 1.1 GB（Electron 227 MB、主运行时 496 MB、dsh 树 331 MB），与薄壳包同一量级：打包 Electron 应用并不会让负载变小。

## 为什么没有复用上游的打包入口

[`package-target.ts`](../desktop/scripts/package-target.ts) 把目标类型绑在**发布目标**上（`DesktopPackageTargetName` 只有 mac-arm64、mac-x64、win-x64）：代码签名、公证、`electron-updater`、COS 上传。Linux 不是发布目标，因为分发由玲珑包自己负责。把 `linux-x64` 加进那个类型会牵动整条上传与更新类型链，实测一次性引发 12 处类型错误，因此 `prepare-offline.sh` 直接调用底层打包步骤，顺序与 `package-target.ts` 一致。

## 构建

先在宿主机准备产物，再在玲珑容器内组装。

```sh
sh apps/desktop-linglong/prepare-offline.sh
ll-builder build -f apps/desktop-linglong/linglong.yaml
ll-builder export --ref com.deepseek.dsh-desktop-official
```

`prepare-offline.sh` 会构建仓库、打包 dsh 与 vendor 包集、准备 Electron 运行时与主运行时、准备 dsh 树，并用 `electron-builder --linux --x64 --dir` 产出到 `apps/desktop-linglong/stage/`。容器构建只复制该目录树、安装图标与桌面入口、写入启动包装脚本。

## 本模块依赖的上游改动

同一次改动给 `apps/desktop` 补上了 Linux 平台路径。

| 文件 | 改动 |
|---|---|
| [`desktop-build-paths.mjs`](../desktop/scripts/desktop-build-paths.mjs) | `linux-x64` 进入 `SUPPORTED_TARGETS`；`desktopTargetPlatform` 返回 `linux` |
| [`desktop-build-paths.d.mts`](../desktop/scripts/desktop-build-paths.d.mts) | `DesktopBuildTarget` = 发布目标 + `linux-x64` |
| [`prepare-runtime.ts`](../desktop/scripts/prepare-runtime.ts) | 平台推导与 Electron 可执行路径认识 `linux` |
| [`prepare-dsh.ts`](../desktop/scripts/prepare-dsh.ts) | Electron 可执行路径按打包目标解析，不再用构建宿主平台 |
| [`prepare-cli.ts`](../desktop/scripts/prepare-cli.ts) | POSIX 的 `dsh` 启动脚本在 Linux 上被赋予可执行权限 |
| [`electron-builder-config.mjs`](../desktop/scripts/electron-builder-config.mjs) | Linux 目标不生成更新源 |

## 已知边界

- 本包没有代码签名、公证与应用内更新，因为玲珑容器禁止提权。Electron 沙箱因此用 `--no-sandbox` 关闭。
- Linux 上 Office 转换选用 WASM 引擎，因为锁定的 `libreoffice-kit` 只为 macOS 与 Windows 声明了原生包。
- Electron 在容器内需要的运行期库集合尚未在真机验证：[`linglong.yaml`](linglong.yaml) 的 `depends` 只保证构建期可解析，运行期库由基础层提供。

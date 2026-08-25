# Agent Note: 玲珑打包桌面客户端的可移植性修复

Status: implemented

[English](2026-08-24-linglong-git-exec-path-and-pnpm-bundling.md) | 中文

## Problem

玲珑打包的桌面客户端（com.deepseek.dsh-desktop）经 `buildext.apt.depends` 随包携带 git，但合并后的文件树破坏了 git 的运行时约定：二进制的编译期 exec-path 是 `/usr/lib/git-core`（容器内不存在），而 buildext 把 helper 搬到了 `<prefix>/lib/git-core`。所有远程操作（`git push`/`fetch`/`clone`） 都以 `git: 'remote-https' is not a git command` 失败，直到用户手动 `export GIT_EXEC_PATH`。一次真实的推送会话为推送一个分支花了六次全权 审批升级和几十步排查。配置了 Git LFS 的仓库还会在 post-checkout/pre-push 钩子上失败（git-lfs 未随包），被迫 `--no-verify`。pre-push typecheck 钩子 （`pnpm run typecheck`）在容器内无法启动：pnpm 的 deps 预检查先因只读 HOME 打不开 store 的 SQLite 索引，HOME 可写后又要求整体重建 node_modules 并在无 TTY 时中止。所有修复必须可移植：打出的 `.uab` 要在任意机器行为 一致，因此任何内容都不得引用构建者的身份、HOME 状态或字面写死的包路径。

## Decision

随包 git 被包装，使其 exec-path 在每台机器上由自身位置推导。 `wrap-git-exec-path.sh`（由 `build-linglong.sh` 在 `ll-builder build` 之后、 `verify-tools.sh` 之前运行）把 `<prefix>/bin/git` 改名为 `git.real`，并安装 一个薄包装脚本：`export GIT_EXEC_PATH="${GIT_EXEC_PATH:-$PREFIX/lib/git-core}"` （PREFIX 由 `dirname $0` 推导）后 `exec git.real`。幂等与导出后复核以 `git.real` 文件存在为判据，绝不用文件内容 grep（真实 git 二进制的用法文本 里就含 `GIT_EXEC_PATH` 字样）。launcher 在 `ConfigureChildEnv` 里经 `packagedGitExecPath` 为整个 harness 进程树设置 `GIT_EXEC_PATH`：前缀由 `os.Executable()` 推导，仅在 `<files>/lib/git-core` 存在时设置，并由 `env_test.go` 单测覆盖。pre-push 钩子改为 `npm run typecheck` 而非 `pnpm run typecheck`，完全跳过 pnpm 的 deps 预检查，执行的是同一组命令。 pnpm 出厂内置、离线可用：`prepare-offline.sh` 按 packageManager 锁定的版本 下载 pnpm tarball 到 `stage/node/lib/node_modules/pnpm`，并生成 `node/bin/pnpm` 薄包装（exec 捆绑的 node 与 pnpm CLI）；`linglong.yaml` 组装时把 `bin/pnpm` 软链到它，仅在捆绑缺失时回退 corepack 薄包装。 git-lfs 加入 `buildext.apt.depends` 与 `tools.yaml`。`verify-tools.sh` 优先校验捆绑 pnpm，并在合并后的 git 未被包装时大声失败——`git --version` 绿色不再能掩盖远程 helper 不可用。

## Alternatives considered

**字节补丁 git 二进制的 exec-path 字符串。** webkit helper 路径就是这么修的， 但 `/usr/lib/git-core`（17 字节）无法替换为长度不同的 `<prefix>/lib/git-core`，且前缀只有在运行时推导才在各机器一致；包装是唯一 长度安全的形态。

**用 grep `GIT_EXEC_PATH` 判定"已包装"。** 测试后发现真实 git 二进制的用法 文本含该字符串，会对新合并的 git 误判而跳过包装，予以否决。

**把开发者的 pnpm store / corepack 缓存打进包。** store 的 SQLite 索引与 corepack 缓存是机器相关状态；快照在别的机器上是陈旧、超大且错误的。

**继续以 corepack 作为 pnpm 来源。** 首次调用要下载 pnpm 并缓存到 `$HOME/.cache`；容器内 HOME 默认只读，默认沙箱与离线环境下工具不可用。

**保留 `pnpm run typecheck` 并翻转其配置。** 实测 pnpm 11 对两个拼写的 `verify-deps-before-run` 都不生效并继续走向破坏性重建中止；`npm run` 执行同样命令但没有该 bootstrap。

**加宽 `config.d/10-mounts.json` 使 HOME 目录可写。** 观测到的只读 HOME 是 harness 沙箱（`workspace-write` 模式）造成的，不是玲珑挂载——full-access 无需任何挂载变更就让 HOME 可写，且随包挂载模板只有用户拷贝后才生效。 加宽它既修不了默认体验，在没有所列目录的机器上还会失败。此事项押后到 产品层窄授权（`~/.cache`、`~/.local/share`、`~/.git-credentials` 及其锁、 `~/.dsh-tools` 等工具目录可写，并放行 git 的出站网络），而不是包内改动。

## Consequences

任何机器上安装重建后的 `.uab`，git 远程操作开箱可用、LFS 钩子通过、 typecheck 钩子在容器内能跑、pnpm 离线可用——包里没有任何构建者相关状态。 代价是真 git 二进制多一层包装（若 buildext 不再产出 `bin/git` 连同 `lib/git-core`，包装需同步更新）、pnpm 与 git-lfs 带来少量包体增长、 以及钩子命令假设 npm 存在——仓库所有脚本本就经 npm 运行，这是既有前提。 未完成的仍在产品层：可写 HOME 目录与出站网络在窄授权落地前仍被 full-access 沙箱预设门控；挂载模板与 README 本次刻意未更新。

## Testing

`go test ./internal/appenv/`（CGO_ENABLED=0）覆盖 `packagedGitExecPath`。 包装脚本在合成前缀上端到端验证：默认 exec-path 从 `/usr/lib/git-core` 重指到推导的 `<prefix>/lib/git-core`，重跑幂等，显式 `GIT_EXEC_PATH` 仍然优先。完整验证需在宿主机跑 `sh apps/desktop-launcher/build-linglong.sh` 重建，并在干净机器上、默认 沙箱下、无审批升级与 `--no-verify` 地重放分支推送场景。

## Related

同一工具链机制的既有记录： [玲珑容器工具链可用性](../../implemented/feature/2026-08-19-linglong-container-toolchain.zh.md)。

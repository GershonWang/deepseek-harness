# Agent Note: 玲珑容器工具链可用性

Status: implemented

[English](2026-08-19-linglong-container-toolchain.md) | 中文

## Problem

`apps/desktop-launcher` 以 `dsh web` 子进程方式在玲珑沙箱容器内运行 harness，harness 的 bash 工具链（`tool-bash`）在其中执行。因此可用工具集合 = 基础运行时 `org.deepin.base` + `buildext.apt.depends` 随包带入的软件。真实的碰壁发生在运行时：git 缺失导致仓库操作直接不可用；Node 必须捆绑 24，因为 beige 的 20 无法启动 harness；用户项目目录、凭据和 CA 证书对容器不可达。本 note 记录该方案如何把这些"运行时碰壁"前移为三层防线。

## Decision

三层防线，除 harness 清单注入（预设 overlay，不新增 `packages/` 包）外全部落在 `apps/desktop-launcher/`。以自包含为普通用户分发基线，宿主环境视为不可依赖。

**层 1 —— 构建期清单与校验。** `linglong/tools.yaml` 是单一事实来源：`tools:` 段（git、python3、curl、wget、jq、unzip、xxd、pnpm），`installable:` 按需白名单（go、ripgrep），`excluded:`（gcc、clang、rustc，永不随包）。`linglong.yaml` 经 `buildext.apt.depends` 随包带入工具（python3、python3-pip、curl、wget、unzip、zip、jq、xxd、ca-certificates；git 更早已加入）。`verify-tools.sh` 在 `ll-builder build` 之后、`export` 之前于宿主侧校验合并产物树 `linglong/output/binary/files`，因为 buildext 在 preCommit 才合并进 `$PREFIX`，构建容器看不到合并结果。任一二进制缺失或版本校验失败即非零退出并中止导出；`test-verify-tools.sh` 固定通过与失败两条路径。

**层 2 —— 运行时自检与模型可见工具清单。** 启动器状态栏打开设置弹框，其工具链分区经 `CheckTools(DefaultToolSpecs())` 探测 `git/python3/node/curl/jq/pnpm` 并列出已安装与可安装工具；"Git 凭据"分区负责凭据管理。`configurePackagedEnvForHome` 在目录存在时把 `$HOME/.dsh-tools/bin` 前置进 PATH、`$HOME/.dsh-tools/lib` 进 LD_LIBRARY_PATH，按需安装经 harness 重启后生效。harness 侧注入复用随包的 `standard` 预设（见下方 Phase D）。

**层 3 —— 重/罕见工具按需安装。** `toolinstall.go` 下载静态或自带运行时的产物（规避 glibc 与 postinst 耦合），校验 sha256，原子解包到 `$HOME/.dsh-tools/<name>-<ver>`（临时目录再 `mv`，并拒绝 tar 路径逃逸），更新 `current/<name>` 软链。目录经容器 HOME 映射落在宿主磁盘，玲珑卸载默认保留。可安装范围受 `tools.yaml` 的 `installable` 白名单约束。

**凭据与数据可达性。** `gitcred.go` 读取、写入、清除 `~/.git-credentials` 的 `github.com` 条目（git store 格式 `https://user:token@github.com`），保留其它 host 行；面板的保存/清除调用它，状态行从不回显令牌。可选模板 `linglong/config.d/20-host-credentials.json` 以只读方式绑定宿主 `~/.git-credentials` 与 `~/.ssh`，供需要直用宿主凭据的环境。容器 HOME 即宿主主目录，且 `ll-cli uninstall` 不清用户数据（源码核实），已存凭据在重装后保留；文档建议导出备份。`ca-certificates` 随包进 `$PREFIX` 供 git/https/python 校验；私有 CA 是文档化的追加 + `update-ca-certificates` 项。linyaps 默认转发代理环境变量。

**Phase D —— 模型可见容器工具清单。** 调查结论为分支 A：渲染后的系统提示已被日志化——`packages/core/agent-loop/src/agent.ts` 以 `request/header.header.system` 写盘，指令内容另以 `user/message` 事件落账——因此向 persona 追加工具文本即可满足"model-visible ⟺ logged"，无需新增 `SessionEventMap` 成员。预设只在会话选中时才挂载，且随包默认是 `standard`（由 web-app bundle 补丁设定），新增预设永远不会到达模型。因此部署改为 overlay `standard`：`linglong/harness-overlay/config/agent-presets/standard/agent.cordis.yml` 是仓库标准 roster 并在 persona 的 `text` 末尾追加容器工具链段；`linglong.yaml` build 将其复制覆盖到 `${PREFIX}/harness/config/agent-presets/standard/`。默认 id 与其余 roster 保持不变，所有打包会话都携带工具清单。

## Alternatives considered

**在构建容器内校验工具（层 1）。** 否决：buildext 的合并在 preCommit、`build:` 阶段之后，容器永远看不到合并后的 `$PREFIX`；宿主侧对 `linglong/output/binary/files` 的校验是唯一能核实实际交付内容的时点。

**只用运行时探针、不设构建期门禁（层 1）。** 仅有运行时健康面板会让缺失必需工具的包先导出、装完才报。构建期门禁把缺失工具变成构建失败并给出对应的 apt 包提示。

**在容器内用包管理器安装按需工具（层 3）。** 否决：需要 root、postinst 钩子，并在容器内留下逐次更新残留；`$HOME` 下的静态产物解包干净、规避 glibc 耦合，代价是没有自动更新。

**以宿主凭据只读挂载为默认（凭据）。** 设计以容器内本地存储（`~/.git-credentials`）为默认、宿主挂载为可选高级项，因为前者更简单，且安全语义（harness 本就代表用户执行代码）以文档明示而非暗示。

**新增 `desktop-tools` 预设而非 overlay `standard`（Phase D）。** 调查后否决：预设只在会话选中时挂载，打包默认是 `standard`，新预设只会出现在 roster 上却永远不组合默认会话（除非改 `packages/` bundle）。overlay `standard` 的 persona 能在保持默认 id 的同时触达每个会话。

**为工具清单新增 `SessionEventMap` 成员（Phase D 分支 B）。** 不需要：persona/系统提示文本可经 `request/header.header.system` 从会话日志完整重放，注入本身已被日志化。

## Consequences

包体增大：python3 + pip 约 +100 MB，git 拖入 perl 依赖栈（数十 MB），再加小工具，`.uab` 从 302 MB 基线增长到估计 375–400 MB；按需层正是 go 与 ripgrep 不进包体的原因。非静态产物（例如按需安装 go 后编译出的用户代码）依赖容器 glibc，属用户责任且已文档化。玲珑的私有映射隐藏 `~/.linglong/<appid>` 并隔离 `~/.ssh`；凭据面板展示真实宿主路径，挂载模板覆盖 `.ssh`。`standard` overlay 是冻结副本，源预设演进时会产生漂移，故带再同步注释；`tools.yaml` 的 installable 条目在从官方页面填实前仍是占位 sha256；按需安装依赖网络。

## Testing

`test-verify-tools.sh` 固定构建门禁；launcher 的 Go 模块以 `go test` 固定行为（toolinstall、toolcheck、gitcred、面板状态纯函数、env 注入）。verify-tools 门禁通过从夹具产物树删除一项来演练，用户机器上的真实玲珑 build/export 是计划交由彼处的运行时验收。

## Related

同一子系统的后续修复——随包 git 的 exec-path 包装、捆绑 pnpm、git-lfs、pre-push 钩子改走 npm： [玲珑打包桌面客户端的可移植性修复](../bug-fix/2026-08-24-linglong-git-exec-path-and-pnpm-bundling.zh.md)。

# Agent Note: 工具链市场卡片提示容器内运行时可用性

Status: implemented

[English](2026-09-10-toolchain-market-runtime-availability-hint.md) | 中文

## 问题

`apps/desktop-launcher` 的工具链市场弹框对工具标记「已安装/可安装」的唯一依据，是市场仓库（容器内 `$HOME/.dsh-tools`）里有没有对应版本目录。而在玲珑沙箱内，同一条命令可以由三个互不相干的来源独立提供——随包内置工具、`/opt/host-tools` 下的宿主导入挂载、基础运行时——交互式终端还可能额外看到经家目录 shell rc 文件（如 nvm）漏入的宿主命令。现象因此显得自相矛盾：终端里 `node v24.13.1` 可用、内置面板列出 `node 24.9.0`，市场卡片却写着「可安装」，卡片上没有任何信息说明哪份 node 会生效、来自哪里。

## 决策

未安装工具的卡片现在会渲染一行运行时提示「容器内已可用：命令 版本（来源）」，触发条件是容器 PATH 能解析出该工具清单 `Provides` 声明的命令。各层保持既有职责：

- `internal/domain`：`ToolCheck` 记录 `exec.LookPath` 的解析路径。
- `internal/toolchain/check.go`：`Check` 为固定随包清单填充 `Path`；新增 `ProbeCommands` 按需探测清单声明的命令——`LookPath` 命中即视为可用，`--version` 失败不回退可用性判定，只把版本留空。
- `internal/app/app.go`：`annotateRuntime` 合并固定自检与按需探测，按顺序匹配每个未安装工具的 `Provides`；`classifyRuntimeSource` 按前缀归类解析路径——`/opt/host-tools/` 为宿主导入，launcher 自身位置推导的随包 bin 目录为随包，市场仓库跳过（`Installed` 标志已覆盖该语义），其余归系统。函数以参数接收 `home` 与随包前缀，除 `ProbeCommands` 外不读进程环境，分类结果确定可测。
- `frontend/app.js` 渲染提示行；已安装与安装中卡片不受影响，`toolchain:status` 载荷只增字段。

提示刻意只覆盖 launcher 自身子进程的 PATH。经 shell rc 文件进入交互式终端的命令（nvm 等）对非交互探测天然不可见；把它们标成容器来源是错误的，因此这条边界是明示的限制，不是遗漏。

## 备选方案

**纯前端用现有内置行匹配。** 内置行只覆盖 12 个固定随包探测项、无法归类来源，还会把 UI 语义计算塞进渲染层。在覆盖面与分层上皆输。

**把徽标文案改成「未安装」。** 零结构改动，但依然回答不了「已有的命令从哪来」。徽标继续描述仓库，提示行描述运行时——两段文案合起来消除歧义，又不让任何一方语义过载。

**扩展 `DefaultSpecs` 一次探测全部清单命令。** 每次刷新多跑几十个 `--version` 进程，且固定清单要手工跟随目录维护。按需 `LookPath` 对未命中零成本，只对命中跑版本命令。

## 后果

容器内已有命令时卡片能自我解释；通过市场安装依然有意义——`~/.dsh-tools/bin` 注入在镜像 PATH 之前，安装完成后市场版本即接管 launcher 的子进程。每次工具刷新增加少量 `LookPath` 调用，且只对命中跑 `--version`，受共享的 5 秒探测超时约束。状态载荷新增字段；旧前端忽略未知字段，wire 兼容。

## 测试

`apps/desktop-launcher` 的 `go test` 钉住来源分类（`TestClassifyRuntimeSource`）、组装语义包括跳过市场来源后回退到下一条命令（`TestAnnotateRuntime`）、按需探测包括无版本命中的情形（`TestProbeCommands`）。`node --test frontend/test-app.cjs` 断言卡片渲染带版本与来源的提示行、已装与未命中卡片不渲染、探测读不到版本时省略版本段。

## 关联

本提示在 UI 上呈现的三层容器工具链决策：[玲珑容器工具链可用性](2026-08-19-linglong-container-toolchain.zh.md)。

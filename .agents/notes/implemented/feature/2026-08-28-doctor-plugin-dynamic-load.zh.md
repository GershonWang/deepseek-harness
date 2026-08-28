# Agent Note: 插件动态加载检查

Status: implemented

[English](2026-08-28-doctor-plugin-dynamic-load.md) | 中文

## Problem

第三方插件可能 import 当前安装不再提供的依赖（例如玲珑版缺少 `@deepseek-ai/dsh-host-apiproxy`），导致 Cordis Loader 在 import 阶段直接崩溃，harness 完全无法启动。doctor 的静态检查——bundle 可解析、patch 可合成——发现不了这种故障：bundle 能解析、patch 能合成，但插件模块的 import 在运行时失败。用户面对白屏或无限重启，却不知道是哪个插件坏了。

doctor 的 plugin 分类此前只检查静态事实：profile bundle 能否解析、patch 层能否干净合成、用户 patch 目标是否存在。这些都从不执行插件代码。

## Decision

在 `@deepseek-ai/dsh-doctor` 中新增实时加载检查（`plugin-dynamic-load`，category `plugin`，severity `fatal`），通过子进程探测真正启动插件树并报告结果。

**Loader probe**（`packages/support/doctor/src/loader-probe.ts`）：独立脚本，解析 profile、修复模块回退、写入空根 `cordis.yml`、合成 patch 栈、调用 `boot()`（`provideCmdline` 必须提供 `appReady`，否则 `sdk-app` 的 `exitOnStdinEnd` 在激活时抛错）、dispose 树，退出码 0=成功、1=加载失败（原因写 stderr）、2=超时。`--include` 参数只加载指定的第三方 bundle，供二分法测试子集。

**二分法**（`packages/support/doctor/src/bisect-by.ts`）：从 profile 专属二分中抽出的通用 `bisectBy<T>(items, isBad)`，谓词回答"该子集激活时坏行为是否存在"。现有 `bisectThirdPartyBundles` 改为委托给它。

**检查与修复**（`packages/support/doctor/src/checks/plugins.ts`）：检查项用 `loadProfile` 列出第三方 bundle，全量启动一次；失败后用 `bisectBy(names, subset => probe(subset).code !== 0)` 二分定位元凶。定位成功时报告点名该 bundle，`fixable: true`、`suggestedLevel: 2`、完整 probe 输出作为 detail。L2 修复从 profile manifest 的 `dsh.profile.bundles` 移除元凶（第三方 bundle 是 profile 层，不是用户 patch 行），先把 `package.json` 字节级备份到修复备份目录，再重新启动验证，失败则还原备份字节。

## 为什么改 manifest 而不是注释 patch 文件

第三方 bundle 位于 profile 的 `package.json` 的 `dsh.profile.bundles`，从不来自用户的 `cordis.patch.yml`。在 patch 文件里注释行无法禁用一个 bundle 层。从 manifest 移除 bundle 也正是 `DSH_SAFE_MODE=plugins` 应用的排除方式，且 `writeProfileManifest` 是受支持的机器编辑 API（launcher 加载时自己就会回写 manifest）。按条目插 `disabled: true` patch 需要枚举每个 patch id、跨层重复 id 会冲突，还会改写用户自己的编辑面（web profile 通过 HMR 实时重载 patch）。

## Deferred

无：桌面启动器自动诊断与启动页已随检查一并落地。桌面 supervisor 进入 `StateFailed`（容器模式）时后台运行 doctor，前端失败页显示诊断进度，结果就绪后自动弹出诊断报告。

## Testing

`loader-probe.spec.ts`（5 个用例）在临时 home 上 spawn probe：全新 profile 加载；import 缺失依赖的坏 bundle 在全量与 `--include` 子集下都失败且 stderr 含依赖名；排除坏 bundle 的 `--include` 子集正常加载；未知 `--include` 退出 1 并打印响亮消息；永不 settle 的顶层 await 加载报超时。`plugins-dynamic-load.spec.ts`（10 个用例）覆盖检查注册、无第三方通过、健康 bundle 中的元凶二分、无法定位的交互对、以及修复的移除+备份+验证+幂等分支。

## Alternatives considered

- **注释 patch 文件禁用** —— 拒绝：bundle 层不是 patch 行；按 id 的 `disabled` patch 需要枚举 id、重复 id 冲突、改写用户 patch 面。
- **在 doctor 进程内全量激活干跑** —— 拒绝：doctor 不应污染自身进程；子进程探测隔离副作用并可用超时约束。
- **静态 import 扫描** —— 拒绝：发现不了传递依赖缺失（真实故障模式）和条件 import。

## Consequences

doctor 现在能抓到最常见的真实启动故障模式——第三方插件 import 不再解析——而不是在 harness 白屏前一直显示全绿。probe 较慢（每次真实启动耗时数秒），所以检查只全量启动一次，仅在失败时二分。probe 沿承静态检查的开发环境怪癖：在本仓库中，从 web-app anchor 解析 bundle 需要 vitest 提供的 pnpm 虚拟 store `NODE_PATH`；打包安装从 app anchor 解析，无此问题。
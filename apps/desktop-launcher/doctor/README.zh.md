---
description: "诊断并修复 DeepSeek Harness 安装：覆盖运行环境、配置、插件与会话数据的检查，给出结构化报告、隔离导致启动失败的插件包，并在修复前备份被改动的状态。"
kind: "package-library"
---

# @dsh-desktop/doctor

[English](README.md) | 中文

## 概述

`@dsh-desktop/doctor` 检查一份 harness 主目录并报告损坏之处：Node 与磁盘前提、启动环境、`settings.yaml` 与用户补丁 YAML、profile 包的可解析性，以及会话与附件数据。桌面启动器把本包的 `cli.js` 当作启动前预检，doctor 面板也复用它。包可以用 `registerCheck` 贡献自己的检查。诊断过程不写任何文件：`runRepair(level)` 会重新诊断，只应用调用方授权等级之内的修复，并先把受影响的状态复制到 `~/.dsh/backups/doctor-<时间戳>`。

## 目录

- [使用本包](#use-this-package)
- [理解实现](#understand-the-implementation)
- [延伸阅读](#further-exploration)
- [已知限制与待办](#known-limitations-and-deferred-work)
- [开发备注](#dev-note)

-----

<a id="use-this-package"></a>
## 使用本包

### 何时使用

当一次安装无法启动、装完插件后行为异常，或需要在启动前先做检查时，用这个库。本仓库里唯一的消费者是桌面启动器：它在启动 harness 之前运行 `cli.js`，并从该子进程的环境里剥离自己的 `DSH_SAFE_MODE`，让预检报告上一次启动失败的真实原因，而不是继承当前 shell 的覆盖值；当某个包拥有内置检查看不到的失效模式时，它通过 `registerCheck` 追加检查。

当工作属于一次普通 harness 运行的一部分时，应改用插件入口：本包是一个由启动器、CLI 或测试调用的库，不注册任何 profile 层。

### 入口

```ts
import { runDiagnosis, runRepair, registerCheck } from '@dsh-desktop/doctor'

const report = await runDiagnosis()                 // reads an explicit path, else $DSH_HOME, else ~/.dsh
const failed = report.checks.filter(check => !check.result.ok)
const repair = await runRepair(1)                   // authorize fixes at level 1 (mild); 2 and 3 escalate
```

`runDiagnosis(dshHome?, { quick })` 并发运行所有已注册检查并返回报告：按注册顺序每个检查一条记录，外加统计 `total`、`ok`、`failed`、`fatal`、`fixable` 的 `summary`。单个检查抛出的异常会被收进它自己的结果里，不会中断整轮运行，因此一个坏插件掩盖不了其他结论。CLI 通过 `cli.js` 暴露同一套能力，启动器以 `node <doctor>/lib/types/cli.js` 拉起它（打包态为 `<prefix>/harness/doctor`，源码态为 `apps/desktop-launcher/doctor`）：

```sh
node <doctor>/lib/types/cli.js            # human-readable report
node <doctor>/lib/types/cli.js --json     # the same report as machine-readable JSON
node <doctor>/lib/types/cli.js --quick    # skip the live loader probe (fast static preflight)
node <doctor>/lib/types/cli.js --repair 2 # diagnose, then apply fixes whose suggestedLevel is at most 2
```

`--repair [level]` 取 1（温和）、2（中等）、3（破坏性），省略等级时默认 1，其它取值在诊断开始前就被拒绝。失败是数据而不是异常：`summary.fatal > 0` 表示在对应检查通过之前该安装无法启动，`summary.fixable` 则统计本轮可修复的失败数。

-----

<a id="understand-the-implementation"></a>
## 理解实现

<details>
<summary>实现细节 —— 点击展开</summary>

框架维护一份进程级检查注册表，针对同一个已解析主目录并发运行全部条目，并把修复当作对同一份报告的第二遍处理。

### 源码地图

| 文件 | 职责 |
|---|---|
| [`src/index.ts`](src/index.ts) | 检查注册表、`runDiagnosis`、`runRepair`、备份保留 |
| [`src/cli.ts`](src/cli.ts) | 供启动器子进程使用的参数解析、人类可读渲染与进程退出码 |
| [`src/types.ts`](src/types.ts) | 报告、检查、严重级别与修复等级类型 |
| [`src/checks/env.ts`](src/checks/env.ts) | Node 版本、磁盘剩余空间、启动环境 |
| [`src/checks/config.ts`](src/checks/config.ts) | `settings.yaml` 与用户补丁 YAML 的有效性 |
| [`src/checks/plugins.ts`](src/checks/plugins.ts) | 包可解析性、补丁可组合性与目标、第三方清单、实时探针 |
| [`src/checks/data.ts`](src/checks/data.ts) | 会话日志完整性、损坏会话归档、附件存储 |
| [`src/loader-probe.ts`](src/loader-probe.ts) | 真正启动一个 profile 并以退出码汇报的独立子进程 |
| [`src/auto-disabled.ts`](src/auto-disabled.ts) | doctor 禁用过的包留下的跨进程记录，供桌面壳读取 |
| [`src/bisect.ts`](src/bisect.ts) | 二分定位导致 profile 加载失败的第三方包 |
| [`src/bisect-by.ts`](src/bisect-by.ts) | 插件隔离所依赖的通用子集二分框架 |
| — | 不发布运行时不变式伴生模块；本框架自身不拥有事件流或可变运行时数据，其注册、报告与修复约定由单元测试保证。 |

### 检查集合

默认运行十一项检查，按描述其对象的分类归组。`env-node-version`、`env-disk-space`、`env-bootstrap-env` 覆盖运行时前提，并拒绝被发现的文件不应设置的启动变量。`cfg-settings-yaml` 与 `cfg-user-patch` 解析用户配置。`plugin-bundles-resolvable` 证明 profile 声明的每个包都能解析到已安装的层，`plugin-patch-composable` 与 `plugin-patch-targets` 组合补丁列表并校验其目标，`plugin-third-party-list` 汇报清单，`plugin-dynamic-load` 通过在子进程里用真实 Loader 启动一个 profile 构成第十二项检查。`data-sessions-integrity` 与 `data-attachments` 覆盖已存会话日志与附件文件。

严重级别区分前提与缺陷：`fatal` 阻止启动，`error` 破坏某个功能，`warning` 使其降级，`info` 仅作汇报。`--quick` 精确地移除 `plugin-dynamic-load`——它是唯一会拉起探针的检查，因此静态预检保持快速，而完整运行仍能观察到只在挂载时失败的插件。

### 修复

`runRepair(level)` 先诊断，为整轮修复建立一个 `backups/doctor-<时间戳>` 目录，再按注册顺序遍历失败条目。当失败不可修复、其 `suggestedLevel` 超过请求等级，或对应检查没有 `fix` 时跳过；否则修复实现会拿到该备份目录，从而在写入前保存修复前状态。于是修复是有序的、受等级约束的，并且可以依据提出请求的同一份报告回滚。doctor 保留最近五个备份目录，其余按名字里的时间戳排序后清理。

</details>

-----

<a id="further-exploration"></a>
## 延伸阅读

需要消费报告的启动链路或产生报告的 profile 模型时，读这些页面。

- [桌面启动器](../README.zh.md) —— 拉起 `cli.js` 的启动前预检与 doctor 面板，以及它们依赖的 `DSH_SAFE_MODE` 剥离。
- [启动包](../../../packages/boot/app-boot/README.zh.md) —— env 检查所断言的 profile 解析、补丁组合与启动环境规则。
- [插件管理器](../../../packages/boot/plugin-manager/README.zh.md) —— 安装与启用 doctor 要检查可解析性的那些包。
- [架构](../../../docs/architecture.zh.md) —— profile、它的包与层在启动时如何组合。

-----

## 已知限制与待办

<a id="known-limitations-and-deferred-work"></a>

这些限制界定单次运行能证明什么、不能证明什么。它们是当前的包约束，不是任务清单。

- **实时探针只启动一个 profile** —— 只有 `plugin-dynamic-load` 能观察到挂载期失败，而 `--quick` 会移除它；静态运行看不到只在挂载时才失败的插件。
- **修复授权属于调用方** —— 仅在 `suggestedLevel` 处于请求等级之内时修复才执行，因此 `--repair 1` 有意只修一部分；等级 3 的修复会删除状态，绝不被隐含授权。
- **备份有上限而非归档** —— doctor 只保留最近五个 `backups/doctor-*` 目录，更早的会被清理；需要长期留存时必须在五次修复之前另存。
- **第三方清单只做汇报** —— `plugin-third-party-list` 以 `info` 级别列出已安装的第三方包，是否禁用它们仍由人或启动器决定。

<a id="dev-note"></a>
### 开发备注

<details>
<summary>维护者工作上下文 —— 点击展开</summary>

本包是玲珑分支私有的。它位于 `apps/desktop-launcher/doctor` 而非 `packages/`，因此合并上游永远不会碰到它，也不是 pnpm workspace 成员：由 `node apps/desktop-launcher/tools/doctor-build.mjs` 构建，再由 `linglong/prepare-offline.sh` 把产物 stage 到 `<prefix>/harness/doctor`。启动器直接拉起 `cli.js`，没有任何 `dsh` 子命令挂载它，因此不存在会冲突的上游 CLI 接线。

</details>

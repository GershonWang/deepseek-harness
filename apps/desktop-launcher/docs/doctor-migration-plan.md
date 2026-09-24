# doctor 迁入 apps/desktop-launcher 迁移实施方案

> **For agentic workers:** REQUIRED SUB-SKILL: 用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务执行本方案。步骤使用 `- [ ]` 复选框跟踪。

**Goal:** 把 fork 自研的诊断修复能力（doctor 包、`dsh doctor` CLI 入口、`DSH_SAFE_MODE` 安全模式）全部收敛到 `apps/desktop-launcher/` 之内，使上游树中不再残留该能力的任何接线，合并上游时 doctor 相关冲突面归零。

**Architecture:** doctor 从 `packages/support/doctor/` 迁到 `apps/desktop-launcher/doctor/`，成为**非 pnpm workspace 成员**的目录包（`pnpm-workspace.yaml` 的 `apps/*` 只匹配深度 1，`apps/desktop-launcher/doctor` 天然不被匹配，无需改上游配置）。依赖解析、构建、类型检查、单测覆盖率全部由 `apps/desktop-launcher/tools/` 下的 fork 自有脚本承担；打包态由 `linglong/prepare-offline.sh` 把 doctor 的构建产物放进 harness 闭包同级目录。CLI 入口从 `apps/cli` 摘除，Go 壳直接 spawn doctor 入口；安全模式改用上游既有的 `--patch` overlay 机制实现，删除对 `app-boot` 的侵入。

**Tech Stack:** TypeScript（doctor，ESM + tsc 构建）、Go（Wails 壳）、Cordis Loader（`--patch` overlay 与 loader-probe）、pnpm deploy 闭包、玲珑打包。

---

## 0. 背景与已核实的事实

以下事实均经实测，不是推断。实施时不需要重新验证，但**任何一条被上游改动打破时都要停下来重新评估方案**。

| 编号 | 事实 | 核实方式 |
|---|---|---|
| F1 | doctor 全部 25 个文件、21 个提交均由 fork 作者创建，上游历史中不存在任何 `doctor` 路径 | 上游全量可达对象扫描 + 逐 blob 哈希比对，命中 0 |
| F2 | 上游从未有过诊断/修复框架；上游首个运行时启动诊断能力出现于 2026-09-16，晚于 doctor 创建（2026-08-28） | 上游历史路径扫描 + 提交时间 |
| F3 | `pnpm-workspace.yaml` globs 为 `packages/*/*`、`apps/*`、`vendor/*` 等；`apps/desktop-launcher/doctor` 不被任何 glob 匹配 | 读 `pnpm-workspace.yaml` |
| F4 | `apps/desktop-launcher/` 无 `package.json`，不是 workspace 成员，零门禁成本 | 目录检查 |
| F5 | 仓库根 `node_modules/@deepseek-ai/` 不含 `dsh-app-boot` 等 doctor 依赖 | 目录检查 |
| F6 | `prepare-offline.sh` 的 `inject_workspace_pkg` 只遍历 `packages/` 与 `vendor/` | 读脚本 |
| F7 | `tsdown.config.ts` 的 workspace globs 不含 `apps/desktop-launcher` | 读配置 |
| F8 | `vitest.config.ts` 覆盖率 `include` 为 `packages/*/*/src/**/*.{ts,tsx}` | 读配置 |
| F9 | 上游 `--patch` overlay 支持 `- id: <entry>\n  disabled: true`，能禁用第三方 bundle 的 entry | doctor 自身 `bisect.ts` 与上游 `apps/web/tests/pin-browse-picker.overlay.yml` 双向印证 |
| F10 | 上游公开 API `loadLayeredEnv()` 会在 `.env` 声明 bootstrap-only 变量时抛错；`loadEnv()` 不校验 | 读 `packages/boot/app-boot/src/index.ts` |
| F11 | `isBootstrapOnly` / `BOOTSTRAP_NAMES` / `BOOTSTRAP_PREFIXES` 在上游是模块私有，fork 把它改成 `export` | 读 fork 对 `index.ts` 的 diff |
| F12 | ~~`packages/support/doctor` 与 `apps/desktop-launcher/doctor` 距仓库根均为 3 层，`tsconfig.json` 的 `extends` 与 `references` 相对路径原样可用~~ **此条已被实测推翻，见下方修正** | 路径深度比对（结论错误） |
| F12′ | 两者虽同为 3 层，但 `../..` 分别落在 `packages/` 与 `apps/`，**不是**同一个目录：`extends` 的 `../../../tsconfig.base.json` 与 `../../../vendor/*` 恰好都对，而 `references` 里指向 `packages/` 下各包的 4 条必须从 `../../boot/...` 改为 `../../../packages/boot/...` | 首次 `doctor-build` 报 `TS6053: File '.../apps/util/home-paths' not found` |
| F13 | `harnessArgs` 中 `--patch` 必须排在 `--port` 之前，否则被 `web` 的 `passThroughOptions` 吞掉 | `internal/appenv/env.go` 既有注释与实测记录 |

---

### 0.1 上游基线的取法

方案中的回退命令（`git checkout ... -- <file>`）需要指向一个**固定的上游基线**。基线就是 `upstream/master` 的当前提交，撰写本方案时对应上游 `release-dsh-0.1.7-alpha.2`（2026-09-22，PR #4978），且已经是本地 HEAD 的祖先。

执行前先把它解析成一个变量，不要在各条命令里散写提交号：

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
git fetch upstream
BASE=$(git rev-parse upstream/master)
echo "上游基线: $BASE"
```

**若 `$BASE` 已经前进到更新的上游提交**，回退仍然安全（这些文件在更晚的提交里没有被改动过），但要重新核对一次本节的 F1–F13，尤其是 F9（`--patch` overlay 语义）与 F10（`loadLayeredEnv` 抛错行为）——它们是方案里唯二依赖上游具体行为的假设。

---

## 1. 迁移后的目标架构

```
apps/desktop-launcher/
├── doctor/                      ← 从 packages/support/doctor/ 整体迁入（非 workspace 目录包）
│   ├── package.json             ← private，exports 指向 lib/types/*.js
│   ├── tsconfig.json            ← extends ../../../tsconfig.base.json（相对路径不变）
│   ├── src/                     ← 7 个源文件 + checks/
│   ├── tests/                   ← 原有测试原样迁入
│   └── README.md / README.zh.md
├── tools/                       ← fork 自有工具（新增）
│   ├── doctor-link-deps.mjs     ← 开发态建立 doctor/node_modules 依赖链接
│   ├── doctor-build.mjs         ← tsc 构建 doctor 到 lib/types
│   ├── doctor-verify.mjs        ← doctor 类型检查 + 单测 + 逐文件覆盖率
│   ├── safe-mode-overlay.mjs    ← 生成安全模式 overlay（阶段 2）
│   └── fix-deploy-closure.mjs   ← 从 scripts/ 迁入（阶段 3）
├── internal/                    ← Go 壳
│   ├── appenv/doctor.go         ← 新增：doctor 入口路径解析
│   ├── appenv/env.go            ← 安全模式改为追加 --patch overlay
│   ├── preflight/preflight.go   ← doctor argv 改为直连 doctor 入口
│   └── app/app.go               ← 去掉 os.Setenv("DSH_SAFE_MODE", ...)
├── frontend/                    ← 不变
└── linglong/prepare-offline.sh  ← 新增 stage doctor 步骤
```

数据流（迁移后）：

```
Go 壳
  ├─ appenv.Resolve()      → node <harness/lib/bin.js> web --patch <supervisor-overlay.yml> [--patch <safe-mode-overlay.yml>] --port N
  └─ preflight.Runner      → node <doctor/lib/types/index.js> --json --quick
                                   └─ plugin-dynamic-load → node <doctor/lib/types/loader-probe.js> --include ...   （同级解析，不依赖包导出、不依赖 tsx）
```

---

## 2. 影响面清单

分四类：**迁入**（进入 `apps/desktop-launcher/`）、**删除**（上游树中的 fork 新增文件）、**回退**（上游文件中的 fork 改动，恢复成上游原文）、**下沉**（上游目录中的 fork 工具挪到 `apps/desktop-launcher/tools/`）。

| 阶段 | 文件 | 类型 | 上游冲突分类 |
|---|---|---|---|
| 1 | `packages/support/doctor/**`（25 文件） | 迁入 | C（独立目录，本就不冲突） |
| 1 | `apps/cli/src/doctor.ts`（134 行） | 删除 | B（新文件在上游目录内） |
| 1 | `apps/cli/src/args.ts`（+58/−2） | 回退 | A（每次必冲突） |
| 1 | `apps/cli/src/bin.ts`（+6） | 回退 | A |
| 1 | `apps/cli/package.json`（+1） | 回退 | A |
| 1 | `apps/cli/tests/args.spec.ts`（+10） | 回退 | A |
| 1 | `apps/cli/tsconfig.json`（+3） | 回退 | A |
| 1 | `tsconfig.base.json`（+1） | 回退 | A |
| 1 | `tsconfig.host.json`（+1） | 回退 | A |
| 1 | `packages/support/`（空目录随 doctor 迁走而消失） | — | 附带修掉当前唯一的红灯门禁 `verify-subsystem-pages` |
| 2 | `packages/boot/app-boot/src/profile.ts`（+58/−8） | 回退 | A |
| 2 | `packages/boot/app-boot/src/index.ts`（+6/−3） | 回退 | A |
| 2 | `packages/boot/app-boot/tests/safe-mode.spec.ts`（+88） | 删除 | B |
| 2 | `packages/support/doctor/src/bisect.ts`（阶段 1 后位于 launcher 内） | 改造 | — |
| 2 | `packages/support/doctor/src/checks/env.ts`（同上） | 改造 | — |
| 3 | `scripts/fix-deploy-closure.mjs`（152 行） | 下沉 | B |
| 3 | `scripts/verify-client-ui-i18n.ts`（+85） | 下沉 | A |
| 3 | `scripts/verify-package-readme-model-experience.ts`（+1） | 回退 | A |
| 3 | `scripts/doc-standard.spec.ts`（+1） | 回退 | A |
| — | `pnpm-lock.yaml` | 不可消除 | A（上游本身高频漂移，1811 次） |

**阶段 1 消灭 6 个 A 类文件，阶段 2 再消灭 2 个，阶段 3 再消灭 3 个；doctor 相关 A 类文件从 11 个降到 0。**

### 2.1 无法达成的部分（必须提前知道）

- **`pnpm-lock.yaml` 永远会冲突。** 它是上游改动最频繁的文件（自 2026-03-01 起 1811 次），本地任何依赖变更都会让它偏离。缓解手段是"每次合并上游后重跑 `pnpm install` 并单独一个 commit 还原非预期漂移"，已在 HEAD 上实践过。
- **阶段 2 的 `config` 安全模式层在纯 launcher 侧无法做到与上游 `userLayer: false` 完全等价**（见 §3.3 决策点 D2）。
- **不需要也不要追求"只改 apps/desktop-launcher"**：`packages/support/` 目录消失、`tsconfig` 接线删除、`app-boot` 回退，这些都是**减小**偏离的动作，即使它们发生在 `apps/desktop-launcher/` 之外也完全符合本方案的意图。判断标准是"是否新增上游树中的偏离"，不是"是否触碰了上游文件"。

---

## 3. 关键技术决策

### 3.1 doctor 的非 workspace 包身份与依赖解析

**问题：** doctor 不在 workspace 内（F3），因此 `pnpm install` 不会为它建立 `node_modules`，而仓库根 `node_modules/@deepseek-ai/` 又没有它的 workspace 依赖（F5）。开发态直接 `node apps/desktop-launcher/doctor/lib/types/index.js` 会 `ERR_MODULE_NOT_FOUND`。

**方案：** 两条路径分别解决，互不影响。

- **开发态**：`tools/doctor-link-deps.mjs` 在 `apps/desktop-launcher/doctor/node_modules/@deepseek-ai/` 下建符号链接指向 `packages/<group>/<pkg>` 源码目录，`js-yaml` 指向仓库根 `node_modules/js-yaml`。由 lefthook 作业与 `doctor-build.mjs` 幂等调用。
- **打包态**：`prepare-offline.sh` 把 `apps/desktop-launcher/doctor` 的构建产物拷到 `<stage>/harness/doctor/`。Node 从 `<stage>/harness/doctor/lib/types/index.js` 向上查找，命中 `<stage>/harness/node_modules`，那里是 `pnpm deploy` 产生的完整闭包。

**为什么不打包成单文件 bundle（否决备选）：** bundle 会把 `@deepseek-ai/dsh-app-boot` 内联进 doctor，进程里出现第二份 app-boot，`PluginPackages` 与 schemastery schema 的实例身份会分叉，而 doctor 的 patch 组合与 loader-probe 恰恰依赖这些身份。收益（省一次符号链接）远小于风险。

**备选（未采用）：** 把 doctor 放进 `packages/experimental/` 以留在 workspace 内。否决理由：`prepare-offline.sh` 明确跳过 experimental 包的闭包注入，doctor 不会被 stage；且违反"收敛到 apps/desktop-launcher"的目标。

### 3.2 loader-probe 的解析改为同级路径

**问题：** 当前 `loaderProbeEntry()`（`src/checks/plugins.ts:350`）优先 `require.resolve('@deepseek-ai/dsh-doctor/loader-probe')`，失败时回退到 `../loader-probe.ts` 并加 `--import tsx/esm`。doctor 离开 workspace 与闭包后，`require.resolve` 必然失败，而打包态没有 tsx——回退分支是死路。

**方案：** 删除 `require.resolve` 分支，改为**按同级产物探测**，两个运行面各自命中正确的兄弟文件。注意相对路径是 `../`（本文件产物在 `lib/types/checks/`，探针在 `lib/types/`），不是 `./`。

```ts
/**
 * 定位 loader-probe 子进程入口，并说明该入口是否需要 tsx 加载。
 *
 * 按同级产物探测，而不是走包导出：doctor 已迁出 pnpm workspace 成为 launcher 私有
 * 目录包，打包态不经 node_modules 暴露该导出，`require.resolve` 必然失败。
 *
 * 两个运行面必须都覆盖，`tsc` 把 src 平铺到 `lib/types`，本文件在两个面里的同级兄弟
 * 文件不同名：
 * - 产物面（打包安装、CLI 调用）：`lib/types/checks/plugins.js` 的兄弟是
 *   `lib/types/loader-probe.js`，直接跑，不需要 tsx——打包态也没有 tsx。
 * - 源码面（vitest 直接加载 `src/checks/plugins.ts`）：兄弟是 `src/loader-probe.ts`，
 *   必须用 `--import tsx/esm` 启动。只在源码面回退到 tsx，因此不会把 tsx 依赖带进
 *   打包运行路径。
 *
 * @returns 探针脚本绝对路径，以及是否需要 tsx 加载。
 */
function loaderProbeEntry(): { path: string; needsTsx: boolean } {
  const built = new URL('../loader-probe.js', import.meta.url)
  if (existsSync(built)) return { path: fileURLToPath(built), needsTsx: false }
  return { path: fileURLToPath(new URL('../loader-probe.ts', import.meta.url)), needsTsx: true }
}
```

调用点**保持原样**（`probe.needsTsx ? ['--import','tsx/esm', probe.path] : [probe.path]`）。

**为什么不能只留产物面：** 实测过一版只返回 `../loader-probe.js` 的实现，doctor 的测试立刻从 75 通过掉到 67 通过 9 失败——测试从 `src/` 加载，`src/loader-probe.js` 不存在。原设计的双分支是对的，被替换掉的只是它那失效的**判据**（包导出），不是双分支本身。

**这也修正了原注释的适用范围**：原设计声称双分支是为了"既能从 bundle 又能从源码工作"，迁移后 doctor 仍有两个运行面（打包产物 + vitest 源码），因此双分支继续存在，只是判据从"包导出能否解析"换成"同级产物是否存在"。

### 3.3 安全模式：用上游 `--patch` overlay 替代 `DSH_SAFE_MODE`

**问题：** 当前安全模式靠 `app-boot/src/profile.ts` 读 `process.env.DSH_SAFE_MODE` 决定是否剔除第三方 bundle、是否跳过用户补丁层。这要求改上游文件。

**`plugins` 层（剔除第三方 bundle）——可以完全用上游机制实现，且已实测通过。** 依据 F9 与阶段 0 的 spike 0.3：overlay 里 `- id: <entry>\n  disabled: true` 能真正禁用 entry，`--dump-config` 会把它渲染成

```yaml
# == dshmarket, patched by <overlay>
- id: dsh-market
  name: dshmarket
  disabled: true
```

**但禁用规则必须是「只禁用第三方 bundle 自己 `insert` 的 entry」，绝不能禁用它们 `patch` 的 id。** 这条限制是 spike 0.3 实测出来的，不是设计偏好：

- 第三方 bundle 的 patch 分两类：`insert` 行（它自己新增的插件）与 `id` 行（对**已存在 entry** 的 config 覆盖）。
- 被 `id` 指向的 entry **往往属于官方层**。本机实测：`@michengai/dsh-archive-manager` 通过 `id` patch 了 `workspace`、`ui-workspace`、`session-projection-cache` 三个 entry，而这三个分别由官方 `@deepseek-ai/dsh-web-app` 与 `@deepseek-ai/dsh-base` 插入。
- 按「禁用所有 id」的朴素规则生成 overlay，会把这三个**官方插件**一起禁掉——安全模式反而把 harness 的 Web 界面打坏。

修正后的规则只取 `insert[].id`，本机得到 8 个目标，实测全部生效且官方 entry 一个未被触碰（`disabled: true` 总数 41 → 49，恰好等于目标数）。

**顺带暴露了 `bisect.ts` 的既存缺陷**（见 §3.4 问题 B）：它只遍历 `patch.id`、从不看 `patch.insert[].id`，因此在本机会去禁用官方 entry 而完全禁用不掉第三方的插件。这印证了 Task 8 把它改走 loader-probe `--include` 的必要性——那条路径按 bundle 名选择，不涉及 entry 级判断，天然避开这个陷阱。

实现：新增 `tools/safe-mode-overlay.mjs`，读 profile 的 bundle 清单，对每个非 `@deepseek-ai/` bundle 的**每个 `insert` 行的 id** 生成 `- id: <id>\n  disabled: true`，写成一个 overlay 文件；Go 壳把它作为**第二个 `--patch`** 追加进 `harnessArgs`（F13 的顺序约束同样适用：必须在 `--port` 之前）。

**`config` 层（跳过用户补丁层）——上游 CLI 未暴露该开关。** 上游 `loadProfile` 支持 `userLayer: false`，`apps/cli/src/profile-boot.ts` 的 `prepareProfile(name, userLayer = true, ...)` 也把它透传下去了，但**没有任何 CLI 参数能把它置为 false**。注意用户补丁层有两处：`<profileDir>/cordis.patch.yml` 与 `$DSH_HOME/cordis.patch.yml`（后者优先级更高，见 `composeProfile` 的层序说明），两层都要覆盖才算等价。

### 3.4 doctor 的两处上游私有 API 依赖

**问题 A：`isBootstrapOnly` 等 3 个符号上游不导出（F11）。** 仅 `src/checks/env.ts` 的 `env-bootstrap-env` 检查用到，用于判定 `<DSH_HOME>/.env` 里是否有 bootstrap-only 变量。

**方案（推荐）：** 改用上游公开的 `loadLayeredEnv()`（F10）。它在 `.env` 声明 bootstrap-only 变量时抛出的错误信息与判定口径**就是**上游的真值来源，不需要复制任何常量。因为该函数会物化 `process.env`，必须在子进程中调用——doctor 已有 loader-probe 的子进程基建可复用。

**备选：** 在 doctor 内复制 `BOOTSTRAP_NAMES` / `BOOTSTRAP_PREFIXES` 两个字面量，并加一个 fork 自有的生成/校验脚本从 `packages/boot/app-boot/src/index.ts` 抽取比对，防止漂移。缺点是引入一个 TS 源码解析器；优点是检查保持纯函数、无子进程开销。

**问题 B：`extraPatchFiles` 上游没有。** 仅 `src/bisect.ts` 用于注入临时禁用 patch。

**方案：** 把 bisect 改为驱动现有的 loader-probe（`--include <第三方子集>`），用退出码判定，不再需要向 `loadProfile` 传额外 patch 文件。附带收益：bisect 从"只做 patch 组合"升级为"真实 boot"，与 `plugin-dynamic-load` 检查的口径一致，且真实 boot 才能暴露缺依赖类失败。

---

## 4. 阶段 0：可行性 spike

**目的：** 在动任何搬迁之前，用最小代价证伪三个假设。任何一条失败都要回头改方案，不要带着疑问往下走。

- [x] **Step 0.1：确认 doctor 的 workspace 依赖都在打包闭包内**

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
node -e '
const deps = Object.keys(require("./packages/support/doctor/package.json").dependencies);
console.log(deps.join("\n"));
'
ls apps/desktop-launcher/linglong/stage/harness/node_modules/@deepseek-ai/ | grep -E "app-boot|cmdline|home-paths|atomic-write|web-app|cordis-plugin-include"
```
Expected：上面 6 个包全部出现在 `stage` 的 `node_modules/@deepseek-ai/` 下。若缺任何一个，先解决它为什么不在闭包内（通常是 peer-only 漏装，`inject_workspace_pkg` 负责补齐），再继续。

- [x] **Step 0.2：验证非 workspace 目录包能被 Node 解析依赖**

在仓库根创建临时目录验证解析链路（**不要提交**）：

```sh
mkdir -p /tmp/doctor-spike/node_modules/@deepseek-ai
ln -s "$PWD/packages/boot/app-boot" /tmp/doctor-spike/node_modules/@deepseek-ai/dsh-app-boot
cat > /tmp/doctor-spike/probe.mjs <<'EOF'
const mod = await import('@deepseek-ai/dsh-app-boot')
console.log('resolved ok:', typeof mod.loadProfile)
EOF
node /tmp/doctor-spike/probe.mjs
rm -rf /tmp/doctor-spike
```
Expected：`resolved ok: function`。这证明"符号链接 + 向上查找"的解析策略可用。

- [x] **Step 0.3：验证上游 `--patch` overlay 能禁用第三方 bundle entry**

用当前安装生成一个"禁用全部第三方 entry"的 overlay，再启动 harness，确认第三方插件消失且官方插件仍在：

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
node --import tsx/esm -e '
const { loadProfile } = await import("./packages/boot/app-boot/src/index.ts")
const { createRequire } = await import("node:module")
const anchor = createRequire(import.meta.url).resolve("@deepseek-ai/dsh-web-app/package.json")
const p = loadProfile("spike", "web", anchor)
const third = p.layers.filter(l => !l.packageName.startsWith("@deepseek-ai/"))
console.log("第三方 bundle:", third.map(l => l.packageName).join(", ") || "(无)")
const lines = third.flatMap(l => l.patches.filter(x => x.id).flatMap(x => [`- id: ${x.id}`, "  disabled: true"]))
require("node:fs").writeFileSync("/tmp/spike-safe-overlay.yml", lines.join("\n") + "\n")
console.log("overlay 行数:", lines.length)
' --input-type=module
```
Expected：打印出第三方 bundle 清单与 overlay 行数。若清单为空，说明当前安装没有第三方插件，需要先装一个再验证；若 `layers[].patches[].id` 全为空，**该方案不成立，停下来重新设计**。

随后人工验收：

```sh
pnpm dsh web --patch /tmp/spike-safe-overlay.yml
```
Expected：harness 正常启动，`/tmp` 下第三方插件的 UI 入口消失，官方插件功能完好。

> **⚠️ 上面的临时 overlay 生成代码只用于 spike 探测，规则是错的，不要照抄进实现。** 它按「第三方 layer 的每个 patch id + insert id」生成禁用项，实测会误禁官方 entry。实际执行时改用 `--dump-config` 对比（无需启动服务），并按 §3.3 的修正规则只取 `insert[].id`。完整结论见文末「spike 结果」。

- [x] **Step 0.4：记录 spike 结论**

把三条结论写进本文件末尾的"spike 结果"小节，再进入阶段 1。**三条全绿才开工。**

---

## 5. 阶段 1：doctor 本体搬迁 + CLI 入口外移（原子提交序列）

**为什么必须原子：** doctor 一旦离开 workspace，`apps/cli/src/doctor.ts` 的 `import ... from '@deepseek-ai/dsh-doctor'` 立即失效；`tsconfig.base.json` 的 path 别名与 `tsconfig.host.json` 的 project reference 也会指向不存在的目录。中间不存在"能构建"的状态，因此搬迁与 CLI 摘除必须在同一批提交内完成。

**提交粒度：本阶段的 5 个 Task 依次执行，但只在 Task 1–4 全部完成并验证通过后做一次提交。** 各 Task 末尾的 `git add`/`git commit` 步骤只作为工作进度检查点（可以用 `git stash` 或直接在同一个工作树里继续），**不要逐 Task 提交**——那会在历史里留下 `apps/cli` 无法构建的提交。Task 5（打包链路）验证通过后单独提交。

### Task 1: 迁移目录并改造包清单

**Files:**
- Move: `packages/support/doctor/` → `apps/desktop-launcher/doctor/`
- Modify: `apps/desktop-launcher/doctor/package.json`
- Modify: `apps/desktop-launcher/doctor/src/checks/plugins.ts`
- Modify: `apps/desktop-launcher/doctor/tsconfig.json`

- [x] **Step 1.1：用 git mv 保留历史**

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
mkdir -p apps/desktop-launcher
git mv packages/support/doctor apps/desktop-launcher/doctor
rm -rf apps/desktop-launcher/doctor/lib apps/desktop-launcher/doctor/node_modules
git rm -r --cached apps/desktop-launcher/doctor/lib 2>/dev/null || true
git status --short | head -40
```
Expected：`R` 重命名记录出现在 `packages/support/doctor/...` → `apps/desktop-launcher/doctor/...`，且 `lib/`、`node_modules/` 未被跟踪（它们本就被 `.gitignore` 覆盖，删掉只是清理工作副本）。

- [x] **Step 1.2：改写 package.json 为非 workspace 目录包**

替换 `apps/desktop-launcher/doctor/package.json` 全文：

```json
{
  "name": "@dsh-desktop/doctor",
  "description": "Linglong 桌面启动器的诊断与修复框架（fork 自有，非上游包）",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "main": "lib/types/index.js",
  "types": "lib/types/index.d.ts",
  "exports": {
    ".": {
      "types": "./lib/types/index.d.ts",
      "default": "./lib/types/index.js"
    },
    "./package.json": "./package.json"
  },
  "dependencies": {
    "@deepseek-ai/cordis-plugin-include": "*",
    "@deepseek-ai/dsh-app-boot": "*",
    "@deepseek-ai/dsh-atomic-write": "*",
    "@deepseek-ai/dsh-cmdline": "*",
    "@deepseek-ai/dsh-home-paths": "*",
    "@deepseek-ai/dsh-web-app": "*",
    "js-yaml": "^4.2.0"
  },
  "peerDependencies": {
    "@deepseek-ai/cordis": "*"
  },
  "devDependencies": {
    "@types/js-yaml": "^4.0.0",
    "vitest": "^4.1.8"
  }
}
```

**改动理由（逐条）：**
- `name` 改为 `@dsh-desktop/doctor`：它不再是上游发布物的一部分，继续占 `@deepseek-ai/` 域会在排查时误导"这是官方包"。若你希望保留原名（例如已有外部文档引用），改成 `@deepseek-ai/dsh-doctor` 也不影响本方案任何一步——这属于决策点 D1。
- 删掉 `publishConfig` / `repository` / `files`：私有包不发布，这些字段只会在 `pnpm run hygiene` 类检查里产生无意义的期望。
- `exports` 全部指向 `lib/types/*.js`：迁移后 doctor 不进 tsdown workspace（F7），没有 `lib/index.js` 单文件 bundle，运行形态就是 tsc 平铺产物。
- 删掉 `./loader-probe` 导出：见 Step 1.3。
- 依赖版本改为 `"*"`：非 workspace 成员不能写 `workspace:*`，而该字段在迁移后只作为 `doctor-link-deps.mjs` 的清单来源与人类文档，实际解析由符号链接决定。（若 `pnpm install` 或某门禁对非 workspace `package.json` 报错，说明有额外的全局扫描，需回到 §3.1 重新评估。）

- [x] **Step 1.3：loader-probe 改为同级解析**

在 `apps/desktop-launcher/doctor/src/checks/plugins.ts` 中，把 `loaderProbeEntry` 改为 §3.2 给出的实现（按同级产物探测、返回 `{ path, needsTsx }`），调用点保持原有的 `probe.needsTsx ? ['--import','tsx/esm', probe.path] : [probe.path]` 分支不变。保留原有的模块级中文注释，并写清"两个运行面各自解析到哪个文件"。

- [x] **Step 1.4：修正 tsconfig 的 `references` 相对路径**

**不要照抄"相对路径原样可用"的判断**——见 F12′，`../..` 在旧位置是 `packages/`、在新位置是 `apps/`，指向 `packages/` 下各包的 4 条引用必须补一级：

```sh
sed -i 's|"../../boot/|"../../../packages/boot/|; s|"../../util/|"../../../packages/util/|' apps/desktop-launcher/doctor/tsconfig.json
```

`extends` 的 `../../../tsconfig.base.json` 与 `../../../vendor/cordis`、`../../../vendor/include` 三条**保持不变**（它们本来就以仓库根为基准）。

```sh
node node_modules/typescript/bin/tsc -p apps/desktop-launcher/doctor/tsconfig.json
```
Expected：退出码 0，`apps/desktop-launcher/doctor/lib/types/` 下出现 `index.js`、`cli.js`、`loader-probe.js`。若报 `TS6053: File '.../apps/util/...' not found`，说明这条路径没改对。

- [ ] **Step 1.5：提交**

```sh
git add -A apps/desktop-launcher/doctor packages/support
git commit -m "refactor(launcher): 将 doctor 从 packages 迁入 apps/desktop-launcher"
```

### Task 2: fork 自有工具链

**Files:**
- Create: `apps/desktop-launcher/tools/doctor-link-deps.mjs`
- Create: `apps/desktop-launcher/tools/doctor-build.mjs`
- Create: `apps/desktop-launcher/tools/doctor-verify.mjs`

- [x] **Step 2.1：写依赖链接工具**

创建 `apps/desktop-launcher/tools/doctor-link-deps.mjs`：

```js
#!/usr/bin/env node
/**
 * 为 apps/desktop-launcher/doctor 建立依赖解析链接。
 *
 * 为什么需要：doctor 迁出 packages/ 后不再是 pnpm workspace 成员（pnpm-workspace
 * 的 apps/* 只匹配深度 1），pnpm install 不会为它建 node_modules；而仓库根
 * node_modules 也没有它的 workspace 依赖，Node 向上查找必然失败。
 *
 * 解析来源一律取自 doctor/package.json 的 dependencies，避免在工具里另维护一份
 * 包列表——那份列表会比 package.json 先过期。
 *
 * 幂等：已存在且指向正确目标的链接跳过，指向错误的先删后建。
 */
import {
  existsSync, lstatSync, mkdirSync, readFileSync, readdirSync, readlinkSync, rmSync, symlinkSync,
} from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const toolsDir = dirname(fileURLToPath(import.meta.url))
const launcherDir = resolve(toolsDir, '..')
const repoRoot = resolve(launcherDir, '..', '..')
const doctorDir = join(launcherDir, 'doctor')

/**
 * 列出目录下的一级子目录名。
 * @param dir - 目标目录。
 * @returns 子目录名数组。
 */
function listDirs(dir) {
  return readdirSync(dir, { withFileTypes: true }).filter(entry => entry.isDirectory()).map(entry => entry.name)
}

/**
 * 把 workspace 包名解析成仓库内的源码目录。包目录名等于去掉 `@deepseek-ai/`
 * 域后的短名，位于 packages/<group>/<pkg> 或 vendor/<group>/<pkg>。vendor 包的
 * 目录名与包名并非总是一致，因此以“目录下有 package.json”为准而不是猜路径。
 * @param name - doctor 依赖里的包名。
 * @returns 源码目录绝对路径；非 workspace 包或未找到时返回 undefined。
 */
function resolveWorkspacePackage(name) {
  if (!name.startsWith('@deepseek-ai/')) return undefined
  const short = name.slice('@deepseek-ai/'.length)
  for (const top of ['packages', 'vendor']) {
    const topDir = join(repoRoot, top)
    if (!existsSync(topDir)) continue
    for (const group of listDirs(topDir)) {
      const candidate = join(topDir, group, short)
      if (existsSync(join(candidate, 'package.json'))) return candidate
    }
  }
  return undefined
}

const manifest = JSON.parse(readFileSync(join(doctorDir, 'package.json'), 'utf8'))
let linked = 0
let reused = 0

for (const name of Object.keys(manifest.dependencies ?? {})) {
  // 非 workspace 依赖（js-yaml）从仓库根 node_modules 取，那里是 pnpm 装的实体。
  const target = resolveWorkspacePackage(name) ?? join(repoRoot, 'node_modules', name)
  if (!existsSync(target)) {
    console.error(`doctor-link-deps: 依赖目标不存在 ${name} → ${target}；先跑 pnpm install`)
    process.exit(1)
  }
  const linkPath = join(doctorDir, 'node_modules', name)
  mkdirSync(dirname(linkPath), { recursive: true })
  if (existsSync(linkPath)) {
    const current = lstatSync(linkPath)
    const pointsAtTarget = current.isSymbolicLink()
      && resolve(dirname(linkPath), readlinkSync(linkPath)) === resolve(target)
    if (pointsAtTarget) {
      reused += 1
      continue
    }
    rmSync(linkPath, { recursive: true, force: true })
  }
  symlinkSync(relative(dirname(linkPath), target), linkPath, 'dir')
  linked += 1
}

console.log(`doctor-link-deps: 新建 ${linked} 个链接，复用 ${reused} 个`)
```

- [x] **Step 2.2：写构建工具**

创建 `apps/desktop-launcher/tools/doctor-build.mjs`：先调用 `doctor-link-deps.mjs`，再执行 `tsc -b apps/desktop-launcher/doctor/tsconfig.json`，最后断言 `lib/types/index.js` 与 `lib/types/loader-probe.js` 都存在。任一断言失败时以非零退出并打印缺失路径——静默产出一个缺入口的构建比构建失败更难排查。

- [ ] **Step 2.3：写校验工具**

创建 `apps/desktop-launcher/tools/doctor-verify.mjs`：以 `apps/desktop-launcher/doctor` 为 root 跑 `vitest run --coverage`，并把逐文件 100% 覆盖率门槛搬过来。上游门禁 `pnpm run test:coverage` 的 `include` 是 `packages/*/*/src/**`（F8），doctor 迁走后会自动脱离该门禁；不补这一层，doctor 现有的 10 个 spec / 77 个用例与 100% 覆盖率纪律会无声消失。

- [ ] **Step 2.4：验证工具链**

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
pnpm install --frozen-lockfile
pnpm run build                      # 先构建 doctor 依赖的 references
node apps/desktop-launcher/tools/doctor-build.mjs
node apps/desktop-launcher/doctor/lib/types/index.js --json --quick | head -20
node apps/desktop-launcher/tools/doctor-verify.mjs
```
Expected：doctor 以 JSON 报告输出；校验工具报告全部用例通过且覆盖率达标。

- [ ] **Step 2.5：提交**

```sh
git add apps/desktop-launcher/tools
git commit -m "build(launcher): 为迁入的 doctor 补自有构建与校验工具链"
```

### Task 3: Go 壳直连 doctor

**Files:**
- Create: `apps/desktop-launcher/internal/appenv/doctor.go`
- Modify: `apps/desktop-launcher/internal/appenv/env.go`
- Modify: `apps/desktop-launcher/internal/preflight/preflight.go`
- Modify: `apps/desktop-launcher/internal/app/app.go`
- Test: `apps/desktop-launcher/internal/appenv/doctor_test.go`

- [x] **Step 3.1：先写失败的测试**

创建 `apps/desktop-launcher/internal/appenv/doctor_test.go`，断言在给定 `PREFIX` 结构下 `ResolveDoctor()` 返回：打包态为 `<prefix>/harness/doctor/lib/types/index.js` 且 `Command` 是捆绑的 node；开发态为仓库内 `apps/desktop-launcher/doctor/lib/types/index.js`。跑 `go test ./internal/appenv/ -run TestResolveDoctor`，Expected：编译失败 / 断言失败（函数尚不存在）。

- [x] **Step 3.2：实现 doctor 入口解析**

创建 `apps/desktop-launcher/internal/appenv/doctor.go`，按 `env.go` 的 `Resolve()` 同构实现 `ResolveDoctor()`：四条候选路径（`DSH_DESKTOP_DOCTOR_BIN` 环境变量 → 打包态 → 开发态 → 失败），返回 `Config`。**复用** `resolveNode()`，不要另写一份 node 查找逻辑。

- [x] **Step 3.3：改 doctor argv 组装**

`internal/preflight/preflight.go` 的 `args()` 当前拼的是 `[dshScript, "doctor", ...extra]`。改为直接用 `ResolveDoctor()` 的 `Command`/`Args` 作为基址再追加 extra。`DoctorArgs()` 与 `Env()` 的对外签名保持不变，因为 `internal/app` 的 doctor 面板复用了它们——**保持调用方零改动**是本步的验收点。

- [x] **Step 3.4：改 app.go 的 doctor 触发点**

`internal/app/app.go` 的 `New()` 里 `preflight.NewRunner(dshCmd, dshScript, ...)` 改为传入 doctor 的 `Config`；`dshCmd`/`dshScript` 若仅服务于 doctor，一并删除。逐个 grep `dshScript` / `dshCmd` 确认无残余引用后再删。

- [x] **Step 3.5：跑 Go 测试**

```sh
cd apps/desktop-launcher
go build ./...
go test ./...
```
Expected：全部通过。

- [ ] **Step 3.6：提交**

```sh
git add apps/desktop-launcher/internal
git commit -m "refactor(launcher): Go 壳直连 doctor 入口，不再经 dsh doctor 子命令"
```

### Task 4: 摘除 apps/cli 与 tsconfig 接线

**Files:**
- Delete: `apps/cli/src/doctor.ts`
- Modify: `apps/cli/src/args.ts`, `apps/cli/src/bin.ts`, `apps/cli/package.json`, `apps/cli/tests/args.spec.ts`, `apps/cli/tsconfig.json`
- Modify: `tsconfig.base.json`, `tsconfig.host.json`

- [x] **Step 4.1：确认回退就是"恢复上游原文"**

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
git diff "$BASE" HEAD -- apps/cli/ tsconfig.base.json tsconfig.host.json
```
把这份 diff 通读一遍，确认其中**全部**内容都属于 doctor 接线（此前已核实为是）。若出现任何与 doctor 无关的改动，单独处理，不要一起回退。

- [x] **Step 4.2：逐个回退**

```sh
git rm apps/cli/src/doctor.ts
git checkout "$BASE" -- apps/cli/src/args.ts apps/cli/src/bin.ts apps/cli/package.json apps/cli/tests/args.spec.ts apps/cli/tsconfig.json tsconfig.base.json tsconfig.host.json
git status --short
```
Expected：只有这 8 个文件显示为修改/删除，且 `git diff` 里不再出现任何 doctor 字样。

- [x] **Step 4.3：验证 `dsh` 仍然完好**

```sh
node apps/cli/lib/bin.js --help | head -30
node apps/cli/lib/bin.js web --help 2>&1 | head -10
```
Expected：`--help` 正常；`doctor` 子命令不复存在（这是预期的功能移除，见决策点 D3）。

- [x] **Step 4.4：跑上游侧门禁**

```sh
pnpm run hygiene
node --import tsx/esm scripts/verify-subsystem-pages.ts
```
Expected：`verify-subsystem-pages` 现在**退出 0**——`packages/support/` 目录随 doctor 迁走而消失，当前唯一的红灯随之修掉。

- [ ] **Step 4.5：提交**

```sh
git add -A apps/cli tsconfig.base.json tsconfig.host.json
git commit -m "refactor(cli): 移除 dsh doctor 子命令与 doctor 的 tsconfig 接线"
```

### Task 5: 打包链路

**Files:**
- Modify: `apps/desktop-launcher/linglong/prepare-offline.sh`

- [x] **Step 5.1：新增 doctor stage 步骤**

在 `prepare-offline.sh` 的 `fix-deploy-closure.mjs` 调用之后、闭包瘦身之前，插入一段把 doctor 构建产物放进 `<stage>/harness/doctor/` 的逻辑：先跑 `tools/doctor-build.mjs`（它依赖 `pnpm run build` 已构建的 references），再把 `doctor/package.json` 与 `lib/` 拷进 stage。

**注意 `inject_workspace_pkg` 保持不变**：它遍历 `packages/` 与 `vendor/`（F6），doctor 迁走后不再被它看到——这正是我们要的结果，doctor 由上面这段新逻辑单独负责。

- [x] **Step 5.2：本地验证打包产物可运行**

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
sh apps/desktop-launcher/linglong/prepare-offline.sh
apps/desktop-launcher/linglong/stage/node/bin/node \
  apps/desktop-launcher/linglong/stage/harness/doctor/lib/types/index.js --json --quick | head -20
```
Expected：doctor 在 stage 环境下输出合法 JSON 报告，且**不含** `plugin-dynamic-load`——`--quick` 按设计跳过需要真实 boot 的加载探测（实测 11 项）。探针本身的端到端验收见"验收标准"第 5 条：那里必须让 home 里存在第三方 bundle，否则该检查走短路分支、根本跑不到探针。

- [ ] **Step 5.3：提交**

```sh
git add apps/desktop-launcher/linglong/prepare-offline.sh
git commit -m "build(launcher): 打包链路 stage doctor 产物并绕过 workspace 闭包注入"
```

---

## 5.5 阶段 1 执行中发现的两项必要工作

这两项在原方案里被漏掉了，但**不做就会留下真实缺陷**，因此补为阶段 1 的组成部分（与 Task 1–4 同批提交）。

- [x] **Step 5.1：给 doctor 建自己的 vitest 配置（否则全部测试静默消失）**

**问题（已实测）：** 根 `vitest.config.ts` 的 `testIncludes` 是 `apps/*/tests/**/*.spec.{ts,tsx}`——`apps/` 后**只匹配一级**。doctor 迁到 `apps/desktop-launcher/doctor/tests/` 后深度为 2，根配置收集不到它：

```sh
./node_modules/.bin/vitest list --filesOnly 2>/dev/null | grep -c desktop-launcher
```
Expected：迁移前为 0（**这就是问题**）。doctor 的全部测试会从 `pnpm run test` 里消失，且没有任何门禁会报错。

**为什么不去改根配置：** 那是上游文件，改它等于把 fork 的接线长期挂在 `vitest.config.ts` 上，每次合并上游都在同一个文件冲突——正是本次迁移要消除的东西。

**方案：** 新增 `apps/desktop-launcher/doctor/vitest.config.ts`，只做两件事——限定 `include` 为 `tests/**/*.spec.ts`，以及把包名解析指回仓库根的 `tsconfig.base.json`。

```ts
import { fileURLToPath } from 'node:url'
import tsconfigPaths from 'vite-tsconfig-paths'
import { defineConfig } from 'vitest/config'

const repoFacade = fileURLToPath(new URL('../../../tsconfig.base.json', import.meta.url))

export default defineConfig({
  plugins: [tsconfigPaths({ projects: [repoFacade] })],
  test: { include: ['tests/**/*.spec.ts'] },
})
```

**路径必须是 `../../../`（三层）。** 实测写成 `../../` 会让插件去找 `apps/tsconfig.base.json`，它只打印一行解析警告就继续跑——**测试仍然"全绿"，但解析落到了 `doctor/node_modules` 指向的已构建产物上**，变成对产物而非对源码的验证，违反仓库"源码面与产物面不可混用"约定。这个错误不会让任何测试变红，只会让验证失去意义，所以务必用下面的命令确认没有该警告。

```sh
./node_modules/.bin/vitest run --root apps/desktop-launcher/doctor 2>&1 | grep -c "An error occurred while parsing"
```
Expected：0。

- [x] **Step 5.2：给 doctor 补一个真正的命令行入口**

**问题（已实测）：** `src/index.ts` 是**纯库模块**，没有任何 argv 处理；`dsh doctor` 的全部命令行逻辑（参数解析、人类可读渲染、退出码）都在 `apps/cli/src/doctor.ts`（134 行）里。因此 Task 4 不能只是删掉它——删了就没有任何进程能按启动器需要的形态调用 doctor。

**方案：** 把该文件的能力整体搬到 `apps/desktop-launcher/doctor/src/cli.ts`：

1. 原样保留 `severityIcon` / `formatHuman` / `formatRepairHuman` / `runDoctor`，只把 `import ... from '@deepseek-ai/dsh-doctor'` 改成相对导入 `./index.js` 与 `./types.js`。
2. 新增 `main(argv)`：用 `node:util` 的 `parseArgs` 解析 `--json` / `--quick` / `--repair <1|2|3>`，参数错误返回 **2**（与"发现问题"的 1 区分，便于 Go 壳把调用错误和诊断结论分开）。
3. 文件末尾 `process.exitCode = await main(process.argv.slice(2))`。
4. `package.json` 的 `main` 指向 `lib/types/index.js`（库入口），`cli.js` 作为独立可执行入口由 Go 壳直接 `node <path>/cli.js` 调用，**不设 `bin`**——doctor 不是可安装的 CLI 包。

**必须覆盖的三种 argv 形态**（Go 壳实际使用的全部，见 `internal/preflight`）：

| argv | 用途 | 退出码 |
|---|---|---|
| `--json --quick` | 每次启动的快速预检 | fatal>0 → 1 |
| `--json` | doctor 面板全量诊断 | fatal>0 → 1 |
| `--json --repair <level>` | 分级修复 | 有跳过项 → 1 |

```sh
node apps/desktop-launcher/doctor/lib/types/cli.js --json --quick | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const r=JSON.parse(s);console.log(r.checks.length, JSON.stringify(r.summary))})'
node apps/desktop-launcher/doctor/lib/types/cli.js --repair 9; echo "exit=$?"
```
Expected：第一条打印 `11 {...}`（quick 模式按设计跳过 `plugin-dynamic-load`，故为 11 而非 12）；第二条打印用法错误并 `exit=2`。

- [x] **Step 5.3：构建工具用 `tsc -p` 而非 `tsc -b`**

`tsc -b` 会沿 project references 遍历并构建**整张引用图**（实测从 doctor 出发覆盖 23 个项目，含 `packages/llm/llm`、`packages/attachment/attachment` 等与 doctor 无直接关系的包）。doctor 是 fork 私有包，它的构建不应该存在任何可能改写上游包构建产物的路径。`tsc -p` 只编译 doctor 自身。

代价是必须先跑过根构建，因此 `tools/doctor-build.mjs` 要先检查 6 个引用项目的 `lib/types/index.d.ts` 是否存在，缺失时报出"先运行 pnpm run build"而不是让 `tsc` 抛一串 TS6305。

```sh
rm -rf apps/desktop-launcher/doctor/lib
node apps/desktop-launcher/tools/doctor-build.mjs
```
Expected：三个入口均生成；同时 `git status --porcelain` 在 `packages/` 与 `vendor/` 下**不出现任何未跟踪文件**。

---

## 6. 阶段 2：安全模式去 app-boot 化

**前置：** 决策点 D2 已定。

### Task 6: 生成安全模式 overlay

**Files:**
- Create: `apps/desktop-launcher/tools/safe-mode-overlay.mjs`
- Test: `apps/desktop-launcher/doctor/tests/safe-mode-overlay.spec.ts`

- [ ] **Step 6.1：先写失败的测试**

在 `apps/desktop-launcher/doctor/tests/` 下新建 spec：给一个含官方 bundle + 两个第三方 bundle 的假 profile，断言生成的 overlay 里**只**含第三方 bundle 的 entry id 且每个都 `disabled: true`，官方 bundle 的 entry 一个都不出现。跑 `node apps/desktop-launcher/tools/doctor-verify.mjs`，Expected：失败（模块不存在）。

- [ ] **Step 6.2：实现 overlay 生成**

`tools/safe-mode-overlay.mjs` 接收 `--profile <name> --out <path>`，用 doctor 已构建的 `loadProfile`（与 §3.4 的 anchor 解析同源）拿到 layers，按下面的规则生成 overlay 文本并写盘，最后把生效的 entry id 清单打到 stdout 供 Go 壳记日志。

```js
// 只取第三方 bundle 自己 insert 的 entry id。
// 绝不能取 patch.id：那些 id 指向的是官方层已存在的 entry（第三方只是覆盖其
// config），禁用它们等于把官方插件一起关掉——阶段 0 spike 0.3 实测：本机
// @michengai/dsh-archive-manager 会命中官方 workspace / ui-workspace /
// session-projection-cache 三个 entry。
const ids = []
for (const layer of thirdPartyLayers) {
  for (const patch of layer.patches) {
    for (const row of Array.isArray(patch.insert) ? patch.insert : []) {
      if (row?.id) ids.push(String(row.id))
    }
  }
}
```

**边界条件（必须处理，不要省）：**
- **第三方 bundle 列表为空时**：直接告知调用方"无需 overlay"，**不要**写出空 overlay 后继续按"安全模式已生效"推进——那会让用户以为跳过了第三方插件，而实际只是碰巧没有第三方插件。
- **某 layer 的 `patches` 为空，或其所有 `insert` 行都没有 id**：跳过该 layer 并打印警告。这类 bundle 在当前机制下**无法被安全模式禁用**，必须让用户看见这条限制，而不是静默产出一个"看起来禁用了实际没禁用"的 overlay。
- **生成失败一律非零退出**：Go 壳必须能区分"没有第三方插件"与"overlay 生成失败"。把后者当成前者，安全模式就变成了一个没有效果却告知用户已生效的按钮。
- **回归断言**：测试里必须包含一条"官方 entry 不被触碰"的用例——用固定的假 profile 断言生成的 overlay 里**不含**任何官方 layer 的 `patch.id`。这是本步唯一会造成功能性破坏的失误点。

- [ ] **Step 6.3：验证测试通过并提交**

```sh
node apps/desktop-launcher/tools/doctor-verify.mjs
git add apps/desktop-launcher/tools apps/desktop-launcher/doctor/tests
git commit -m "feat(launcher): 新增安全模式 overlay 生成器，用上游 --patch 机制禁用第三方 bundle"
```

### Task 7: Go 壳改用 overlay

**Files:**
- Modify: `apps/desktop-launcher/internal/appenv/env.go`
- Modify: `apps/desktop-launcher/internal/app/app.go`
- Test: `apps/desktop-launcher/internal/appenv/env_test.go`

- [ ] **Step 7.1：先改测试**

把 `internal/app/preflight_test.go` 与 `app_test.go` 里对进程级 `DSH_SAFE_MODE` 的断言（当前共 4 处：`preflight_test.go:132/159/171`、`app_test.go:145`）改为断言 overlay 路径出现在 `harnessArgs` 的 `--patch` 序列里，且位置在 `--port` 之前（F13）。跑 `go test ./...`，Expected：失败。

- [ ] **Step 7.2：实现**

- `env.go`：`harnessArgs` 增加"可选的第二个 overlay"参数；安全模式开启时，先调 `safe-mode-overlay.mjs` 生成 overlay 到运行时目录，再把它的路径追加进 `--patch` 序列。生成失败时不要静默降级为普通启动——那会让"安全模式"变成一个没有效果却告知用户已生效的按钮。
- `app.go`：删除 `StartSafeModeLevel` 里的 `os.Setenv("DSH_SAFE_MODE", level)` 与 `ExitSafeMode` 里的 `os.Unsetenv`，改为设置/清除 App 内的 overlay 状态并重启。
- `preflight/preflight.go`：删除子进程环境里剥离 `DSH_SAFE_MODE` 的那段过滤（`Env()` 与 `NewRunner` 中的逐行过滤），以及对应的 `preflight_test.go` 断言。

- [ ] **Step 7.3：跑测试并提交**

```sh
cd apps/desktop-launcher && go test ./...
git add apps/desktop-launcher/internal
git commit -m "refactor(launcher): 安全模式改用 --patch overlay，不再依赖进程环境变量"
```

### Task 8: 回退 app-boot 与 doctor 的两处上游 API 依赖

**Files:**
- Delete: `packages/boot/app-boot/tests/safe-mode.spec.ts`
- Modify: `packages/boot/app-boot/src/profile.ts`, `packages/boot/app-boot/src/index.ts`
- Modify: `apps/desktop-launcher/doctor/src/bisect.ts`, `apps/desktop-launcher/doctor/src/checks/env.ts`

- [ ] **Step 8.1：改造 bisect 走 loader-probe**

把 `bisect.ts` 的 `tryLoadWithDisabled` 从"写 patch 文件 + `loadProfile(extraPatchFiles)`"改为"spawn loader-probe，用 `--include` 传入仍启用的第三方子集，以退出码判定成功/失败"。删除对 `PROFILE_PATCH_FILENAME` 与临时 patch 目录的写入逻辑。同步更新它的单元测试。

- [ ] **Step 8.2：改造 env 检查**

按决策点 D4 选定 E1 或 E2 实现 `env-bootstrap-env` 检查，删除 `import { BOOTSTRAP_NAMES, BOOTSTRAP_PREFIXES, isBootstrapOnly } from '@deepseek-ai/dsh-app-boot'` 与文件末尾的 re-export。

- [ ] **Step 8.3：确认 doctor 不再触碰上游私有 API**

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
grep -rn "BOOTSTRAP_NAMES\|BOOTSTRAP_PREFIXES\|isBootstrapOnly\|extraPatchFiles\|skipThirdPartyBundles" apps/desktop-launcher/ | grep -v node_modules
```
Expected：无输出（或仅有 E2 方案下 doctor 内自发明的同名常量，此时需人工确认它不来自上游 import）。

- [ ] **Step 8.4：回退 app-boot**

```sh
git rm packages/boot/app-boot/tests/safe-mode.spec.ts
git checkout "$BASE" -- packages/boot/app-boot/src/profile.ts packages/boot/app-boot/src/index.ts
git diff "$BASE" HEAD -- packages/boot/ | head -40
```
Expected：`packages/boot/` 与上游完全一致，diff 为空。

- [ ] **Step 8.5：验证**

```sh
cd apps/desktop-launcher && go test ./... && cd ../..
node apps/desktop-launcher/tools/doctor-verify.mjs
pnpm run test -- --project host packages/boot/app-boot
```
Expected：全部通过。App-boot 回到上游原文，其既有测试不受影响。

- [ ] **Step 8.6：提交**

```sh
git add -A packages/boot apps/desktop-launcher/doctor
git commit -m "refactor(app-boot): 回退安全模式与私有导出，doctor 改用上游公开 API"
```

---

## 7. 阶段 3：门禁下沉与收尾

### Task 9: 下沉 fork 自有工具与门禁

**Files:**
- Move: `scripts/fix-deploy-closure.mjs` → `apps/desktop-launcher/tools/fix-deploy-closure.mjs`
- Move: `scripts/verify-client-ui-i18n.ts` 的 launcher 部分 → `apps/desktop-launcher/tools/verify-launcher-i18n.mjs`
- Modify: `lefthook.yml`, `apps/desktop-launcher/linglong/prepare-offline.sh`, `scripts/run-gates.ts`
- Modify: `scripts/verify-package-readme-model-experience.ts`, `scripts/doc-standard.spec.ts`

- [ ] **Step 9.1：迁移 `fix-deploy-closure.mjs` 并改引用**

`git mv` 后，把 `prepare-offline.sh` 里的 `node scripts/fix-deploy-closure.mjs` 改为新路径。**先 grep 全仓确认没有第二处引用**（预计只有 `prepare-offline.sh`）。

- [ ] **Step 9.2：下沉 i18n 门禁**

把 `scripts/verify-client-ui-i18n.ts` 中针对 launcher 前端（HTML 内联文案 + launcher 壳）的规则抽成 `apps/desktop-launcher/tools/verify-launcher-i18n.mjs`，从 `scripts/verify-client-ui-i18n.ts` 与 `scripts/run-gates.ts` 中删除对应分支与注册。参照既有先例挂 lefthook 作业：

```yaml
    - name: launcher frontend i18n
      glob: 'apps/desktop-launcher/frontend/**'
      run: node apps/desktop-launcher/tools/verify-launcher-i18n.mjs
```

（`launcher frontend layout` 作业已经用同一模式，见 `lefthook.yml`。）

- [ ] **Step 9.3：回退两处白名单**

```sh
git checkout "$BASE" -- scripts/verify-package-readme-model-experience.ts scripts/doc-standard.spec.ts
```
doctor 已不在 `packages/` 下，这两处为它开的白名单自动失效且必须删除。

- [ ] **Step 9.4：跑门禁全量**

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
pnpm run hygiene && pnpm run doc-sync && pnpm run lint && pnpm run typecheck
```
Expected：全部退出 0。

- [ ] **Step 9.5：提交**

```sh
git add -A scripts lefthook.yml apps/desktop-launcher
git commit -m "chore(launcher): 下沉 fork 自有脚本与门禁，回退 scripts 白名单"
```

### Task 10: 锁文件与文档

- [ ] **Step 10.1：重装依赖并单独处理锁文件漂移**

```sh
pnpm install
git status --short pnpm-lock.yaml
git diff pnpm-lock.yaml
```
Expected：理想情况下无差异。若有差异，逐条判断是否由 doctor 摘除引起；**非预期漂移单独一个 commit 还原**（参照 HEAD 上已有的做法）。

- [ ] **Step 10.2：更新文档**

- `apps/desktop-launcher/docs/fork-divergence.md`：其记录的基线已过期（记的是 23 个 A 类 / 304 文件，实测为 55 A + 5 B / 85 文件）。把 doctor 相关条目标记为已消除，并重新校准计数。
- `apps/desktop-launcher/docs/merge-conflict-convergence.md`：把 §6 的收敛选项状态更新为"已执行"，并链接本方案。
- `apps/desktop-launcher/docs/index.md`：注册本文件。
- `apps/desktop-launcher/doctor/README.md` / `README.zh.md`：包定位从"上游树内的支撑包"改为"launcher 私有诊断框架"，并说明非 workspace 的依赖解析方式。
- `docs/superpowers/plans/2026-08-28-dsh-doctor-and-safe-mode.md`：在开头注明其 File structure 一节已被本方案取代，避免后续读者按旧路径找文件。

- [ ] **Step 10.3：最终验收（见 §8）后提交**

```sh
git add -A
git commit -m "docs(launcher): 更新 doctor 迁移后的文档与基线"
```

---

## 8. 验收标准

每一条都要有实际命令输出作为证据，不允许"应该没问题"。

1. **上游树中 doctor 相关偏离归零**
   `git diff "$BASE" HEAD --stat -- packages/ apps/cli tsconfig.base.json tsconfig.host.json scripts/` 的输出中，不再出现任何 doctor / safe-mode 相关文件。
2. **合并上游不产生 doctor 冲突**
   用上游最新提交做一次 `git merge-tree --write-tree` 预演，冲突文件中不含 `apps/cli/*`、`packages/boot/app-boot/src/*`、`packages/support/*`。
3. **红灯门禁消失**
   `node --import tsx/esm scripts/verify-subsystem-pages.ts` 退出 0。
4. **doctor 功能等价**
   `node apps/desktop-launcher/doctor/lib/types/index.js --json --quick` 输出 12 项检查；`--repair 1` 在临时 DSH_HOME 上可完成修复并留下备份。
5. **loader-probe 端到端可用**
   仅在 home 里**至少存在 1 个第三方 bundle** 时，`plugin-dynamic-load` 才会真正 spawn 探针（第三方 bundle 数为 0 时它直接返回 ok，跑不到探针，不构成证据）；此时该检查返回 ok，且 stderr 无 `ERR_MODULE_NOT_FOUND`。
6. **安全模式等价**
   `plugins` 层：harness 启动后第三方插件消失、官方插件与用户数据完好。
   `config` 层：按 D2 选定方案验收。
7. **doctor 测试与覆盖率不退化**
   `node apps/desktop-launcher/tools/doctor-verify.mjs` 通过，用例数与迁移前一致（10 spec / 77 用例），逐文件 100%。
8. **Go 壳测试通过** `cd apps/desktop-launcher && go test ./...`。
9. **打包可用** `sh apps/desktop-launcher/linglong/prepare-offline.sh` 成功，且 stage 内 doctor 与 harness 均可独立运行。
10. **门禁全绿** `pnpm run hygiene && pnpm run doc-sync && pnpm run lint && pnpm run typecheck && pnpm run test:coverage`。

---

## 9. 待决策项（开工前必须定）

| 编号 | 决策 | 选项与取舍 | 建议 |
|---|---|---|---|
| **D1** | doctor 包名 | ①`@dsh-desktop/doctor`：域清楚，但任何现存引用（外部文档、issue、用户脚本）会失配 ②保留 `@deepseek-ai/dsh-doctor`：零失配，但私有包占官方域，排障时易误导 | ①，因为 doctor 不再发布、引用面仅限本仓 |
| **D2** | `config` 安全模式层如何实现 | ①launcher 启动前临时改名 `<profileDir>/cordis.patch.yml` 与 `$DSH_HOME/cordis.patch.yml`，退出后恢复：纯 launcher 侧，但需崩溃安全恢复（进程被杀会留下改名状态） ②向上游提 PR 暴露 `--no-user-layer`（`prepareProfile` 已支持，只差 CLI 参数）：最干净，但依赖上游接受且周期不可控 ③去掉 `config`/`full` 两层，只保留 `plugins`：最简，但用户配置层损坏的场景失去救援手段 | ②作为长期解 + ①作为过渡；若不愿做临时改名，退到③并把 UI 上的两级安全模式相应收成一级 |
| **D3** | `dsh doctor` 是否还需要作为 CLI 子命令存在 | ①移除（本方案默认）：`apps/cli` 6 个文件全部回到上游 ②保留：需继续用 tsconfig path + 门禁白名单把它接回 workspace，等于放弃阶段 1 的一半收益 | ①，因为其唯一消费者是启动器 |
| **D4** | `env-bootstrap-env` 检查如何摆脱 3 个私有导出 | E1 子进程调 `loadLayeredEnv`：零复制、口径即上游真值，代价是一次子进程 ②E2 生成式常量副本 + 校验脚本：保持纯函数，代价是多一个 TS 源码解析器 | E1 |
| **D5** | `scripts/fix-deploy-closure.mjs` 是否随之下沉 | ①下沉（本方案默认） ②留在 `scripts/`：不影响 doctor 冲突面，但 `scripts/` 里的 fork 文件本身就是一类需要盘点的偏离 | ① |
| **D6** | 本方案的执行方式 | ①subagent 逐任务 ②当前会话内分阶段执行 | ① |

---

## 10. 回滚策略

- **阶段 1** 是一次目录搬迁 + 一批回退，`git revert` 单个 commit 即可回到出发点；doctor 在 `packages/` 与在 launcher 内的源码完全相同（除 Step 1.2/1.3 两处），回滚不会丢功能。
- **阶段 2** 是唯一有行为变更的阶段。它在阶段 1 全部验收通过之后才开始，且 Task 6 先落地 overlay 生成器并有测试护住；Task 8 回退 app-boot 之前，Task 7 已让 launcher 走通新路径，因此任何时刻都只有一个安全模式实现是活跃的。
- **`config` 层若选 ①（临时改名）**，必须在实现里带崩溃安全恢复：启动时若发现"改名状态残留且 harness 未运行"，无条件恢复原名。这一条要有专门的测试，否则一次断电就会让用户的 `cordis.patch.yml` 永久失踪。

---

## spike 结果

**阶段 0 已于 2026-09-24 全部执行完毕，三条全绿。**

| 检查 | 结论 | 证据 |
|---|---|---|
| Step 0.1 依赖在闭包内 | ✅ 通过 | doctor 的 7 个依赖（`dsh-app-boot`、`dsh-cmdline`、`dsh-home-paths`、`dsh-atomic-write`、`dsh-web-app`、`cordis-plugin-include`、`js-yaml`）在 `linglong/stage/harness/node_modules/` 下全部存在；且逐个统计非 doctor 消费者为 50 / 19 / 24 / 17 / 6 / 93 / 20，**没有任何一个是"只因 doctor 才在闭包里"**，doctor 迁走后闭包不会缺件 |
| Step 0.2 非 workspace 解析链路 | ✅ 通过 | 在临时目录用符号链接模拟 `doctor/node_modules`，`import '@deepseek-ai/dsh-app-boot'`、`dsh-cmdline`、`cordis-plugin-include`、`js-yaml` 全部解析成功，`require.resolve('@deepseek-ai/dsh-web-app/package.json')` 取得 install anchor，`loadProfile` 正常返回 9 层 profile |
| Step 0.3 `--patch` 禁用第三方 entry | ✅ 通过（**规则需修正**） | `--dump-config` 对比：第三方 `insert` 行的 8 个 entry 全部被渲染为 `disabled: true`，官方 entry 零触碰，`disabled: true` 总数 41 → 49 恰等于目标数 |

### spike 0.3 的两条衍生结论

1. **禁用规则必须是「只禁第三方 `insert` 的 entry」**，不能禁它们 `patch` 的 `id`。朴素规则在本机会误禁官方 `workspace` / `ui-workspace` / `session-projection-cache` 三个 entry，把 Web 界面打坏。§3.3 与 Task 6 Step 6.2 已按此修正。
2. **`bisect.ts` 存在既存缺陷**：它只遍历 `patch.id`、不看 `patch.insert[].id`，因此在本机会去禁用官方 entry，同时完全禁用不掉第三方的插件——bisect 的二分结果目前是不可信的。Task 8 改走 loader-probe `--include` 正好绕开 entry 级判断，是修这个缺陷的正确路径。

### 对后续阶段的影响

- 阶段 1 无需因 spike 调整。
- 阶段 2 的 `plugins` 层实现路径已确认可行；`config` 层仍需 D2 决策。
- 新增一条实施要求：`safe-mode-overlay.mjs` 的测试必须包含"官方 entry 不被触碰"的回归断言（已写入 Step 6.2）。

---

## 阶段 1 执行记录

**阶段 1 的 Task 1–5 与 §5.5 的 Step 5.1–5.3 已执行完毕，并已作为单个原子提交落成**（`refactor(doctor): 把 doctor 迁出上游 workspace，改由启动器直连`，54 个文件）。

§5 的复选框反映真实执行状态，有两类例外需说明：

- **5 个提交步骤（1.5／2.5／3.6／4.5／5.3）有意未执行**。§5 开头已规定"不要逐 Task 提交"——doctor 一旦离开 workspace，`apps/cli` 与两处 tsconfig 立即无法构建，逐 Task 提交会在历史里留下坏提交。这 5 步只作为工作进度检查点，实际工作在同一工作树内连续完成，最后一次性提交。
- **Step 2.3 与 Step 2.4 已决策"不做"**。Step 2.3 的前提（doctor 有 100% 覆盖率纪律）经实测不成立——实测 66.36%；它原本要承接的 `bisect.ts` 也已查明是无调用方的死代码并删除。Step 2.4 的最后一条命令依赖 Step 2.3，故一并记为不做。见下节。

### 已完成并验证的部分

| 项 | 状态 | 证据 |
|---|---|---|
| `packages/support/doctor` → `apps/desktop-launcher/doctor` | ✅ | `git status` 记录 25 条 `R`（重命名），历史保留；`packages/support/` 空目录已移除 |
| `verify-subsystem-pages` 红灯转绿 | ✅ | 迁移前 exit 1（`packages/support/README.md` 缺 group README），迁移后 `54 group(s) checked, all conform` exit 0 |
| 包清单改写为 `@dsh-desktop/doctor` + `private` | ✅ | 依赖用 `*` 声明，实际解析由 `tools/doctor-link-deps.mjs` 建立链接（7 个链接，幂等） |
| 依赖链接工具 | ✅ | 按 `package.json` 的 `name` 匹配而非目录名——`@deepseek-ai/cordis-plugin-include` 的目录是 `vendor/include`，按目录名推导必然漏掉 |
| `loaderProbeEntry` 双运行面解析 | ✅ | 见 §3.2；只留产物面的版本实测导致 9 个测试失败 |
| tsconfig `references` 路径修正 | ✅ | 见 F12′ |
| `apps/cli` 与两处 tsconfig 回到上游 | ✅ | `git diff "$BASE" -- apps/cli/ tsconfig.base.json tsconfig.host.json` 输出为空，doctor 字样 0 处 |
| doctor 构建 | ✅ | `tools/doctor-build.mjs` exit 0，三个入口齐全；从零重建（删 `lib`）0 污染 |
| doctor CLI 端到端 | ✅ | `--json --quick` → 11 项检查、exit 0（quick 按设计跳过加载探测）；`--json` → 12 项检查、exit 0——但该夹具下 `plugin-dynamic-load` 走的是"第三方 bundle 数为 0"的短路分支，**不构成探针跑成功的证据**，探针另以直跑方式验证（见 V7）；`--repair 9` → exit 2 |
| doctor 测试套件 | ✅ 73/73 | 修复 loader-probe 的 home 隔离后全绿，见下方"测试套件的唯一失败" |

### Step 2.3 未完成：其前提经实测不成立

这是阶段 1 **唯一一项计划内但未实施的工作**。原方案要求在 `apps/desktop-launcher/tools/doctor-verify.mjs` 里"把逐文件 100% 覆盖率门槛搬过来"，依据是这句话：

> 不补这一层，doctor 现有的 10 个 spec / 77 个用例与 100% 覆盖率纪律会无声消失。

**实测表明 doctor 从来没有 100% 覆盖率纪律**，因此"把 100% 门槛搬过来"无法实现——它会在 10 个文件里的 8 个上立即失败。

#### 实测覆盖率

测量方式：按根 `vitest.config.ts` 的覆盖率口径忠实复现（同样的 `setupFiles`、`pool: 'forks'`、仓库根相对 `include`），收集范围为 doctor 的 tests。两次独立运行结果逐位一致。

```sh
# 临时配置等价于根 config 的 coverage 段 + doctor 的 test.include
vitest run --config <等价配置> --coverage --coverage.reportOnFailure=true
```

> `--coverage.reportOnFailure=true` 必须加：vitest 4 里该选项默认 `false`，测试一旦失败就不输出覆盖率表，会让人误以为覆盖率没跑。

| 文件 | % Stmts | % Branch | % Funcs | % Lines |
|---|---:|---:|---:|---:|
| `auto-disabled.ts` | **100** | 100 | 100 | 100 |
| `checks/config.ts` | **100** | 100 | 100 | 100 |
| `bisect-by.ts` | 94.44 | 87.5 | 100 | 100 |
| `checks/plugins.ts` | 91.84 | 79.48 | 100 | 92.3 |
| `index.ts` | 90.54 | 85.71 | 83.33 | 90.62 |
| `checks/env.ts` | 90.47 | 85.71 | 100 | 90.24 |
| `checks/data.ts` | 86.81 | 74.41 | 88.88 | 94.87 |
| `bisect.ts` | **32.69** | 33.33 | 33.33 | 35.41 |
| `cli.ts` | 0 | 0 | 0 | 0 |
| `loader-probe.ts` | 0 | 0 | 0 | 0 |
| **合计** | **66.36** | 52.28 | 68.22 | 67.66 |

`cli.ts` 与 `loader-probe.ts` 是自执行入口，进程内测试看不到它们——仓库对同类文件的处理是**排除**而非覆盖：根 `coverage.exclude` 里有 `packages/*/*/src/bin.ts`、`packages/*/*/src/worker.ts`、`packages/ptc-runtime/ptc-runtime-node/src/process-entry.ts`，注释给出的理由正是"导入自执行入口会在单元进程里启动它，其薄入口由真实子进程测试覆盖"。

#### 迁移前后它到底有没有被门禁约束

**迁移前：名义上在门禁内。** doctor 位于 `packages/support/doctor/`，其 `src/**` 命中根 `coverage.include` 的 `packages/*/*/src/**/*.{ts,tsx}`，tests 命中 `testIncludes` 的 `packages/*/*/tests/**/*.spec.{ts,tsx}`；根 `coverage.exclude` 的 92 条里只有 `packages/*/*/src/types.ts` 匹配到它（排除 types-only 文件），没有任何 doctor 专属豁免。所以按配置它受逐文件 100% 约束。

**但它的实测覆盖率是 66.36%，不可能通过那道门禁。** 由此只能是两种情况之一：要么 fork 的覆盖率 job 从未真正跑过（`ci.yml` 只在 `pull_request` 触发、`ci-master.yml` 只在 `master` 触发，而本仓库工作在 `linglong-dev`，离线无法判定是否开过 PR），要么它一直是红的。无论哪种，**当时都不存在一份"已满足的 100% 纪律"可供失去**。

**迁移后：测试照跑，门禁不再适用。** doctor 的 9 个 spec / 73 个用例仍会被执行——`apps/desktop-launcher/doctor/vitest.config.ts` 让根配置收集到它（该文件顶部注释解释了原因）。失去的只是覆盖率这一层的约束。原方案把它描述为"保护力下降"，方向正确，但**高估了失去的东西**。

#### 顺带查出的真实测试盲区

`bisect-by.ts` 是通用二分框架（9 个用例、94.44% 覆盖）；`bisect.ts` 是把该框架接到真实 bundle 上的编排层，但 `bisect.spec.ts` 的 3 个用例**只走了 3 条提前返回路径**（无第三方 bundle／全部启用也能加载／全禁用仍失败）。真正"找出罪魁 bundle"的路径——第 154 行的全启用基线判断、第 164 行起的 `bisectBy` 委托与结果组装——**没有任何测试**，这正是 `bisect.ts` 只有 32.69% 的原因。

**这与覆盖率门槛无关，是独立的功能盲区**：doctor 的核心能力（定位哪个第三方 bundle 导致 profile 加载失败）没有被端到端断言过。

#### 处置：已决策不设门槛，并删除死代码

**决策：选上面第 3 项。** doctor 的测试继续跑，覆盖率不再约束；[merge-conflict-convergence.md](./merge-conflict-convergence.md) 的代价①记为"已显式接受"。

**关于上节 `bisect.ts` 盲区，其根因不是"测试没写全"，而是该模块已无人调用**——补 happy-path 测试没有意义，改为删除：

- 全仓源码面搜 `bisectThirdPartyBundles` 只命中它自己的定义与自己的 spec。真正定位元凶的是 `checks/plugins.ts` 的 `locateCulprit()`，它直接 import `bisect-by.js` 并自己持有编排。
- `bisect.ts` 用 `loadProfile(..., '')` 判定失败，而 `loadProfile` 的 `home` 是**默认参数**：传空串不触发默认值，`''` 被按字面使用，于是 profile 目录变成 **cwd 相对**的 `profiles/web`，`initProfile` 还会在 cwd 里把它创建出来。实测：删掉仓库根的 `profiles/` 后单独跑该 spec，目录被重新创建（它在 `.gitignore` 内，所以一直没有暴露）。更关键的是，`loadProfile` 只做组合期判定，看不见插件模块 import 失败——而这正是该 check 存在的理由，`locateCulprit` 因此改用 probe 的真实启动。
- 该 spec 的 3 个用例是同一个调用的三份近似重复，全部走"无第三方 bundle"这一条提前返回；其中一个用例的名字（"profile fails even with all disabled"）与它实际测的分支不符，`attempts >= 0` 则是恒真断言。

已删除 `src/bisect.ts`（190 行）与 `tests/bisect.spec.ts`（46 行），保留 `bisect-by.ts`（`checks/plugins.ts` 在用），并同步了 [Agent note](../../../.agents/notes/implemented/feature/2026-08-28-doctor-plugin-dynamic-load.md) 里"`bisectThirdPartyBundles` 改为委托给它"这句已不成立的话。

> **Step 2.3 与 Step 2.4 因此记为"不做"**：Step 2.3 原本要承接的那个模块已被删除，逐文件 100% 门槛又因前提不成立而无法实现。§5 里这两个步骤的复选框保持未勾选。

### Task 3：Go 壳直连（已完成）

`preflight.Runner` 不再组装 `dsh doctor` 的 argv，改为 `node <doctor>/lib/types/cli.js` 直连。连带改动与取舍：

- **doctor 的位置独立解析**（`appenv.resolveDoctor()`）：doctor 既不挂在 dsh 的 argv 上，位置就与 dsh 入口脚本无关，必须独立探测。优先级为环境变量覆盖 → 打包态 `<prefix>/harness/doctor` → 源码态 `<cwd>/doctor`，与 `Resolve()` 既有的四条分支同一前提（源码态 cwd 是 `apps/desktop-launcher`）。
- **删掉 `.js` 后缀启发式**：`app.New` 原先靠 `cfg.Args[0]` 是否以 `.js` 结尾反推"Command 是 node 还是 dsh 本体"。这条推断只在 `DSH_DESKTOP_DSH_BIN` 分支之外成立，现在由 `appenv` 直接给出 `DoctorNode`，推断消失。
- **argv 单一来源**：`Runner.Command(extra...)` 同时返回可执行文件、完整 argv 与"doctor 是否已配置"。app 层的 doctor 面板与预检共用它，环境与 argv 不会分叉。`App` 的 `dshCmd`/`dshScript` 两个字段因此全部删除，而不是改名保留。
- **新增 `DoctorNotConfigured` 归类**：与 `DoctorNoOutput` 分开。前者是"根本没跑起来"（安装缺件），后者是"跑了但没说话"，排查方向不同，合并会把用户引向错误方向。中英文案同步补齐。
- **顺带修掉的旧瑕疵**：`RunDoctorRepair` 原先用 `args[len(args)-1] = "2"` 改写 argv 末项来表达修复等级，现改为直接构造等级并收敛到 1/2/3。
- **不硬失败**：doctor 缺失时 `runDoctor` 返回归类错误，调用方照既有注释"预检是尽力而为的前置检查，doctor 本身失败绝不阻塞启动"放行。这是原有设计，不是本次新增的兜底。

证据：`go vet ./...` exit 0；`go test ./...` 全部包 ok；`resolveDoctor` 新增 5 条测试（环境覆盖、只覆盖 node、源码态探测、缺失返回空 CLI，以及一条**对着真实目录树**跑 `Resolve()` 的集成断言，确认源码态解析到 `apps/desktop-launcher/doctor/lib/types/cli.js`）。

`gofmt -w` 曾顺带格式化 3 个与本次无关的文件（`internal/terminal/{session,types}.go`、`internal/toolchain/install_test.go`，均为既存空白漂移），已 `git checkout` 回退以保持改动面最小。

### Task 5：打包链路（已完成）

`linglong/prepare-offline.sh` 新增两步：1.1 步调用 `tools/doctor-build.mjs`（`pnpm run build` 不会编译 doctor——它既不是 pnpm workspace 成员，也不在 tsdown 的 globs 里），2.2 步把 `lib/types` 与 `package.json` stage 到 `$STAGE/harness/doctor`。

**关键设计：doctor 必须 stage 在 harness 树内部，而不是旁边。** 这样 doctor 对 `@deepseek-ai/dsh-app-boot` 等包的 import 靠 Node 逐级向上查找即可命中 `$STAGE/harness/node_modules`，无需任何符号链接或额外安装步骤；放到 harness 外面就得自己造一套解析链。

另加三个入口（`index.js`/`cli.js`/`loader-probe.js`）的存在性断言：缺入口是**最难排查的失败形态**——构建与打包都成功，launcher 起来后才在运行期报错，而预检失败按设计不阻塞启动，用户只会看到"检查被跳过"。

证据：`bash -n` 语法通过；用**真实依赖闭包**做忠实模拟（`harness/node_modules` 指向 `doctor/node_modules`），staged `cli.js` 成功跑出 11 项检查与合法 JSON，证明向上查找成立。

### 文档同步（Task 1 的收尾，含一处迁移引入的真实缺陷）

doctor 的 `README.md`/`README.zh.md`/`README.i18n.yaml` 原先文档化的是已删除的 `dsh doctor` 子命令与 `@deepseek-ai/dsh-doctor` 包名，属"代码改了文档没改"的硬伤，已同步改写（消费者、入口、CLI 调用方式、源码地图新增 `src/cli.ts`、开发备注说明 fork 私有身份），双语行数对齐（均 126 行）并重录配对哈希。

**双语配对门抓到一处迁移引入的真实缺陷**：README 里的相对链接深度失效——`../../boot/app-boot/README.md` 从 `packages/support/doctor/` 出发是对的，从 `apps/desktop-launcher/doctor/` 出发会指向不存在的 `apps/boot/`。已改为 `../../../packages/boot/...`，32 条链接（双语各 16 条）逐条验证目标存在。

门禁：`verify-translation-pairing`（doctor 这一对无告警）、`verify-repository-references`、`verify-subsystem-pages`、`verify-package-readme-limitations`（308 → 307，正是 doctor 移出 `packages/` 所致）、`verify-package-invariants` 全部 exit 0。剩余配对告警来自 `linglong/stage`、`linglong/output`、`linglong/overlay` 下的构建产物与 overlay，均为 gitignore 覆盖的未受控目录，属既存环境噪音。

### Task 4 的范围遗漏：两处 gate 脚本的 doctor 白名单

Task 4 只列出了 `apps/cli/*` 与两处 tsconfig，实测**漏了两个上游脚本**：

- `scripts/doc-standard.spec.ts` 的 `PACKAGE_LIBRARIES` 表
- `scripts/verify-package-readme-model-experience.ts` 的 `NO_MODEL_EXPERIENCE_SECTION` 表

两张表都以 `packages/<组>/<包>` 路径为键（fork 在创建 doctor 时各加了一行）。doctor 搬走后键指向不存在的路径，两个门禁立刻转红：前者断言 `src/index.ts` 存在（`expected false to be true`），后者报 `no-section allowlist entry does not name a scanned package`。

**教训**：这两张表是"按路径索引包"的白名单，属于**容易被漏掉的隐性上游接线**。只按目录搜索接线点（`apps/cli`、tsconfig）会漏掉它们；可靠的做法是**改完后按旧路径全仓搜索**，而不是只搜索已知的接线文件。

处理方式是**删除**而非改指：两个门禁都只扫描 `packages/*/*`，`apps/desktop-launcher/doctor` 在其范围之外，改指新路径只会让门禁再报一次"不是被扫描的包"。删除后两个脚本逐字节回到上游（相对 `upstream/master` 的 diff 为空），上游偏离文件数因此从 9 降到 7——这正是提交信息里"这 7 个文件回到上游基线"的来源。

### 锁文件：必须同批修改，且要防止非预期漂移

`pnpm-lock.yaml` 有三处 doctor 残留（`apps/cli` 的 importer 依赖声明、`packages/support/doctor` 条目、仅被 doctor 的 devDependencies 引用的那个 vitest 快照）。不改会让 `pnpm install --frozen-lockfile` 报 `ERR_PNPM_OUTDATED_LOCKFILE`，而 `prepare-offline.sh` 是 `set -eu`，打包链路会直接断在这里。

**但直接跑 `pnpm install --lockfile-only` 会引入非预期漂移**——实测它同时加回了 `glob@7.2.0` 条目、把 `rimraf@2.6.3` 的依赖边从 `glob 7.2.3` 改成 `7.2.0`、并刷新了 `glob@7.2.3` 的 deprecated 文案。这三处与上一个提交（`fix(lock): 还原 pnpm-lock 的两处非预期漂移`）修掉的**完全同类**，是该提交正文明确记录过的回归。

处理方式是**以 pnpm 的输出为准，只还原这三处漂移**：最终锁文件改动为 68 行纯删除、零新增；`pnpm install --frozen-lockfile --lockfile-only` 校验 exit 0 且锁文件哈希不变，证明与清单一致。

**这条经验对后续阶段通用**：本仓库的 `pnpm install` 在当前镜像下会稳定引入 glob/rimraf 漂移，任何需要动锁文件的操作都要事后逐条核对 diff。

### 测试套件的唯一失败：已定位并修复（迁移前即存在的缺陷）

`tests/loader-probe.spec.ts > loads a fresh profile with no third-party bundles (exit 0)` 曾长期期望 exit 0、实得 1。

- **根因（已取到子进程 stderr）**：`EROFS: read-only file system, open '/home/Jokul/.dsh/.credentials.yaml.lock'`。web profile 的**必需**插件 `@deepseek-ai/dsh-client-connection` 经 `packages/util/atomic-write` 的文件锁写凭据文件，而它**自行解析** harness home（`resolveDshHome()`：显式参数 → `DSH_HOME` → `~/.dsh`），拿不到探针的 `--dsh-home`。`runLoaderProbe` 与测试又都主动 `delete env.DSH_HOME`，于是它落到用户真实的 `~/.dsh`；沙箱把它设为只读，写入被拒，必需插件激活失败，探针以 exit 1 退出。
- **手工照抄命令行反而 exit 0 的原因**：vitest 之外探针解析不出官方 bundle `@deepseek-ai/dsh-base`／`@deepseek-ai/dsh-web-app`（报 `cannot resolve profile bundle ... from the dsh installation`）而把它们 skip，`client-connection` 从未加载，也就不会去碰凭据文件。vitest 内 tsx 经仓库 tsconfig 的 paths 解析到了它们，完整 profile 才真正启动。
- **同一根因会让 doctor 误诊**：home 不可写且用户装了第三方 bundle 时，探针失败被解读成"树没起来"，`bisectBy` 再去二分——每个子集都因同一个 EROFS 失败，于是把**完全健康**的第三方 bundle 指为元凶，报告 `fixable: true`、`suggestedLevel: 2`，L2 修复据此停用它。包内 doctor 实测复现：`插件 third-party-healthy 导致启动失败（缺少运行依赖或损坏）`，而只挂它时 stderr 零次提及该 bundle、只有 EROFS。
- **修复**：探针在 boot 前把 `DSH_HOME` 导出给整棵树（§3.2 同级解析的同一处），让所有组件解析到同一个 home。修复后：探针 exit 0、凭据落进临时 home、用户真实 `~/.dsh/.credentials.yaml` 的 mtime 不变（隔离真正生效）、`plugin-dynamic-load` 对同一夹具由"指认无辜 bundle"变为 `ok: true`、doctor 套件 **73/73 全绿**。
- **这不是迁移引入的问题**：该用例相对迁移前唯一变化是 `loader-probe.ts` 的 `@module` JSDoc 标签，行为未变。

### 一条未能归因的观察（如实记录）

阶段 1 执行过程中，`git status` 一度出现 **216 个未跟踪文件**（54 个 `.js` + 108 个 `.map` + 54 个 `.d.ts`），分布在 `packages/llm/llm/src`、`packages/boot/app-boot/src`、`packages/attachment/attachment/src` 等约 10 个包的 `src/` 下，mtime 统一为 15:57。其数量关系（54 源文件 × 4 种产物扩展名）与"某个缺 `outDir` 的配置就地 emit"完全吻合。

已全部清理。**未能复现，也未能归因到任何一条执行过的命令**，排查记录如下：

- `tsc -b` 被排除：`tsc -b --dry` 显示它只涉及 23 个项目，**全部都有 `outDir`**；对 `packages/util/values` 与 `packages/boot/app-boot` 分别做 `--force` 全量重建均 0 污染；且产物时间戳显示 15:57 只有 doctor 自己被构建（其余项目当时被报 "up to date"，`lib/types` mtime 为 11:05 或更早）。
- 引用写错的那版 tsconfig（首轮 `TS6053`）重跑 0 污染。
- `verify-subsystem-pages` 门禁重跑 0 污染。

**已采取的防御措施**：`tools/doctor-build.mjs` 从 `tsc -b` 改为 `tsc -p`（§5.5 Step 5.3），doctor 的构建不再遍历引用图，这条路径被彻底移除。**若后续在干净检出上再次出现同类产物，应视为未解决的构建环境问题单独排查。**

## 阶段 1 真机验证清单（本沙箱无法执行）

本沙箱**无 `gcc`、无 `pkg-config`**，wails（webkit2gtk-4.1）构建不可能；`prepare-offline.sh` 第 1 行的 `pnpm install --frozen-lockfile` 还会因默认 pnpm store 只读而报 `ERR_SQLITE_ERROR`。因此下列验证**必须在真机或 CI 上跑**，是阶段 1 唯一尚未闭环的部分。

按顺序执行；任一步失败即停，不要跳到下一步——后面的步骤都以它的结果为前提。

### V1：锁文件与清单一致（最易踩的一步）

```sh
pnpm install --frozen-lockfile
```

Expected：exit 0。本次改动从 `apps/cli/package.json` 摘掉了 doctor 依赖，锁文件必须同步摘掉 `apps/cli` 的 importer 声明、`packages/support/doctor` 条目与仅被 doctor devDeps 引用的 vitest 快照。

若报 `ERR_PNPM_OUTDATED_LOCKFILE`，说明锁文件与清单不一致。**不要直接跑 `pnpm install --lockfile-only` 了事**——实测它会在本仓库稳定引入 `glob@7.2.0` / `rimraf@2.6.3` 依赖边 / `glob@7.2.3` deprecated 文案三处非预期漂移（见上文「锁文件」节的取证）。正确做法是只修 doctor 相关条目。

### V2：根构建未被"移出 workspace 成员"破坏

```sh
pnpm run build
```

Expected：exit 0。

这是本次提交唯一的**结构性**风险：从 330 个 workspace 项目里移除了一个成员。已取得的廉价证据是 `tsconfig.host.json` 的 267 条 project reference **悬空 0 条**、构建配置里 doctor 引用 **0 处**，但廉价证据不等于跑过构建。

（本沙箱已单独跑过决定性的那一步 `tsc -b tsconfig.host.json`，结果见下文「根构建实测」。`build:native-system` 与 `build:web` 与 doctor 无关，未跑。）

### V3：doctor 单独构建

```sh
rm -rf apps/desktop-launcher/doctor/lib
node apps/desktop-launcher/tools/doctor-build.mjs
ls apps/desktop-launcher/doctor/lib/types/{index,cli,loader-probe}.js
```

Expected：三个入口均存在；同时 `git status --porcelain` 在 `packages/` 与 `vendor/` 下**不出现任何未跟踪文件**（若有，说明 `tsc -p` 仍在污染上游包，属未解决的构建环境问题）。

### V4：doctor 在源码面跑通

```sh
node apps/desktop-launcher/doctor/lib/types/cli.js --json --quick | head -c 200
node apps/desktop-launcher/doctor/lib/types/cli.js --json | head -c 200
node apps/desktop-launcher/doctor/lib/types/cli.js --repair 9; echo "exit=$?"
```

Expected：第一条 11 项检查（quick 跳过 `plugin-dynamic-load`）；第二条 12 项检查且 `plugin-dynamic-load` 成功 spawn（**不得出现 `ERR_MODULE_NOT_FOUND`，也不得依赖 tsx**）；第三条打印用法错误并 `exit=2`。

### V5：真实打包

```sh
bash apps/desktop-launcher/linglong/prepare-offline.sh
```

Expected：exit 0，且

```sh
ls apps/desktop-launcher/linglong/stage/harness/doctor/lib/types/{index,cli,loader-probe}.js
```

三个入口齐全。缺入口时脚本会显式报错退出——这条断言是刻意加的，因为缺入口只在运行期暴露，而预检失败按设计不阻塞启动，用户只会看到"检查被跳过"。

### V6：打包态的依赖解析（本设计的核心假设）

```sh
cd apps/desktop-launcher/linglong/stage/harness
DSH_HOME=$(mktemp -d) ./node/bin/node doctor/lib/types/cli.js --json --quick | head -c 200
```

Expected：合法 JSON，**不得出现 `ERR_MODULE_NOT_FOUND`**。

这一条验证的是本设计的关键假设：doctor 放在 harness 树**内部**，因此它对 `@deepseek-ai/dsh-app-boot` 等包的 import 靠 Node 逐级向上查找即可命中 `harness/node_modules`，无需任何符号链接。本沙箱已在**真实打包闭包**上验证过：包内 `cli.js --json --quick` 输出合法 JSON（11 项、exit 0），包内 `loader-probe.js` 直跑也能把官方 bundle 全部解析出来（口径见 V7）。

### V7：打包态启动 + 预检真跑

装出玲珑包后启动 launcher，确认：

- 预检阶段正常出现并跑出报告（而不是 `PreflightError`）
- 日志/界面里 doctor 的路径解析到 `<prefix>/harness/doctor/lib/types/cli.js`

**验收口径（曾读错，务必按此读）**：`plugin-dynamic-load` 返回 ok **不等于**探针跑成功——`locateCulprit`（`src/checks/plugins.ts`）在第三方 bundle 数为 0 时直接返回 `fullOk: true`，根本不 spawn 探针。要真正验收"同级解析、打包态不依赖 tsx"，必须满足二者之一：让 home 里至少有 1 个第三方 bundle 再跑该检查；或直跑 `<prefix>/harness/doctor/lib/types/loader-probe.js --dsh-home <临时 home>`，要求 exit 0 且 stderr 无 `ERR_MODULE_NOT_FOUND`。后者已在本沙箱对真实打包闭包执行过：官方 bundle 全部解析成功、完整 web profile 启动、依赖命中 `harness/node_modules/@deepseek-ai/dsh-atomic-write`——**这条结论来自直跑，不来自检查项返回 ok**。

### V8：反向验证——doctor 缺失时必须降级而非阻塞启动

```sh
mv <prefix>/harness/doctor <prefix>/harness/doctor.bak
# 启动 launcher
mv <prefix>/harness/doctor.bak <prefix>/harness/doctor
```

Expected：launcher **正常启动**，预检显示"未找到随包的 doctor，本次跳过启动前检查"（`preflight.doctor.notConfigured`），harness 照常放行。

这是本次新增 `DoctorNotConfigured` 归类的验收路径。它与 `DoctorNoOutput` 分开归类，是因为"根本没跑起来"（安装缺件）与"跑了但没说话"排查方向完全不同；合并会让用户被引向错误方向。**验证要点是"不阻塞启动"**——预检是尽力而为的前置检查，这是既有设计，不是本次新加的兜底。

### V9：doctor 测试套件全绿

```sh
cd apps/desktop-launcher/doctor && ../../node_modules/.bin/vitest run
```

Expected：73/73 全绿。

**已在沙箱内通过**：修复 loader-probe 的 home 隔离后，9 个 spec／73 个用例全绿。根因、影响与修复见上方"测试套件的唯一失败"。本项因此从"必须找一台 `~/.dsh` 可写的机器复核"降为重新打包后的回归确认——原先那条失败与真实 home 是否可写无关，而是探针把 home 解析到了真实用户目录。

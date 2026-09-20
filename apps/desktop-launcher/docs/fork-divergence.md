# fork 上游偏离审计与收敛方案

> **目的**：盘点本 fork 相对上游 `deepseek-ai/deepseek-harness` 的**全部偏离**，判断哪些能收敛进 `apps/desktop-launcher/`，哪些不能。
> **范围**：只做审计与统计。**本文档不包含任何代码改动**；文中的处置建议均为待批准的方案。
> **分工**：启动器**自身**的缺陷由 [AUDIT.md](./AUDIT.md)（759 行，N3/N17/S1–S7 等编号发现）负责，本文档只负责"fork 与上游的偏离"这一角度，两者互补不重复。

---

## 一、审计基准

| 项 | 值 |
|---|---|
| 本地 HEAD | `e8fbf5a08fbb24714b41523d5a5dc91cfefa6680` |
| 分支 | `linglong-dev` |
| 上游参照 | `upstream/master` = `ddefc45fbc7f`（2026-09-17 21:19:19 +0800，`release-dsh-0.1.6-alpha.2`） |
| 合并关系 | `git merge-base --is-ancestor upstream/master HEAD` → **真**（上游 master 已全部包含） |
| fork 领先上游 | 441 个提交 |

复现命令：

```sh
git diff --shortstat upstream/master...HEAD
git diff --name-status upstream/master...HEAD
```

> **基准有效性**：`git ls-remote upstream refs/heads/master` 返回 `ddefc45fbc7f8e46dd73185e68295696d1297887`，与本地 `upstream/master` **完全一致**，故本地参照不是陈旧引用，下文的改动热度排序与"当前无冲突"结论均成立。

---

## 二、总量统计

`304 个文件，+44438 行，−96 行`，按区域分布：

| 区域 | 文件数 | 新增 | 删除 | 性质 |
|---|---:|---:|---:|---|
| `apps/desktop-launcher` | 136 | +29136 | −0 | C 类·预期归处 |
| `docs/superpowers` | 11 | +6843 | −0 | C 类·独立新目录 |
| `.agents` | 105 | +3647 | −0 | C 类·独立新目录 |
| `packages/support`（doctor） | 24 | +3562 | −0 | C 类·独立新目录 |
| `packages/client` | 11 | +493 | −14 | **A/B 类·触碰上游** |
| `apps/cli` | 5 | +207 | −2 | **A/B 类·触碰上游** |
| `packages/boot` | 3 | +143 | −11 | **A/B 类·触碰上游** |
| `packages/host` | 3 | +50 | −3 | **A 类·触碰上游** |
| `scripts/fix-deploy-closure.mjs` | 1 | +152 | −0 | **B 类·触碰上游** |
| `pnpm-lock.yaml` | 1 | +167 | −65 | **A 类·机械** |
| `.gitignore` | 1 | +20 | −0 | **A 类·配置** |
| `lefthook.yml` | 1 | +13 | −1 | **A 类·配置** |
| `tsdown.config.ts` | 1 | +4 | −0 | **A 类·纯注释** |
| `tsconfig.host.json` | 1 | +1 | −0 | **A 类·配置** |

按文件状态分：**新增 281 个，修改 23 个，删除 0 个**。

账目校验（与 `--shortstat` 完全一致）：

```
C 类   276 文件      +43188
A 类    23 文件      +618  −96
B 类     5 文件      +632
────────────────────────────────
合计   304 文件      +44438 −96   ✓
```

---

## 三、偏离分类定义

| 类别 | 含义 | 上游同步代价 | 数量 |
|---|---|---|---|
| **A** | 修改了上游**已有**文件 | **必然文本冲突**，需人工 merge | 23 |
| **B** | 新增文件，但落在上游**已有目录**里 | 不冲突，但会被上游重构/门禁扫到，且属"上游目录里的自有代码" | 5 |
| **C** | 独立新目录 | 不冲突 | 276 |

---

## 四、A 类：23 个被修改的上游文件

合计 `+618 −96`。**冲突风险**按"上游最近 300 次 master 提交所覆盖窗口（含合并共 3135 个提交）内改动该文件的次数"排序：

| 风险 | 上游改动次数 | 文件 | 改动 | 做了什么 |
|---|---:|---|---|---|
| 极高 | 447 | `pnpm-lock.yaml` | +167 −65 | 机械变更（新增 workspace 包） |
| 极高 | 195 | `tsconfig.host.json` | +1 | 引用 `packages/support/doctor` |
| 高 | 51 | `packages/boot/app-boot/src/index.ts` | +3 −3 | 把 `BOOTSTRAP_NAMES`/`BOOTSTRAP_PREFIXES`/`isBootstrapOnly` 改为导出 |
| 高 | 45 | `apps/cli/package.json` | +1 | 加 `@deepseek-ai/dsh-doctor` 依赖 |
| 高 | 28 | `packages/client/ui-conversation/src/client/skeleton/InputBar.tsx` | +59 | 壳内剪贴板图片粘贴（window 捕获阶段 paste 监听） |
| 中 | 14 | `packages/client/modules/src/index.ts` | +23 −4 | `newlineCount` 性能优化 + `resolveSync` 跨 Node 版本兜底 |
| 中 | 12 | `packages/boot/app-boot/src/profile.ts` | +52 −8 | **安全模式** `DSH_SAFE_MODE` + `skipThirdPartyBundles` + `extraPatchFiles` |
| 中 | 12 | `apps/cli/src/bin.ts` | +6 | `case 'doctor'` 分发 |
| 中 | 8 | `packages/client/modules/tests/node-half.client.spec.ts` | +81 | 上述改动的测试 |
| 中 | 8 | `apps/cli/src/args.ts` | +56 −2 | `doctor` 子命令定义 + 排除首参展开 |
| 中 | 7 | `packages/client/ui-attachment/tests/message-image.client.spec.tsx` | +1 −1 | 标签补齐 |
| 中 | 6 | `apps/cli/tests/args.spec.ts` | +10 | doctor 参数测试 |
| 中 | 5 | `.gitignore` | +20 | 启动器产物 + 测试残留 |
| 低 | 3 | `packages/host/directory-picker-auto/src/index.ts` | +1 −1 | 导出 `overrideDirectoryPickerBackend` |
| 低 | 2 | `tsdown.config.ts` | +4 | **纯注释** |
| 低 | 2 | `lefthook.yml` | +13 −1 | typecheck 改用 npm + 启动器布局门禁 |
| 低 | 1 | `packages/host/directory-picker-auto/tests/resolve.spec.ts` | +30 −1 | 测试 |
| 低 | 1 | `packages/host/directory-picker-auto/src/resolve.ts` | +19 −1 | `DSH_DIRECTORY_PICKER` 覆盖 |
| 低 | 1 | `packages/client/ui-attachment/src/client/labels.ts` | +6 −1 | 新标签 |
| 近零 | 0 | `packages/client/ui-theme/src/styles/base.css` | +16 −4 | 字体栈前置 Noto/WQY |
| 近零 | 0 | `packages/client/ui-attachment/src/ImageLightbox.tsx` | +30 −3 | 灯箱加载/失败状态 |
| 近零 | 0 | `packages/client/ui-attachment/src/ImageLightbox.module.css` | +18 | 上述样式 |
| 近零 | 0 | `packages/client/ui-attachment/tests/image-lightbox.client.spec.tsx` | +1 −1 | 标签补齐 |

---

## 五、B 类：5 个落进上游目录的新增文件

合计 **632 行**。

| 行数 | 文件 | 说明 |
|---:|---|---|
| 152 | `scripts/fix-deploy-closure.mjs` | `pnpm deploy --legacy` 缺陷绕行脚本；**唯一调用方是 `apps/desktop-launcher/linglong/prepare-offline.sh:38`**，可直接搬进启动器 |
| 140 | `packages/client/ui-conversation/tests/desktop-clipboard.client.spec.ts` | 壳内剪贴板桥接测试 |
| 134 | `apps/cli/src/doctor.ts` | doctor 的 CLI 入口 |
| 118 | `packages/client/ui-conversation/src/client/desktop-clipboard.ts` | 与宿主壳的剪贴板桥接 |
| 88 | `packages/boot/app-boot/tests/safe-mode.spec.ts` | 安全模式测试 |

---

## 六、C 类：独立新目录

| 文件数 | 目录 | 备注 |
|---:|---|---|
| 136 | `apps/desktop-launcher/` | 含 `AUDIT.md`(759 行，审计启动器自身)、`linglong/`、`frontend/` |
| 105 | `.agents/notes/` | 全部为新增，零修改，**不产生冲突**；仓库共 1007 篇笔记 |
| 24 | `packages/support/doctor/` | 上游**完全没有** `packages/support/` 这个目录，整目录新增 |
| 11 | `docs/superpowers/` | 全部为新增，零修改 |

> 上表为**审计基准时**（HEAD `e8fbf5a08f`）的计数。此后为整理文档，`apps/desktop-launcher/` 下新建了 `docs/` 子目录并把 `AUDIT.md` 移入其中，当前文件数与基准相差 `docs/` 的净增。

---

## 七、逐项处置建议与可行性证据

| # | 目标 | 建议 | 可行性 | 证据 |
|---|---|---|---|---|
| 1 | `packages/host/directory-picker-auto/`（3 文件，+50 −3） | **直接删除**，改用上游 overlay | ✅ **已证实** | 上游已有 `apps/web/tests/pin-browse-picker.overlay.yml`，内容为 `disabled: directory-picker` + `insert: directory-picker-browse / ui-directory-picker-browse`，与该改动**功能完全重复** |
| — | 同上·前提 | 启动器追加第二个 `--patch` | ✅ **已证实** | 上游 `--patch` 明确可重复：`Repeatable single-value collector: --patch a.yml --patch b.yml` |
| 2 | `tsdown.config.ts`（+4） | **直接回退** | ✅ **已证实** | diff 全部为注释行 |
| 3 | `.gitignore`（+20） | 启动器产物下沉到新建的 `apps/desktop-launcher/.gitignore`（该文件目前不存在） | 高 | git 原生支持嵌套 `.gitignore` |
| 4 | `packages/boot/app-boot/src/profile.ts` 安全模式（+52 −8） | **不可删除**（承重），出路是**上游化** | ✅ 已定性 | 见 §10.1：`DSH_SAFE_MODE` 正是"harness 已损坏仍能恢复"的机制 |
| 5 | `apps/cli/*` + `apps/cli/src/doctor.ts`（4 文件，+207 −2） | 见 §10.5 的路径 B/D | ✅ 调研已完成 | doctor 必须在 harness 起不来时运行，插件式命令不成立 |
| 6 | `packages/boot/app-boot/src/index.ts`（+3 −3） | 无干净替代，建议**上游化** | ✅ 已排除替代 | 上游 `loadEnv` 只 warn 不抛错（`packages/boot/app-boot/src/index.ts:~95`），无法用"调用并捕获"替代 `isBootstrapOnly` 判定 |
| 7 | `packages/client/modules/*`（2 文件，+104 −4） | 本质是上游 bug/性能修复，建议**上游化** | 高 | `newlineCount` 逐码点遍历→`indexOf`（自称约 80× 提升）；`resolveSync` 跨 Node 版本签名差异兜底 |
| 8 | `ui-theme/base.css`（+16 −4） | 启动器已用 `FONTCONFIG_FILE` 注册字体，改为在**启动器自己的 fontconfig** 里做 alias | 中 | 该文件上游改动次数为 **0**，风险极低，可最后处理 |
| 9 | `ui-conversation/InputBar.tsx` + `desktop-clipboard.ts`（+59 +118 +140） | **无法等价外移**，见 §十一 | ✅ 调研已完成 | 缺少 `File → DraftAttachmentId` 的公开接口 |
| 10 | `ui-attachment/*`（5 文件，+56 −6） | 给灯箱加"加载中/加载失败"状态；两处 i18n key **上游已存在**，属纯增量通用改进，建议**上游化** | 高 | `image.loading`/`image.loadFailed` 上游 `ui-conversation/src/client/locales.ts:40-41` 已定义，`locales.ts` **未被 fork 修改** |
| 11 | `scripts/fix-deploy-closure.mjs`（152） | 搬进 `apps/desktop-launcher/`，同步改 `prepare-offline.sh:38` 的路径 | ✅ 高 | 调用方只有启动器 |
| 12 | `lefthook.yml`（+13 −1） | 布局门禁移到启动器自己的脚本；`npm` 换法可用 `.npmrc` 替代 | 中 | 注释已说明是为规避玲珑容器内 `pnpm` 的 deps 检查 |
| 13 | `tsconfig.host.json`（+1） | doctor 并入启动器后自动消失 | 取决于 §10 | — |
| 14 | `pnpm-lock.yaml`（+167 −65） | **不可消除** | ❌ | workspace 只要增删包就必然变化，且上游改动 447 次，是全仓最热的文件 |

---

## 八、审计发现的注释与文档缺陷

### 8.1 `profile.ts:891` 的注释与实际改动不符（已确认）

```ts
// 上游把 reload 生命周期交给 YAML 组合（profile-context + dsh-hmr），manifest 的
// dsh.profile.patchReload 已退役，这里不再校验；let 是安全模式过滤所需。
```

经查：`patchReload` 在上游和本地**整个仓库的代码里都不存在**，只出现在上游一篇 Agent Note（`2026-08-22-single-dsh-application-launcher.md`）里。该 hunk 实际只做了 `const layers` → `let layers`，**没有任何校验被移除**。

→ 这句注释宣称了一个并不存在于本 diff 的行为变更，会误导后续做偏离审查的人。

### 8.2 `DSH_DIRECTORY_PICKER` 无任何文档

该环境变量在 `.agents/notes/` 中出现 **0 次**。结合 §七 #1（与上游 overlay 功能重复），进一步支持"这是冗余实现"的判断。

### 8.3 消除偏离会牵连的文档面

| 关键词 | 提及的笔记文件数 |
|---|---:|
| `剪贴板` | 15 |
| `doctor` | 9 |
| `directory-picker` | 6 |
| `DSH_SAFE_MODE` / `安全模式` | 4 |
| `desktop-clipboard` | 2 |
| `DSH_DIRECTORY_PICKER` | **0** |

注意仓库规则：**已归档的笔记是冻结的，不可编辑**。因此若这些笔记已归档，消除偏离时无法同步更新，需另开新笔记说明。

---

## 九、调研纠错记录

两轮子代理调研**各出现一次同类错误**：把 fork 自己的代码误判为"上游已有"。两处均已用 `upstream/master` 权威引用证伪，本文档只采用核实过的结论。

| 误判 | 子代理的说法 | 实测结论 |
|---|---|---|
| `DSH_SAFE_MODE` | 声称是"上游原生开关"（引 `profile.ts:970-972`） | **错**。上游 `profile.ts` 中 `DSH_SAFE_MODE` 出现 **0** 次，本地 **2** 次；子代理引用的是**本地文件**行号，而那段代码正是本 fork 的改动本身 |
| `createRequire` 兜底 | 声称 `client/modules` 的兜底"已被上游吸收" | **错**。`git diff upstream/master...HEAD -- packages/client/modules/src/index.ts` 的新增行里 `createRequire` 出现 **2** 次，上游 master 没有这段代码 |

第一处纠错有实质后果：它曾是一个"无需改上游即可让 `profile.ts` 恢复一致"的推荐路径的基础，该路径因此不成立。

---

## 十、专项一：`doctor` 收敛方案

### 10.1 目标与硬约束

**目标**：把 `packages/support/doctor` 从上游目录树中移出，使自有代码收敛进 `apps/desktop-launcher/`。

**硬约束（不可违反）**：doctor 的核心用途是**启动前预检与修复**——桌面启动器在主 harness 起不来时调用它。因此它必须能在**用户配置已经损坏**的情况下运行。

**已实测的约束推论**：`apps/cli/src/profile-boot.ts` 未被 fork 修改（`git diff` 为空），其

```
profile-boot.ts:169  export function prepareProfile(name: string, userLayer = true, fromDefaultProfile?: string)
profile-boot.ts:171    const profile = loadProfile(NAME, name, INSTALL_ANCHOR, undefined, { userLayer })
```

是**真实的上游行为**：`userLayer` 默认 `true`，CLI 没有暴露关闭入口。于是当用户的 `cordis.patch.yml` 损坏（非法 YAML 或 `!!js` 解析失败）时，`dsh --profile <任意>` 会在解析用户层时直接抛错。

**结论：`DSH_SAFE_MODE` 正是让"harness 已损坏仍能恢复"得以成立的那个机制，而它是 fork 自己补进 `profile.ts` 的。** 因此这条偏离是**承重的**：

- 直接删除 → 启动器的恢复路径失效（启动器当前还在 `internal/preflight/preflight.go:117-121` 主动剥离 `DSH_SAFE_MODE`）
- 正确出路是**上游化**：上游 `loadProfile` 本就支持 `userLayer: false`（其 JSDoc 明确写着这是给 "a recovery diagnostic" 用的），只是 CLI 没暴露。让上游加一个 `--no-user-layer` 之类的开关，即可永久消除该偏离。

→ 这构成一个**循环**：doctor 的 CLI 入口若走 `dsh --profile` 路径，就依赖 `DSH_SAFE_MODE`——而它正是另一处待消除的偏离。这是 §10.5 决策的核心。

### 10.2 现状实测事实

#### 包构成（24 个文件）

```
packages/support/doctor/
├── package.json                        ← 门禁违规的来源
├── tsconfig.json                       ← extends tsconfig.base.json，references 7 个 workspace 项目
├── src/            12 个文件
│   ├── index.ts                        主 API（runDiagnosis / runRepair）
│   ├── types.ts
│   ├── invariant.ts                    ← 空实现，门禁要求整个删除
│   ├── auto-disabled.ts
│   ├── bisect.ts / bisect-by.ts
│   ├── loader-probe.ts                 带 #! shebang，被 execFile 直接 spawn
│   └── checks/{config,data,env,plugins}.ts
└── tests/          11 个 spec（77 项测试，当前全绿）
```

**包内没有任何 `.md`**——没有包 README，也没有 group README。

#### 运行时依赖（全部为上游包，外加 js-yaml）

| 依赖 | 引用次数 |
|---|---:|
| `@deepseek-ai/dsh-atomic-write` | 5 |
| `@deepseek-ai/dsh-app-boot` | 4 |
| `js-yaml` | 2 |
| `@deepseek-ai/dsh-invariants` | 1 |
| `@deepseek-ai/dsh-home-paths` | 1 |
| `@deepseek-ai/dsh-cmdline` | 1（`loader-probe.ts:39` 的 `provideCmdline`） |
| `@deepseek-ai/cordis-plugin-include` | 1 |
| `@deepseek-ai/cordis` | 1 |

#### 消费者（仅 4 处）

| 位置 | 内容 |
|---|---|
| `apps/cli/src/doctor.ts` | **134 行，纯格式化分发**（import `runDiagnosis`/`runRepair`，输出人类可读或 JSON，返回退出码） |
| `apps/cli/package.json:120` | `"@deepseek-ai/dsh-doctor": "workspace:^"` |
| `tsconfig.host.json:202` | `{ "path": "./packages/support/doctor" }` |
| `pnpm-lock.yaml` | workspace link |

`linglong/output/...` 与 `apps/desktop-launcher/linglong/stage/...` 里也出现该依赖名，但那是**被 gitignore 的构建产物**，不是源。

#### 启动器的调用形态

```go
// preflight.go:114  NewRunner 剥离 DSH_SAFE_MODE、注入 DSH_HOME
// preflight.go:171  Diagnose(ctx, quick) → quick=true 时追加 "--quick"
// preflight.go:204  Repair(ctx, level)
args := []string{dshScript, "doctor", ...extra}     // preflight.go:141 DoctorArgs
```

调用点：`app.go:816`（doctor 面板，`--json`）、`app.go:901`（`--repair`, `1`）。

#### 打包链路

```sh
# prepare-offline.sh:34
pnpm --filter @deepseek-ai/dsh deploy --legacy --prod ... "$STAGE/harness"
node scripts/fix-deploy-closure.mjs "$STAGE/harness"
# :49   删除 typescript
# :60+  inject_workspace_pkg 遍历 packages/*/* 与 vendor/*，把闭包缺失的包补进去
# :133  inject-link-bridge.sh 往 web-frontend/dist 注入脚本
```

**关键**：doctor 是被 `inject_workspace_pkg` 的 `packages/*/*/` 循环补进闭包的。**它一旦离开 `packages/`，这个循环就找不到它**——这是落点方案 O1 的核心风险来源（实测结论见 §10.7）。

### 10.3 门禁违规实测：4 个门禁、18 项

全部为实测（`exit=1`）：

#### `pnpm run constraints`（`check-workspace-constraints.ts`）— 11 项

```
@deepseek-ai/dsh-doctor: package.json version must match root version 0.1.6-alpha.2
@deepseek-ai/dsh-doctor: release member must not set "private": true
@deepseek-ai/dsh-doctor: release member must set publishConfig.access to "public"
@deepseek-ai/dsh-doctor: release member repository must use git+https://github.com/deepseek-ai/deepseek-harness.git
  with directory packages/support/doctor
@deepseek-ai/dsh-doctor: @deepseek-ai/cordis must be a peerDependency
@deepseek-ai/dsh-doctor: @deepseek-ai/cordis must also be a devDependency
@deepseek-ai/dsh-doctor: package.json must set "main": "lib/index.js"
@deepseek-ai/dsh-doctor: package.json must set "types": "lib/types/index.d.ts"
@deepseek-ai/dsh-doctor: package.json exports["."].types must be "./lib/types/index.d.ts"
@deepseek-ai/dsh-doctor: package.json exports["."].default must be "./lib/index.js"
@deepseek-ai/dsh-doctor: package.json files must be ["lib/index.js","lib/invariant.js","lib/types/**/*.js","lib/types/**/*.d.ts"]
```

**根因：位置错了，不是配置错了。** `scripts/check-workspace-constraints.ts:58`：

```ts
const standardReleaseMemberDirectory =
  /^(?:packages\/(?!experimental\/)[^/]+\/[^/]+|apps\/(?!desktop(?:-host)?$)[^/]+|vendor\/[^/]+)$/
```

该正则把**每一个 `packages/<组>/<包>`（`experimental/` 除外）都判定为 release member**。release member 必须非 private、`publishConfig.access: "public"`、且 `repository` 指向 `deepseek-ai/deepseek-harness`。

而 `@deepseek-ai/dsh-doctor` 是 fork 自有代码：

- 它**无法**以 `@deepseek-ai` 作用域发布（该作用域不属于 fork）
- 它的 `repository` **不可能**是上游仓库

→ 其中 **3 项（private / publishConfig / repository）结构上永远无法就地修复。唯一的出路是把它移出 `packages/`。**

**另一处约定违背**：doctor 的 `package.json` 把 `.` 指向**源码**（`"main": "src/index.ts"`），而上游同类包（`app-boot`、`cmdline`、`atomic-write` 等）**一律**把 `.` 指向构建产物、源码面另开 `./src/*` 通道：

```json
"main": "lib/index.js",
"exports": {
  ".": { "types": "./lib/types/index.d.ts", "default": "./lib/index.js" },
  "./src/*": "./src/*",
  "./package.json": "./package.json"
}
```

注意 `lib/index.js`、`lib/types/index.d.ts`、`lib/types/invariant.js` **都已实际构建存在**，所以改成约定形式是可做的。

#### `verify-package-readme-limitations` — 1 项

```
packages/support/doctor/README.md: package manifest has no sibling README
  with the `## Known Limitations and Deferred Work` section
```

#### `verify-package-invariants` — 5 项

```
exports["./invariant"] must target ./lib/types/invariant.d.ts and ./lib/invariant.js
files must publish lib/invariant.js
@deepseek-ai/dsh-invariants must be a workspace:^ peerDependency
@deepseek-ai/dsh-invariants must be a workspace:^ devDependency
src/invariant.ts: empty install function is unnecessary; omit the companion and its publication wiring
```

#### `verify-subsystem-pages` — 1 项

```
packages/support/README.md: package group has no group README declaring subsystem ownership
```

#### 对照证据：为什么 `apps/desktop-launcher/` 是零成本

| 目录 | 有无 package.json | 落入 release-member 正则 | 门禁错误数 |
|---|---|---:|---:|
| `apps/desktop-launcher/` | **无** | 否 | **0** |
| `packages/support/doctor/` | 有（`@deepseek-ai/dsh-…`） | 是 | **18（4 个门禁）** |

**这从仓库自身的门禁设计上，直接验证了"把自有代码收敛进 `apps/desktop-launcher/`"是正确的方向**——启动器目录之所以零成本，正是因为它没有 package.json、不被当作 workspace 包。

#### 排除项：其余门禁错误与本 fork 无关

同一轮 `constraints` 输出里还有 `packages/code-runtime/code-runtime`、`packages/e2b/e2b`、`packages/e2b/fs-e2b`、`packages/e2b/subprocess-e2b`、`packages/experimental/code-runtime-python`、`packages/fs/tool-present`、`packages/workflow/workflow-worker-thread` 报 "expected a package here (no package.json found)"。经查这些目录**只含 `lib/` 与 `node_modules/`，没有 `src/`、没有 `package.json`**，是上游删包后残留的构建产物（即 `pnpm run clean` 的清理对象），**不是 fork 缺陷**。

#### 严重性校准

`constraints` 属于 `scripts/run-gates.ts:297 ciSharedStaticGates()` 与 `hygieneLeafGates()`，即上游 CI 的静态门禁。但本 fork 的 GitHub Actions 是否启用**无法确认**（`gh` 未认证）。同时 [AUDIT.md](./AUDIT.md) 已把"无 CI 流水线"列为发现项 9 / 10。

→ 因此这 18 项是**潜在红灯**，不是当前故障。但它意味着这些门禁目前对 fork 完全失去保护作用。

### 10.4 决策点 D1：doctor 包的落点

| 选项 | 做法 | 门禁 | 满足目标？ | 改动面 | 风险 |
|---|---|---|---|---|---|
| **O1** | 移出 `packages/`，进 `apps/desktop-launcher/`（非 workspace 包，由启动器构建） | 全部消失（不再被扫描） | ✅ 是 | 大 | **中**（实测后由"中高"下调，见 §10.7） |
| **O2** | 移到 `packages/experimental/doctor` + 改名 `@deepseek-ai/dsh-experimental-doctor` + manifest 全面合规 | 3 项 release-member 消失，其余仍需修 | ❌ 否（仍在 `packages/`） | 中 | 低 |
| **O3** | 原地不动，只修可修项 | 仍剩 3 项红灯 | ❌ 否 | 小 | 低但无效 |
| **O4** | 把 doctor 框架上游化 | 归零（成为合法上游包） | ✅ 是（若上游接受） | 取决于上游 | 不可控 |

**O2 的额外代价**：`experimentalPackageDirectory` 的校验（`check-workspace-constraints.ts:297`）要求包名以 `@deepseek-ai/dsh-experimental-` 开头。doctor 不是实验性原型，这个改名是**语义错配**；且 `packages/experimental/` 本身是上游目录。

**O2 仍需修的项**（8 项，均属良构实践）：version、main、types、exports、cordis peer+dev、files、group README、包 README。

### 10.5 决策点 D2：doctor 的 CLI 入口

前置事实：`apps/cli/src/args.ts` 上游**没有任何命令注册表**，`plugin` 是硬编码的 `if (first === 'plugin')`。命令提供者机制确实存在，但在 **booted tree 层**：`@deepseek-ai/dsh-cmdline` 的 `provideCmdline`（`packages/boot/cmdline/src/index.ts:84`）/ `parseCmdline`（`:165`），由 `apps/cli/src/profile-boot.ts:318` 在每次 boot 时调用；ACP/SDK bundle 即用此机制。

| 路径 | 机制 | "harness 已损坏"下 | 上游改动 |
|---|---|---|---|
| **保持现状** | `apps/cli` 4 个文件不动 | ✅ 是 | 是（4 个文件） |
| **A** | fork 自有 standalone bundle（照 `packages/bundle/sdk-minimal` 的"完整树"模式，挂 `inject: ['cmdlineArgs']` + `parseCmdline`）+ `dsh --profile <name>` | 依赖 `DSH_SAFE_MODE` 才能容忍用户层损坏 → **而它正是待消除的偏离** | 不需要 |
| **B** | 启动器直接 `node <自有脚本>` import doctor 核心 | ✅ **唯一 100% 满足**（完全绕开 profile 装载） | 不需要 |
| **C** | `--dump-config` / `--dump-default-config` | 不可行：只打印不执行，且禁止带 app 参数 | — |
| **D** | 让上游暴露 `--no-user-layer`（`loadProfile` 已支持，CLI 未暴露） | ✅ 可行 | **需要上游改动** |

补充：应用入口门禁 `scripts/verify-application-entrypoints.ts` 只扫描**仓库内 `bin` 字段**与**带 shebang 的源文件**，覆盖不到 Go 启动器运行时 spawn 的目标；上游自己也已破例（`loader-probe.ts` 带 shebang 并被 `execFile(process.execPath, …)` 直接运行）。→ 路径 B 不违反门禁，但需要在 fork 内显式记录为约定例外。

### 10.6 推荐组合与分阶段

**推荐：D1=O1 + D2=路径 B**（三个 spike 已全部跑完，O1 判定可行）

理由链：

1. O1 是**唯一同时满足"目标达成 + 门禁归零"**的选项。
2. D1=O1 会切断 `apps/cli` → doctor 的依赖，因此 **D2 必须选 B 或 D**；路径 A 因循环依赖 `DSH_SAFE_MODE` 而出局。
3. 路径 B 不需要上游改动，是 B/D 中唯一可自主完成的。

**备选：D1=O2 + D2=保持现状**——先让门禁变绿（修 8 项良构问题 + 移到 `experimental/` + 改名），把"移出 `packages/`"降级为长期目标。风险低、可立即执行，但不满足既定目标。

| 阶段 | 内容 | 是否可逆 |
|---|---|---|
| **P0** | 三个 spike（只读实验） | — |
| **P1** | 修 doctor manifest 全部良构问题（version/main/types/exports/cordis/files/invariant）+ 补 2 个 README | 可逆 |
| **P2** | 按 spike 结论执行 O1 或退到 O2 | O1 可回退（纯移动） |
| **P3** | 按 D2 结论改造入口（B：删 `apps/cli/src/doctor.ts` + 回退 args.ts/bin.ts/package.json；或 D：提上游） | 是 |

P1 无论选哪条路都要做，且**当前就能让 18 项降到 3 项**。

### 10.7 P0 spike 执行结果（已完成，全部只读）

#### Spike-1 ✅ 通过——doctor 的代码已被内联进 CLI 自身产物

**本地开发构建** `apps/cli/lib/doctor-bbX1MKl7.js`（40K）：含 doctor 全部内部符号（`removeOrphanedPatchEntries` 3、`webAppAnchor` 7、`composeEntries` 4、`pruneDoctorBackups` 2）。

**打包闭包** `stage/harness/lib/doctor-C60dnfeU.js`（闭包根本身即 `@deepseek-ai/dsh` v0.1.6-alpha.2）：

```
对 @deepseek-ai/dsh-doctor 的 import 数: 0        ← 决定性
pluginBundlesResolvable 2 / webAppAnchor 8 / pruneDoctorBackups 2   ← 实现内联
```

其**全部外部 import** 只有 5 个非 node 内置项，且**全部存在于闭包内**：`@deepseek-ai/dsh-app-boot`、`@deepseek-ai/dsh-home-paths`、`@deepseek-ai/dsh-atomic-write`、`@deepseek-ai/cordis-plugin-include`、`js-yaml`。

> **方法说明**：该闭包是**陈旧构建产物**（早于 `removeOrphanedPatchEntries` 的引入），因此首轮用新符号探测时计数为 0。改用重构前即存在的稳定符号重测，才得出上述结论。

→ **doctor 的运行时并不加载 `node_modules/@deepseek-ai/dsh-doctor/`。闭包里那个包是 `inject_workspace_pkg` 注入的死重（除下述探测路径外）。移出 `packages/` 不会破坏运行时解析。**

#### Spike-2 ⚠️ 有条件通过——发现一处真实耦合：loader-probe 依赖 doctor 包在闭包内

**已验证的部分**：

- 单文件打包**可行性已被现有产物证明**：`doctor-*.js` 就是一个把 doctor 完整打包、仅留 5 个外部 import 的单文件 chunk。
- `app-boot` 的 worker **不受影响**：chunk 把 `@deepseek-ai/dsh-app-boot` 留作**外部** import，worker 由闭包在运行时解析。
- tsx / typescript **确实不在闭包内**（`prepare-offline.sh:49` 主动删除），`.bin` 内也无 `tsx`/`tsc`。

**发现的耦合（O1 必须承接）**——`packages/support/doctor/src/checks/plugins.ts:324`：

```ts
function loaderProbeEntry(): { path: string; needsTsx: boolean } {
  try {
    const resolved = require.resolve('@deepseek-ai/dsh-doctor/loader-probe')
    return { path: resolved, needsTsx: false }      // ← 打包态走这条
  } catch {
    return { path: fileURLToPath(new URL('../loader-probe.ts', import.meta.url)), needsTsx: true }
  }
}
```

其 JSDoc 已明确预见本问题：*"spawn with `--import tsx/esm` fails in packaged installs where tsx is a dev-only dependency"*。

实测闭包内该产物存在：`stage/harness/node_modules/@deepseek-ai/dsh-doctor/lib/types/loader-probe.js`（9874 B）。

→ **链路**：打包态靠 `require.resolve` 命中**包导出** `./loader-probe` → 拿构建产物 → `needsTsx: false` → 普通 `node <path>` 运行，**绕过 tsx 缺失**。

**一旦 doctor 离开 `packages/`**：`inject_workspace_pkg` 不再注入它 → `require.resolve` 失败 → 回落到 `needsTsx: true` → **闭包无 tsx → 打包态的深度动态加载检查失效**。

**O1 必须承接的代价**：在启动器的 staging 步骤里**显式放置** doctor 的 `loader-probe` 产物与声明该导出的 `package.json`，或改为让探测脚本自带该文件。这是一项**具体且有限**的工作，不再是未知风险。

**附带澄清**：tsx 不在闭包内**不是缺陷**——上游设计已用"包导出优先"绕开它。打包态全量诊断（`app.go:816` 的 `--json`）的探测路径是通畅的。

#### Spike-3 ✅ 通过——`apps/desktop-launcher/` 下的包对门禁完全不可见

经验验证正则（`node -e` 实跑）：

```
release-member  exp=false  packages/support/doctor        ← 11 项错误的来源
NOT-member      exp=true   packages/experimental/doctor   ← O2 的落点
NOT-member      exp=false  apps/desktop-launcher/doctor   ← O1 的落点
```

扫描范围（`check-workspace-constraints.ts:22`）：

```ts
const workspaceGlobs = [
  { dir: 'vendor', depth: 1 }, { dir: 'packages', depth: 2 },
  { dir: 'native', depth: 1 }, { dir: 'native/system/packages', depth: 1 },
  { dir: 'apps', depth: 1 },                                   // ← 只有一层
]
```

`packageDirs('apps', 1)` 只读 `apps/*` 中**带 package.json** 的目录；`checkHierarchyShape()` **只遍历 `packages/`，完全不检查 `apps/`**。

→ `apps/desktop-launcher/doctor/package.json`（深度 2）**既不是 release member，也不受层级检查**。O1 让 11 项约束错误**归零且无替代义务**。

#### P0 总结论

| Spike | 结果 | 对方案的影响 |
|---|---|---|
| 1 | ✅ 通过 | 运行时无障碍；**O1 的主要风险被排除** |
| 2 | ⚠️ 有条件通过 | 需承接"loader-probe 产物 staging"这一项确定工作 |
| 3 | ✅ 通过 | O1 落点对门禁完全不可见 |

**风险重新评级：O1 由「中高」下调为「中」**，剩余风险集中在一条已知的、可验证的 staging 步骤上。建议推进 P1 + O1；Spike-2 的 staging 细节需在 P2 实施时同步验证。

---

## 十一、专项二：客户端粘贴能否外移

### 11.1 结论：**不能等价外移**，存在一个已被验证的硬约束

从 `ui-conversation` 外部**无法把一个 `File` 变成一条附件草稿**：

```
contract/input.ts:180   addAttachments(ids: readonly DraftAttachmentId[]): boolean
contract/input.ts:226   addAttachments(ids: readonly DraftAttachmentId[]): boolean
```

`addAttachments` 吃的是 **`DraftAttachmentId[]`，不是 `File[]`**；而 `DraftAttachmentId` 只能由 `ConversationController.createDrafts()` 生产，该方法**不在公开的 `IConversation` 上**：

```
IConversation 公开成员（service.ts:40-71）：
  input / blocks / send / updateQueue / cancel / loadOlder
  createDrafts 出现次数: 0
```

→ 因此 `desktop-clipboard.ts` 里的 `intakeFiles` 能力**没有任何公开替代**。

### 11.2 其他已确认事实

| 事实 | 证据 |
|---|---|
| **粘贴没有任何扩展点** | 全仓 `PASTE_COMMAND` 只出现在 `ui-conversation/src/client/input/editor/keymap.ts:146-170`，且 `intakeFiles`/`pasteText` 由包内 `view-binding.ts:138-144` 提供 |
| `conversation.input.attachments` 是 **single** slot，且已被上游 `ui-attachment` 占住 | `ui-attachment/src/client/index.ts:17-20` 用 `ctx.slots.inject` 注册；`onAddFiles` 是 **owner props**，只发给声明的子 slot |
| `keyboard` 明确不跨插件边界 | `input/hub.ts:154-158` 注释：`package-internal — handed through the composer-bar entry's inject, never across a plugin boundary` |
| `editor`、`gate.current.intakeFiles` 均不可达 | 组件内私有 |
| `machineBusy` / `locked` **可部分重算** | 通过 `useInput(s => s.phase)` / `useSession(s => s.removed)` |
| 外部客户端插件机制本身是**存在且官方支持**的 | `dsh.client.platform === 'web'` + `exports["./client"]`（缺失即 throw）；真实先例 `packages/client/ui-goal`；**外部包先例** `packages/experimental/agent-team-web-profile`（README 记载 `dsh plugin --profile web add <bundle>`） |

### 11.3 路径对比

| 路径 | 改上游文件？ | 改动面 | 风险 | 判定 |
|---|---|---|---|---|
| A 外部客户端插件 + 能力接口 | 需要（几行） | `service.ts` + `contract/input.ts` | 低 | **唯一干净的公开扩展点做法** |
| B 外部 bundle（`dsh plugin add`） | 同 A | 同 A + 一个外部包 | 中（需网络/pnpm） | 依附 A |
| C 启动器 dist 注入 + DOM hack | **不需要** | `linglong/` 一个 js + 一处调用 | 中高 | 能work，**脆**（依赖上游 DOM） |
| C′ 启动器注入、不 hack DOM | 不需要 | 同上 | — | **不可行**（拿不到 addFiles） |
| D 上游加扩展点 | 需要（上游 PR） | 上游几十行 + 文档 + 测试 | 低 | **长期最干净** |
| E fork 内小重构 | 需要（几行） | InputBar 一处 | 低-中 | 折中：把 59 行缩成几行 |

现有可复用的注入手法：`linglong/inject-link-bridge.sh` 已把 `dsh-link-bridge.js` 幂等拷进 `dist/assets/` 并在 `dist/index.html` 插入 `<script>`，且 `dsh-link-bridge.js:29` 已有 `if (window.parent === window) return;` 判据，与 `desktop-clipboard.ts:13` 的 `isShellEmbedded()` 同源。这条路**不依赖 iframe 同源**（脚本由 harness 自己服务）。

### 11.4 ui-attachment：可从 4 个文件缩到 1 个

`ImageLightbox` 是 `ui-attachment` 的**包内私有组件**，外部插件既不能替换也不能包装，因此这四处改动**无法靠注册 slot 外移**。但可以缩小：

- `labels.ts` 的 `loading`/`loadFailed` 两个字段**可以删掉**——让 `ImageLightbox` 内部直接用上游**已存在**的 `t('image.loading')` / `t('image.loadFailed')`（`ui-conversation/src/client/locales.ts:40-41`）。
- 这样 4 个文件变成 1 个（`ImageLightbox.tsx`）+ 1 个 CSS。
- 或者干脆放弃：上游 `MessageImage.tsx` 已用同样的 key 覆盖了历史图，`ImageLightbox` 的 loading/error 只是"预览不空白"的体验增强。

---

## 十二、结论汇总

1. **当前不存在冲突。** `upstream/master` 已经是 HEAD 的**祖先**，fork 已包含上游最新 master，没有待合并内容。A 类文件的代价只会在**上游下次前进后重新同步时**才显现。
2. **违规面很小但很"热"。** 真正触碰上游的只有 **28 个文件、约 1250 行**，占全部偏离（304 文件 / +44438 −96）的 **约 3%**。其余 97% 已经待在预期位置。
3. **用户的目标方向被仓库门禁本身证明是对的。** `packages/support/doctor` 触发 **4 个门禁 18 项违规**，其中 3 项（private / publishConfig / repository）**结构上无法就地修复**；而 `apps/desktop-launcher/` 因无 package.json 而零门禁成本。
4. **有一处是纯冗余**：`packages/host/directory-picker-auto/` 的 `DSH_DIRECTORY_PICKER` 改动，与上游**已有的** `apps/web/tests/pin-browse-picker.overlay.yml` 功能完全重复，可直接删除。
5. **有一处是纯注释**：`tsdown.config.ts` 的 +4 行全是注释，可直接回退。
6. **有三处本质是上游 bug 修复或通用改进**（`client/modules` 的性能与跨 Node 版本兼容、`ui-attachment` 的灯箱状态、`app-boot/src/index.ts` 的导出），建议上游化而不是长期藏在 fork 里。
7. **安全模式（`profile.ts`）是承重偏离**，不可删除，出路是上游化。
8. **客户端粘贴无法等价外移**，硬约束是缺少 `File → DraftAttachmentId` 的公开接口；最干净的做法需要上游加一个扩展点。
9. **严格做到"只有 `apps/desktop-launcher/` 不同"不可能**：`pnpm-lock.yaml`、`tsconfig.host.json` 这类 workspace 机械文件只要动过包结构就必然变化，且它们是上游最热的两个文件（447 / 195 次）。

---

## 十三、消除不掉的部分

| 项 | 原因 |
|---|---|
| `pnpm-lock.yaml` | workspace 增删包必然变化；且是上游最热的文件（300 次窗口内改动 **447** 次） |
| `tsconfig.host.json` | 只要 doctor 还是 workspace 包就必然被引用（O1 才能消除）；上游热度 **195** 次 |
| `apps/cli/src/args.ts` 等 4 文件 | 仅当 D2 选 B 或 D 才能消除 |
| `packages/boot/app-boot/src/profile.ts` 安全模式 | **承重**，不可删除；只能上游化 |
| `packages/boot/app-boot/src/index.ts` 的 3 个导出 | 无干净替代（上游 `loadEnv` 只 warn 不抛错，无法替代 `isBootstrapOnly`） |

---

## 十四、验收标准

doctor 迁移完成后必须同时满足：

1. `pnpm run constraints` **exit 0**（且不新增其他包的违规）。
2. `verify-package-readme-limitations`、`verify-package-invariants`、`verify-subsystem-pages` 三项**对 doctor 零输出**。
3. `git diff upstream/master...HEAD` 中 `apps/cli/`、`tsconfig.host.json`、`packages/support/` **消失**（O1+路径 B 的目标态）。
4. doctor 的 77 项测试全绿，且**测试必须跟随源码迁移**（`packages/*/*/src/**` 覆盖率门槛将不再覆盖 doctor——这是**保护力下降**，需显式接受或另行建立替代门禁）。
5. 实跑启动器预检链路：故意破坏一个 profile 的 `cordis.patch.yml`，确认 `dsh doctor` 等价路径仍能给出报告。

---

## 十五、未确认项

1. 打包态下 doctor 的 `exports["."]` 指向 `src/index.ts` 是否真会失败——未实跑确认。
2. doctor 在用户 `cordis.patch.yml` 已损坏时能否跑完全部检查——`checks/plugins.ts` 多处调用 `loadProfile`，未逐行确认是否吞掉解析异常，**需实跑验证**。
3. `packages/support/doctor/src/index.ts` 的检查注册表是**进程内全局数组**，不存在跨进程/外部注册 seam；若将来要外挂检查需另设计。
4. 本 fork 的 GitHub Actions 是否启用（`gh` 未认证，无法查询）——决定这 18 项是"红灯"还是"潜在红灯"。
5. **未评估每项改动功能上是否仍然必要**——本文档只评估了"能否搬走"。搬迁前需逐项确认功能未被上游演进覆盖。

---

## 附录：相关文档

| 文档 | 内容 |
|---|---|
| [AUDIT.md](./AUDIT.md) | 启动器**自身**缺陷清单（759 行）：N3/N17/N18/N19、S1–S7、N30、附录 D–G 等 |
| [./index.md](./index.md) | 本目录索引 |

两者的分工：`AUDIT.md` 回答"启动器自己哪里有问题"，本文档回答"fork 和上游差在哪里、怎么收敛"。

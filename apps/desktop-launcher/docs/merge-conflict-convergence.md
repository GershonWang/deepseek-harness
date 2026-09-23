# 合并冲突面收敛方案（评审稿）

> **目的**：把「每次合并上游代码都要处理冲突」这件事量化到文件级，回答三个问题——冲突究竟出在哪些文件、为什么是这些文件、怎么消除。
> **范围**：只做审计与方案。**本文档不含任何代码改动**，全部处置建议均为待批准方案。
> **分工**：[fork-divergence.md](./fork-divergence.md) 回答「fork 与上游差在哪里」（其基准已过期，见 §1）；本文档只回答「这些差异里哪些会在合并时变成冲突、怎么收敛」。启动器**自身**缺陷由 [AUDIT.md](./AUDIT.md) 负责。

---

## 一、基准与复现

| 项 | 值 |
|---|---|
| 上游 | `upstream/master` = 发布 `dsh 0.1.7-alpha.2` 的合并（2026-09-22），已 `git fetch` 核实为远端当前 tip |
| 本地 | 分支 `linglong-dev`，HEAD = 提交「fix(lock): 还原 pnpm-lock 的两处非预期漂移」 |
| 合并关系 | 上游 tip 已是 HEAD 的祖先（已全部并入），**当前无待合并内容**；下述冲突面只在上游下次前进后才显现 |
| 偏离总量 | 49 个文件落在上游目录树内（另有 `apps/desktop-launcher/`、`docs/superpowers/`、`.agents/` 三处独立目录，不产生冲突）。阶段 1 执行前为 85 个 |

复现命令：

```sh
git fetch upstream master

# 冲突面清单（排除三处独立目录）
git diff --name-status upstream/master HEAD -- . \
  ':(exclude)apps/desktop-launcher' ':(exclude)docs/superpowers' ':(exclude).agents'

# 复现「某一次上游合并当时」的冲突面：参数为合并提交的两个父提交
git merge-tree --write-tree <fork 侧父提交> <上游侧父提交>
```

> **本文档为何不写裸提交哈希**：仓库闸门 `verify-repository-references` 禁止维护文件出现提交标识，`apps/desktop-launcher/docs/` 下已有的四份文档是被显式豁免的特例。本文档采用「日期 + 提交标题 + 复现命令」作锚点以保持闸门为绿。若需要哈希级追溯，按同一先例把本文档加入 `scripts/verify-repository-references.ts` 的 `excludedFiles` 即可（**该项改动需单独批准**，见 §9）。

---

## 二、前提纠正：`packages/support/doctor` 是 fork 自有代码

### 2.1 上游从未有过 doctor

覆盖上游全部 13211 个提交的完整历史探查，全部为空：

```sh
git log --format="" --name-only upstream/master | grep -i doctor        # 空
git log -m --format="" --name-only upstream/master | grep -i doctor     # 空（展开 merge diff）
git log --full-history upstream/master -- 'packages/support/doctor'     # 0 个提交
git log --full-history upstream/master -- 'packages/*/doctor'           # 0 个提交
git log -S'@deepseek-ai/dsh-doctor' upstream/master                     # 空
```

即：**上游历史上任何时刻都不存在名为 doctor 的路径或包**。

### 2.2 但上游确实有过 `packages/support/` 目录组——它被改名了

这正是「记得上游有 `packages/support/doctor`」这一印象的来源：组名是真的，里面的包不是。

| 日期 | 事件 |
|---|---|
| 2026-07-24 | 上游 `packages/support/` 组首现（测试与支撑包组） |
| 2026-08-13 | 上游提交 `refactor: apply repository naming contract` 把该组整体改名，**这是最后一次触碰 `packages/support/` 的提交** |
| 2026-08-28 | **fork** 提交 `feat: dsh-doctor 诊断修复框架 + 插件安全模式 + 桌面端集成`，创建 `packages/support/doctor` |

改名去向（实测自该提交的 `--name-status`）：

| 原路径 | 新路径 |
|---|---|
| `packages/support/README.md` / `.zh.md` / `.i18n.yaml` | `packages/test-support/` 同名文件 |
| `packages/support/acp-snapshot` | `packages/test-support/acp-snapshot` |
| `packages/support/agent-loop-testkit` | `packages/test-support/agent-loop-testkit` |
| `packages/support/llm-mock-server` | `packages/test-support/llm-mock-server` |
| `packages/support/llm-replay` | `packages/test-support/llm-replay` |
| `packages/support/loader-smoke` | `packages/test-support/loader-smoke` |
| `packages/support/invariants` | `packages/runtime-diagnostics/invariants` |

### 2.3 关键：fork 是「复活了一个已退役的组名」

在 fork 创建 doctor 的那个提交上实测：

```sh
git ls-tree <doctor 创建提交> packages/ | grep support
#   packages/support      ← fork 新建，只含 doctor
#   packages/test-support ← 上游改名后的组，已存在

git merge-base --is-ancestor <上游改名提交> <doctor 创建提交>   # 真：改名已并入
```

也就是说，这**不是**「fork 基线太旧、沿用旧路径」造成的，而是**起名时上游已退役该组名 15 天，改名提交也已在 fork 树里**。

### 2.4 直接后果：一个当前就是红灯的门禁

```sh
node --import tsx/esm scripts/verify-subsystem-pages.ts   # exit 1
# verify-subsystem-pages: package-group documentation violations found:
#   packages/support/README.md: package group has no group README declaring subsystem ownership
```

根因与 §2.3 同源：上游改名时删掉了 `packages/support/README.md`，fork 复活该组后没有补组 README，而门禁要求每个包组有 README 声明子系统归属。

其余 doctor 相关门禁实测为绿：

| 门禁 | 结果 |
|---|---|
| `verify-package-readme-limitations` | exit 0（308 个包 README 全部合规） |
| `verify-package-invariants` | exit 0（38 个伴生模块合规） |
| `verify-repository-references` | exit 0 |
| `verify-subsystem-pages` | **exit 1** |

### 2.5 结论对方案的影响

「放弃自定义诊断、改用上游 doctor」**不成立**——没有可替换对象。上游最接近的三样东西都不是「预检 + 修复」：

| 上游机制 | 实际职责 |
|---|---|
| `apps/cli/src/startup-diagnostics.ts`（68 行） | 启动失败时把 `StartupError` 落盘到 `DSH_HOME/logs`，只存档 |
| `apps/desktop` 的 fatal recovery + crash report | Electron 壳的致命错误对话框与崩溃报告 |
| `packages/runtime-diagnostics` | 运行时不变式断言，与安装诊断无关 |

同时，`apps/desktop-launcher/` 内的「自定义诊断」**不重复实现任何一项检查**，它是产品化编排层：

| 文件 | 职责 |
|---|---|
| `internal/preflight/preflight.go` | doctor 子进程客户端：argv 组装、JSON 解析、`Classify` 分桶 |
| `internal/app/preflight.go` | 启动前门控状态机：quick → 自动修复 L1 → 复查 → 待确认 → 深度修复 L2 → 耗尽 |
| `internal/app/app.go` | 失败后自动诊断、doctor 面板、安全模式、全新环境降级 |
| `internal/app/autodisabled.go` | 读 doctor 的 `auto-disabled.json` 留痕并提示用户 |

因此「放弃自定义诊断」不是去重，而是**砍掉整个启动前恢复 UX**——doctor 本体是库/CLI，没有 UI、没有门控策略、没有降级入口。

---

## 三、冲突实测：最近两次上游合并

用 `git merge-tree --write-tree` 复现两次合并当时的冲突面（不依赖事后回忆）：

| 合并 | 上游提交数 | 冲突文件 | 归因 |
|---|---:|---|---|
| 并入 `0.1.7-alpha.2` 发布合并（2026-09-23） | 162 | `apps/cli/package.json` | **doctor**（依赖行）——唯一冲突 |
| 并入上游 master（2026-09-22） | 1299 | `apps/cli/src/args.ts`、`apps/cli/src/bin.ts`、`packages/boot/app-boot/src/profile.ts` | **doctor + 安全模式**（3 个） |
| 同上 | | `packages/client/ui-open-in-app/`（7 个）、`packages/host/open-in-app/src/icons.ts` | **open-in-app**（8 个） |
| 同上 | | `pnpm-lock.yaml` | 机械（workspace 成员变化） |

三条实测结论：

1. **`apps/desktop-launcher/` 零冲突**——上游没有这个目录，它是纯新增。冲突不可能出现在这里。
2. **最近一次合并的唯一冲突是 doctor 的依赖行**（`apps/cli/package.json`），你的体感来自这里。
3. **但最大冲突源不是 doctor**：1299 提交那次合并里，open-in-app 贡献 8 个冲突文件，doctor 只有 3 个。

---

## 四、当前冲突面全量清单（49 文件）

> **本节已按阶段 1（doctor 迁出 `packages/`）的执行结果重校。** 原 85 文件清单中的 A 组（doctor 框架本体，25 文件）与 B 组（doctor 的 CLI 与 TS 接线，9 文件）已消除；D 组由 7 降到 5（两处 gate 脚本的 doctor 白名单已删除，脚本回到上游）。实测命令见 §一。

按 fork 特性归组。**「冲突属性」**列决定该组在上游下次前进时的代价。

| 组 | 文件数 | 冲突属性 | 内容 |
|---|---:|---|---|
| C 安全模式 | 3 | **A 类 2 + B 类 1** | `packages/boot/app-boot/src/profile.ts`、`src/index.ts`、`tests/safe-mode.spec.ts`（新增） |
| D 门禁适配 | 5 | **A 类 4 + B 类 1** | `scripts/verify-repository-references.{ts,spec.ts}`、`verify-client-ui-i18n.{ts,spec.ts}`、`fix-deploy-closure.mjs`（新增） |
| E open-in-app / 宿主逃逸 | 24 | **A 类** | `packages/host/open-in-app/`、`packages/client/ui-open-in-app/`、`packages/util/launch-environment/` |
| F 壳内剪贴板 | 3 | **A 类 1 + B 类 2** | `ui-conversation/src/client/desktop-clipboard.ts`（新增）、`InputBar.tsx`、对应测试（新增） |
| G 机械配置 | 3 | **A 类** | `.gitignore`、`lefthook.yml`、`tsdown.config.ts` |
| H 其它客户端 | 11 | **A 类** | `client/modules`、`ui-attachment`、`ui-theme/base.css`、`host/directory-picker-auto` |
| | **49** | **A 类 45 + B 类 4** | |

**已消除的两组（阶段 1）**：A 组 25 个文件整体迁到 `apps/desktop-launcher/doctor/`（C 类新目录，不产生冲突）；B 组 9 个文件里，`apps/cli/` 6 个、`tsconfig.host.json`、`tsconfig.base.json` 回到上游原文，`pnpm-lock.yaml` 的 doctor 条目同步摘除后也与上游一致。**这四个文件（`pnpm-lock.yaml` 1811 次、`tsconfig.host.json` 346 次、`tsconfig.base.json` 241 次、`apps/cli/package.json` 160 次）是全仓上游热度最高的四个**，其中 `apps/cli/package.json` 正是上一次合并的唯一冲突文件。

分类口径（与 [fork-divergence.md](./fork-divergence.md) 第三章一致）：**A 类** = 修改了上游已有文件，上游前进时**必然文本冲突**；**B 类** = 新增文件但落在上游已有目录里，不直接冲突但会被上游门禁扫到；**C 类** = 独立新目录，不冲突。

---

## 五、归因：冲突 = 上游热文件 ∩ fork 改动

冲突概率与「上游改动该文件的频次」正相关。以下是当前 A 类文件的上游热度（统计窗口：2026-03-01 起至上游 tip）：

| 文件 | 上游改动次数 | 所属组 |
|---|---:|---|
| `pnpm-lock.yaml` | 1811 | B（机械，**不可消除**） |
| `tsconfig.host.json` | 346 | B |
| `tsconfig.base.json` | 241 | B |
| `apps/cli/package.json` | 160 | B |
| `scripts/verify-package-readme-model-experience.ts` | 140 | D |
| `ui-conversation/.../InputBar.tsx` | 135 | F |
| `packages/boot/app-boot/src/index.ts` | 86 | C |
| `packages/boot/app-boot/src/profile.ts` | 54 | C |
| `apps/cli/src/args.ts` | 30 | B |
| `packages/client/modules/src/index.ts` | 29 | H |
| `apps/cli/tests/args.spec.ts` | 24 | B |
| `lefthook.yml` | 22 | G |
| `apps/cli/src/bin.ts` | 20 | B |

**关键结构性观察**：doctor 虽然本体是 C 类（不冲突），但它的**接线**占用了全仓最热的两个 TypeScript 工程文件（`tsconfig.host.json` 346 次、`tsconfig.base.json` 241 次）。B 组 9 个文件里有 6 个是纯 doctor 接线——这是「冲突集中在 doctor」这一体感真正的来源。

---

## 六、收敛方案

每项给出：消除哪些文件、代价、风险。**四档可独立取舍**，不是打包方案。

### 方案 A：doctor 的 CLI 入口外移（doctor 留在原地）

**做法**：删除 `apps/cli` 的 doctor 子命令，改由启动器自带的 Node 入口直接 import doctor 核心。

实测依据：`apps/cli` 的**全部**改动都是 doctor 相关（`args.ts` 的 `DoctorInvocation` 与 `--repair` 解析、`bin.ts` 的 `case 'doctor'` 分发、`package.json` 的依赖行、`tests/args.spec.ts` 的用例、`tsconfig.json` 的项目引用）。

| 项 | 值 |
|---|---|
| 消除文件 | **6**：`apps/cli/package.json`、`src/args.ts`、`src/bin.ts`、`tests/args.spec.ts`、`tsconfig.json` 回到上游一致；`src/doctor.ts` 删除 |
| 保留 | `tsconfig.host.json`、`tsconfig.base.json`（doctor 仍是 workspace 包，必须接线） |
| 代价 | 启动器新增一个 Node 入口脚本并改 `preflight.Runner` 的 argv 来源 |
| 风险 | **低**。doctor 仍在 `packages/` 内 → 打包闭包的 `inject_workspace_pkg` 循环仍能找到它 → `loader-probe` 的 `require.resolve` 路径不变 |
| 代价之外 | `dsh doctor` 作为命令行子命令消失，只能经启动器触达（**需确认你是否还需要手动执行它**） |

### 方案 B：doctor 整体迁出 `packages/`（进 `apps/desktop-launcher/`）

**做法**：`packages/support/doctor/` → `apps/desktop-launcher/doctor/`。

| 项 | 值 |
|---|---|
| 消除文件 | **10** = 方案 A 的 6 个 + `tsconfig.host.json` + `tsconfig.base.json` + `scripts/verify-package-readme-model-experience.ts` + `scripts/doc-standard.spec.ts`（后两者的改动只是为 doctor 加白名单） |
| 附带收益 | §2.4 的 `verify-subsystem-pages` 红灯**自然消失**（不再存在 `packages/support` 组）；同时消除「复活已退役组名」这一结构性问题 |
| 代价 | ① `pnpm run test:coverage` 的 per-file 100% 门槛不再覆盖 doctor（**保护力下降**，需显式接受或另建替代门禁）；② 打包 staging 必须显式放置 `loader-probe` 产物与声明该导出的 `package.json`，否则打包态的动态加载检查会退化为 `needsTsx: true` 而闭包内无 tsx（[fork-divergence.md](./fork-divergence.md) §10.7 Spike-2 已实测） |
| 风险 | **中**。收益明确但需承接一项确定工作与一项保护力下降 |

### 方案 C：安全模式上游化

**做法**：向 `deepseek-ai/deepseek-harness` 提 PR，让 CLI 暴露 `loadProfile` 早已支持的 `userLayer: false`（例如 `--no-user-layer`）。

| 项 | 值 |
|---|---|
| 消除文件 | **3**：`packages/boot/app-boot/src/profile.ts`、`src/index.ts`、`tests/safe-mode.spec.ts` |
| 前提 | 上游接受。这是**唯一**能永久消除该偏离的路径（[fork-divergence.md](./fork-divergence.md) §10.1 已论证：`DSH_SAFE_MODE` 是「harness 已损坏仍能恢复」的承重机制，不可直接删除） |
| 风险 | **不可控**（取决于上游），但改动本身很小 |

### 方案 D：open-in-app / 宿主逃逸收敛

**做法**：当前**最大冲突源**（1299 提交那次合并贡献 8 个冲突文件）。三条候选路径：

| 路径 | 说明 |
|---|---|
| D1 上游化 | 若该能力（沙箱内经宿主通道探测并启动宿主应用）对上游有普适价值，提 PR |
| D2 外移为启动器自有客户端插件 | 参照 [fork-divergence.md](./fork-divergence.md) §11.2 已证实的机制（`dsh.client.platform === 'web'` + `exports["./client"]`，真实先例 `packages/experimental/agent-team-web-profile`），把 `ui-open-in-app` 的 fork 改动搬进启动器自有包 |
| D3 接受 | 明确记录为「长期维护成本」，不收敛 |

**注意**：`packages/host/open-in-app/src/resolver.ts` 等宿主侧改动（+225 行）**无法**靠客户端插件机制外移，D2 只能覆盖客户端那一半。需先做一次可行性调研（本次未做）。

### 方案 E：门禁适配下沉（D 组）

| 文件 | 现状 | 收敛路径 |
|---|---|---|
| `verify-repository-references.{ts,spec.ts}` | 加了 `excludedFiles` 白名单（launcher 四份文档 + `remote.go`） | 向**上游**提一个「白名单可配置」的 seam，或接受该偏离（上游热度仅 3–4 次，**优先级最低**） |
| `verify-client-ui-i18n.{ts,spec.ts}` | 加了 HTML 扫描 + 带壳前端规则（+85 行） | 带壳前端是 fork 独有产物，可把该检查下沉为启动器自有脚本 + `lefthook` 作业（`lefthook.yml` 已有先例：`launcher frontend layout` 作业）。**收益明确** |
| `verify-package-readme-model-experience.ts`、`doc-standard.spec.ts` | 各 +1 行，为 doctor 加白名单 | **随方案 B 自动消失** |

### 方案 F：机械配置下沉（G 组）

| 文件 | 收敛路径 |
|---|---|
| `.gitignore`（+20） | 启动器产物规则下沉到新建的 `apps/desktop-launcher/.gitignore`（git 原生支持嵌套，**上游热度仅 12 次**） |
| `lefthook.yml`（+14/−1） | 布局门禁移到启动器自有脚本；`pnpm` → `npm` 的换法可用 `.npmrc` 替代 |
| `tsdown.config.ts`（+4） | 纯注释，可直接回退 |

### 不可消除项（明确接受）

| 项 | 原因 |
|---|---|
| `pnpm-lock.yaml` | workspace 只要增删包就必然变化，且上游热度 1811 次，是全仓最热文件 |
| `tsconfig.host.json` / `tsconfig.base.json` | 只要 doctor 还是 workspace 包就必然被引用（**方案 B 才能消除**） |
| `packages/client/modules/*`（H 组） | 本质是上游 bug/性能修复（`newlineCount` 遍历 → `indexOf`；`resolveSync` 跨 Node 版本兜底），建议**上游化**而非长期藏在 fork |

---

## 七、优先级与分期

按「收益 / 风险」排序，而非按组号：

| 阶段 | 内容 | 收益 | 风险 | 可逆 | 状态 |
|---|---|---|---|---|---|
| **P0** | 补 `packages/support/README.md` 组 README | 消除当前唯一的红灯门禁 | 极低 | 是 | **已作废**——阶段 1 把 `packages/support/` 整体移走，不再需要组 README |
| **P1** | 方案 E 的 `verify-client-ui-i18n` 下沉 + 方案 F 三件套 | 消除 5 个 A 类文件，均为低热度 | 低 | 是 | 未执行（= 迁移方案 Task 9） |
| **P2** | 方案 A（CLI 入口外移） | 消除 6 个 A 类文件，含最热的 `apps/cli/package.json` | 低 | 是 | **已执行**（阶段 1，与 P3 同批原子提交） |
| **P3** | 方案 B（doctor 迁出 `packages/`） | 在 P2 基础上再消除 4 个文件 + 结构性消除组名问题 | 中（需承接 loader-probe staging 与覆盖率下降） | 是（纯移动） | **已执行**（阶段 1）；**但覆盖率下降的承接未完成**——见 [doctor-migration-plan.md](./doctor-migration-plan.md) 的 Step 2.3 一节 |
| **P4** | 方案 D（open-in-app 调研 → 收敛） | **当前最大冲突源**（24 文件 / 单次合并 8 冲突） | 待调研 | 是 | 未执行——**现已是剩余最大冲突源** |
| **P5** | 方案 C（安全模式上游化 PR） | 消除 3 个文件 | 不可控 | 是 | 未执行（决策点 D2，已决定暂缓） |

> **P2 + P3 已由阶段 1 完成**，冲突面从 85 降到 49，且全仓上游热度最高的四个文件（`pnpm-lock.yaml`、`tsconfig.host.json`、`tsconfig.base.json`、`apps/cli/package.json`）全部回到上游原文。
>
> **接下来的建议顺序**：P4（现为最大冲突源，24 文件 / 单次合并贡献 8 个冲突，远大于安全模式的 1 个）→ P1（低热度、低风险）→ P5（受上游节奏约束）。

---

## 八、验收标准

每个阶段独立验收，不以「全绿」代替：

1. `node --import tsx/esm scripts/verify-subsystem-pages.ts` **exit 0**（P0）。
2. 各阶段声明的文件在 `git diff upstream/master HEAD` 中**消失**，且 `git diff` 中不新增其他上游文件的改动。
3. doctor 的 **77 项测试全绿**（10 个 spec），且测试跟随源码位置迁移。
4. 实跑启动器预检链路：故意破坏一个 profile 的 `cordis.patch.yml`，确认诊断仍能给出报告（**方案 A/B 必做**，因为 CLI 入口形态变了）。
5. 打包态验证：`pnpm run build` 后在玲珑 stage 内实跑一次 `--json` 全量诊断（**方案 B 必做**，验证 `loader-probe` 的 `require.resolve` 路径未断）。
6. 闸门基线：`verify-repository-references`、`verify-package-readme-limitations`、`verify-package-invariants` 保持 exit 0。

---

## 九、未决事项

1. **`dsh doctor` 是否还需保留为命令行子命令**——方案 A 会删掉它。若你日常手动执行 `dsh doctor`，方案 A 需改设计。
2. **是否接受 doctor 失去覆盖率门槛**——方案 B 的代价之一，[fork-divergence.md](./fork-divergence.md) §10 列为「保护力下降」，需显式接受或另建替代门禁。
3. **本文档是否加入 `verify-repository-references` 豁免清单**——加入后可写裸提交哈希，与同目录另四份文档一致；**该改动需单独批准**。
4. **open-in-app 的收敛路径未调研**——D1/D2/D3 三选一需要一次独立可行性调研（本文档只给出候选与已证实的机制先例）。
5. **[fork-divergence.md](./fork-divergence.md) 基准已过期**——其记录为「23 个 A 类文件 / 304 文件偏离」，实测现为「A 类 45 + B 类 4 / 49 文件偏离」（口径：排除 `apps/desktop-launcher/`、`docs/superpowers/`、`.agents/` 三处独立目录）。该文档的处置建议需按新基准重校；注意其总偏离计数（含 fork 私有目录）也同步过期：记 304 文件 / +44438 −96，实测 374 文件 / +58868 −113。

---

## 附录：相关文档

| 文档 | 内容 |
|---|---|
| [fork-divergence.md](./fork-divergence.md) | fork 与上游的偏离全貌、doctor 与客户端粘贴两个专项的收敛方案（基准待更新） |
| [AUDIT.md](./AUDIT.md) | 启动器**自身**缺陷清单 |
| [i18n.md](./i18n.md) | 外壳国际化方案 |
| [index.md](./index.md) | 本目录索引 |

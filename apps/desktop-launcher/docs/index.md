# 启动器审计与方案文档

本目录存放 `apps/desktop-launcher/` 相关的**审计与方案文档**。

## 文档分工

| 文档 | 回答的问题 | 体量 |
|---|---|---|
| [fork-divergence.md](./fork-divergence.md) | **fork 和上游差在哪里、怎么收敛**——逐项盘点 304 个文件 / +44438 行的偏离，量化冲突风险，并给出 `doctor` 包与客户端粘贴两个专项的收敛方案 | 632 行 |
| [AUDIT.md](./AUDIT.md) | **启动器自己哪里有问题**——59 条编号发现（`N3`／`N17`／`S1–S7`／`N30` 等），含按状态排序的**条目总览表**与附录 A–H（修复批次、验证边界、起手顺序、复核记录） | 902 行 |
| [i18n.md](./i18n.md) | **外壳文案怎么国际化**——真源选型（GUI `<html lang>` 上报）、字典与闸门规范、P0–P2 分期实施与验证方案；对应 `AUDIT.md` 第 24 条 | 263 行 |
| [merge-conflict-convergence.md](./merge-conflict-convergence.md) | **合并上游时冲突出在哪些文件、怎么消除**——实测最近两次合并的冲突面，把 49 个偏离文件按 fork 特性归组并量化上游热度，给出 A–F 六档可独立取舍的收敛方案与分期 | 320 行 |
| [doctor-migration-plan.md](./doctor-migration-plan.md) | **doctor 怎么搬进启动器、每一步怎么做**——把自研诊断能力（doctor 包、`dsh doctor` 入口、`DSH_SAFE_MODE`）全部收敛进 `apps/desktop-launcher/` 的可执行方案：目标架构、影响面清单、阶段 0 spike、四阶段任务（含 TDD 步骤与确切命令）、验收标准、待决策项与回滚策略 | 1233 行 |

五份文档角度互补、互不重复：`AUDIT.md` 审的是**实现质量**，`fork-divergence.md` 审的是**与上游的关系**，`merge-conflict-convergence.md` 审的是**合并成本**，`i18n.md` 是一份**待执行的功能方案**，`doctor-migration-plan.md` 是一份**已部分执行的迁移方案**（阶段 0 与阶段 1 已落地）。

> `AUDIT.md` 原先在启动器根目录，现已移入本目录并**保留原文件名**——因为仓库内多数引用是裸文件名（`AUDIT.md` N16／N19／N26 等），保名可让它们继续有意义；另 2 条 Agent Notes 曾写完整旧路径，已在 `bee1a78ff9` 一并修正。

> 本目录的索引**刻意不叫 `README.md`**：仓库的翻译配对闸门按**基名**收 `README.md`（`scripts/translation-pairing.ts:132` 的 `README_ARTIFACT`），叫这个名字会让一份内部单语索引背上双语配对义务，而本目录其余文档都是单语。闸门范围的完整判据见 `AUDIT.md` 开头第 5 行。

## 阅读顺序

1. **想了解偏离全貌** → [fork-divergence.md](./fork-divergence.md) 第一~三章（基准、总量、分类）
2. **想知道每一项怎么办** → 同文档第七章「逐项处置建议与可行性证据」
3. **想评估 doctor 迁移** → 同文档第十章（含三个 spike 的实测结论）
4. **想评估客户端粘贴** → 同文档第十一章
5. **想先看启动器还有哪些没修** → [AUDIT.md](./AUDIT.md) 开头的「条目总览」表（按状态排序：未修 → 部分修复 → 已修）
6. **想知道某条结论的证据强度** → 同文档的「状态与验证等级」节，以及附录 H（2026-09-20 对全部 33 条未闭环条目的独立复核）
7. **想做外壳国际化** → [i18n.md](./i18n.md) 第五节（真源与数据流）与第七节（分期实施）；实施前先读第九节的已知边界与第十节的未决事项
8. **想减少每次合并上游的冲突** → [merge-conflict-convergence.md](./merge-conflict-convergence.md) 第三节（最近两次合并的实测冲突面）与第六节（A–F 六档收敛方案）；动手前先读第九节的未决事项
9. **想把 doctor 真正搬进启动器** → [doctor-migration-plan.md](./doctor-migration-plan.md) 第三节（关键技术决策）、第四~七节（分阶段任务）与文末「阶段 1 执行记录」；阶段 0 三条 spike 与阶段 1 的 Task 1–5 **已执行并验证**，阶段 2 开工前必须先定第九节的 D2

## 状态约定

本目录各文档的状态标注是**几套不同词汇**，不要混读：

| 文档 | 标注 | 含义 |
|---|---|---|
| `AUDIT.md` | ✅ 已复核／实测复核 | 审计者逐行读代码或实跑命令取得证据 |
| `AUDIT.md` | ⚠️ 静态审查 | 经代码阅读得出，未独立复跑 |
| `AUDIT.md` | ⏳ 待生效 | 改动已完成，但需一次真实构建或发版才到达用户 |
| `fork-divergence.md` | ✅ **已证实** | 有实跑输出或权威引用支撑 |
| `fork-divergence.md` | ⚠️ **调研中** | 结论未出，**不作为决策依据** |
| `fork-divergence.md` | 未标注 | 基于代码阅读的推断，需实施时验证 |
| `i18n.md` | 实施中 | P0、P1、P2、P5 已完成（壳前端中文清零、前端字典 246 键 + Go 字典 48 键中英同集、i18n 闸门覆盖带壳前端、Go 侧四个扫描用例兜底）；打包态实测（8.6）已完成，实施侧无待办 |
| `merge-conflict-convergence.md` | 实测 | 结论由 `git merge-tree`、全历史路径探查与实跑门禁取得，命令随文给出 |
| `merge-conflict-convergence.md` | 待调研 | 结论未出，**不作为决策依据**（如 open-in-app 的收敛路径） |
| `doctor-migration-plan.md` | 实测 | F1–F13 全部由实跑命令或权威读码取得；方案中的每条命令与预期输出均已给出 |
| `doctor-migration-plan.md` | 阶段 1 已执行 | 阶段 0 三条 spike 全绿；阶段 1 的 Task 1–5 与 §5.5 全部完成并验证，证据见文末「阶段 1 执行记录」。阶段 2–3 未开始，D2 未定 |

**重要**：`fork-divergence.md`、`i18n.md` 与 `merge-conflict-convergence.md` 中的处置建议**均未执行**——它们记录的是审计结论与待批准方案，不代表任何代码已被改动。`AUDIT.md` 与 `doctor-migration-plan.md` 则不同：前者状态行同时记录「已修」与「未修」，其中 26 条已修项各自有对应提交与验证证据；后者的阶段 0 与阶段 1 Task 1–5 已落地，逐项附验证证据。

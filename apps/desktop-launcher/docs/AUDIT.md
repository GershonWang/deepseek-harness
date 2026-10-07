# DeepSeek Harness 桌面启动器待办清单

本文是 `apps/desktop-launcher` 及其玲珑打包链路**尚未修复**的问题清单，面向维护者。已修复、已失效或前提不成立的条目一律不在本文——修完一条就删一条，本文清空之日即删除本文之时。被删条目的完整历史与验证证据在 git 里：最后一份含全部条目与附录的版本是提交 `c91f00f1ce`（可用 `git show c91f00f1ce:apps/desktop-launcher/docs/AUDIT.md` 取回）。

它不在文档闸门覆盖范围内：`verify-translation-pairing` 的语料范围由 `scripts/translation-pairing.ts:172-179` 的 `isTranslationScopeFile` 判定，只收 README 三件套、仓库根的 paired docs、`.agents/notes/**` 与仓库根的 `docs/**`／`python/**`；`verify-md-wrap` 与 `verify-md-links` 的 glob 也不含 `apps/`。因此**没有自动化手段保证本文与代码同步**：改动任一条目后请在同一次提交里更新本文，修完即删除该条。

## 收录范围

本清单只收**修复点落在 `apps/desktop-launcher/` 内**的缺陷。本 fork 的约束是与上游尽量一致：`apps/desktop-launcher/` 之外的文件一律不动，只有玲珑客户端真的无法工作时才最小量修改上游代码。因此下列缺陷**不入清单**（卡点写在括号里，避免重复提出）：

- **要改上游配置文件的**：`.gitlab-ci.yml` 与 `.github/workflows/`（上游未改动，新增即是新增偏离）、`lefthook.yml`（上游文件，fork 已改 +13/−1，再加只会加重偏离）。
- **根因或改动点在上游工具／产物的**：ll-builder 生成的 `buildext.sh`（`|| echo "$?"` 吞错）与根 `linglong/entry.sh`（`CFLAGS="-g"`）、ll-builder 把 `failed to copy` 降级为警告、上游 overlay 的写入不落盘、`WEBKIT_EXEC_PATH` 所需的 `DEVELOPER_MODE` webkit 发行物、上游基础镜像里坏掉的 `xdg-open`。
- **要上游 loader／服务端配合的**：插件树挂载复用（启动预热／按需加载）。
- **在 app 之外的上游包里、且属加固而非功能缺陷的**：`packages/client` 侧发起的 `postMessage(..., '*')`（接收侧已有 `event.source !== window.parent` 校验）。

`9 / 10` 与 `N-extra2` 只保留能在 app 内完成的那一半（体积断言、`make` 汇总入口）；CI 与 pre-push 接线按上述约束不做。

## 复核基点

- 分支 `linglong`，HEAD `56dc9f3840`（2026-10-07），玲珑包 `0.1.5.1`。
- 产物证据取自当日交付层 `~/.cache/linglong-builder/merged/acbfe51b…/files`、仓库根 `linglong/` 的当日构建工作区与 `apps/desktop-launcher/linglong/stage/`。
- **环境限制**：本机无 `ll-builder` 与玲珑容器、无 gcc；未跑真实构建与安装、GUI 启动、需外网的 `DSH_TC_E2E=1`。涉及容器与真机的结论都需在用户机器上闭环。
- 行号以该基点为准，代码改动后会漂移；按函数名/符号名定位比按行号可靠。

## 状态与验证等级

| 标记 | 含义 |
|---|---|
| 未修 | 缺陷完整存在 |
| 部分修复 | 已修一部分，剩余部分列在条目内 |
| ✅ 已复核 | 逐行读代码或实跑命令取得证据 |
| ⚠️ 静态审查 | 经代码阅读得出，未独立复跑 |

## 条目总览

| # | 级别 | 条目 | 状态 |
|---|---|---|---|
| N3 | 高危 | 工具索引来自个人 fork 的可变分支，且无签名 | 部分修复 |
| 8 / 13 | 中危 | WebKit 依赖链未裁剪，`depends.yaml` 无人使用 | 未修 |
| 9 / 10 | 中危 | 无体积门禁（CI 按约束不做） | 未修 |
| 11 | 中危 | `inject_workspace_pkg` 仍是黑名单模式 | 未修 |
| 23 | 中危 | 打包态与外部 harness 共享 `~/.dsh` | 未修 |
| 32 | 中危 | 注入链路仍是三层补丁 | 未修 |
| 35 | 中危 | 外链桥只在容器模式生效 | 未修 |
| S6 | 中危 | 项目配置 tool ID 未校验 | 未修 |
| N22 | 中危 | 端到端审计只覆盖 `versions[0]` | 未修 |
| N32 | 中危 | 探针把健康 bundle 指为元凶并据此给出 L2 修复 | 未修 |
| N16 | 中危 | `verify-tools.sh` 的一致性校验不覆盖多版本 | 部分修复 |
| 17 | 低危 | WebKit 单进程模式 | 未修 |
| 25 | 低危 | 系统托盘 | 未修 |
| 31 | 低危 | connector probe 非幂等 | 未修 |
| N-extra2 | 低危 | 自动化测试无汇总入口（hook 接线按约束不做） | 未修 |
| N24 | 低危 | `ToolVersion.LibRel` 无消费点 | 未修 |
| N26 | 低危 | `fonts-wqy-microhei` 声明的中文字族看不到 | 未修（待查） |
| N29 | 低危 | `*.tsbuildinfo` 随包交付 | 未修 |
| N31 | 低危 | `plugin-patch-composable` 一律标「可修复 L2」 | 未修 |
| N34 | 低危 | `plugin-dynamic-load` 的「未能定位」用例偶发失败 | 未修 |
| 22 | 低危 | `/tmp/dsh-webkit-4.1` 符号链接仍建在 `/tmp` | 部分修复 |
| 27 | 低危 | 窗口位置未记忆 | 部分修复 |
| 28 | 低危 | 窗口背景色硬编码 | 部分修复 |
| N-extra | 低危 | 前端异步错误兜底缺口 | 部分修复 |
| N-extra3 | 低危 | 打包脚本中重复与漂移的事实 | 部分修复 |

---

# 一、高危

## N3 工具索引来自个人 fork 的可变分支，且无签名

- **状态**：部分修复｜✅ 已复核（2026-10-07）
- **位置**：`internal/toolchain/remote.go:44`（`defaultIndexURL`）、`internal/toolchain/remote_test.go`（守卫 `TestDefaultIndexURL_PinnedToCommit`）
- **问题**：索引地址指向个人账号 fork（`GershonWang/deepseek-harness`）上的文件，可变引用；索引同时提供下载 URL 与 `sha256`，于是「sha256 校验」与下载来源出自同一份未经认证的数据，只保证传输完整、**不提供来源认证**。`DSH_TOOLCHAIN_INDEX_URL` 还可直接覆盖该地址。
- **影响**：控制该账号/分支、或能改写该 URL 的中间人，可让用户在「工具链市场」安装任意代码；解压产物经 `ReconcileBinLinks` 软链进 `~/.dsh-tools/bin`，而该目录被前置进 harness 子进程 `PATH`。
- **已修部分**：默认地址已固定到不可变提交哈希（现为 `1cf258638a04e3db574061f6ac7ace4e3bf898c4`，其 `index.json` blob `5d7e4947…` 与工作区一致；实跑 curl HTTP 200、sha256 `9e744b9c…` 逐字节一致），信任对象由「上游账号」收敛为「这份二进制」；中英 README 已撤掉「sha256 即安全」的表述。
- **剩余问题**：仍无**离线公钥签名**。它能在保留「只发索引不发客户端」更新方式的同时提供来源认证，但需要密钥托管与签名发布流程，属产品决策。
- **修复建议**：索引附 ed25519/minisign 签名，公钥编译进客户端；`docs/toolchain-index-release.md` 补签名与验证步骤。
- **验收**：篡改索引内容后客户端拒绝加载（负例用例）；发布文档含签名流程。

---

# 二、中危

## 8 / 13 WebKit 依赖链未裁剪，`depends.yaml` 无人使用

- **状态**：未修｜✅ 已复核（2026-10-07）
- **位置**：`linglong/linglong.yaml`（webkit 段）、`linglong/verify-tools.sh`、构建产物 `linglong/depends.yaml`
- **问题**：去重结果没问题（单一实体 `libwebkit2gtk-4.1.so.0.19.7` 92.8 MB + 两条软链，补丁版胜出），但 `skip_existing` 在源码与生成物中都没有该配置键，去重完全依赖 ll-builder 默认行为，仓库既没声明也没校验。更关键的是**依赖链没有裁剪或比对**：`lib/x86_64-linux-gnu` 实测 **293 MB / 352 项**（180 个普通文件 + 172 条软链），而 `depends.yaml` 有 **175 条**，且没有任何受控文件消费它——`git grep depends.yaml` 唯一命中 `clean-linglong.sh` 的一句注释。
- **影响**：apt 默认 Recommends 带进了与嵌入式本地 Web 应用无关的栈——`gstreamer-1.0` 22 MB、`mfx` 12 MB、`lapack` 7 MB、`ImageMagick-6.9.13` 4.3 MB、`OpenNI2` 1.3 MB、`directfb-1.7-7` 1.2 MB、`perl5` 1.1 MB，另有 `blas`/`caca`/`enchant-2`，合计约 50 MB+。
- **修复建议**：以 `depends.yaml` + `tools.yaml` 为准做一次依赖链比对，摘掉用不到的多媒体/图形栈；把 `skip_existing` 从注释变成显式配置或校验。
- **验收**：比对脚本对当前清单输出可裁清单；体积断言（`lib/x86_64-linux-gnu` 不超过阈值）落到 `build-linglong.sh`，超阈值即中止导出。

## 9 / 10 无体积门禁（CI 按约束不做）

- **状态**：未修｜✅ 已复核
- **位置**：`build-linglong.sh`（断言落点）；`.gitlab-ci.yml` 与 `.github/workflows/` 是上游未改动文件，按「收录范围」不动
- **问题**：全仓无任何体积断言；`build-linglong.sh` 只在末尾用 `du -h` 打印一行产物体积，超阈值不会失败。GCC 工具链、Node 头文件、依赖链之类的回归因此不会被拦下。
- **影响**：打包全靠手工 `sh apps/desktop-launcher/build-linglong.sh`，回归只能靠事后比对。
- **修复建议**：在 `build-linglong.sh` 的 export 之前加体积断言：`lib/gcc` 与 `node/include` 必须不存在（现在由 `prune-gcc-toolchain.sh` 与 `prepare-offline.sh` 保证，但没有断言），`lib/x86_64-linux-gnu` 不超过阈值。与 `verify-merged-deps.sh` 同处调用，失败即中止导出。
- **验收**：故意把 `lib/gcc` 造回来或把阈值调低时，`build-linglong.sh` 在导出前非零退出。

## 11 `inject_workspace_pkg` 仍是黑名单模式

- **状态**：未修｜✅ 已复核
- **位置**：`linglong/prepare-offline.sh`（`inject_workspace_pkg` 及其调用循环）
- **问题**：遍历 `packages/*/*/` 后在循环内排除 `test-support`／`typert/generator`；函数内另有 experimental 与非 `@deepseek-ai/*` 两条黑名单。**没有任何显式白名单**，因此任何新增的 workspace 包都会默认进入生产闭包。
- **影响**：`packages/experimental/` 现已有 23 个包目录；黑名单模式下漏排一类就会静默进包，且不会有任何断言提示。
- **修复建议**：改为显式白名单，只注入标准 preset 实际列出的包。
- **验收**：新增一个 workspace 包后闭包内不出现它（除非显式加入白名单）。

## 23 打包态与外部 harness 共享 `~/.dsh`

- **状态**：未修｜✅ 已复核
- **位置**：`internal/appenv/env.go`（四处构造 `supervisor.Config`）、`internal/app/preflight.go`、`internal/preflight/preflight.go`
- **问题**：普通启动路径完全不注入 `DSH_HOME`，子进程与 launcher 共享同一份环境快照；预检与 doctor 也指向 `home/.dsh`。只有用户主动点「全新环境启动」的降级路径才用 `~/.dsh-fallback`。
- **影响**：`README.zh.md`「已知事项」已记录——不同版本的外部 harness 可能把 `~/.dsh/.credentials.yaml` 写成内置 harness 无法解析的格式，导致启动即崩、进入重启循环。
- **修复建议**：打包态改用独立 `DSH_HOME`（如 `~/.config/dsh-desktop/dsh/`），与 npx／外部安装彻底隔离。
- **验收**：打包态与外部 harness 各自读写不同的 home；外部写入不影响的启动。

## 32 注入链路仍是三层补丁

- **状态**：未修｜✅ 已复核
- **位置**：`scripts/fix-deploy-closure.mjs`（**fork 自有文件**，`83a153f124` 从 desktop fork 移植）、`linglong/prepare-offline.sh`（`inject_workspace_pkg`）、`linglong/inject-link-bridge.sh`
- **问题**：让打包态跑起来至少依赖三层对 `pnpm deploy` 与上游架构的补丁，三层齐在且仍被调用；其中一层还停在 `apps/desktop-launcher/` 之外的仓库根 `scripts/`。
- **影响**：上游迭代时任何一层都可能失效，而失效方式通常是静默的；散落在 app 之外的那一层还额外增加与上游的文件树差异。
- **修复建议**：①把 `scripts/fix-deploy-closure.mjs` 移进 `apps/desktop-launcher/tools/`——它是 fork 自有文件，移动只会减少偏离，并把三层收敛到同一目录；②长期方向是上游把闭包打成官方 preset／bundle，下游只做组装。
- **验收**：仓库根 `scripts/` 不再有 fork 自有的闭包补丁；三层补丁全部位于 `apps/desktop-launcher/` 内，且打包态仍能启动。

## 35 外链桥只在容器模式生效

- **状态**：未修｜✅ 已复核
- **位置**：`linglong/inject-link-bridge.sh`（现状）、`internal/webviewperm/permission_linux.c`（可复用的 GTK 侧信号接管模式）、`README.zh.md`（已知事项）
- **问题**：桥在打包时注入 GUI dist，因此只覆盖容器内运行的 harness。连接外部服务时，外部 harness 服务的是未注入的 GUI，其中 `target="_blank"` 外链点击没有反应。
- **影响**：容器模式与外部服务模式行为不一致，外部模式下外链是死链。
- **修复建议**：改为在 launcher 内从 GTK 侧接管 WebKit 信号——`internal/webviewperm/permission_linux.c` 已演示「遍历顶层窗口取回 `WebKitWebView` 并连信号」的成熟做法，同一条路可连 `create`／`decide-policy`，把新窗口请求交给 `BrowserOpenURL`。这样两种模式都覆盖，且完全落在 `apps/desktop-launcher/` 内，不需要动 `packages/client`。
- **前置**：先做一次小 spike，确认该 Wails 版本下 `create` 与 `decide-policy` 的触发时机（Wails 自身是否已处理新窗口请求）。
- **验收**：连接外部服务时 `target="_blank"` 外链可点击；容器模式下行为不变。

## S6 项目配置 tool ID 未校验

- **状态**：未修｜⚠️ 静态审查
- **位置**：`internal/toolchain/project.go`（`ResolveProject`／`ApplyProject`）、`internal/toolchain/catalog.go`（`currentLink`、`SetActiveVersion`）
- **问题**：`.dsh-toolchain.yml` 的 `tools:` 键从不与清单比对，`id` 直接进入 `currentLink(dir, id)` 与 `versionDir(dir, id, version)`；带 `../` 的 ID 会让 `os.Remove(link)` 删除安装目录之外的同名文件。
- **影响**：需前端直接调用 `ApplyProjectToolchain`（`internal/app/app.go`）才能触达；该能力后端已实现而前端未接，影响面受限。
- **修复建议**：在 `ResolveProject` 里用 `LookupTool` 校验每个 ID，非法即报错跳过。
- **验收**：含 `../` 的配置项被拒绝且有测试固定。

## N22 端到端审计只覆盖 `versions[0]`，新增版本没有实证防线

- **状态**：未修｜✅ 已复核（2026-10-07）
- **位置**：`internal/toolchain/e2e_install_test.go`、`internal/toolchain/install.go`（空 version 落到 `LatestVersion()`）
- **问题**：`TestE2E_CatalogInstall` 是清单里「地址可达、归档与清单 sha256 一致、解压布局符合 `bin_rel`/`bin_names`、声明的命令都出现在 `bin/`」的唯一实证手段，但它对每个工具只装 `versions[0]`；`DSH_TC_E2E_IDS` 也只能按工具 ID 过滤。
- **影响**：`jdk` 现有五条版本线（`21.0.12.1`/`8u504`/`25.0.4.1`/`17.0.20.1`/`11.0.32.1`），其中**四条非推荐版本**不在任何自动化覆盖内；镜像站轮换或 sha256 抄错一个字符，只会在用户点安装时暴露（grpcurl 的 sha256 抄错一字符就是由它首次跑出）。
- **修复建议**：让该用例遍历每个工具的 `versions`，或增加一个按版本过滤的环境变量。
- **验收**：`DSH_TC_E2E=1` 跑过全部版本线，或至少可指定版本过滤并覆盖非推荐版本。

## N32 探针因与插件无关的原因失败时，`bisectBy` 会把健康的第三方 bundle 指为元凶

- **状态**：未修｜✅ 实测复核（2026-09-25；2026-10-07 迁移后复跑）
- **位置**：`doctor/src/checks/plugins.ts`（`locateCulprit`、`plugin-dynamic-load` 检查与其 `fix`）、`doctor/src/bisect-by.ts`、`doctor/src/loader-probe.ts`（每次探测都无条件改写 `<home>/profiles/web/cordis.yml`；`selectLayers` 的 `--include` 为空表示「挂全部层」，因此**无法**表达「一个第三方层都不挂」）
- **问题**：`locateCulprit` 先用全量探针判断「树没起来」，再把全部第三方 bundle 交给 `bisectBy` 二分。`bisectBy` 的文档契约要求调用方保证 `isBad([]) === false`，但算法自身从不探测空集，收尾的 `verifyBad = isBad([result])` 对「全局失败」同样恒真——于是任何与具体 bundle 无关的失败（home 不可写、超时、缺 node、探针自身异常）都会被判成「某个 bundle 有罪」，返回二分命中的第一个名字。
- **影响**：报告把健康的第三方插件指为元凶，用户据此手工禁用或卸载它会造成真实损失。自动修复本身是自还原的（始终不通过则整体还原 manifest 并返回 `ok:false`），但修复过程会临时把全部第三方 bundle 从 profile 的 `package.json` 里摘掉，中途被中断就停在该状态，而 `recordAutoDisabled` 只在成功分支调用，用户拿不到事后提示。
- **复现**：把 `~/.dsh` 置于只读后跑全量诊断，探针以 `EROFS … open '/home/Jokul/.dsh/profiles/web/cordis.yml'` 退出；`plugin-dynamic-load` 却报「插件 dshmarket 导致启动失败（缺少运行依赖或损坏）」，并给出 `fixable: true`、`suggestedLevel: 2`。换成可写的影子 home 复跑，同一检查变为「所有 3 个第三方插件加载正常」。
- **修复建议**：①给探针加一个能表达「只挂官方层」的入口（如 `--include none`），`locateCulprit` 在全量失败后先跑这个基线，基线**也**失败即返回 `culprit: null`，由检查走已有的诚实分支（`fixable:false`「未能定位」）且不提供 L2 修复；②`locateCulprit` 在二分前用该基线显式验证 `bisectBy` 的契约；③给探针退出码分档（插件加载失败 vs 环境/IO 失败），检查按档决定是否归因。
- **验收**：home 不可写时报告不再点名任何 bundle，且不提供 L2 修复；有测试固定该分支。

## N16 `verify-tools.sh` 的一致性校验不覆盖多版本

- **状态**：部分修复｜✅ 实测复核（2026-10-07）
- **位置**：`linglong/verify-tools.sh`、`linglong/tools.yaml`（`installable` 段）
- **问题**：`installable` 每个工具只有一组 `version`/`url`/`sha256`，因此「sha256 含占位符即失败」这条检查只覆盖**推荐版本**；校验也只 `diff` ID 集合，不比对 `version`/`url`/`sha256`。
- **影响**：`jdk` 在 `index.json` 里有五条版本线，而 `tools.yaml` 的 `installable.jdk` 只有 `21.0.12.1` 一组——其余**四条**即便写成占位符也能通过构建。
- **已修部分**：缺 `index.json` 与缺 `python3` 均已改为硬失败并点明原因，不再静默跳过。
- **修复建议**：把 `installable` 段扩展为多版本格式，让脚本逐版本比对 `index.json`。
- **验收**：任一版本的 sha256 写成占位符时构建失败。

---

# 三、低危

## 17 WebKit 单进程模式

- **状态**：未修｜✅ 已复核
- **位置**：`internal/packaging/webkit_linux.go`
- **问题**：只设 `WEBKIT_INJECTED_BUNDLE_PATH` 与 `WEBKIT_DISABLE_DMABUF_RENDERER`（NVIDIA 兜底），无 `WEBKIT_DISABLE_COMPOSITING_MODE` 或单进程开关。
- **影响**：容器内 WebKit 多进程渲染在部分宿主上不稳定，缺可回退的开关。
- **修复建议**：提供可配置的单进程/禁用合成开关（放进 `Config` 而非硬编码）。
- **验收**：开关可经配置打开且生效。

## 25 系统托盘

- **状态**：未修｜✅ 已复核
- **位置**：`main.go`、`apps/desktop-launcher/go.mod`、`internal/app/app.go`（`OnBeforeClose`）
- **问题**：无托盘库、无 `HideOnClose`，`OnBeforeClose` 只保存窗口状态。关窗即退出，后台任务随之中断。
- **影响**：harness 跑长任务时关窗会中断。
- **修复建议**：引入托盘（`OnBeforeClose` 返回 true 并隐藏窗口），托盘菜单提供退出。
- **验收**：关窗后进程与 harness 继续运行，可从托盘退出。

## 31 connector probe 非幂等

- **状态**：未修｜✅ 已复核
- **位置**：`internal/connector/connector.go`（`Probe` 与其调用点）
- **问题**：只判 `200 <= code < 400`，不校验响应体；探测通过即切 `ModeExternal`。
- **影响**：填入任意返回 2xx/3xx 的地址都会显示「已连接」，随后客户端连不上。
- **修复建议**：探测改为校验响应体确实是 harness 服务（例如命中随包 GUI 的标记或已知路由特征）。**不要**改成请求 `/api/health`——仓库里没有该端点（只有一处测试夹具），那会把改动推给上游。
- **验收**：指向静态文件服务器或任意 2xx 页面时探测失败；指向真实 harness 时通过。

## N-extra2 自动化测试无汇总入口（hook 接线按约束不做）

- **状态**：未修｜✅ 实测复核（2026-10-07）
- **位置**：`Makefile`（汇总入口落点）、`frontend/test-app.cjs`、`frontend/test-i18n.cjs`、`doctor`（`vitest.config.ts`）、`linglong/test-verify-tools.sh`；`lefthook.yml` 是上游文件（fork 已改 +13/−1），按「收录范围」不再追加
- **问题**：这些测试都能跑，但没有任何单一入口把它们串起来——`Makefile` 的 `test` 只跑 `go test ./...`（且需 cgo 与 GTK 开发库），前端、doctor 与打包脚本自测各自为政。
- **影响**：本轮实测 `go test ./internal/...`（13 个包）、`node --test frontend/test-app.cjs`（69 例）、`frontend/test-i18n.cjs`（20 例）、doctor 的 `vitest run`（11 文件 87 例）、`sh linglong/test-verify-tools.sh`（6 项）全部可跑，却没有任何出口自动执行它们。
- **附加问题**：`preview.mjs verify` 在 `buildPreview` 里用正则剥掉全部 `<script>`，再用手写 fixture 重建弹框 DOM，因此它一行 `app.js` 都不执行；实测当时那批缺陷全部存在时它仍全数通过。
- **修复建议**：在 `apps/desktop-launcher/Makefile` 增加一个 `check` 目标，串起 `CGO_ENABLED=0 go test ./internal/...`、两个前端用例、doctor 的 `vitest run` 与 `linglong/test-*.sh`。`Makefile` 在 app 内，不需要动上游。
- **验收**：`make check` 一条命令跑完全部套件；故意改坏一处前端逻辑时它非零退出。

## N24 `ToolVersion.LibRel` 无消费点

- **状态**：未修｜✅ 已复核
- **位置**：`internal/toolchain/catalog.go`（字段声明、`ReconcileBinLinks`）、`internal/toolchain/install.go`（只写进 `tool.yml`）
- **问题**：`lib_rel` 被声明、被解析、被写进安装目录的 `tool.yml`，但**没有任何读取点**：库目录绑定按 `root/lib`、`root/lib64` 是否存在决定，与清单声明的 `lib_rel` 无关。
- **影响**：清单作者以为改 `lib_rel` 就能改变 `LD_LIBRARY_PATH` 注入的库目录；发行包布局与声明不符时不会报错，只会静默少绑或不绑。
- **修复建议**：要么让 `ReconcileBinLinks` 真正消费该字段（未声明时保留现有探测作为回退），要么删掉字段与 `tool.yml` 里那一行。
- **验收**：清单改 `lib_rel` 能改变绑定，或字段被删除。

## N26 `fonts-wqy-microhei` 声明为容器中文字族来源，但产物与运行时都看不到它

- **状态**：未修（待查）｜✅ 实测复核
- **位置**：`linglong/linglong.yaml`（`buildext.apt.depends` 里的 `fonts-wqy-microhei`，以及 `build:` 段「中文族由下面的 apt 依赖提供」的注释）
- **问题**：该依赖被声明为容器里中文字体的来源，但实测已导出产物层与基座层里**没有任何 wqy／微米黑字体**（`find -iname '*wqy*' -o -iname '*microhei*' -o -iname '*.ttc'` 为空），`share/` 下只有 `applications`、`dsh-fonts`、`icons`；`0.1.5.1` 交付层同结论。
- **影响**：中文回退字体实际不来自这个依赖；运行时 `/usr/share/fonts` 又被宿主目录整体挂载覆盖，所以该依赖既进不了包、也改变不了运行时——注释描述的链路与事实不符，排查中文显示问题时会把人引向错误方向。产物侧已由 `verify-merged-deps.sh` 显式记为 `none:` 认领（不阻塞构建）。
- **证据边界**：未在真实容器里跑 `fc-list` 确认最终渲染走的是哪一路字体。
- **修复建议**：确认运行时中文来源（宿主挂载 vs 随包字体）后二选一——删掉该依赖并更正注释，或让中文字体随包落到 `${PREFIX}/share/dsh-fonts`（`install-container-fonts.sh` 已在该目录装配拉丁与等宽字体，可复用同一路径）。
- **验收**：注释与产物、运行时三者一致。

## N29 `*.tsbuildinfo` 随包交付

- **状态**：未修｜✅ 实测复核（2026-10-07）
- **位置**：`harness/node_modules/@deepseek-ai/**`、`harness/node_modules/gaxios/**`；来源是 `linglong/prepare-offline.sh` 整目录复制各包的 `lib/`
- **问题**：`0.1.5.1` 交付层与当日 stage 各有 **52 个 / 2,908,124 B** 的 tsc 增量编译元数据进产物（`@deepseek-ai` 侧 50 个 + gaxios 2 个），属「把构建中间态当交付物」。
- **影响**：体积少量增加；`.tsbuildinfo` 记录编译机上的文件清单与编译设置。
- **修复建议**：`prepare-offline.sh` 在复制后统一删除 `*.tsbuildinfo`（按文件名前缀删会漏掉 gaxios 的两个）。取舍：它只服务于增量编译，运行时无人读取。
- **验收**：产物内 `find -name '*.tsbuildinfo'` 为空。

## N31 `plugin-patch-composable` 一律标「可修复 L2」，但修复实现只覆盖五类告警中的两类

- **状态**：未修｜✅ 实测复核（2026-09-25；2026-10-07 迁移后复跑）
- **位置**：`doctor/src/checks/plugins.ts`（检查、`fix`、`removeOrphanedPatchEntries` 的判据）、`doctor/src/index.ts`（修复失败记为 skipped）；告警源头 `vendor/include/src/index.ts`
- **问题**：`plugin-patch-composable` 只要 `composeEntries` 收到**任何**一条告警就返回 `ok:false, fixable:true, suggestedLevel:2`，界面上因此出现「可修复 L2」；但它的 `fix` 只做一件事——按「条目 id 不在基础层合成结果里」从**用户补丁文件**里删条目。loader 在补丁合成期会发出五类告警（`insert` 目标不存在、`insert` 目标不是 group、非 insert 缺 id、非 insert 目标不存在、name 与目标不符），该判据只覆盖第 1、4 类；第 2、3、5 类（含最常见的 name 不符）必然走到 `{kind:'none-removed'}`，`fix` 返回「无法定位失效补丁条目，未做修改」。告警若来自随包 bundle 层而非用户文件，同样无法通过编辑用户文件消除。
- **影响**：用户按界面提示点「修复」必然失败；而真正的问题（补丁条目被整条跳过、用户配置静默不生效）只以一行英文告警呈现，没有指出「哪个文件、哪一行、该改成什么」。
- **修复建议**：①让 `fixable` 与实际能力一致——收窄为「告警中至少有一条是 `removeOrphanedPatchEntries` 能处理的」，其余返回 `fixable:false`，并在 `message`／`detail` 里点名文件路径、条目 id、声明名与目标当前名；②（可选）对 name 不符增加一种修复——删掉该条目的 `name` 键、保留 id 与 config，代价是 id 被复用给另一插件时过期守卫消失，结果文案必须写明。不建议让 loader 对 name 不符硬失败（`vendor/` 上游代码，且会把「插件改名」升级为整树启动失败）。
- **验收**：五类告警各自得到与实现能力一致的 `fixable` 取值，且有测试逐类固定。

## N34 `plugin-dynamic-load` 的「未能定位」用例依赖条目求值顺序，测试偶发失败

- **状态**：未修｜✅ 实测复核（2026-10-07）
- **位置**：`doctor/tests/plugins-dynamic-load.spec.ts`（`reports an unlocatable failure when only the pair of bundles breaks`、`repair > reports it cannot fix when no single bundle reproduces the failure`）；被测分支 `doctor/src/checks/plugins.ts`（诚实分支与整体还原分支）
- **问题**：两个用例都构造「两个 bundle 单独都能加载、只有同时挂载才失败」的交互故障（`trip-bundle` 的模块体把 `globalThis.__tripLoaded` 置真，`partner-bundle` 的模块体据此抛错），据此断言检查/修复走到「未能定位」。但这对 bundle 是否失败取决于**哪个模块体先求值**：Cordis loader 等待条目初始化任务用的是 `Promise.allSettled`（`vendor/loader/src/config/tree.ts`），条目求值并非严格按声明顺序串行；`partner` 先求值时两个模块都不抛错，全量探测直接通过，检查如实返回「所有 N 个选装插件加载正常」，用例随即失败。
- **影响**：doctor 的测试套件偶发失败（本轮 9 次运行中 3 次失败，失败点在 check 用例与 fix 用例之间跳动），N32 唯一覆盖「未能定位」分支的用例不可信；把它接进 CI 会得到随机红灯，而按失败信息排查会指向并不存在的产品缺陷。
- **修复建议**：①把交互故障改成确定性的依赖关系（例如 `partner.js` 直接 `import` `trip.js` 的导出，或用 `top-level await` 建立顺序）；②若产品语义上交互故障本就无法稳定复现，则把断言从 `ok === false` 改为「要么定位到某个 bundle、要么诚实报告未能定位」，并把顺序依赖写进用例注释。
- **验收**：`vitest run tests/plugins-dynamic-load.spec.ts` 连跑 10 次全过。

## 22 `/tmp/dsh-webkit-4.1` 符号链接仍建在 `/tmp`

- **状态**：部分修复｜✅ 已复核
- **位置**：`internal/packaging/webkit-exec-path.txt`（该路径字面量的唯一来源，由 `webkit_linux.go` 的 `//go:embed` 读入）、`internal/packaging/webkit_linux.go`
- **问题**：路径常量仍是 `/tmp/dsh-webkit-4.1`，Go 源码内 grep `XDG_RUNTIME_DIR` 零命中。
- **影响**：多用户主机上 `/tmp` 内的路径可被其他用户预置，属 TOCTOU 面。
- **已修部分**：`webkitHelperLinkUsable()` 校验读回链接并确认 `WebKitNetworkProcess` 可访问，能防悬空或指向旧包。
- **修复建议**：改用 `$XDG_RUNTIME_DIR`（只需改单源文件一处，打包与启动两侧同步生效）。
- **验收**：产物内不再出现 `/tmp/dsh-webkit-4.1`。

## 27 窗口位置未记忆

- **状态**：部分修复｜✅ 已复核
- **位置**：`internal/app/appconfig.go`、`main.go`
- **问题**：尺寸与最大化状态已记忆，**位置 X/Y 没有**（`WindowState` 无该字段）。
- **影响**：每次启动都回到默认位置。
- **修复建议**：`WindowState` 增加位置字段，启动时经 Wails 选项恢复，关闭时保存。
- **验收**：移动窗口后重启回到原位置。

## 28 窗口背景色硬编码

- **状态**：部分修复｜✅ 已复核
- **位置**：`main.go`（`BackgroundColour`）、`frontend/styles.css`
- **问题**：`BackgroundColour` 仍硬编码 `{30, 30, 30, 255}`，Go 侧无任何 WebKitGTK 主题设置。
- **影响**：系统浅色主题下窗口底色与页面不一致（首帧闪烁）。
- **已修部分**：前端已跟随系统（`color-scheme: light dark` + `prefers-color-scheme: light` 整套浅色变量）。
- **修复建议**：把背景色接入同一套主题判定（GTK 侧读系统偏好或由前端上报）。
- **验收**：浅色主题下窗口底色与页面一致。

## N-extra 前端异步错误兜底缺口

- **状态**：部分修复｜✅ 实测复核
- **位置**：`frontend/app.js`（`#btn-about`、`#market-refresh` 的 `api()` 调用）
- **问题**：两处 `await api().X()` 无 try/catch，Go 侧 panic 时按钮卡在中间态且无反馈；全仓无 `unhandledrejection` 兜底。
- **影响**：失败时界面停在「刷新中」等中间态，用户得不到任何提示。
- **已修部分**：转义、样式选择器与开发态提示三处已修（`setDoctorSummaryHtml`／`setDoctorSummaryText` 拆分、进度条改用 `dataset.toolId`、提示条收敛到 `renderTools` 单点写入）。
- **修复建议**：给这两处（及同类调用）补 try/catch 或加全局 `unhandledrejection` 兜底，失败时复位按钮并提示。
- **验收**：模拟 Go 侧失败时按钮复位且有提示。

## N-extra3 打包脚本中重复与漂移的事实

- **状态**：部分修复｜✅ 实测复核
- **位置**：`linglong/prepare-offline.sh` ↔ `linglong/linglong.yaml`
- **问题**：捆绑 Node 版本硬编码两处（均为 `24.9.0`）；pnpm「是否已下载」的守卫一处查 `bin/pnpm.cjs`、一处查目录存在 + `bin/pnpm.mjs`；三行包装器在两地逐字重复。升级 Node 时只改一处会让 stage 与容器 fallback 下载不同版本。
- **影响**：两处漂移不会报错，只会产出不一致的构建。
- **已修部分**：`prepare-offline.sh` 的 README glob 已锚定 `"$pkgdir"/README*` 并加 `-f` 守卫。
- **修复建议**：把 Node 版本与包装器收敛为单一来源（由 `linglong.yaml` 生成，或脚本读取同一变量）。
- **验收**：改一处即可同时改变 stage 与容器 fallback 的 Node 版本。

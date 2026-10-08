# DeepSeek Harness 桌面启动器待办清单

本文是 `apps/desktop-launcher` 及其玲珑打包链路**尚未修复**的问题清单，面向维护者。已修复、已失效或前提不成立的条目一律不在本文——修完一条就删一条，本文清空之日即删除本文之时。被删条目的完整历史与验证证据在 git 里：最后一份含全部条目与附录的版本是提交 `c91f00f1ce`（可用 `git show c91f00f1ce:apps/desktop-launcher/docs/AUDIT.md` 取回）。

它不在文档闸门覆盖范围内：`verify-translation-pairing` 的语料范围由 `scripts/translation-pairing.ts:172-179` 的 `isTranslationScopeFile` 判定，只收 README 三件套、仓库根的 paired docs、`.agents/notes/**` 与仓库根的 `docs/**`／`python/**`；`verify-md-wrap` 与 `verify-md-links` 的 glob 也不含 `apps/`。因此**没有自动化手段保证本文与代码同步**：改动任一条目后请在同一次提交里更新本文，修完即删除该条。

## 收录范围

本清单只收**修复点落在 `apps/desktop-launcher/` 内**的缺陷。本 fork 的约束是与上游尽量一致：`apps/desktop-launcher/` 之外的文件一律不动，只有玲珑客户端真的无法工作时才最小量修改上游代码。因此下列缺陷**不入清单**（卡点写在括号里，避免重复提出）：

- **要改上游配置文件的**：`.gitlab-ci.yml` 与 `.github/workflows/`（上游未改动，新增即是新增偏离）、`lefthook.yml`（上游文件，fork 已改 +13/−1，再加只会加重偏离）。
- **根因或改动点在上游工具／产物的**：ll-builder 生成的 `buildext.sh`（`|| echo "$?"` 吞错）与根 `linglong/entry.sh`（`CFLAGS="-g"`）、ll-builder 把 `failed to copy` 降级为警告、上游 overlay 的写入不落盘、`WEBKIT_EXEC_PATH` 所需的 `DEVELOPER_MODE` webkit 发行物、上游基础镜像里坏掉的 `xdg-open`。
- **要上游配合的**：打包态的三层注入补丁（`tools/fix-deploy-closure.mjs`、`prepare-offline.sh` 的 `inject_workspace_pkg`、`inject-link-bridge.sh`）已全部收敛进 `apps/desktop-launcher/` 内（见 git 历史），但"上游把 dsh 闭包打成官方 preset／bundle、下游只做组装"这一步只能由上游做。
- **机制上做不到的**：`/tmp/dsh-webkit-4.1` 这个短路径无法按用户隔离——补丁脚本做的是构建期字节替换，新串必须是字面量且不长于原串（`/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1` 40 字节，带 `injected-bundle/` 的 57 字节），而 `$XDG_RUNTIME_DIR` 含 uid、只在运行期可知。现有的 `webkitHelperLinkUsable` 已挡住他人预置的链接、悬空链接与指向旧包的链接，残余的 TOCTOU 要根治只能换机制（每次启动在用户命名空间里把私有 tmpfs 挂到固定路径），不属最小改动。
- **要上游 loader／服务端配合的**：插件树挂载复用（启动预热／按需加载）。
- **在 app 之外的上游包里、且属加固而非功能缺陷的**：`packages/client` 侧发起的 `postMessage(..., '*')`（壳的 `frontend/app.js` 接收侧已按 `e.source !== frame.contentWindow` 校验来源，且只接受 `dshDesktop === true` 的消息）。

`9 / 10` 与 `N-extra2` 能在 app 内完成的那一半（体积断言、`make check` 汇总入口）已落地并移出清单；CI 与 pre-push 接线按上述约束不做。

## 复核基点

- 分支 `linglong`，HEAD `220bb0610a`（2026-10-08），玲珑包 `0.1.5.2`。
- 产物证据取自交付层 `~/.cache/linglong-builder/merged/8c11d079…/files`（2026-10-07）、仓库根 `linglong/` 的构建工作区与 `apps/desktop-launcher/linglong/stage/`。`8c11d079` 与上一版 `acbfe51b` 的 `lib/x86_64-linux-gnu` 体积与条目数一致（同为 304,866,155 B、180 个普通文件 + 172 条软链）。
- **环境限制**：本次复核时宿主已提供 `ll-builder`／`ll-cli`（实测在 PATH，来自 `~/.dsh-linglong/bin` 的宿主透传），`make` 与 `gcc` 仍缺；本次仍只做静态复核，未跑真实构建与安装、GUI 启动、需外网的 `DSH_TC_E2E=1`。因此「必须先有玲珑构建器」不再是本机的执行阻塞（N26 的字体抽取、8/13 的裁剪实测、11 的基线生成都不用等外部机器）；涉及容器与真机行为的结论仍以本机闭环为准。
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
| 11 | 中危 | 注入集合的受审清单机制已落地，基线待建立 | 部分修复 |
| 23 | 中危 | 打包态与外部 harness 共享 `~/.dsh` | 未修 |
| 35 | 中危 | 外链桥只在容器模式生效 | 未修 |
| 25 | 低危 | 系统托盘 | 未修 |
| N26 | 低危 | 中文字族不随包，无中文字体的机器上显示豆腐块 | 未修（待决策） |

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
- **问题**：去重结果没问题（单一实体 `libwebkit2gtk-4.1.so.0.19.7` 92.8 MB + 两条软链，补丁版胜出），但 `skip_existing` 在源码与生成物中都没有该配置键，去重完全依赖 ll-builder 默认行为，仓库既没声明也没校验。更关键的是**依赖链没有裁剪或比对**：`lib/x86_64-linux-gnu` 实测 **304,866,155 B（约 291 MiB）/ 352 项**（180 个普通文件 + 172 条软链），而 `depends.yaml` 有 **175 条**，且没有任何受控文件消费它——`git grep depends.yaml` 唯一命中 `clean-linglong.sh` 的一句注释。
- **影响**：apt 默认 Recommends 带进了与嵌入式本地 Web 应用无关的栈——`gstreamer-1.0` 22 MB、`mfx` 12 MB、`lapack` 7 MB、`ImageMagick-6.9.13` 4.3 MB、`OpenNI2` 1.3 MB、`directfb-1.7-7` 1.2 MB、`perl5` 1.1 MB，另有 `blas`/`caca`/`enchant-2`，合计约 50 MB+。
- **修复建议**：以 `depends.yaml` + `tools.yaml` 为准做一次依赖链比对，摘掉用不到的多媒体/图形栈；把 `skip_existing` 从注释变成显式配置或校验。
- **验收**：比对脚本对当前清单输出可裁清单；体积断言（`lib/x86_64-linux-gnu` 不超过阈值）落到 `build-linglong.sh`，超阈值即中止导出——**这半已落地**（`linglong/verify-artifact-size.sh` 由 `build-linglong.sh` 在导出前调用并硬失败，上限 `340 MiB`／整棵 `1 GiB`，提交 `866648b81f`），本条只差依赖链的裁剪与比对。

## 11 注入集合的受审清单机制已落地，基线待建立

- **状态**：部分修复｜✅ 已复核（2026-10-08）
- **位置**：`linglong/prepare-offline.sh`（`inject_workspace_pkg`、`assert_injected_inventory`、调用循环）、`linglong/injected-packages.txt`（基线，尚未生成）、`linglong/test-prepare-offline-inject.sh`
- **问题**：注入是黑名单模式（遍历 `packages/*/*/` 与 `vendor/*/`，只排除 experimental、非 `@deepseek-ai/*`、`test-support`、`typert/generator`），**没有任何显式白名单**，因此任何新增的 workspace 包都会默认进入生产闭包，且不会有任何断言提示。
- **已修部分**：每次 prepare 记录本次注入的包名，与仓库内的基线 `linglong/injected-packages.txt` 逐行比对——基线缺失时写入并提示（首次建立），存在时多一个或少一个都失败并打印 diff。于是"新增包进闭包"必须先经人工评审并更新基线，达到本条要的验收口径。4 条自测覆盖建立基线／一致通过／新增失败／移除失败。
- **为什么不是"只注入 preset 列出的包"的白名单**：实测会挡掉必需包。按 package.json 的 dependencies／peer／optional 统计，闭包里 317 个工作区 `@deepseek-ai` 包中有 **45 个未被任何 manifest 引用**，其中含 `dsh-base`、`dsh-client-web`、`dsh-browser-use`、`dsh-acp-app` 等 **bundle**——bundle 由 profile／preset 的补丁层按包名引用、不写依赖，因此"按依赖判定"的白名单会把它们挡在闭包外，直接破坏打包。
- **剩余问题**：基线文件只在一次真实 `prepare-offline.sh` 运行后才会生成，那之前该断言只建立基线、不拦截。生成后需复核包清单并提交。
- **验收**：`linglong/injected-packages.txt` 已提交且与真实注入集合一致；此后新增一个 workspace 包（被注入）时 `prepare-offline.sh` 非零退出并列出 diff。
## 23 打包态与外部 harness 共享 `~/.dsh`

- **状态**：未修｜✅ 已复核
- **位置**：`internal/appenv/env.go`（四处构造 `supervisor.Config`）、`internal/app/preflight.go`、`internal/preflight/preflight.go`
- **问题**：普通启动路径完全不注入 `DSH_HOME`，子进程与 launcher 共享同一份环境快照；预检与 doctor 也指向 `home/.dsh`。只有用户主动点「全新环境启动」的降级路径才用 `~/.dsh-fallback`。
- **影响**：`README.zh.md`「已知事项」已记录——不同版本的外部 harness 可能把 `~/.dsh/.credentials.yaml` 写成内置 harness 无法解析的格式，导致启动即崩、进入重启循环。
- **修复建议**：打包态改用独立 `DSH_HOME`（如 `~/.config/dsh-desktop/dsh/`），与 npx／外部安装彻底隔离。
- **验收**：打包态与外部 harness 各自读写不同的 home；外部写入不影响的启动。

## 35 外链桥只在容器模式生效

- **状态**：未修｜✅ 已复核
- **位置**：`linglong/inject-link-bridge.sh`（现状）、`internal/webviewperm/permission_linux.c`（可复用的 GTK 侧信号接管模式）、`README.zh.md`（已知事项）
- **问题**：桥在打包时注入 GUI dist，因此只覆盖容器内运行的 harness。连接外部服务时，外部 harness 服务的是未注入的 GUI，其中 `target="_blank"` 外链点击没有反应。
- **影响**：容器模式与外部服务模式行为不一致，外部模式下外链是死链。
- **修复建议**：改为在 launcher 内从 GTK 侧接管 WebKit 信号——`internal/webviewperm/permission_linux.c` 已演示「遍历顶层窗口取回 `WebKitWebView` 并连信号」的成熟做法，同一条路可连 `create`／`decide-policy`，把新窗口请求交给 `BrowserOpenURL`。这样两种模式都覆盖，且完全落在 `apps/desktop-launcher/` 内，不需要动 `packages/client`。
- **前置**：先做一次小 spike，确认该 Wails 版本下 `create` 与 `decide-policy` 的触发时机（Wails 自身是否已处理新窗口请求）。
- **验收**：连接外部服务时 `target="_blank"` 外链可点击；容器模式下行为不变。

# 三、低危

## 25 系统托盘

- **状态**：未修｜✅ 已复核
- **位置**：`main.go`、`apps/desktop-launcher/go.mod`、`internal/app/app.go`（`OnBeforeClose`）
- **问题**：无托盘库、无 `HideOnClose`，`OnBeforeClose` 只保存窗口状态。关窗即退出，后台任务随之中断。
- **影响**：harness 跑长任务时关窗会中断。
- **修复建议**：引入托盘（`OnBeforeClose` 返回 true 并隐藏窗口），托盘菜单提供退出。
- **验收**：关窗后进程与 harness 继续运行，可从托盘退出。

## N26 中文字族不随包，分发到未装中文字体的机器上会显示豆腐块

- **状态**：未修（待决策）｜✅ 实测复核（2026-10-08）
- **位置**：`linglong/linglong.yaml`（`buildext.apt.depends` 与 `build:` 段的字体注释）、`linglong/install-container-fonts.sh`
- **问题**：壳的 CSS 字体栈把中文族交给运行时的 fontconfig，而容器里 `/usr/share/fonts` 被宿主目录整体挂载覆盖，随包的只有 `${PREFIX}/share/dsh-fonts` 下的拉丁与等宽字体。宿主没装中文字体时，界面里的中文会显示成豆腐块。
- **已核实的事实**：① 曾经声明的 `fonts-wqy-microhei` 只进构建容器，`/usr` 不随 layer 导出，因此**从未随包**（三版产物层与基座层实测无任何 wqy 实体）——该假声明已删除、注释已更正，让人以为「中文族有人管」的误导没有了。② 字体在容器里真实可用：`/usr/share/fonts/truetype/wqy/wqy-microhei.ttc`，5,177,387 字节的真实 TTC。③ 它由 `depends` 阶段安装，而 `depends` 在 `build:` 段**之后**才装进容器——本段看不到它。
- **影响**：中文能否显示取决于宿主。装有中文字体的机器上不可见（本机测试即如此），精简系统或非中文环境的机器上会暴露。
- **修复建议**（两步）：①在 `build:` 段复用 webkit 段的既有模式（`apt-get download fonts-wqy-microhei` + `dpkg-deb -x`，见 `linglong/linglong.yaml` 的 webkit 段）从 `.deb` 里取出 `wqy-microhei.ttc`；②把它复制进 `${PREFIX}/share/dsh-fonts/`（该目录已被 fontconfig 注册，见 `install-container-fonts.sh`）。
- **为什么不用「把依赖移到 `build_depends` 再从 `/usr` 复制」**：那条路要向 `build_depends` 新增一个 apt 包，它会进 `verify-container-deps.sh` 的逐包校验与 `/usr` 下的 `*.dpkg-new` 全局扫描；更关键的是 AUDIT N19 的「apt 报告成功但写入不落盘」一旦复现，从 `/usr` 复制拿到的就是缺失实体或字符设备，正是本条要防的失败。从 `.deb` 抽取与 `depends`／`build_depends` 都无关，这两个问题一并消失。
- **待确认项（需产品决策）**：产物增大约 5 MB，与 8/13 的裁剪目标相反。技术路径上已无未验证的未知项——旧方案里 `verify-container-deps.sh` 的扫描风险随 `.deb` 抽取方案消失，本机也已有 `ll-builder` 可闭环验证。
- **验收**：产物含 `share/dsh-fonts/wqy-microhei.ttc`；在未装中文字体的机器上中文不显示豆腐块。

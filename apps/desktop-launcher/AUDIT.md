# DeepSeek Harness 桌面启动器审计清单

本文是 `apps/desktop-launcher` 及其玲珑打包链路的缺陷清单与整改待办，面向维护者。

它不在文档闸门覆盖范围内（`verify-translation-pairing` 的语料谓词只认 README 基名、`docs/`、`.agents/notes/`、`python/`；`verify-md-wrap` 与 `verify-md-links` 的 glob 都不含 `apps/`），因此**没有自动化手段保证本文与代码同步**：改动上面任一条目后，请在同一次提交里更新本文对应小节，并注明验证状态。

## 审计基准

- 分支 `linglong-dev`，基点提交 `dfd0e9d186`（与 `linglong` 的文件树完全相同，tree 均为 `eea557d697a7bce2b28f435e1ca070ef70ef73d7`）。
- 玲珑包版本 `0.1.2.5`，产物 `com.deepseek.dsh-desktop_0.1.2.5_x86_64_main.uab` = **361 MB**（361,331,344 字节），解压后 **759 MB**。
- 体积分布：`lib/` 320 MB（其中 `lib/x86_64-linux-gnu` **293 MB / 352 个 `.so`**）、`harness/` 254 MB、`node/` 147 MB、`bin/` 39 MB；生产闭包含 **264 个** `@deepseek-ai/*` 包。
- 条目编号：首轮审计的条目沿用原编号 1–37；审计轮次新增条目沿用 `N` 系列（`N1`–`N26`，其中 `N20`–`N24` 来自 2026-09-14 的 JDK 多版本清单那一轮，`N25` 来自同日的 JDK 17 下架，`N26` 来自同日的依赖交付核查）；审计者未编号的其余静态审查发现为 `S1`–`S7`。
- 行号以审计基点为准，代码改动后可能漂移。
- `N20`–`N24` 的基点：分支 `linglong-dev` 提交 `92d150b3cb`（索引钉在 `ee9c181bf6`），玲珑包版本 `0.1.2.7`；行号以该提交为准。
- `N25` 的基点：提交 `9621d91bff`（JDK 17 下架前的清单状态）；行号以该提交为准。
- `N26` 与 N18/N19 的修正基点：提交 `54483bf5c7`（2026-09-14 首次带依赖闸门的真实构建）；证据来自 `~/.cache/linglong-builder/merged/` 下真实产物层与基座层的实体清点。

## 状态与验证等级

| 标记 | 含义 |
|---|---|
| ✅ 已复核 | 审计者逐行读取代码或实跑命令取得证据 |
| ⚠️ 静态审查 | 经代码阅读得出，审计者未独立复跑 |
| ❓ 待验证 | 结论依赖尚未执行的端到端运行 |

正文按严重级别分组，分组是本次审计的重新评估。首轮清单中定级为「中」但本次降入低危的条目（3、24、25）在该条目内注明了理由；定级未变的条目沿用原级别。

---

# 一、高危

## N3 工具索引来自个人 fork 的可变分支，且无签名

- **状态**：部分修复｜✅ 实测复核
- **位置**：`apps/desktop-launcher/internal/toolchain/remote.go:44`（常量）、`apps/desktop-launcher/internal/toolchain/remote_test.go:147-169`（回归守卫 `TestDefaultIndexURL_PinnedToCommit`）、`README.md:147`/`README.zh.md:147`
- **问题**：索引地址是 `https://raw.githubusercontent.com/GershonWang/deepseek-harness/linglong/apps/desktop-launcher/internal/toolchain/tools/index.json`——**个人账号 fork 的 `linglong` 分支**，可变引用。索引同时提供下载 URL 与 `sha256`，因此「sha256 校验」与下载来源出自同一份未经认证的数据，只保证传输完整，**不提供来源认证**。`DSH_TOOLCHAIN_INDEX_URL` 还可直接覆盖该地址。
- **影响**：控制该账号/分支、或能改写该 URL 的中间人，可让用户在「工具链市场」安装任意代码；解压产物经 `ReconcileBinLinks` 软链进 `~/.dsh-tools/bin`，而该目录被前置进 harness 子进程 `PATH`。`README.md:147` 把 "after sha256 verification" 当作安全属性，实际不成立。
- **已修（本次，采纳审计建议的第二选项）**：默认地址固定到**不可变提交哈希**。首次钉的是 `47d123e212ced431eb582e83f7d58a081b39d43c`（发布 43 项清单的那个提交）；2026-09-14 随 JDK 多版本清单前移到 `ee9c181bf66f655d24810012c4c9f61e59c9940c`（该提交的 `index.json` blob `acf8d0ce…` 与工作区一致，动机与后续条目见 N20–N24）；同日 JDK 17 下架后二次前移到 `ff0b924d11a2ca5cef4a908bec0ec54282ae7dc8`（该提交的 `index.json` blob `599f8341…` 与工作区一致，见 N25）。信任对象由此从「上游账号」收敛为「这份二进制」：控制分支不再能替换索引内容。配套：
  1. 新增回归守卫 `TestDefaultIndexURL_PinnedToCommit`——断言默认引用是 40 位提交哈希、且路径未被改到别处（先写测试确认它在旧值 `linglong` 上失败，再改常量使其通过）。
  2. 中英 README 撤掉「sha256 即安全」的表述，改写为「对清单的一致性校验，清单自身由客户端固定的提交哈希锚定」；并同步修正同段末尾「增删工具不必发客户端」——该句在固定引用后已不成立，现说明发布索引需改常量并重发客户端。
- **仍待决定（需产品决策，本次未做）**：审计建议的第一选项——**离线公钥签名**。它能在保留「只发索引不发客户端」更新方式的同时提供来源认证，但需要密钥托管与签名发布流程，属于用户尚未持有的流程变更，故不在无人确认时擅自引入。
- **验证**：`TestDefaultIndexURL_PinnedToCommit` 先失败（`默认索引引用 "linglong" 不是 40 位提交哈希`）后通过；`go test ./internal/toolchain ./internal/appenv` 通过；`gofmt -l` 无输出；`CGO_ENABLED=0 go vet ./...` 退出码 0；实跑 curl 按该提交哈希取回的索引 HTTP 200 且 sha256 `74d548e3…` 与仓库内 `index.json` 逐字节一致。**2026-09-14 复核（换钉后）**：`go test ./internal/toolchain` 通过；实跑 curl 新钉住的 `ee9c181b…` 取回 HTTP 200、sha256 `7f4ea9bb6914294aafca7a055f299fc3d9790e8adcd6f8baeea3df752081f046`，与仓库内 `index.json` 逐字节一致（43 项工具，`jdk21` 三个版本）。**2026-09-14 二次换钉复核（JDK 17 下架后）**：`go test ./internal/toolchain` 通过（含 `TestDefaultIndexURL_PinnedToCommit`）；实跑 curl 按 `ff0b924d11…` 取回 HTTP 200、sha256 `8742a8e633260fa4102475b59e5f0d08889e0ef247b6f460125fb06416b615a2`，与仓库内 `index.json` 逐字节一致（43 项工具，`jdk21` 两个版本）。**边界**：本环境无 gcc（审计已记录 gcc 工具链被裁），`CGO_ENABLED=1 go vet ./...`（含 wails cgo 路径）无法执行。

## 33 WebKit helper 字节补丁与版本号硬编码

- **状态**：部分修复｜✅ 实测复核
- **位置**：`apps/desktop-launcher/linglong/patch-webkit-exec-path.sh`、`apps/desktop-launcher/internal/packaging/webkit-exec-path.txt`（短路径单一来源）、`apps/desktop-launcher/linglong/linglong.yaml`（`build:` 段的 webkit 块）
- **问题**：直接对 `libwebkit2gtk-4.1.so` 做二进制字符串替换，且 `linglong.yaml:137-138` 把版本号硬编码为 `libwebkit2gtk-4.1.so.0.19.7`。webkit 小版本一变，这两行直接找不到文件。
- **附加缺陷**：`patch-webkit-exec-path.sh:40-42` 在两个计数都为 0 时打印一行说明并 `sys.exit(0)`，调用方不看 stdout，随后无条件建软链并继续打包导出 → **补丁未生效也能产出「安装成功但 GUI 起不来」的包**。建议改为非零退出，或在构建后断言 `/tmp/dsh-webkit-4.1` 字符串确实已替换。
- **长期方案**：`WEBKIT_EXEC_PATH`（需 `DEVELOPER_MODE` 构建）或让玲珑 layer 正确导出 `/usr/lib/...`。
- **已修（第一步）**：版本号不再硬编码——`linglong.yaml` 用 `set -- .../libwebkit2gtk-4.1.so.0.*` 解析构建容器内的唯一实体，命中 0 个（`sh` 不展开 glob 时 `$#` 仍为 1，故同时判 `[ -e "$1" ]`）或多个都硬失败；补丁脚本在找不到硬编码路径、替代串比原串长、替换后仍残留原路径三种情况下均非零退出，写盘前完成全部自检，替换失败不再产出畸形 `.so`。
- **已修（本次，第一步收尾 + 消除一处现存隐患）**：
  1. **短路径单源化**。`/tmp/dsh-webkit-4.1` 原本在补丁脚本与 `webkit_linux.go` 里各写一遍，改一处就会让包内 helper 路径与 launcher 建的符号链接不一致——正是最坏的「装得上、GUI 起不来」，而打包与启动两个环节都不会报错。现落到 `internal/packaging/webkit-exec-path.txt`：Go 侧 `//go:embed`，补丁脚本读同一文件。两条守卫测试固定契约：短路径必须短于原路径（等长字节替换的前提）、打包脚本不得再出现该路径的字节字面量；两条都做了变异验证（注入回归后确实失败，非摆设）。
  2. **补丁脚本增加反向自检**：除"旧路径必须消失"外，新增"新路径必须出现"——写坏或漏写同样会让旧路径消失，而 launcher 会据此建一个指向不存在路径的符号链接。
  3. `build:` 段的 webkit 唯一命中断言由 `-e` 收紧为 `-f`（见 N19）。
- **仍待办（第二步）与新增证据**：原计划改走 `WEBKIT_EXEC_PATH` 或让 layer 导出 `/usr/lib/...`，本轮取证后判定**两条都不可行**：
  1. `WEBKIT_EXEC_PATH` —— 实测已发布包的 `libwebkit2gtk-4.1.so.0.19.7` 里 `strings` **不存在**该字符串，说明该代码路径未编入发行版构建（与 `linglong.yaml` 注释"需 DEVELOPER_MODE"一致）。
  2. layer 导出 `/usr/lib/...` —— 被 **N19** 阻塞：字节补丁存在的前提正是"运行时 `/usr` 只读、layer 不导出 `/usr` 写入"，而这正是 N19 记录的上游 overlay 缺陷。N19 不解决，这一步无法落地。
  因此第二步的可行前提不在本仓库：要么上游给出 `DEVELOPER_MODE` 的 webkit 发行物，要么 N19 的上游缺陷修复。原先担心的"届时 `webkit_linux.go` 的短路径约定必须同步修改"已由本次单源化消解——改 `webkit-exec-path.txt` 一处即同时作用于打包与启动两侧。
- **对审计原文的更正**：原文称 `internal/packaging/webkit_linux.go` 的该函数"尚无单测"，该说法已过时——`webkit_linux_test.go` 早已覆盖 `webkitHelperLinkUsable`（链接缺失/悬空/指向非当前包目录）与渲染后端两条路径；本轮又补上面两条契约测试。
- **验证**：
  1. `sh apps/desktop-launcher/linglong/test-patch-webkit-exec-path.sh` 4 项全过（替换成功且新旧路径一增一减、无硬编码路径、短路径过长、单一来源文件缺失）。
  2. `go test ./internal/packaging` 全绿（含新增两条契约测试）。
  3. 变异检查：把短路径字面量写回脚本 → `TestPatchScriptReadsSharedShortPath` 失败；把短路径改长 → `TestWebkitExecPathIsEqualLengthReplacement` 失败。两者还原后复绿。
  4. **真实产物往返**：取已发布包里的 `libwebkit2gtk-4.1.so.0.19.7`（92,804,704 字节），先用脚本同构的方式还原出原始形态（`exec` 2 处、`bundle` 1 处），再用改动后的脚本重新打补丁，结果与已发布包 **sha256 逐字节一致**（`765432e2…`，即审计正文引用的那个哈希）——证明本次改动没有改变补丁产物。对已打过补丁的文件再跑一次则正确拒绝（退出码 1，不写盘）。
  5. **边界**：本环境无 ll-builder 与玲珑容器，未在真实构建里跑过；上述往返验证用的是已安装的 0.1.2.7 产物，不是新构建。

## N18 `buildext.apt.depends` 的安装命令吞掉错误，依赖可能整段没装上

- **状态**：部分修复（2026-09-14 修正落点）｜✅ 实测复核
- **位置**：`linglong/buildext.sh`（由 `linglong.yaml` 的 `buildext:` 段生成，每次构建覆盖；`linglong/` 被 `.gitignore:41` 忽略）；仓库侧落点 `linglong/verify-container-deps.sh`、`linglong/verify-merged-deps.sh`、`linglong/linglong.yaml`（`build:` 段开头与导出前）
- **问题**：生成脚本的两条命令都以 `|| echo "$?"` 结尾——

  ```sh
  apt -o APT::Sandbox::User=root update || echo "$?"
  apt -o APT::Sandbox::User=root -y install libwebkit2gtk-4.1-0 … xdg-utils || echo "$?"
  ```

  apt 失败（网络、锁、磁盘、文件系统错误）不会中止构建，只打印一个数字；`README.zh.md` 声明的运行时依赖全部经由这一条路径拉入。
- **证据**：`/home/Jokul/Desktop/日志.txt` 两轮构建各自出现两次 apt 阶段（`L683`/`L834`、`L1657`/`L1888`，`Unpacking`/`Setting up libwebkit2gtk-4.1-0 (2.50.4-1~deb12u1deepin2)`），但容器内的 2.50.4 内容一个字节都没有落进包；两轮全程没有任何环节报错。
- **建议**：改为失败即中止（`set -e` 语义），或在 `build:` 段加一条"关键包必须存在于容器"的断言（与第 33 条第一步同源）。
- **已修（本次，采纳建议的后半）**：`build:` 段开头新增 `linglong/verify-container-deps.sh`，从 `linglong.yaml` 的 `buildext.apt` 段（`build_depends` + `depends`，去重并剥离注释）解析依赖清单，逐项硬断言：dpkg 状态为 `install ok installed`；已装版本等于 apt 候选（本次安装确实生效）；`dpkg --verify` 无输出；包内实体是普通文件；`/usr` 下无 `*.dpkg-new` 残留。任一不满足即非零退出，构建在组装之前中止，不再产出"依赖没装上却照常导出"的包。
- **未修（工具链侧）**：`|| echo "$?"` 由 ll-builder 从声明生成，`linglong.yaml` 的 `buildext:` 只有包名列表（`apt.build_depends`/`apt.depends`），本仓库改不掉那两行命令。本次改动把"apt 失败不中止"从静默变成构建故障，但没有消除它。
- **验证**：`sh apps/desktop-launcher/linglong/test-verify-container-deps.sh` 7 项全过——依赖齐全（含两段重复包名去重与整行/行尾注释剥离）、版本落后于候选、依赖未安装、字符设备实体、`*.dpkg-new` 残留、`dpkg --verify` 不一致，以及真实 `linglong.yaml` 的完整解析。
- **2026-09-14 修正（首次真实构建暴露）**：上一版把 `build_depends` 与 `depends` 一起放在 `build:` 段首校验，**首次真实构建即被它拦下**——8 个 `depends` 包（`fonts-wqy-microhei`、`git`、`git-lfs`、`wget`、`jq`、`xxd`、`zip`、`xdg-utils`）全部报「没有装上」，另有 6 个基础层包报 `dpkg --verify` 不一致。根因有两处，且都不在被校验的依赖身上：

  1. **时机错位**：ll-builder 只把 `build_depends` 装进构建容器——实测本次生成的 `linglong/buildext.sh` 全文只有 `apt update` 与 `apt -y install libwebkit2gtk-4.1-0`；`depends` 是在 build 段**之后**（preCommit 的合并阶段）才安装并合进 `$PREFIX`。同一事实 `verify-tools.sh` 的头部注释与 `linglong.yaml` 的 webkit 注释早已写明，闸门却与之矛盾。旧日志佐证：`[Start Build]`（`日志.txt:878`）之后才出现 `Setting up wget/xdg-utils/git/git-lfs`（`:1722`–`:1871`），随后才是 `[Install Files]`（`:1937`）；而上一版成功导出的产物层 `~/.cache/linglong-builder/merged/50f29c89…/files` 里 `bin/git`、`bin/git-lfs`、`bin/wget`、`bin/jq`、`bin/xxd`、`bin/xdg-open` 一应俱全——depends 确实进了包，只是在 `build:` 阶段还看不见。
  2. **继承包的文档被基础镜像裁剪**：基座层 `org.deepin.base` 的 `.list` 保留 `/usr/share/doc`、`/usr/share/man` 条目而实体在镜像制作时已删除（实测该层 614 个包、`/usr/share/doc` 实体 0 条，`libgtk-3-0:amd64.list` 列 6 条 doc 路径且全部缺失）。因此 `libgtk-3-0`、`libglib2.0-0`、`python3`、`curl`、`unzip`、`ca-certificates` 的 `dpkg --verify` 永远非空。失败输出里**没有** `*.dpkg-new` 残留、也没有字符设备，说明 N19 的真实故障本轮并未发生。

- **修正后的落点**：`build:` 段只校验 `build_depends`（并放行基座裁剪的 `/usr/share/doc`、`/usr/share/man` 路径，理由写在比对处注释里）；`depends` 改由宿主侧 `verify-merged-deps.sh` 在**合并产物树**上校验，`build-linglong.sh` 在 export 前调用，失败即中止导出。
- **验证（修正后）**：`test-verify-container-deps.sh` 10 项全过（新增「只装 build_depends 即通过」「仅文档/手册缺失放行」「文档缺失不掩盖实体缺失」「真实 yaml 不再要求 depends 已装」四项）；`test-verify-merged-deps.sh` 7 项全过；`verify-merged-deps.sh` 对**上一版真实产物层**（`merged/50f29c89…/files`）实跑 16/16 OK、退出码 0。

## N19 容器内新装的文件不落盘，包内实体停留在基础层旧版本

- **状态**：已修（2026-09-14，采纳本条建议的实现；待真实构建验证）｜✅ 实测复核
- **位置**：`linglong/overlay/prepare_base/upperdir/`（构建器 base overlay）；受影响实体 `usr/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0.19.7`；仓库侧落点 `linglong/linglong.yaml`（`build:` 段）、`linglong/verify-container-deps.sh`、`linglong/verify-merged-deps.sh`
- **问题**：构建容器里 apt 装的是 webkit2gtk **2.50.4**，但整个 overlay 的 upperdir 里**没有任何 2.50.4 的实体内容**。每个新解包的文件只留下一个**字符设备 `c 0,0` 的 `<名字>.dpkg-new`**（例如 `libwebkit2gtk-4.1.so.0.19.7.dpkg-new`、`webkit2gtk-4.1/MiniBrowser.dpkg-new`），而实体文件保持 **2026-04-07** 的旧版本（`.so.0.19.7`，92,804,704 字节）不变。
- **已排除**：**不是构建器缓存**。清空 `~/.cache/linglong-builder` 后重建（缓存内 webkit 副本 52 份 → 2 份），失败原样复现，sha256 仍是 `765432e2…`；清空 `linglong/` 工作区重建同样无效。因此本条**替代**原先「缓存复用」的定性。
- **影响**：`buildext.apt.depends` 声明的依赖升级无法进入产物；`find linglong ~/.cache/linglong-builder -name 'libwebkit2gtk-4.1.so.0.2*'` 恒为零——新版内容从未落盘。附带结论：**修复前不要指望「升级 WebKit」带来任何行为变化**，包内恒定 2.48.5。
- **旁证**：该次打包日志中 `failed to copy …/libwebkit2gtk-4.1.so.0 …: 无效的参数` 出现 3 次（见 N17），构建仍声明完成并导出 345 MB 产物。
- **建议**：不要在 prepare_base/overlay 路径上继续投入；改为在 `build:` 段自行 `apt-get download` + `dpkg-deb -x`，把目标库直接装进 `${PREFIX}`（普通文件写入，不经 overlay 合并）。
- **已修（本次）**：把该症状变成构建期硬失败。① `build:` 段开头调用 `verify-container-deps.sh`，其中两条直接针对本条的现场特征：包内实体若是字符设备即失败；`/usr` 下存在任何 `*.dpkg-new` 残留即失败（另加 `dpkg --verify` 比对实体与包记录）。② `build:` 段原有的 webkit 唯一命中断言从 `-e` 收紧为 `-f`——`-e` 对字符设备同样为真，而下面紧接着就是 `cp -a "$1"`，会把设备节点原样复制进产物。
- **未采纳审计的替代实现（需说明）**：`apt-get download` + `dpkg-deb -x` 直接写 `${PREFIX}` 未在本轮实施。两个原因：其一，buildext 的合并发生在 **preCommit**（`build:` 之后），直接写进 `${PREFIX}` 的内容与随后合并进来的 `/usr` 旧实体谁胜出取决于 ll-builder 的去重时序，而该时序在仓库里只有注释、没有可核对的声明（见第 8/13 条）；其二，本环境没有 ll-builder 与玲珑容器，任何对依赖交付机制的改写都无法端到端验证，只能在用户机器上盲试。断言是当前唯一"改了就能验证"的一步。
- **验证边界（如实说明）**：日志证据表明 webkit **实体**在容器内确实停在旧版本，`dpkg --verify` 因而会对不上——但这条推断**未在真实容器里复跑**（本环境无 ll-builder，见上）。已在本机用 fixture + stub 覆盖该分支：`test-verify-container-deps.sh` 的"字符设备实体"与"`*.dpkg-new` 残留"两项。
- **2026-09-14 修复（采纳本条建议的实现）**：`build:` 段不再从容器 `/usr` 复制 webkit，改为 `apt-get download libwebkit2gtk-4.1-0` + `dpkg-deb -x` 解出实体，**显式比对「下载到的版本 == apt 候选」**（不符即中止），打 exec-path 补丁后写进 `${PREFIX}`，并打印交付版本（`webkit: 交付 2.50.4-1~deb12u1deepin2（来自 apt 候选 .deb，已打 exec-path 补丁）`）。这条路径写的是普通文件，不经过 overlay，因此不经 ll-builder 的合并时序。上一轮"未采纳"的两条理由就此解除：其一，唯一依赖合并时序的地方是**我们**写进 `${PREFIX}` 的那份与基础层旧实体的去重，而既有版本已经证明补丁版胜出（日志里 `patch-webkit` 后产物带补丁，见 N17 修正）；其二，本环境仍无 ll-builder，端到端只能等真实构建，故本条状态写作"待真实构建验证"。
- **同时给的产物侧判据**：构建器那条冗余的 `failed to copy` 在 `verify-builder-log.sh` 里按已知项放行（其余照旧硬失败），而"到底交付了哪一份 webkit"改由 `verify-merged-deps.sh` 断言：`lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0` 存在、是普通文件、且含 exec-path 补丁短路径（短路径取自 `internal/packaging/webkit-exec-path.txt`）。若将来那份合并覆盖了我们的补丁版，补丁标记消失，构建即失败——这正是原先"静默沿用旧库"的可观测替代。
- **仍未解决（上游）**：overlay 写入不落盘本身没有变——容器内 `/usr` 下的实体依旧是旧版、`*.dpkg-new` 字符设备依旧产生。本条的修法是**绕开**它（不再从那棵树取物），而不是修好它；`build_depends` 里的 webkit 因此只用来拉齐 apt 列表，产物不再依赖容器里那份实体。
- **2026-09-14 修正（首次真实构建暴露）**：① 里的容器内断言原先连 `depends` 一并要求，而 `depends` 在该阶段根本不存在（见 N18 修正），首次真实构建即因此被拦下；现已收窄为只校验 `build_depends`，并把基座裁剪的文档/手册路径从 `dpkg --verify` 比对中放行。产物侧的字符设备扫描移入 `verify-merged-deps.sh`——`find -type c` 直接认真实设备节点，比原先 `[ -c ]`（跟随软链）更贴近本条现场，且不受基座包影响。②的 webkit `-f` 断言不变：`build:` 段复制的正是 `build_depends` 提供的实体。

## N17 builder 的 `failed to copy` 只警告不中止，包会静默沿用旧库

- **状态**：已修（2026-09-14，待真实构建验证）｜✅ 实测复核
- **位置**：`.uab` 组装阶段的构建器（ll-builder / linyaps builder，仓库外工具）；本体日志见 `/home/Jokul/Desktop/日志.txt:1941`；仓库侧落点 `build-linglong.sh`、`linglong/verify-builder-log.sh`、`linglong/verify-merged-deps.sh`
- **问题**：`failed to copy …/libwebkit2gtk-4.1.so.0 …: 无效的参数` 之后 `[Install Files]`（`L1942`）、`[Commit Contents]`（`L1945`）、`[Runtime Check]`（`L1949`）照常执行，产物以 345 MB 导出。最终包内仍是 4 月的 2.48.5。
- **影响**：依赖升级会被静默丢弃。比第 33 条更隐蔽——第 33 条至少会在下一次找不到文件时炸掉，这条连炸都不炸。
- **建议**：仓库侧按 N19 第 1 条加构建后硬断言；并向上游反馈该 `failed to copy`（附带 `linglong/overlay/prepare_base/upperdir` 下 5,830 个 `.dpkg-new` 字符设备与 `.wh..opq` 白障的证据）。
- **已修（本次）**：仓库侧事后拦截。`build-linglong.sh` 把 `ll-builder build` 的完整输出保留到 `linglong/build.log`（该路径在 `.gitignore:41` 的 `/linglong/` 内），导出前调用 `verify-builder-log.sh`，命中 `failed to copy` 即打印命中条数与位置并非零退出。同时保住构建器自身的退出码：POSIX `sh` 没有 `pipefail`，因此用子 shell 把 `$?` 写进状态文件再读回，避免 `tee` 的退出码掩盖构建失败。
- **未修（工具链侧）**：构建器仍把复制失败降级为警告。仓库侧只能拦住"带着旧文件出包"，无法让它别丢文件；上游反馈仍是必要动作。
- **验证**：`sh apps/desktop-launcher/linglong/test-verify-builder-log.sh` 5 项全过（正常日志通过；仅 webkit 的已知冗余失败通过并说明豁免处数；webkit 之外仍失败；豁免与真实失败并存时仍失败；日志缺失时非零退出）。另单独实测了状态捕获惯用法：子 shell 内 `exit 7` 被正确读回为 7，`tee` 同时把输出落盘。
- **2026-09-14 修正（首次真实构建暴露）**：首次带该门禁的真实构建被它拦下，而它在本次构建里抓到的**只有 webkit 这一条**——实测两次历史构建 + 本次共 3 处 `failed to copy`，**全部是同一个文件** `libwebkit2gtk-4.1.so.0`（从 overlay 复制到 `output/_build`）。而产物里那份 webkit 是 `build:` 段自己解出并打过补丁的（日志 `patch-webkit: replaced 2 exec path + 1 injected-bundle path` 为证），构建器那次复制是冗余的：它的失败恰恰避免了基础层的旧库覆盖我们的补丁版。因此判据从"日志里出现 `failed to copy`"改成"**产物里有没有一份打过补丁的 webkit**"：`verify-builder-log.sh` 只放行 webkit 这一种并打印豁免处数，其余照旧硬失败；`verify-merged-deps.sh` 断言 `lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0` 存在、是普通文件、且含 exec-path 补丁短路径（短路径取自 `internal/packaging/webkit-exec-path.txt` 这一单一来源）。本次真实日志实跑：`共 1 处，豁免 webkit 1 处`、退出码 0。

---

# 二、中危

## 8 / 13 WebKit 依赖链未裁剪，`depends.yaml` 无人使用

- **状态**：未修｜✅ 已复核
- **位置**：`apps/desktop-launcher/linglong/linglong.yaml:132-140`、`apps/desktop-launcher/linglong/verify-tools.sh`、构建产物 `linglong/depends.yaml`
- **问题**：去重结果没问题（单一实体 `libwebkit2gtk-4.1.so.0.19.7` 92.8 MB + 两条软链，补丁版胜出），但 `skip_existing` **只写在注释里**，源码与生成物中都没有该配置键——去重完全依赖 ll-builder 默认行为，仓库既没声明也没校验。更关键的是**依赖链没有裁剪或比对**：`lib/x86_64-linux-gnu` 实测 **293 MB / 352 个 `.so`**，而 `depends.yaml` 只有 **175 条**，且没有任何受控文件引用它（`git grep depends.yaml` 零命中）。原清单第 13 条担心的是「静态文件会过期」；实际情况相反——它每次构建由 builder 重新生成，天然同步，**缺的是被使用**。
- **多合并的可见证据**：apt 默认 Recommends 带进了与嵌入式本地 Web 应用无关的栈——`gstreamer-1.0` 22 MB、`mfx` 12 MB、`lapack` 7 MB、`ImageMagick-6.9.13` 4.3 MB、`OpenNI2` 1.3 MB、`directfb-1.7-7` 1.2 MB、`perl5` 1.1 MB，另有 `blas`/`caca`/`enchant-2`。
- **建议**：以 `depends.yaml` + `tools.yaml` 为准做一次依赖链比对，摘掉用不到的多媒体/图形栈（约 50 MB+），并把 `skip_existing` 从注释变成显式配置或校验。第 8 与第 13 条应合并成一个任务。

## 9 / 10 无 CI 流水线与体积门禁

- **状态**：未修｜✅ 已复核
- **位置**：`.gitlab-ci.yml`、`.github/workflows/`、`apps/desktop-launcher/build-linglong.sh:27-31`
- **问题**：`.gitlab-ci.yml` 只有 `python-v*` 触发的 wheel 流水线，`grep linglong|uab|ll-builder|verify-tools|go test` 零命中；`.github/workflows/` 同样零命中。`Makefile` 有 `go test ./...` 目标但无任何 CI 调用。全仓无任何体积断言；`build-linglong.sh:27-31` 连 `verify-tools.sh` 失败都只告警后继续导出。
- **影响**：打包全靠手工 `sh apps/desktop-launcher/build-linglong.sh`；GCC 工具链、Node 头文件之类的回归不会被拦下。
- **建议**：一条流水线跑 `verify-tools.sh` + `pnpm run build` + `go test ./...` + 体积断言（`lib/gcc` 必须为 0、`node/include` 必须为 0、`lib/x86_64-linux-gnu` 不超过阈值）。

## 11 `inject_workspace_pkg` 仍是黑名单模式

- **状态**：未修｜✅ 已复核
- **位置**：`apps/desktop-launcher/linglong/prepare-offline.sh:92-97`（另见函数内 `:66-68`、`:71-72`）
- **问题**：遍历 `packages/*/*/` 后在循环内排除 `test-support`/`typert/generator`；函数内另有 experimental 与非 `@deepseek-ai/*` 两条黑名单。**没有任何显式白名单**，所以任何新增的 workspace 包都会默认进入生产闭包。原清单指出 experimental 就是经此漏入的——该条已单独修掉（见附录），但黑名单模式本身未改。
- **建议**：改为显式白名单，只注入标准 preset 实际列出的包。

## 14 启动预热 / 按需加载插件

- **状态**：未修｜✅ 已复核
- **位置**：`internal/supervisor/supervisor.go:319`、`:383-427`
- **问题**：每轮监护循环都 `s.spawn()` 全新进程，退避重启后回到同一路径 → **每轮重试全量重载插件树**。唯一相关的是 `:29-31` 的 `fatalLoadPattern` 快速失败（避免坏插件下反复重载），不是预热。

## 19 postMessage 的 `targetOrigin`

- **状态**：部分实现｜✅ 已复核
- **位置**：`frontend/app.js:93-101`（已收敛）、`apps/desktop-launcher/linglong/dsh-link-bridge.js:40-44`（已收敛）、`packages/client/ui-conversation/src/client/desktop-clipboard.ts:51`（**仍为 `'*'`**）
- **问题**：壳→iframe 的回包已改为 `new URL(frame.src).origin`（仅解析失败才回退 `'*'`），注入的外链桥用 `window.location.ancestorOrigins[0]` 兜底。**但 harness 侧发起剪贴板请求的方向仍是 `window.parent.postMessage({...}, '*')`**。
- **建议**：改仓库内 `packages/client` 那一处；接收侧已有 `event.source !== window.parent` 校验。

## 20 宿主挂载缺二次确认

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/app/app.go:1273-1292`、`internal/hosttools/hosttools.go:116`、`frontend/app.js:1118-1124`、`frontend/app.js:1280-1296`
- **问题**：`AddHostTool` 直接写 `config.d` 挂载配置（`Options: ["rbind","ro"]`），Go 侧与前端**都没有确认步骤**。对照：卸载工具有两击确认（`app.js:1001`），全新环境启动有 `confirm()`（`app.js:1214`）——唯独能力最强、风险最高的这个动作没有。
- **影响**：用户不清楚「挂载后沙箱内所有进程都能读这个目录」。只读（`ro`）限制了写入风险，但读取范围没有任何提示。
- **建议**：复用现成两击确认模式，改动面很小。

## 23 打包态与外部 harness 共享 `~/.dsh`

- **状态**：未修｜✅ 已复核
- **位置**：`internal/supervisor/supervisor.go:56-59`、`internal/app/preflight.go:255-257`、`internal/preflight/preflight.go:114-128`
- **问题**：普通启动路径完全不注入 `DSH_HOME`，子进程与 launcher 共享同一份环境快照；预检与 doctor 也指向 `home/.dsh`。只有用户主动点「全新环境启动」的降级路径才用 `~/.dsh-fallback`。
- **影响**：`README.zh.md`「已知事项」已记录——不同版本的外部 harness 可能把 `~/.dsh/.credentials.yaml` 写成内置 harness 无法解析的格式，导致启动即崩、进入重启循环。
- **建议**：打包态改用独立 `DSH_HOME`（如 `~/.config/dsh-desktop/dsh/`），与 npx/外部安装彻底隔离。

## 32 注入链路仍是三层补丁

- **状态**：未修｜✅ 已复核
- **位置**：`scripts/fix-deploy-closure.mjs`（152 行）、`apps/desktop-launcher/linglong/prepare-offline.sh:59-100`、`apps/desktop-launcher/linglong/inject-link-bridge.sh`
- **问题**：让打包态跑起来至少依赖三层对 `pnpm deploy` 与上游架构的补丁。上游迭代时任何一层都可能失效，而失效方式通常是静默的——N2 就是这样失效的（见附录 A），N14 是仍在的一例。
- **建议**：上游把 desktop launcher 的闭包打成官方 preset / bundle，下游只做组装。

## 35 外链桥只在容器模式生效

- **状态**：未修｜✅ 已复核
- **位置**：`apps/desktop-launcher/linglong/inject-link-bridge.sh`、`apps/desktop-launcher/README.zh.md:142`
- **问题**：桥在打包时注入 GUI dist，因此只覆盖容器内运行的 harness。连接外部服务时，外部 harness 服务的是未注入的 GUI，其中 `target="_blank"` 外链点击没有反应。README 已明确承认这一点。
- **长期方案**：把外链桥做成官方插件或前端特性，不依赖打包时注入。

## 34 基础镜像的 `xdg-open` 是坏的

- **状态**：未修复（上游镜像问题，workaround 已就位）｜✅ 已复核
- **位置**：`apps/desktop-launcher/README.zh.md:142`、`apps/desktop-launcher/linglong/linglong.yaml:172-176`、`linglong/tools.yaml:42`
- **说明**：基础镜像的 `/bin/xdg-open` 只是 systemd-run 转发壳，打不开任何东西。当前靠随包合入真实 xdg-utils 盖过它，并由 `verify-tools.sh` 校验存在性。属上游问题，建议向其反馈。

## N4 X11 `MIT-MAGIC-COOKIE-1` 长度字段字节序写反

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/clipboard/x11.go:387-390`
- **问题**：连接请求声明 `'l'`（LSBFirst），协议要求所有字段按客户端字节序编码，但 auth name/data 长度用 `binary.BigEndian.PutUint16` 写入（应为 LittleEndian）。服务端读到 name 长度 `0x1200`=4608、data 长度 `0x2000`=8192，认证必然失败。此外两次尝试**复用同一条已失败的连接**（X 服务端在 Failed 后关闭连接）。
- **影响**：需要 Xauthority 的 X11 主机上粘贴截图**永久无反应**，且无日志无提示（Wayland 有 `wl-paste` 兜底，纯 X11 会话没有）。
- **覆盖缺口**：`x11_test.go` 的 fakeXServer 只读 12 字节 setup 并对首个无认证请求直接回成功，cookie 路径与其长度编码完全未被覆盖。

## N5 supervisor 重启退避整数溢出

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/supervisor/supervisor.go:368-372`
- **问题**：`attempt` 只在收到 `startCh` 时归零，「启动成功后又崩溃」的循环会一直累加；`RestartDelayMs * (1 << (attempt-1))` 在 `int` 上溢出为负数，随后 `if delay > MaxRestartDelayMs` 对负值不成立，`time.After(负值)` 立即触发。
- **影响**：监护循环退化为无退避的 spawn 风暴，日志疯涨、CPU/内存被打满。约 56 次连续「就绪后崩溃」即进入。
- **建议**：移位前对 `attempt` 设上限，并给 `delay <= 0 → Max` 兜底。

## N6 启动自动诊断的收尾判断用错变量

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/app/app.go:509-529`（配合 `:534-550`、`:711-726`）
- **问题**：epoch 只用于清理 `cancel`/`done`，真正的「是否过期」判断却是 `if !a.startupDoctorRunning`。被取消的旧诊断在新诊断运行期间（`startupDoctorRunning` 又为 true）通过该判断，写入旧结果并把 `startupDoctorRunning` 置 false；**真正的新诊断结束时结果被 `return` 丢弃**。
- **影响**：前端显示「诊断已就绪 + 伪造的错误」，真实结论丢失；失败路径下的自动诊断静默失效——而这正是它最该起作用的时刻。
- **建议**：状态写入同样以 `a.doctorEpoch == myEpoch` 为唯一门禁。

## N7 「全部更新」从不激活新版本

- **状态**：已修（2026-09-14）｜✅ 已复核
- **位置**：`internal/toolchain/install.go`（`InstallTool` 的已安装分支、`installVersion` 的激活条件）、`internal/app/app.go`（`UpdateAllTools`、`installToolAsync`）
- **问题**：`activate` 默认 `false`，生产代码**无任何调用点**传 `InstallOptions.Activate`；而激活条件是 `if activate || !hadOther`，更新时 `hadOther=true` → 装完不激活。
- **影响**：命令仍跑旧版本、`HasUpdate` 恒真、可更新徽标永不清除；再点一次会因 `IsInstalled` 提前返回，却仍提示「已更新 N 个工具，失败 0 个」。
- **修复**：`UpdateAllTools` 与 `installToolAsync`（用户在卡片上安装/选版本）显式传 `Activate: true`；`InstallTool` 的已安装分支不再无条件早退，要求激活时执行一次幂等 `SetActiveVersion`——这条路径正是原缺陷的死局出口（更新下载完成、用户点多少次都不收敛）。依赖自动安装与并存安装仍走默认规则：首次安装自动激活，已有其它版本时不覆盖当前激活（`TestInstallVersion_SecondVersionKeepsActive` 继续守住该语义）。
- **验证**：新增 `TestUpdateAllTools_ActivatesRecommendedVersion`（临时 home 预置「旧版本已激活 + 推荐版本已下载」，不联网）——把 `UpdateAllTools` 的 `Activate` 临时改回 `false` 时该用例以 30s 超时失败，改回后通过；新增 `TestInstallTool_AlreadyInstalledHonorsActivate` 分别固定「不要求激活则不动当前版本」与「要求激活则切过去」两条分支。`go test ./...`（desktop-launcher 全包）通过。

## N8 下载与解压无体积上限

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/toolchain/install.go:411`、`:595-599`、`:701-703`
- **问题**：下载用 `io.Copy(f, reader)` 无 `LimitReader`（只有 10 分钟整体超时）；tar/zip 解压同样无单文件/总量上限。唯一的体积上限是 `remote.go:112` 给**索引**的 4 MiB。
- **影响**：恶意或被篡改的镜像可写满 `~/.dsh-tools` 所在分区（ENOSPC），或触发解压炸弹。
- **修复**：三处上限都放在 `install.go` 顶部，声明为变量只为让测试把阈值压到几十字节走真实归档路径：单归档 `maxArchiveBytes` = 4 GiB（实测最大归档 flutter 3.47.2 约 1.5 GiB）、解压总量 `maxExtractBytes` = 16 GiB（该归档解压后约 4.6 GiB）、条目数 `maxArchiveEntries` = 100 万。下载侧 `io.Copy` 换成 `copyCapped`（`io.LimitReader(limit+1)` 以区分"恰好等于上限"与超限），声明长度超限时在发请求前就失败；解压侧新增 `extractBudget`，字节与条目两个预算同时扣减。超限的 part 会被删掉——同一 URL 只会再次超限，留着会让该工具每次都停在同一步。

## N9 断点续传 part 路径可预测且跟随符号链接

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/toolchain/install.go:431-435`、`:385`、`:394`
- **问题**：part 路径 = `os.TempDir()/dsh-tools-downloads/<sha256(url)[:16]>.part`，内容可由公开索引推算；目录用 `MkdirAll(..., 0o755)`、文件用 `os.OpenFile(destPath, flag, 0o644)`，**无 `O_NOFOLLOW`/`O_EXCL`**。
- **影响**：多用户主机上可让 launcher 以自身权限覆盖任意可写文件；校验失败后的 `os.Remove(partPath)` 会再删一次目标。与第 22 条（`/tmp/dsh-webkit-4.1` 用 `/tmp`）不同：这条是**跟随符号链接写入/删除**。
- **修复**：part 路径改到 `<安装目录>/.downloads/<sha16>.part`——与原路径同文件系统（不跨设备），且落在用户自己的安装目录内，不再出现在 `/tmp` 供其他用户预置文件；目录创建走 `ensurePrivateDir`（`0o700` + `Lstat` 拒绝符号链接与非目录），文件打开走 `openPartFile`（Unix 分支带 `O_NOFOLLOW`，Windows 分支另表）。
- **边界更正**：`os.Remove(partPath)` 在末段是符号链接时删的是链接本身、不是目标，因此原条目"校验失败会再删一次目标"只成立于**目录**被换成符号链接的情况——该情况现由 `ensurePrivateDir` 拦下。

## N10 索引下发的 `BinNames`/`BinDirs` 未校验即 Remove/Symlink

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/toolchain/catalog.go:387-388`、`:320-335`
- **问题**：`_ = os.Remove(filepath.Join(linkDir, name))` 紧接着 `os.Symlink(..., filepath.Join(linkDir, name))`，`name` 来自远程索引的 `bin_names` 取值，**无任何字符或路径校验**；`BinDirs` 相对路径同样直接 join。`ReconcileBinLinks` 也不校验软链目标是否仍在安装目录内。
- **影响**：`bin_names: {"x": "../../.bashrc"}` 之类会先删除 `~/.bashrc` 再建软链；`bin_dirs` 可把任意目录的可执行文件软链进 `~/.dsh-tools/bin`，从而进入 harness 的 `PATH`。
- **修复**：`linkNameOK` 拒绝空名、`.`、`..` 以及含 `/`、`\` 的名字，`binDirOK` 用 `filepath.IsLocal` 校验每条 `bin_dirs`，`ReconcileBinLinks` 用 `filepath.IsLocal` 校验 `current` 相对安装根（先 `filepath.Rel`）；`linkExecutables` 改为返回 error 并跳过非法条目，不再静默照做。
- **行为变化**：软链自愈现在可能返回错误，经 `SetActiveVersion`/`Uninstall` 暴露到界面——被手改或投毒的索引会让"激活"报失败而不是部分生效。`main.go` 启动时的自愈调用由丢弃错误改为记录该错误。

## N11 doctor 检查行的 `Category`/`Severity` 是全行唯一漏转义字段

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`frontend/app.js:1439`（注入点）、`app.js:1448`（sink）
- **问题**：同一行里 `Name`、`Message`、`Detail` 都过了 `escapeHtml`，只有 `[${c.Category} / ${c.Severity}]` 直接拼进模板，而模板整体赋给 `#doctor-checks` 的 `innerHTML`。字段来自 `dsh doctor --json` 的**子进程 stdout**（`internal/app/app.go:776` 解码、`:803-804` 赋值），无枚举校验。
- **影响**：壳内任意 JS 执行 → 直接拿到 `window.go.app.App.*`（`InstallToolchain` / `AddHostTool` / `RunDoctorRepair` / `ConnectExternal` / 终端 / 剪贴板）。壳前端持有全部 Go 绑定，所以壳内 XSS 等价于拿到这些能力。
- **当前可达性**：`dsh doctor` 内置检查写死枚举，远程直接注入不可达；但壳解析的是子进程输出，且 `@deepseek-ai/dsh-doctor` 对外导出 `registerCheck`——任何加进 doctor 进程的检查都能决定这两个字段。
- **建议**：补 `escapeHtml`，并在 Go 侧把这两个字段收敛到白名单。

## N12 修复→自动启动窗口吞掉失败周期重置

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`frontend/app.js:500-503`、`:505-513`、`:1525`、`:1548-1551`、`:1569`
- **问题**：`runRepair` 在 `repairing=true` 期间调 `applyStatus`，而 `updateStartupDoctor` 第一句就是 `if (diagnosisState.repairing) return;`，于是这次快照既不弹窗，也不走退出失败态的重置。
- **影响**：第二个失败周期只显示「正在自动诊断问题…」而永远不弹诊断窗、不再跑 `RunDoctor`；用户手动点诊断会渲染上一轮的**全绿**报告——应用仍在失败却显示「✓ 3 通过，✗ 0 失败」。
- **修复**：把"非失败态就复位标记、清空诊断缓存"的分支提到 `repairing` 守卫之前——复位是状态迁移的事实记录，不能被"修复中不重复自动弹窗"的抑制逻辑跳过；守卫只保留它原本的职责（失败态下不因 supervisor 状态抖动再触发一轮自动弹窗）。

## N13 并发安装同一工具无锁

- **状态**：已修（2026-09-16，仅进程内）｜✅ 实测复核
- **位置**：`internal/toolchain/install.go:208-250`、`:272-303`
- **问题**：安装状态无进程内或跨进程锁。两个并发 `InstallTool` 对同一 URL 会同时写同一个 part 文件，一方失败会删掉另一方正在用的数据；解压阶段两者都 `os.RemoveAll(root)` 再 `os.Rename`。
- **影响**：「全部更新」进行中再点同一卡片，或同时开两个 launcher 实例，会得到随机失败与「已下载但未激活」的中间状态。
- **修复**：`installVersion` 的下载→解包→激活整段在 `(安装目标目录, 工具 ID)` 粒度的进程内互斥下进行，取到锁后复查 `IsInstalled`——等锁期间已由并发安装装好的版本只补做激活，不再重下一遍。键含工具 ID 而不含版本，因为同一工具的不同版本共用同一份 part 路径、解包暂存目录与 bin 软链。锁条目创建后不回收，键空间由"一个安装目录 × 清单工具数"封顶，实现里因此没有"删除后重建"的窗口。顺带把「目标版本已安装」的收尾抽成 `finishInstalled`，供 `InstallTool` 前置判断与本次复查共用（N7 修的就是这类早退漏激活）。
- **残留**：跨进程（同时开两个 launcher 实例）仍会并发写同一目录，覆盖它需要文件锁（陈旧锁回收、NFS 语义），未做。
- **相邻问题（未修，本轮排查时发现）**：`pruneCache` 在后台 goroutine 里按 500 MB 上限清理缓存，与另一个工具的并发安装之间没有协调；最坏情况是刚移入缓存的归档被删掉、下次安装重新下载（缓存命中路径本身会校验 sha256，不会用到损坏内容）。本条锁不覆盖跨工具场景。

## N14 `schemastery` 闭包注入是假阳性，且 `cp -a` 语义导致嵌套

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`apps/desktop-launcher/linglong/prepare-offline.sh:78`、`:83`（另见 `:84-87`）
- **问题**：守卫是 `[ ! -f "$dest/lib/index.js" ]`，而 `@deepseek-ai/schemastery` 的入口是 `lib/index.cjs`（`vendor/schemastery/package.json` 的 `main`）→ 守卫恒真、每次都进入「注入」分支；目标 `lib` 已存在时 `cp -a "$pkgdir/lib" "$dest/lib"` **嵌套复制**。`:84-87` 的 `2>/dev/null || true` 还会吞掉 `package.json`/`bin` 的复制失败。
- **影响**：日志假装在补闭包，**真正缺文件时永远补不上**；实测产物含 `@deepseek-ai/schemastery/lib/lib/` = 184 KB 重复内容。

## N16 `verify-tools.sh` 的一致性校验可静默跳过

- **状态**：部分修复（2026-09-16：缺引用与缺 python3 已改为硬失败；多版本覆盖仍缺）｜✅ 实测复核
- **位置**：`apps/desktop-launcher/linglong/verify-tools.sh:108-143`、`apps/desktop-launcher/linglong/tools.yaml:50-53`（边界声明在注释里）
- **问题**：`if [ -f "$INDEX_JSON" ]` **没有 else**——`index.json` 缺失或改名时，校验与失败判定一起静默消失；缺 `python3` 时只打印 SKIP、不置 `fail=1`。校验也只 `diff` ID 集合，不比对 `version`/`url`/`sha256`。
- **影响**：「界面可安装、实际必失败」的防线形同虚设，且构建照常成功。
- **多版本下的覆盖面（2026-09-14 补充）**：`installable` 每个工具只有一组 `version`/`url`/`sha256`，因此「sha256 含占位符即失败」这条检查只覆盖**推荐版本**：`jdk21` 新加的 `17.0.20.1` 与 `8u504` 即便写成占位符也能通过构建。多版本清单的唯一事实来源是 `index.json`，`tools.yaml` 只在注释里声明这条边界（见 N20–N24）。要恢复覆盖面，需把该段扩展为多版本格式并让脚本逐版本比对。

## S1 supervisor 保留已退出子进程的 `cmd`

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/supervisor/supervisor.go:109`/`:206`/`:224`/`:445`、`internal/supervisor/process_unix.go:35-41`
- **问题**：子进程退出后 Wait goroutine 只清 `s.pid`/`state`，从不清 `s.cmd`；`Restart`/`StopHarness` 会对已回收的 PID 执行 `kill(-pid, SIGTERM/SIGKILL)`。
- **影响**：PID 回绕复用时误杀无关进程组。

## S2 Terminal 反向加锁顺序

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/terminal/session.go:179-197`、`internal/terminal/manager.go:168-177`
- **问题**：`waitLoop` 持 `s.mu` 时回调 `onStatus`（→ `m.mu.RLock`）；`Manager.List` 持 `m.mu.RLock` 时调 `s.Info`（→ `s.mu.Lock`）。Go `RWMutex` 在有 writer 等待时会阻塞新 reader，三者互等即死锁。
- **影响**：`TerminalList` 目前前端未调用，属潜在死锁。
- **修复**：`waitLoop` 在锁内取出 `onStatus` 后先解锁再调用（与同文件 `onOutput` 的既有写法一致），`Manager.List` 先在 `m.mu` 下取出会话切片、再在锁外逐个取 `Info`（与同文件 `CloseAll` 一致）。两条不变式各有一条 `TryLock` 判定用例，并在两侧各做一次变异验证。沙箱无 C 编译器，`-race` 不可用，本轮未跑竞态检测。

## S3 X11 服务端返回数据未校验

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/clipboard/x11.go:414-418`、`:482-489`
- **问题**：setup 成功回复中 `vendorLen`/`nFormats` 完全来自对端，`off := 32 + pad4(vendorLen) + nFormats*8` 未与 `len(body)` 校验，`body[off:off+4]` 越界即 panic；`readReply` 把 32 位线上长度直接当分配量（`make([]byte, length*4)`，最大约 17 GB），一次回复即可触发 OOM。
- **触发面**：`connectSocket` 的回退顺序含 TCP `127.0.0.1:6000`，且抽象 socket `/tmp/.X11-unix/X0` 在本机 X 不在 `:0`（Wayland 会话、X 在 `:1`、容器路径不同的宿主）时无人绑定，任意本地进程可抢占应答。
- **修复**：setup 回复先校验 `len(body) >= 32` 再读 `vendorLen`/`nFormats`，`off+4 > len(body)` 判为截断并点明各字段；`readReply` 的 32 位线上长度超过 `maxReplyBytes/4`（总量 60 MiB，取 `maxImageBytes` 的 3 倍）即报错。复原改动后实测得到修复前的真实 panic：`slice bounds out of range [:4036] with capacity 40` 与 `[:18] with capacity 16`。

## S4 `harness.log` 无轮转、行缓冲无上限

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/supervisor/supervisor.go:489-496`、`:518-536`、`:554-568`、`:593-608`
- **问题**：日志以 `O_APPEND` 永久追加、跨重启不轮转不裁剪；`readyScanner`/`failScanner`/`timedWriter` 的行缓冲 `append` 无上限，只在遇到 `\n` 时消费。
- **影响**：长期运行下磁盘与内存缓慢耗尽。
- **修复**：新增 `logSink` 取代 `openLogFile`，stdout/stderr 共用同一落盘端，累计写入到达 `maxLogBytes`（5 MiB）即轮转为 `harness.log.1` 再续写，磁盘占用上界约为两倍阈值；行缓冲按用途分别设限（`maxLogLineBytes` = 64 KiB）：扫描器侧超长行整行丢弃（半行喂给特征匹配可能误报就绪或加载失败），日志侧必须带 `[truncated]` 标记落盘后继续缓冲同一行剩余部分（那里的缓冲就是日志内容本身，丢弃等于静默缺内容）。
- **行为变化**：`harness.log` 会轮转，只保留一份历史（`harness.log.1`）。

## S5 `ConfigureChildEnv` 非幂等

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`internal/appenv/env.go:203-212`（调用点 `main.go:30`、`internal/app/app.go:1105`/`:1176`/`:1190`/`:1201`/`:1233`）
- **问题**：每次都把固定段前置到 `PATH`/`LD_LIBRARY_PATH` 并覆盖，安装/切换/卸载会重复调用，段重复累积且从不收敛。

## S6 项目配置 tool ID 未校验

- **状态**：未修｜⚠️ 静态审查
- **位置**：`internal/toolchain/project.go:34-75`、`internal/toolchain/catalog.go:137-144`/`:205-219`
- **问题**：`.dsh-toolchain.yml` 的 `tools:` 键从不与清单比对，`id` 直接进入 `currentLink(dir, id)`。带 `../` 的 ID 会让 `os.Remove(link)` 删除 `<dir>` 之外的同名文件。
- **影响**：需前端直接调用 `ApplyProjectToolchain`，影响面受限。

## S7 `preview.mjs` 的产物目录与样式路径

- **状态**：已修（2026-09-16）｜✅ 实测复核
- **位置**：`frontend/tools/preview.mjs:386`、`:230-238`、`main.go:22`
- **问题**：默认产物写入 `frontend/.preview/`，而 `//go:embed all:frontend` 会把它嵌进二进制（该目录已在 `.gitignore`，但 `all:` 前缀不看 gitignore）。另外 `buildPreview` 只把 `styles.css` 改写成绝对路径，`vendor/xterm.css` 保持相对路径 → 在 `.preview/preview.html` 下 404，xterm 样式从未生效。

## N20 多版本下「非推荐版本」恒显可更新，且「全部更新」不会切换

- **状态**：已修（2026-09-14）｜✅ 已复核
- **位置**：`internal/toolchain/catalog.go`（`ToolStatuses` 的 `HasUpdate` 判定）、`internal/app/app.go`（`UpdateAllTools`）、`internal/toolchain/install.go`（激活条件，见 N7）、`frontend/app.js`（徽标与横幅文案）
- **问题**：`HasUpdate` 是 `active != versions[0]` 的字符串比较，而 `versions[0]` 的语义是「推荐版本」而不是「更高版本」。清单在 2026-09-14 首次出现多版本数据后，用户从市场刻意安装并激活 `8u504` 或 `17.0.20.1` 时，卡片会**永久**显示「可更新」。点「全部更新」也纠正不了：`UpdateAllTools` 传空 version（即 `versions[0]`），`InstallTool` 在目标版本已安装时提前返回，而 `Activate` 在生产代码里无人传（N7）——于是既不下载也不切换，却仍提示「已更新 N 个工具，失败 0 个」。
- **影响**：多版本能力的正常用法（项目指定 JDK 8）被界面判成「该更新」；卡片徽标与状态栏的「N 个可更新」长期不收敛，用户按提示操作不会产生任何变化。
- **修复**：新增 `compareVersions`（`internal/toolchain/version.go`），`HasUpdate` 改为「推荐版本确实高于当前激活版本」；`current` 软链缺失时仍报可更新，让「更新」充当一键修复入口。前端横幅改为点明每个工具的目标版本（`JDK (Temurin) → 21.0.12.1`），卡片徽标保持短文案，卡片宽度约 170px 放不下完整句子（目标版本放在徽标 `title` 与横幅里）。更新流程的切换问题随 N7 一并解决。
- **验证**：`TestHasUpdate_VersionOrder` 覆盖「低于推荐→提示 / 等于推荐→不提示 / 高于推荐→不提示 / 链接缺失→提示」四种情况；`TestCompareVersions` 固定比较规则（含 `8u504` 与 `21.0.12.1` 的跨风格比较、无数字标签退化为字节序）；`go test ./...` 通过。

## N21 `Uninstall` 卸载激活版本后按字母序回退，多版本下会激活错误版本

- **状态**：已修（2026-09-14）｜✅ 已复核
- **位置**：`internal/toolchain/catalog.go`（`Uninstall` 的回退选择、`fallbackVersion`）、`internal/toolchain/version.go`（`sortVersionsDesc`、`highestVersion`）
- **问题**：卸载激活版本后取 `vers[len(vers)-1]`，即**字母序最大**的剩余版本。单版本时代这等价于「唯一的那个」；`jdk21` 曾有 `8u504`/`17.0.20.1`/`21.0.12.1` 三条，字母序为 `17.0.20.1` < `21.0.12.1` < `8u504`，因此回退会选中 **`8u504`**。
- **影响**：用户卸载当前 JDK 后，`current/jdk21` 与 `~/.dsh-tools/bin` 下的 `java` 等命令**静默**切到更旧的版本。
- **修复**：`fallbackVersion` 优先回到清单里的推荐版本（用户按推荐装过它时，回到推荐最符合预期），推荐版本不在时用 `compareVersions` 取数值最高的剩余版本；工具已不在清单里（孤儿目录）时同样走数值最高。顺带把 `InstalledVersions` 按版本号降序展示，卡片下拉不再出现 `17.0.20.1`/`21.0.12.1`/`8u504` 这种字母序。
- **验证**：`TestUninstall_FallbackByVersionOrder` 两条子用例分别固定「推荐版本仍在→回到推荐版本（即使存在数值更高的其它版本）」与「推荐版本已卸载→取数值最高的 `17.0.20.1` 而不是字母序最大的 `8u504`」；`TestToolStatuses_InstalledVersionsOrderedByVersion` 与 `TestSortVersionsDesc`/`TestHighestVersion` 固定排序语义；`go test ./...` 通过。

## N22 端到端审计只覆盖 `versions[0]`，新增版本没有实证防线

- **状态**：未修｜✅ 已复核
- **位置**：`internal/toolchain/e2e_install_test.go:39`（`InstallTool(dir, tool.ID, "")`）、`internal/toolchain/install.go:105-112`（空 version 落到 `LatestVersion()`）
- **问题**：`TestE2E_CatalogInstall` 是清单里「地址可达、归档与清单 sha256 一致、解压布局符合 `bin_rel`/`bin_names`、声明的命令都出现在 `bin/`」的唯一实证手段，但它对每个工具只装 `versions[0]`；`DSH_TC_E2E_IDS` 也只能按工具 ID 过滤。`jdk21` 的 `17.0.20.1` 与 `8u504` 因此不在任何自动化覆盖内，只能靠人工下载实测（见附录 B）。
- **影响**：镜像站轮换或 sha256 抄错一个字符，只会在用户点安装时暴露——这正是该审计当初被加进来的原因（grpcurl 的 sha256 抄错一字符由它首次跑出）。
- **建议**：让该用例遍历每个工具的 `versions`，或增加一个按版本过滤的环境变量。
- **2026-09-14 下架 17 后复核**：`17.0.20.1` 已不在清单（见 N25），未覆盖面收窄为 `8u504` 一条。

---

# 三、低危

## 3 精简 Node 闭包

- **状态**：未修（有意决策）｜✅ 已复核
- **位置**：`apps/desktop-launcher/linglong/prepare-offline.sh:165-167`
- **说明**：`stage/node/lib/node_modules/` 为 `corepack`/`npm`/`pnpm`，`npm` 实测 **20 MB**（含 `node_modules` 16 MB、`node-gyp` 3.4 MB、`sigstore` 44 KB）。注释写明保留理由：lefthook 的 pre-push typecheck 走 `npm run`。收益比原估的 40 MB 小。
- **定级说明**：原清单为 🟡 中，本次降为低危——收益仅 20 MB，且已是有理由的既定取舍。

## 12 去掉 `CFLAGS="-g"`

- **状态**：未修（删除点不在本仓库）｜✅ 已复核
- **位置**：仓库根 `linglong/entry.sh:7-9`、`:132`
- **问题**：该文件由 ll-builder 生成（`linglong/` 被 `.gitignore:41` 忽略，`git ls-files linglong/` 为 0），`export CFLAGS="-g $CFLAGS"` 与末尾的 `symbols-strip.sh` 均由 builder 注入/追加。`apps/desktop-launcher/linglong/` 下 grep `CFLAGS` 零命中。
- **建议**：只能在 `linglong.yaml` 的 `build:` 段显式覆盖，或向 ll-builder 反馈。原清单「注释写着 enable strip symbols、实际行为相反」的判断成立。

## 17 WebKit 单进程模式

- **状态**：未修｜✅ 已复核
- **位置**：`internal/packaging/webkit_linux.go`
- **问题**：只设 `WEBKIT_INJECTED_BUNDLE_PATH` 与 `WEBKIT_DISABLE_DMABUF_RENDERER`（NVIDIA 兜底），无 `WEBKIT_DISABLE_COMPOSITING_MODE` 或单进程开关。

## 22 `/tmp/dsh-webkit-4.1` 符号链接仍建在 `/tmp`

- **状态**：部分实现｜✅ 已复核
- **位置**：`internal/packaging/webkit_linux.go:34`、`:35-37`、`:78-90`
- **问题**：路径常量仍是 `/tmp/dsh-webkit-4.1`，launcher 内 grep `XDG_RUNTIME_DIR` 零命中。**已有的防护**：`webkitHelperLinkUsable()` 校验读回链接并确认 `WebKitNetworkProcess` 可访问，能防悬空或指向旧包，防不住 TOCTOU。
- **建议**：改用 `$XDG_RUNTIME_DIR`。

## 24 壳前端 i18n

- **状态**：未修｜✅ 已复核
- **位置**：`frontend/index.html:2`、`frontend/app.js`
- **问题**：`lang="zh-CN"`，无字典、无 `t()`。`verify-client-ui-i18n` 的扫描范围是 `packages/client/*`、`apps/web/src`、`apps/desktop`(Electron) 的 `{main,update-coordinator}` 与 `renderer/*.js`——**`apps/desktop-launcher/frontend` 不在闸门内**。
- **定级说明**：原清单为 🟡 中，本次降为低危——属体验改进，无功能或安全风险。

## 25 系统托盘

- **状态**：未修｜✅ 已复核
- **位置**：`main.go`、`apps/desktop-launcher/go.mod`
- **问题**：无托盘库、无 `HideOnClose`，`OnBeforeClose` 只保存窗口状态。关窗即退出，后台任务随之中断。
- **定级说明**：原清单为 🟡 中，本次降为低危——属体验改进；若「harness 后台跑长任务」是目标场景，可上调。

## 26 启动进度细化

- **状态**：已修（2026-09-15）｜✅ 已复核
- **位置**：`frontend/index.html`（加载页）、`internal/app/startup_progress.go`、`internal/appenv/startup_progress.go`、`internal/supervisor/supervisor.go`
- **问题**：只有 spinner + 「正在启动...」+ 静态提示。现有进度条仅属工具链安装。
- **修复**：加载页改为四个由观测事实触发的阶段（`starting` / `loading` / `plugins` / `serving`），其中 `plugins` 显示进度条与「已加载 n/m 个插件」。计数由 launcher 随 overlay 注入的上报插件（`internal/appenv/startup_progress.mjs`）统计已 settle 的 loader 条目数并向 stderr 上报，supervisor 解析、app 映射阶段、经节流的 `startup:progress` 事件与 1 秒状态快照送到前端；上报不可用时退回前两档粗粒度文案且不显示进度条。设计与取舍见 [Agent Note](../../.agents/notes/implemented/feature/2026-09-15-desktop-launcher-startup-progress.md)。
- **历史数据（`~/.cache/dsh-desktop/harness.log`，`stderr` 首行 → `dsh web:` 就绪）**：新版（2026-09-14 22:41 起三轮）5.36 s / 5.38 s / 6.63 s；此前最近十轮 11.18–14.64 s，中位数 12.20 s。降至约 44%。
- **进度实测（真实客户端 16:07 那次启动）**：spawn → 首行上报 1.24 s（`0/133`）→ 计数首次到齐 5.65 s（`135/135`）→ 就绪行 5.77 s。即加载页约 4.4 s 在走真实计数，`serving` 段 0.12 s；上报覆盖不到的起始 1.24 s 由 `loading` 阶段承接。开发态与打包态复测一致（`0/127` → `129/129`，就绪紧随）。
- **修复补充（同一轮实测暴露）**：上报器原先在就绪后继续上报，而就绪态自身的重组（客户端 HMR、用户补丁层 watcher、目录选择器）会让分子超过分母——该次启动的日志里多出 26 行，最后一条 `161/136`。现改为「首次计数到齐即停止上报」：通道只服务启动期，日志不再被污染，最后一次上报诚实停在 100%；界面行为不变（`serving` 由那条 100% 行触发，且就绪后加载页已被 iframe 取代）。
- **仍不采纳「按时间猜阶段」**：进度只来自真实计数；没有上报时宁可不显示进度条。
- **不在范围内**：WebView 里 harness 自己的 `Loading plugins…`（浏览器侧客户端启动）仍无进度反馈，本次未动。

## 27 窗口位置未记忆

- **状态**：部分实现｜✅ 已复核
- **位置**：`internal/app/appconfig.go:18-22`、`main.go:38-60`、`:74`
- **说明**：尺寸与最大化状态已记忆；**位置 X/Y 没有**（`WindowState` 无该字段，grep `WindowGetPosition|PosX|PosY` 零命中）。

## 28 窗口背景色硬编码

- **状态**：部分实现｜✅ 已复核
- **位置**：`main.go:69`、`frontend/styles.css:35`、`:77-100`
- **说明**：前端已跟随系统（`color-scheme: light dark` + `prefers-color-scheme: light` 整套浅色变量）；但 `BackgroundColour` 仍硬编码 `{30, 30, 30, 255}`，Go 侧无任何 WebKitGTK 主题设置。

## 30 `RunDoctorRepair` 的 level 参数构造

- **状态**：未修｜✅ 已复核
- **位置**：`internal/app/app.go:842-848`
- **问题**：仍是 `if level >= 2 { args[len(args)-1] = "2" }` 阶梯式改 args 末位。

## 31 connector probe 非幂等

- **状态**：未修｜✅ 已复核
- **位置**：`internal/connector/connector.go:39-51`、`:160-173`
- **问题**：只判 `200 <= code < 400`，不校验响应体、不请求 `/api/health`；探测通过即切 `ModeExternal`。填入任意返回 2xx/3xx 的地址都会显示「已连接」。

## N-extra 前端转义与状态机缺口

- **状态**：部分修复（2026-09-16：转义、样式选择器与开发态提示已修）｜✅ 实测复核
- **位置**：`frontend/app.js:375`、`:1368`、`:1388`、`:746`、`:836`、`:1241-1248`、`:1168-1174`、`:62-68`
- **问题**：
  - `setDoctorSummary` 名为文本实为 `innerHTML`，`:1368` 与 `:1388` 的动态拼接未转义（当前错误文案不含标记，属潜伏 XSS）。
  - `renderHostTools` 写进 `#toolchain-notice` 的开发态提示被 `renderTools` 紧接着无条件覆盖（已实测 devMsg 消失）。
  - `updateProgressBar` 用远程索引下发的 tool id 直接拼 CSS 选择器，未做 `CSS.escape`。
  - `#market-refresh`、`#btn-about` 等多处 `await api().X()` 无 catch，Go panic 时按钮卡在中间态且无反馈。
  - `#about-repo` 的 `isHttpUrl` 兜底是「放行默认导航」，`javascript:` 之类不会被 `preventDefault` 拦住（当前 `info.Repo` 是编译期 https 常量，不可利用）。

## N-extra2 三套自动化测试无执行入口

- **状态**：未修｜✅ 已复核（实跑）
- **位置**：`lefthook.yml:52-67`、`vitest.config.ts:118-123`、`apps/desktop-launcher/frontend/test-app.cjs`、`apps/desktop-launcher/linglong/test-verify-tools.sh`
- **实测结果**：`go test ./...` **11 个包全部通过**；`node --test frontend/test-app.cjs` **38 例全部通过**；`test-verify-tools.sh` 4 项全 PASS。但**三者都没有 CI 或 git hook 入口**：pre-push 只跑 `npm run typecheck` 与 `preview.mjs verify`。
- **附加问题**：`preview.mjs verify` 在 `buildPreview` 里用正则**剥掉全部 `<script>`**，再用手写 fixture 重建弹框 DOM，因此它一行 `app.js` 都不执行。实测那 38 例在 N11 与上一条的缺陷全部存在时仍然 38/38 通过。
- **建议**：把 `node --test apps/desktop-launcher/frontend/test-app.cjs` 加进 pre-push，或给该目录加 `package.json` + test 脚本让它进入 workspace 统一跑。

## N-extra3 打包脚本中重复与漂移的事实

- **状态**：部分修复（2026-09-16：README glob 已修）｜✅ 实测复核
- **位置**：`linglong/prepare-offline.sh:16`/`:134`/`:146-150` ↔ `linglong/linglong.yaml:24`/`:43`/`:54-58`（README 是第三、四处）
- **问题**：捆绑 Node 版本硬编码两处（均为 `24.9.0`）；pnpm「是否已下载」的守卫一处查 `bin/pnpm.cjs`、一处查目录存在 + `bin/pnpm.mjs`；三行包装器在两地逐字重复。升级 Node 时只改一处会让 stage 与容器 fallback 下载不同版本。
- **另**：`prepare-offline.sh:86-88` 的 `for f in README*` 在 CWD 已切到仓库根后展开，匹配的是**仓库根**的 README，而不是 `$pkgdir` 的。

## N23 工具 ID `jdk21` 与内容不符（现含 8/21），改名需要一次性迁移

- **状态**：未修（有意延期）｜✅ 已复核
- **位置**：`internal/toolchain/tools/index.json`（`id: jdk21`，`versions` 两条）、`linglong/tools.yaml`、`internal/toolchain/catalog_test.go:14`/`:158`、`internal/toolchain/install.go:131-142`（依赖解析）
- **问题**：ID 在 2026-09-14 扩为多版本后成为误称。改名为 `jdk` 会连带四处：`gradle` 与 `maven` 的 `dependencies: ["jdk21"]`（不改则新用户装这两个工具直接在 `unknown tool: jdk21` 失败，已有 `jdk21-*` 目录的用户因 `ListVersions` 非空而侥幸跳过）、`linglong/tools.yaml` 的 `installable` 键（不改则 `verify-tools.sh` 的 ID 集合 diff 失败）、`catalog_test.go` 的两条断言，以及**已装用户的状态迁移**——安装目录 `~/.dsh-tools/jdk21-<version>`、`current/jdk21` 软链，还有 `.dsh-toolchain.yml` 里手写的工具 ID（该能力后端已实现而前端未接，见 S6）。
- **迁移期风险（尚未发生，记录以备将来）**：改名而不同时迁移时，残留的 `current/jdk21` 仍会被 `ReconcileBinLinks` 扫描（它不校验工具是否还在清单里，`toolBinDirs` 未命中即回退默认布局探测），继续把 `java`/`javac` 软链进 `~/.dsh-tools/bin`；`os.ReadDir` 按名排序使 `jdk` 先于 `jdk21` 处理，**旧目录的软链最后写入并覆盖新的**，表现为「装了新 `jdk`，`PATH` 上的 `java` 仍指向旧目录」。该推导为逐行阅读所得，**未实跑复现**。
- **建议**：下一次代码改动时一并做：改 ID、依赖声明、白名单键与断言，并写一次性迁移（把 `jdk21-*` 目录转为 `jdk-*`、重建 `current` 与 `bin` 软链、清理孤儿目录）。本次选择先铺多版本、后改 ID，代价是届时迁移的目录从 1 个变成 1–2 个。

## N24 `ToolVersion.LibRel` 无消费点

- **状态**：未修｜✅ 已复核
- **位置**：`internal/toolchain/catalog.go:18`（字段声明）、`:338-351`（`ReconcileBinLinks` 无条件探测 `lib/`/`lib64/`）、`internal/toolchain/install.go:312-317`（只写进 `tool.yml`）
- **问题**：`lib_rel` 被声明、被解析、被写进安装目录的 `tool.yml`，但**没有任何读取点**：库目录绑定按 `root/lib`、`root/lib64` 是否存在决定，与清单声明的 `lib_rel` 无关。`jdk21` 三条版本都写着 `"lib_rel": "lib"`，读起来像有约束，实际不是。
- **影响**：清单作者以为改 `lib_rel` 就能改变 `LD_LIBRARY_PATH` 注入的库目录；`appenv` 注入的是 `~/.dsh-tools/lib` 整目录，绑定由探测决定，发行包布局与声明不符时不会报错，只会静默少绑或不绑。
- **建议**：要么让 `ReconcileBinLinks` 真正消费该字段（未声明时保留现有探测作为回退），要么删掉字段与 `tool.yml` 里那一行，避免清单里留下没有效果的配置。

## N25 JDK 17 从清单下架，远端索引重钉仍未完成

- **状态**：已执行（本地清单、文档与索引重钉）｜⏳ 待随发版到达用户｜✅ 已复核
- **位置**：`internal/toolchain/tools/index.json`（`jdk21.versions`、`description`）、`internal/toolchain/remote.go:44`（`defaultIndexURL` 已重钉到 `ff0b924d11`）、`frontend/tools/preview.mjs:202`（预览 mock）
- **说明**：多版本清单上线当天先收窄版本面——`jdk21` 只保留推荐版本 `21.0.12.1` 与 `8u504`，移除 `17.0.20.1`，`description` 同步改为「可选 8 / 21」。动机是把 N20/N21/N22 三条未修的多版本语义缺陷的暴露面从三版本压到两版本，并为随后修复「更新不切换」（N7）留出更小的改动面。**不是 17 自身有故障**：其下载地址在 2026-09-14 实测 `HTTP/2 302` 可达，清单里的 url/sha256/size 未被改动，本次只是不再提供。
- **影响**：
  - 已装 `jdk21-17.0.20.1` 的机器不受影响——`ListVersions` 扫目录而非查清单，该版本仍可切换与卸载；但「可安装版本」下拉里不再出现 17，且因 N20 未修，激活 17 时卡片仍显示「可更新」。
  - **索引已重钉，但仍待发版**：`defaultIndexURL` 已从 `ee9c181bf6`（含 17）移到承载新清单的 `ff0b924d11`，实现侧取证见 N3。已发布的旧客户端在带这次重钉的版本发布前仍会提供 17；本机 `~/.dsh-tools/index.json` 缓存在 24 小时 TTL 内也仍是旧内容，需等 TTL 过期或点「刷新索引」。
  - `test-verify-tools.sh` 只比对工具 ID 集合、`catalog_test.go` 无 17 断言，两者都不受影响；`README.md`/`README.zh.md` 的「当前只有 JDK 8/17/21」与 `preview.mjs` 的预览下拉已同步为两版本。
- **发布前置（已完成的部分）**：承载新 `index.json` 的提交已推送，`git rev-parse ff0b924d11:apps/desktop-launcher/internal/toolchain/tools/index.json` 得 blob `599f8341…`，实跑 curl 取回 HTTP 200 且 sha256 与工作区逐字节一致。
- **建议**：N7 / N20 / N21 的修复已于同日完成（见各条状态），与本次重钉一并发布即可，不必再分两次。

## N26 `fonts-wqy-microhei` 声明为容器中文字族来源，但产物与运行时都看不到它

- **状态**：未修（记录待查）｜✅ 实测复核
- **位置**：`linglong/linglong.yaml`（`buildext.apt.depends` 里的 `fonts-wqy-microhei`，以及 `build:` 段那段"中文族由下面的 apt 依赖提供"的注释）
- **问题**：该依赖被声明为容器里中文字体的来源，但实测三版已导出产物层（`~/.cache/linglong-builder/merged/{50f29c89,db95460f,f3e4963d}/files`）里**没有任何 wqy／微米黑字体**：`find -name '*wqy*' -o -name '*microhei*' -o -name '*.ttc'` 为空，`share/` 下只有 `applications`、`dsh-fonts`、`icons`；基座层同样没有该字体。
- **影响**：中文回退字体实际不来自这个依赖。运行时 `/usr/share/fonts` 又被宿主目录整体挂载覆盖（同一段注释自己写明），所以该依赖既进不了包、也改变不了运行时——注释描述的链路与事实不符，排查中文显示问题时会把人引向错误方向。本条的产物侧后果已由 `verify-merged-deps.sh` 显式记为 `none:` 认领（不阻塞构建），避免它在依赖清单里继续"看起来有人管"。
- **证据边界**：结论来自对三版产物层与基座层的实体清点；**未**在真实容器里跑 `fc-list` 确认最终渲染走的是哪一路字体（本环境无 ll-builder 与玲珑容器）。
- **建议**：确认运行时中文来源（宿主挂载 vs 随包字体）后二选一——删掉该依赖并更正注释，或让中文字体随包落到 `${PREFIX}/share/dsh-fonts`（`install-container-fonts.sh` 已在该目录装配拉丁与等宽字体，可复用同一路径）。

---

# 附录 D：2026-09-16 修复批次

本节记录一轮「快修」的落点与验证证据；正文对应条目的状态行已同步。行号基准为
本批次开始前的 `linglong-dev` HEAD `8cd0e4a5c2`。

| 条目 | 提交 | 改动 | 验证 |
|---|---|---|---|
| N11 Category/Severity 未转义 | `0cdfa08e70` | 补 `escapeHtml`（计数与 SuggestedLevel 由 Go 侧解成 int，不属该面） | 新增 `诊断清单转义每个取自报告的字符串字段`；还原转义后用例失败 |
| 摘要 HTML/文本混用 | `f2af5255e1` | 拆成 `setDoctorSummaryHtml` / `setDoctorSummaryText` | 新增 `诊断摘要的错误文案按纯文本渲染`；text 版改回 innerHTML 后失败 |
| 开发态提示被覆盖 | `97bf46b604` | 提示条收敛到 `renderTools` 单点写入，文案由 `hostToolsNotice` 合成 | 新增 `提示条：开发态说明不被同一渲染周期的 t.Notice 覆盖`；变异后失败 |
| 进度条选择器拼接 | `1dbdf4caef` | 在 `#market-grid` 子树内按 `dataset.toolId` 匹配，不再拼属性选择器 | 新增 `安装进度按 dataset 定位卡片…`；改回拼接后失败 |
| 20 宿主挂载无确认 | `86349a8a27` | 抽出 `consumeConfirmClick`，扫描结果与手填目录共用；首次点击把「沙箱内所有进程都能读取 <路径>（只读）」写进提示行 | 新增三条用例；`consumeConfirmClick` 改成恒返回 true 后两条失败 |
| N4 X11 cookie 字节序 | `17cfd94e3a` | 长度改小端；`setup` 重试经 `reconnect` 换新连接；关闭路径收敛到 `xconn.close` | 新增 `TestDialRetriesAuthOnFreshConnection` 与只回放握手的 `authFakeServer`；长度改回大端、`reconnect` 改空操作后分别失败 |
| N5 退避整数溢出 | `0e704e378e` | 抽出 `backoffDelay`，先封顶再加倍；`max <= 0` 退回 base | 新增 `TestBackoffDelay`（含 attempt=200 与接近 int 上限）；换回旧公式后失败并触发 `negative shift amount` panic |
| N6 诊断收尾归属 | `9382a78e6d` | 抽出 `finishStartupDoctor`，门禁改为 `doctorEpoch`；reset 递增 epoch | 新增三条用例；改回 `!startupDoctorRunning`、去掉 reset 的 epoch 递增后分别失败 |
| S1 保留已退出 cmd | `7ab68f88eb` | Wait goroutine 在 `s.cmd == cmd` 时清零 | 新增 `TestSupervisor_ClearsExitedCmd`（先等真正拉起再断言）；删掉清空语句后失败 |
| S5 env 非幂等 | `4ec119b1d1` | 抽出 `prependPathEnv`，先剔除同名旧段再前置，空段丢弃 | 新增两条用例；PATH 改回无条件前置后失败 |
| S7 预览产物落进 embed 根 | `593e30a184` | 默认产物移到 `apps/desktop-launcher/.preview`；加「不得位于 frontend/ 内」守卫；删除遗留的 2.2 MB 截图 | `preview.mjs verify` 全绿且产物落在新目录；`--out frontend/.preview` 退出码 1 且不建目录 |
| N14 注入守卫假阳性 | `8cfc13d72b` | 入口候选取自包自己的 `module`/`main`/`exports["."]`；`cp -a src/lib/.` 消除嵌套；bin/package.json 不再吞错 | 新增 `test-prepare-offline-inject.sh`（从真实脚本提取函数）；三处分别变异后失败 |
| N-extra3 README glob | `8cfc13d72b` | `README*` 锚定 `$pkgdir` | 同上；glob 退回裸模式后失败 |
| N16 一致性校验静默跳过 | `1fec90f79b` | 缺 `index.json`／缺 `python3` 均改为硬失败并点明参照物 | `test-verify-tools.sh` 新增两条（影子树 + 收窄 PATH）；两处分别变异后失败 |

**本批次未做**（已在正文各自条目内保留）：8/13 依赖链裁剪、9/10 CI 与体积门禁、
23 独立 `DSH_HOME`、N3 离线签名、N8/N9/N10/N12/N13/S2/S3/S4 等安全类改动、
N17–N19 与 N22–N26 的其余部分。（其中 N8/N9/N10/N12/N13/S2/S3/S4 已由**附录 E** 的
D 批完成，本节其余"未做"项仍然未做。）

**验证边界**：以上均为本仓库内可复跑的用例与脚本；未涉及 ll-builder 与玲珑容器，
没有真实构建或真实 X11 主机的端到端验证。

---

# 附录 E：2026-09-16 D 批（安全类）修复

本节是「快修」之后按序实施的 D 批：正文对应条目的状态行已同步。行号基准同附录 D
（本批次开始前的 `linglong-dev` HEAD `8cd0e4a5c2`），代码改动后可能漂移。

| 条目 | 提交 | 改动 | 验证 |
|---|---|---|---|
| N9 part 路径可预测且跟随符号链接 | `2b77f5dd0b` | part 落到 `<安装目录>/.downloads`；`ensurePrivateDir`（`0o700` + `Lstat` 拒绝符号链接与非目录）、`openPartFile`（`O_NOFOLLOW`） | 4 组变异（去掉 `O_NOFOLLOW`、去掉符号链接检查、去掉 `IsLocal` 校验、把 part 放回 `/tmp`）后对应用例失败；`sha256` 不符的用例改为断言"磁盘上不留已安装痕迹" |
| N10 `BinNames`/`BinDirs` 未校验 | `3befc1e27a` | `linkNameOK`/`binDirOK`/`filepath.IsLocal` 三重校验；`linkExecutables` 返回错误并跳过非法条目 | 3 组变异（放开名字、放开 `bin_dirs`、放开 `current` 越界）后失败；另有"内置索引必须全量通过校验"的守卫用例 |
| S3 X11 回复未校验 | `f093943a7a` | setup 回复的长度与截断校验；`readReply` 长度上限 60 MiB | 3 组变异分别复现修复前的真实 panic（`[:4036] with capacity 40`、`[:18] with capacity 16`） |
| N8 下载与解压无体积上限 | `51052f011b` | `copyCapped` + `extractBudget`；归档 4 GiB / 解压 16 GiB / 100 万条目 | 7 组变异后失败，含"服务端用 chunked 绕过 `Content-Length`"那条（首版用例因 Go 自动补 `Content-Length` 而漏判，已改为显式 flush + 断言 part 大小） |
| S4 日志无轮转、行缓冲无上限 | `c7d6f76936` | `logSink` 按 5 MiB 轮转；两处行缓冲上限 64 KiB（扫描器侧丢弃、日志侧带标记落盘） | 5 组变异（两处行上限、运行中轮转、已有尺寸起算、日志不可用降级）后失败 |
| S2 Terminal 反向加锁顺序 | `5f324aaffb` | `onStatus` 在锁外调用；`List` 先取快照再取 `Info` | 两条 `TryLock` 不变式用例，两侧各变异一次后失败；`-race` 需 cgo，沙箱无 C 编译器，未跑竞态检测 |
| N12 修复窗口吞掉周期复位 | `c91341c0e0` | 非失败态的复位分支提到 `repairing` 守卫之前 | 新增前端用例复现该时序；把旧守卫放回函数开头后失败 |
| N13 并发安装同一工具无锁 | `16e368f872` | `(安装目录, 工具 ID)` 粒度进程内锁 + 取锁后复查 `IsInstalled`；抽出 `finishInstalled` | 5 组变异（去掉串行化、去掉复查、锁不分键、键不含目录、取了锁不加锁）后失败；去掉串行化时实测复现"文件解压失败，归档可能已损坏" |

**验证边界**：以上均为本仓库内可复跑的用例（10 个 Go 包全绿、前端 52 项全绿、两个打包
脚本自测通过）。未涉及 ll-builder 与玲珑容器、真实 X11 主机、真实 Wails GUI 与多 GB
真实下载：X11 用例回放的是合成回复，安装用例走本地 `httptest` 归档，前端用例在桩 DOM
上驱动。`-race` 需要 cgo，沙箱无 C 编译器，因此 S2 只有锁状态断言、没有竞态检测。

**本批次未做**：N3 离线签名、N14/N16 的多版本覆盖面、N17–N26 的其余条目、8/13 依赖链
裁剪、9/10 CI 与体积门禁、S6 项目配置 tool ID 校验、N13 的跨进程文件锁。

---

# 附录 A：已结案

以下条目在审计后已修复、已评估后决定不做，或前提不成立。保留在此避免重复提出。

| 原编号 | 结论 | 依据 |
|---|---|---|
| 1 剥离 GCC 工具链 | ✅ 已完成 | `linglong/prune-gcc-toolchain.sh` 存在，`build-linglong.sh:25` 调用；实测产物 `lib/gcc` 不存在 |
| 2 删除 Node 头文件 | ✅ 已完成 | `prepare-offline.sh:169-172` + `linglong.yaml:62-64`；实测 `node/include` 不存在 |
| 4 剔除 experimental 包 | ✅ 已完成 | `prepare-offline.sh:65-68`；264 个包中 experimental 命中数为 0 |
| 5 注入包只拷 `lib/` | ✅ 已完成 | `prepare-offline.sh:81-88` |
| 6 typescript 不在闭包 | ✅ 已完成 | `prepare-offline.sh:48-52`；实测闭包内不存在 |
| 7 `@img/sharp` | ⚪ 已评估保留 | `sharp` 是 `@deepseek-ai/dsh-attachment-local` 的静态依赖（`packages/attachment/attachment-local/package.json:30`），`@img` 下只有 `colour` + `linux-x64`（19 MB），无可裁的多余平台包 |
| 15 状态轮询改事件驱动 | ✅ 已完成 | `internal/app/app.go:385-420` `emitStatusIfChanged`，提交 `c7be7c8b23` |
| 16 Node 二进制 strip | ✅ 已完成 | `prepare-offline.sh:173-176`；实测 `node/bin/node` 已 stripped（111.6 MB，仅减约 10%，非原估的 20–40%） |
| 18 Go 绑定缺授权层 | 🔵 前提不成立 | harness GUI 是跨源 iframe，Wails 只向自己 asset server 的主页注入 runtime，iframe 内 `window.go` 不可达。**遗留待验证点见下** |
| 21 外部服务只确认一次 | 🟡 题面「或」分支已满足 | `NeedConfirmation` 仍按 hostname 每会话一次；但「状态栏常显 hostname」已落地（`app.js:156-175`，提交 `c3928e3192`） |
| 29 前端 JS 去重 | ✅ 已完成 | 只剩 `escapeHtml`（`app.js:20`），`esc` 已不存在 |
| 36 剪贴板桥 X11 only | ✅ 已完成 | `internal/clipboard/x11.go:76-79` + `readWaylandImage`（`:871-925`）。**注意**：依赖宿主/容器存在 `wl-paste`，而 `wl-clipboard` 未随包（`tools.yaml` 与 `buildext.apt.depends` 都没有） |
| 37 GIT_EXEC_PATH | 🔶 现状即描述 | `internal/appenv/env.go:170-193` 由可执行文件位置推导；`verify-tools.sh:146-151` 有断言 |
| N1 `build-deb.sh` 产不出包 | ✅ 已删除 | 脚本及其文档声明已移除，见 `.agents/notes/implemented/simplification/2026-09-13-remove-deb-packaging-path.md` |
| N2 容器工具清单 overlay 是死代码且副本过期 | ✅ 已修复 | 详见下方 |
| N15 `clean-linglong.sh` 清理路径整体漂移 | ✅ 已修复 | 基准拆分为 `APP_DIR`/`LL_SRC`/`LL_WORK`/`LL_WORK_NESTED`，与 `build-linglong.sh` 的 `linglong/output/binary/files`、`.gitignore` 的 `/linglong/` 对齐；补收根 `lib/`、启动器预览产物（普通档）与 `profiles/`/`sessions/`/`backups/`（深度档）；ll-builder 异常退出留下的 `mode=0000` overlayfs workdir 在删除前 `chmod -R u+rwX`。提交 `4eb3a44acb`、`9f5fe65709`。附注：`pnpm run clean`（`scripts/clean.ts`）未覆盖根 `lib/`，收敛为调用统一入口的决定不做 |
| N17 builder 的 `failed to copy` 只警告不中止 | ❌ 未修（工具链侧） | 连续两轮构建在同一位置失败后照常出包，见正文 N17 |
| N18 `buildext.apt.depends` 吞掉安装错误 | ❌ 未修 | 生成脚本 `linglong/buildext.sh` 两条 apt 命令均以 `\|\| echo "$?"` 结尾，见正文 N18 |
| N19 容器内新装的文件不落盘 | ❌ 未修 | 清缓存后仍复现；overlay upperdir 内只有 `c 0,0` 的 `.dpkg-new`，实体仍是 2026-04-07 的 2.48.5，见正文 N19 |
| 字体方案（时间文本挤压） | ✅ 已完成 | 随包字体 + `FONTCONFIG_FILE` 注入 + 前端字体栈前置；提交 `c1ea5016fa`、`38a44fe892`、`2fc0de8ef5`，已在 0.1.2.7 实机验证通过 |

### 字体方案（时间文本挤压）的记录

客户端中 `6分32秒`、`17小时53分` 一类时间文本出现数字与汉字互相挤压，同一页面在 Chrome 中正常，仅基于 WebKitGTK 的客户端复现。

**根因**：CSS 字体栈（`packages/client/ui-theme/src/styles/base.css` 的 `--dsw-font-family`）在容器内全部候选族缺失，退化到「一个同时覆盖拉丁与 CJK 的族」（思源黑体），同一行内拉丁与 CJK 共用一套度量而挤压。宿主用户级 fontconfig 配置会持续把通用族名改指 CJK 族（见下方「前端字体栈」），因此该问题必须在 CSS 字体栈层面收口：单纯在打包侧注册字体与别名不足以解决。

**方案**：随包携带 Noto Sans Display（拉丁）与 JetBrains Mono（等宽，补 Regular 字重），落入 `${PREFIX}/share/dsh-fonts`——层内 `usr/` 与 `etc/` 均不进容器命名空间，`usr/share/fonts` 另被宿主挂载遮蔽，故不能沿用；由启动器在 WebKit 初始化前以 `FONTCONFIG_FILE` 指向 `install-container-fonts.sh` 生成的 `dsh-fonts.conf`，该配置 include 系统配置、显式声明可写 `<cachedir>`（容器 `/var/cache/fontconfig` 只读）、注册字体目录并把 CSS 栈中的族名别名到随包字体。中文族由 `buildext.apt.depends` 的 `fonts-wqy-microhei` 提供，与 webkit 同一机制。

**实测的别名生效边界**：`sans-serif`、`BlinkMacSystemFont`、`PingFang SC`、`Hiragino Sans GB`、`Microsoft YaHei`、`Helvetica Neue`、`SF Mono`、`Fira Code`、`Menlo`、`Consolas` 均可把对应族名指到随包字体；`-apple-system` 是 fontconfig 内建兜底、别名改不动（`fc-match` 恒返回宿主默认）；`monospace` 被系统配置压住，且 CSS 等宽栈本就不含裸 `monospace`，故未设该别名。注：本机 `99-deepin.conf` 以 prepend+strong 把 `sans-serif` 指向思源黑体，我们的别名只在其后生效，分发到没有该配置的机器上则由我们这条接管。

**已知限制**：fontconfig 按家族名匹配，宿主已装同名字体（思源黑体、微软雅黑等）时随包字体不会被选中；该限制只影响本机观感，不影响分发到缺字体机器上的行为。

**验证证据**：改后脚本生成的配置与容器内实际生效那份逐行 diff，差异仅为删除 `monospace` 一行；用新旧两份配置对 CSS 栈的 17 个候选族名逐条复跑 `fc-match`，结果全部一致（`sans-serif` 命中思源黑体、`BlinkMacSystemFont` 与 `PingFang SC` 等命中 `NotoSansDisplay-Regular.ttf`、等宽族命中 `JetBrainsMono-*.ttf`）；`go test ./internal/packaging/` 通过；`sh -n` 语法检查通过；ui-theme 测试 81 通过（1 个既有失败与本次无关）。

**保留的未验证边界**：`fc-match` 只反映 fontconfig 的解析结果，WebKit 的实际渲染选择由引擎内部逻辑决定，二者可能不同。**重新打包 0.1.2.7 后的实机验证已闭环**：时间文本不再挤压。

**前端字体栈**：只靠打包侧的别名不足。宿主用户级 `~/.config/fontconfig/conf.d/99-deepin.conf` 以 `prepend`+`binding="strong"` 把 `sans-serif` 改指思源黑体，该方式胜过任何别名 `<prefer>`；而 CSS 栈尾正是 `sans-serif`，`-apple-system` 也无法用别名改变。实测在当前宿主上 `-apple-system` 与 `sans-serif` 均落到思源黑体——一个同时覆盖拉丁与 CJK 的族，数字与汉字因而共用一套度量。因此 `packages/client/ui-theme/src/styles/base.css` 把 `'Noto Sans Display'`、`'WenQuanYi Micro Hei'` 前置（提交 `2fc0de8ef5`）；`--dsw-font-family` 是全部 `--dsw-font-*` 排版 token 的基础族，33 个组件文件消费这些 token，时间文本用的 `--dsw-font-xs-13` 即由其派生。等宽栈同时前置 `'JetBrains Mono'`。该改动已在 0.1.2.7 的实机运行中验证：时间文本不再挤压。

### N2 的修复记录

原先两个独立故障，现已一并处理：

1. **注入点**。`linglong.yaml` 不再把预设抽到 `${PREFIX}/harness/config/agent-presets`，改为用 `install -Dm644` 直接覆盖 `dsh-agent-presets` 真正读取的 shipped root（`${PREFIX}/harness/node_modules/@deepseek-ai/dsh-agent-presets/presets/standard/agent.cordis.yml`）；预设包布局若变化则构建 fail loud。
2. **副本**。persona 由已移除的 `text` 改为 `prefix`/`suffix`，并按上游 `packages/preset/agent-presets/presets/standard/agent.cordis.yml` 重新同步 roster（此前静默落后四处：`command-goal` 与 `present` 两行被删、`tool-web.fetch` 由 true 变 false、`modelSelectionSettings` 被删）。
3. **防线**。新增 `linglong/verify-preset-overlay.mjs`，在 `build-linglong.sh` 组装前拼接失败即中止：persona 之外的 roster 必须与上游逐行一致，persona 增量与 `tools.yaml` 对账，并把 persona 配置喂给随包 `dsh-persona` 的 schema。`test-verify-preset-overlay.sh` 覆盖通过路径与四条失败路径。

**验证证据**：用打包闭包自己的 `discoverPresets` 实测——注入前后都是同样的四个预设（`cordis[创造模式] minimal[极简模式] ptc[PTC 模式] standard[标准模式]`），条目数与元数据未变，注入后 `standard` 文件含容器段落。UI 预设列表因此不变。闸门与自测各 5 项全过。

**未做的端到端**：仍未触发一次真实会话观察系统提示（headless profile 不挂 `agent-presets`，只有 web-app bundle 设 `default: standard`）。已验证的链路是「发现 → persona 配置通过随包 schema」，而 `resolveConfig` 正是挂载时的校验点。

**原编号 18 的遗留待验证点**：Wails 把消息处理器注册在 webview 的 content manager 上，而 `wails/v2@v2.15.0/internal/frontend/desktop/linux/window.c:56` 回传的是**顶层** URI。请在 iframe 内打开 Web Inspector，试 `window.go` 与 `window.webkit.messageHandlers` 是否可达——可达即为真漏洞，不可达则彻底结案。

---

# 附录 B：验证边界

- 附录 A 中「已完成」条目，以及正文标注 ✅ 的条目，均经逐行读取代码或实跑命令验证。
- 标注 ⚠️ 的条目来自静态代码审查，审计者未逐条复跑；标注 ❓ 的条目依赖尚未执行的端到端运行。
- 保留本条以说明历史判据：N2 修复前「同一 schema + 同一配置 + `resolveConfig` 必然抛错」的函数链已实测，但那只是链路推演；修复后改为用打包闭包的 `discoverPresets` 做运行时发现验证（见附录 A 的修复记录）。两者的共同缺口是仍未触发真实会话观察系统提示。
- 所有体积数据来自 `linglong/output/binary/files` 与 `apps/desktop-launcher/linglong/stage/` 的实际构建产物；二者是 gitignore 的构建工作区，不是受控源码。审计收尾时这些构建缓存曾清理，随后为验证字体方案重新生成。
- 仓库当前的文档闸门并非全绿：`verify-translation-pairing` 语料为 837 ok / 1 out-of-sync / 33 missing（含 `docs/superpowers/**` 与清理前 `linglong/` 下被扫到的构建产物），`verify-md-wrap`、`verify-md-links`、`verify-package-readme-*` 亦有既有失败。这些与本文条目无关，但会影响「闸门全绿」的判断。
- 字体方案的验证边界见附录 A 的对应记录。
- JDK 多版本条目（N20–N24）的证据边界：两份新增归档经真实下载，size 与 sha256 和 Adoptium v3 API 报出的值一致（`8u504` 103542511 字节 / `9c70e102…`；`17.0.20.1` 193252603 字节 / `3808d1d1…`），解包后为单一顶层目录且含 `bin/` 与 `lib/`，`java`/`javac`/`jdb`/`jar` 均可执行、`java -version` 分别报 `1.8.0_504` 与 `17.0.20.1`；`21.0.12.1` 未重下，API 当前 21 资产的 sha256 与清单现值相同。**未在玲珑容器内实跑**，也未走通 launcher 的真实安装路径——市场唯一入口是 Wails 绑定，端到端审计只覆盖 `versions[0]`（见 N22）。索引发布侧已实跑 curl 复核（见 N3 的验证）。`17.0.20.1` 已于 2026-09-14 下架（见 N25），上列 17 的实测数据保留为下架前的历史记录。

---

# 附录 C：建议起手顺序

1. **9 / 10 / 13**（一条流水线带体积断言与 `depends.yaml` 比对）——一次性止住体积与工具链回归。
2. **20**（宿主挂载二次确认）——复用现成两击确认模式，改动最小、安全收益最大。
3. **N3 / N4**——供应链来源认证与 X11 cookie 字节序（N7 那处「用户可见的静默无效」已于 2026-09-14 修复）。
4. **8**（WebKit 依赖链裁剪）——剩余体积里唯一的大块，约 50 MB+。
5. **N22 / N16**（把多版本纳入两道门禁）——多版本的三处用户可见错误（N7 / N20 / N21）已于 2026-09-14 修复，门禁这两条仍待做；**N23** 的 ID 改名与一次性迁移可与此一并做。

（原第 3 项 N2 已完成，见附录 A。）

# DeepSeek Harness 桌面启动器审计清单

本文是 `apps/desktop-launcher` 及其玲珑打包链路的缺陷清单与整改待办，面向维护者。

它不在文档闸门覆盖范围内（`verify-translation-pairing` 的语料谓词只认 README 基名、`docs/`、`.agents/notes/`、`python/`；`verify-md-wrap` 与 `verify-md-links` 的 glob 都不含 `apps/`），因此**没有自动化手段保证本文与代码同步**：改动上面任一条目后，请在同一次提交里更新本文对应小节，并注明验证状态。

## 审计基准

- 分支 `linglong-dev`，基点提交 `dfd0e9d186`（与 `linglong` 的文件树完全相同，tree 均为 `eea557d697a7bce2b28f435e1ca070ef70ef73d7`）。
- 玲珑包版本 `0.1.2.5`，产物 `com.deepseek.dsh-desktop_0.1.2.5_x86_64_main.uab` = **361 MB**（361,331,344 字节），解压后 **759 MB**。
- 体积分布：`lib/` 320 MB（其中 `lib/x86_64-linux-gnu` **293 MB / 352 个 `.so`**）、`harness/` 254 MB、`node/` 147 MB、`bin/` 39 MB；生产闭包含 **264 个** `@deepseek-ai/*` 包。
- 条目编号：首轮审计的条目沿用原编号 1–37；审计轮次新增条目沿用 `N` 系列（`N1`–`N25`，其中 `N20`–`N24` 来自 2026-09-14 的 JDK 多版本清单那一轮，`N25` 来自同日的 JDK 17 下架）；审计者未编号的其余静态审查发现为 `S1`–`S7`。
- 行号以审计基点为准，代码改动后可能漂移。
- `N20`–`N24` 的基点：分支 `linglong-dev` 提交 `92d150b3cb`（索引钉在 `ee9c181bf6`），玲珑包版本 `0.1.2.7`；行号以该提交为准。
- `N25` 的基点：提交 `9621d91bff`（JDK 17 下架前的清单状态）；行号以该提交为准。

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
- **已修（本次，采纳审计建议的第二选项）**：默认地址固定到**不可变提交哈希**。首次钉的是 `47d123e212ced431eb582e83f7d58a081b39d43c`（发布 43 项清单的那个提交）；2026-09-14 随 JDK 多版本清单前移到 `ee9c181bf66f655d24810012c4c9f61e59c9940c`（该提交的 `index.json` blob `acf8d0ce…` 与工作区一致，动机与后续条目见 N20–N24）。信任对象由此从「上游账号」收敛为「这份二进制」：控制分支不再能替换索引内容。配套：
  1. 新增回归守卫 `TestDefaultIndexURL_PinnedToCommit`——断言默认引用是 40 位提交哈希、且路径未被改到别处（先写测试确认它在旧值 `linglong` 上失败，再改常量使其通过）。
  2. 中英 README 撤掉「sha256 即安全」的表述，改写为「对清单的一致性校验，清单自身由客户端固定的提交哈希锚定」；并同步修正同段末尾「增删工具不必发客户端」——该句在固定引用后已不成立，现说明发布索引需改常量并重发客户端。
- **仍待决定（需产品决策，本次未做）**：审计建议的第一选项——**离线公钥签名**。它能在保留「只发索引不发客户端」更新方式的同时提供来源认证，但需要密钥托管与签名发布流程，属于用户尚未持有的流程变更，故不在无人确认时擅自引入。
- **验证**：`TestDefaultIndexURL_PinnedToCommit` 先失败（`默认索引引用 "linglong" 不是 40 位提交哈希`）后通过；`go test ./internal/toolchain ./internal/appenv` 通过；`gofmt -l` 无输出；`CGO_ENABLED=0 go vet ./...` 退出码 0；实跑 curl 按该提交哈希取回的索引 HTTP 200 且 sha256 `74d548e3…` 与仓库内 `index.json` 逐字节一致。**2026-09-14 复核（换钉后）**：`go test ./internal/toolchain` 通过；实跑 curl 新钉住的 `ee9c181b…` 取回 HTTP 200、sha256 `7f4ea9bb6914294aafca7a055f299fc3d9790e8adcd6f8baeea3df752081f046`，与仓库内 `index.json` 逐字节一致（43 项工具，`jdk21` 三个版本）。**边界**：本环境无 gcc（审计已记录 gcc 工具链被裁），`CGO_ENABLED=1 go vet ./...`（含 wails cgo 路径）无法执行。

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

- **状态**：部分修复｜✅ 实测复核
- **位置**：`linglong/buildext.sh`（由 `linglong.yaml` 的 `buildext:` 段生成，每次构建覆盖；`linglong/` 被 `.gitignore:41` 忽略）；仓库侧落点 `linglong/verify-container-deps.sh`、`linglong/linglong.yaml`（`build:` 段开头）
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

## N19 容器内新装的文件不落盘，包内实体停留在基础层旧版本

- **状态**：部分修复｜✅ 实测复核
- **位置**：`linglong/overlay/prepare_base/upperdir/`（构建器 base overlay）；受影响实体 `usr/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0.19.7`；仓库侧落点 `linglong/verify-container-deps.sh`、`linglong/linglong.yaml`（`build:` 段）
- **问题**：构建容器里 apt 装的是 webkit2gtk **2.50.4**，但整个 overlay 的 upperdir 里**没有任何 2.50.4 的实体内容**。每个新解包的文件只留下一个**字符设备 `c 0,0` 的 `<名字>.dpkg-new`**（例如 `libwebkit2gtk-4.1.so.0.19.7.dpkg-new`、`webkit2gtk-4.1/MiniBrowser.dpkg-new`），而实体文件保持 **2026-04-07** 的旧版本（`.so.0.19.7`，92,804,704 字节）不变。
- **已排除**：**不是构建器缓存**。清空 `~/.cache/linglong-builder` 后重建（缓存内 webkit 副本 52 份 → 2 份），失败原样复现，sha256 仍是 `765432e2…`；清空 `linglong/` 工作区重建同样无效。因此本条**替代**原先「缓存复用」的定性。
- **影响**：`buildext.apt.depends` 声明的依赖升级无法进入产物；`find linglong ~/.cache/linglong-builder -name 'libwebkit2gtk-4.1.so.0.2*'` 恒为零——新版内容从未落盘。附带结论：**修复前不要指望「升级 WebKit」带来任何行为变化**，包内恒定 2.48.5。
- **旁证**：该次打包日志中 `failed to copy …/libwebkit2gtk-4.1.so.0 …: 无效的参数` 出现 3 次（见 N17），构建仍声明完成并导出 345 MB 产物。
- **建议**：不要在 prepare_base/overlay 路径上继续投入；改为在 `build:` 段自行 `apt-get download` + `dpkg-deb -x`，把目标库直接装进 `${PREFIX}`（普通文件写入，不经 overlay 合并）。
- **已修（本次）**：把该症状变成构建期硬失败。① `build:` 段开头调用 `verify-container-deps.sh`，其中两条直接针对本条的现场特征：包内实体若是字符设备即失败；`/usr` 下存在任何 `*.dpkg-new` 残留即失败（另加 `dpkg --verify` 比对实体与包记录）。② `build:` 段原有的 webkit 唯一命中断言从 `-e` 收紧为 `-f`——`-e` 对字符设备同样为真，而下面紧接着就是 `cp -a "$1"`，会把设备节点原样复制进产物。
- **未采纳审计的替代实现（需说明）**：`apt-get download` + `dpkg-deb -x` 直接写 `${PREFIX}` 未在本轮实施。两个原因：其一，buildext 的合并发生在 **preCommit**（`build:` 之后），直接写进 `${PREFIX}` 的内容与随后合并进来的 `/usr` 旧实体谁胜出取决于 ll-builder 的去重时序，而该时序在仓库里只有注释、没有可核对的声明（见第 8/13 条）；其二，本环境没有 ll-builder 与玲珑容器，任何对依赖交付机制的改写都无法端到端验证，只能在用户机器上盲试。断言是当前唯一"改了就能验证"的一步。
- **验证边界（如实说明）**：日志证据表明 webkit **实体**在容器内确实停在旧版本，`dpkg --verify` 因而会对不上——但这条推断**未在真实容器里复跑**（本环境无 ll-builder，见上）。已在本机用 fixture + stub 覆盖该分支：`test-verify-container-deps.sh` 的"字符设备实体"与"`*.dpkg-new` 残留"两项。

## N17 builder 的 `failed to copy` 只警告不中止，包会静默沿用旧库

- **状态**：部分修复（仓库侧已拦截）｜✅ 实测复核
- **位置**：`.uab` 组装阶段的构建器（ll-builder / linyaps builder，仓库外工具）；本体日志见 `/home/Jokul/Desktop/日志.txt:1941`；仓库侧落点 `build-linglong.sh:29-42`、`linglong/verify-builder-log.sh`
- **问题**：`failed to copy …/libwebkit2gtk-4.1.so.0 …: 无效的参数` 之后 `[Install Files]`（`L1942`）、`[Commit Contents]`（`L1945`）、`[Runtime Check]`（`L1949`）照常执行，产物以 345 MB 导出。最终包内仍是 4 月的 2.48.5。
- **影响**：依赖升级会被静默丢弃。比第 33 条更隐蔽——第 33 条至少会在下一次找不到文件时炸掉，这条连炸都不炸。
- **建议**：仓库侧按 N19 第 1 条加构建后硬断言；并向上游反馈该 `failed to copy`（附带 `linglong/overlay/prepare_base/upperdir` 下 5,830 个 `.dpkg-new` 字符设备与 `.wh..opq` 白障的证据）。
- **已修（本次）**：仓库侧事后拦截。`build-linglong.sh` 把 `ll-builder build` 的完整输出保留到 `linglong/build.log`（该路径在 `.gitignore:41` 的 `/linglong/` 内），导出前调用 `verify-builder-log.sh`，命中 `failed to copy` 即打印命中条数与位置并非零退出。同时保住构建器自身的退出码：POSIX `sh` 没有 `pipefail`，因此用子 shell 把 `$?` 写进状态文件再读回，避免 `tee` 的退出码掩盖构建失败。
- **未修（工具链侧）**：构建器仍把复制失败降级为警告。仓库侧只能拦住"带着旧文件出包"，无法让它别丢文件；上游反馈仍是必要动作。
- **验证**：`sh apps/desktop-launcher/linglong/test-verify-builder-log.sh` 3 项全过（正常日志通过；含 `failed to copy` 时非零退出并指明位置；日志缺失时非零退出）。另单独实测了状态捕获惯用法：子 shell 内 `exit 7` 被正确读回为 7，`tee` 同时把输出落盘。

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

- **状态**：未修｜✅ 已复核
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

- **状态**：未修｜✅ 已复核
- **位置**：`internal/clipboard/x11.go:387-390`
- **问题**：连接请求声明 `'l'`（LSBFirst），协议要求所有字段按客户端字节序编码，但 auth name/data 长度用 `binary.BigEndian.PutUint16` 写入（应为 LittleEndian）。服务端读到 name 长度 `0x1200`=4608、data 长度 `0x2000`=8192，认证必然失败。此外两次尝试**复用同一条已失败的连接**（X 服务端在 Failed 后关闭连接）。
- **影响**：需要 Xauthority 的 X11 主机上粘贴截图**永久无反应**，且无日志无提示（Wayland 有 `wl-paste` 兜底，纯 X11 会话没有）。
- **覆盖缺口**：`x11_test.go` 的 fakeXServer 只读 12 字节 setup 并对首个无认证请求直接回成功，cookie 路径与其长度编码完全未被覆盖。

## N5 supervisor 重启退避整数溢出

- **状态**：未修｜✅ 已复核
- **位置**：`internal/supervisor/supervisor.go:368-372`
- **问题**：`attempt` 只在收到 `startCh` 时归零，「启动成功后又崩溃」的循环会一直累加；`RestartDelayMs * (1 << (attempt-1))` 在 `int` 上溢出为负数，随后 `if delay > MaxRestartDelayMs` 对负值不成立，`time.After(负值)` 立即触发。
- **影响**：监护循环退化为无退避的 spawn 风暴，日志疯涨、CPU/内存被打满。约 56 次连续「就绪后崩溃」即进入。
- **建议**：移位前对 `attempt` 设上限，并给 `delay <= 0 → Max` 兜底。

## N6 启动自动诊断的收尾判断用错变量

- **状态**：未修｜✅ 已复核
- **位置**：`internal/app/app.go:509-529`（配合 `:534-550`、`:711-726`）
- **问题**：epoch 只用于清理 `cancel`/`done`，真正的「是否过期」判断却是 `if !a.startupDoctorRunning`。被取消的旧诊断在新诊断运行期间（`startupDoctorRunning` 又为 true）通过该判断，写入旧结果并把 `startupDoctorRunning` 置 false；**真正的新诊断结束时结果被 `return` 丢弃**。
- **影响**：前端显示「诊断已就绪 + 伪造的错误」，真实结论丢失；失败路径下的自动诊断静默失效——而这正是它最该起作用的时刻。
- **建议**：状态写入同样以 `a.doctorEpoch == myEpoch` 为唯一门禁。

## N7 「全部更新」从不激活新版本

- **状态**：未修｜✅ 已复核
- **位置**：`internal/toolchain/install.go:115`、`:158-163`、`internal/app/app.go:1086-1109`
- **问题**：`activate` 默认 `false`，生产代码**无任何调用点**传 `InstallOptions.Activate`（`grep Activate` 仅命中定义）；而激活条件是 `if activate || !hadOther`，更新时 `hadOther=true` → 装完不激活。
- **影响**：命令仍跑旧版本、`HasUpdate` 恒真、可更新徽标永不清除；再点一次会因 `IsInstalled` 提前返回，却仍提示「已更新 N 个工具，失败 0 个」。

## N8 下载与解压无体积上限

- **状态**：未修｜✅ 已复核
- **位置**：`internal/toolchain/install.go:411`、`:595-599`、`:701-703`
- **问题**：下载用 `io.Copy(f, reader)` 无 `LimitReader`（只有 10 分钟整体超时）；tar/zip 解压同样无单文件/总量上限。唯一的体积上限是 `remote.go:112` 给**索引**的 4 MiB。
- **影响**：恶意或被篡改的镜像可写满 `~/.dsh-tools` 所在分区（ENOSPC），或触发解压炸弹。

## N9 断点续传 part 路径可预测且跟随符号链接

- **状态**：未修｜✅ 已复核
- **位置**：`internal/toolchain/install.go:431-435`、`:385`、`:394`
- **问题**：part 路径 = `os.TempDir()/dsh-tools-downloads/<sha256(url)[:16]>.part`，内容可由公开索引推算；目录用 `MkdirAll(..., 0o755)`、文件用 `os.OpenFile(destPath, flag, 0o644)`，**无 `O_NOFOLLOW`/`O_EXCL`**。
- **影响**：多用户主机上可让 launcher 以自身权限覆盖任意可写文件；校验失败后的 `os.Remove(partPath)` 会再删一次目标。与第 22 条（`/tmp/dsh-webkit-4.1` 用 `/tmp`）不同：这条是**跟随符号链接写入/删除**。

## N10 索引下发的 `BinNames`/`BinDirs` 未校验即 Remove/Symlink

- **状态**：未修｜✅ 已复核
- **位置**：`internal/toolchain/catalog.go:387-388`、`:320-335`
- **问题**：`_ = os.Remove(filepath.Join(linkDir, name))` 紧接着 `os.Symlink(..., filepath.Join(linkDir, name))`，`name` 来自远程索引的 `bin_names` 取值，**无任何字符或路径校验**；`BinDirs` 相对路径同样直接 join。`ReconcileBinLinks` 也不校验软链目标是否仍在安装目录内。
- **影响**：`bin_names: {"x": "../../.bashrc"}` 之类会先删除 `~/.bashrc` 再建软链；`bin_dirs` 可把任意目录的可执行文件软链进 `~/.dsh-tools/bin`，从而进入 harness 的 `PATH`。

## N11 doctor 检查行的 `Category`/`Severity` 是全行唯一漏转义字段

- **状态**：未修｜✅ 已复核
- **位置**：`frontend/app.js:1439`（注入点）、`app.js:1448`（sink）
- **问题**：同一行里 `Name`、`Message`、`Detail` 都过了 `escapeHtml`，只有 `[${c.Category} / ${c.Severity}]` 直接拼进模板，而模板整体赋给 `#doctor-checks` 的 `innerHTML`。字段来自 `dsh doctor --json` 的**子进程 stdout**（`internal/app/app.go:776` 解码、`:803-804` 赋值），无枚举校验。
- **影响**：壳内任意 JS 执行 → 直接拿到 `window.go.app.App.*`（`InstallToolchain` / `AddHostTool` / `RunDoctorRepair` / `ConnectExternal` / 终端 / 剪贴板）。壳前端持有全部 Go 绑定，所以壳内 XSS 等价于拿到这些能力。
- **当前可达性**：`dsh doctor` 内置检查写死枚举，远程直接注入不可达；但壳解析的是子进程输出，且 `@deepseek-ai/dsh-doctor` 对外导出 `registerCheck`——任何加进 doctor 进程的检查都能决定这两个字段。
- **建议**：补 `escapeHtml`，并在 Go 侧把这两个字段收敛到白名单。

## N12 修复→自动启动窗口吞掉失败周期重置

- **状态**：未修｜⚠️ 静态审查（子代理在一次性副本上复现，审计者未独立复跑）
- **位置**：`frontend/app.js:500-503`、`:505-513`、`:1525`、`:1548-1551`、`:1569`
- **问题**：`runRepair` 在 `repairing=true` 期间调 `applyStatus`，而 `updateStartupDoctor` 第一句就是 `if (diagnosisState.repairing) return;`，于是这次快照既不弹窗，也不走退出失败态的重置。
- **影响**：第二个失败周期只显示「正在自动诊断问题…」而永远不弹诊断窗、不再跑 `RunDoctor`；用户手动点诊断会渲染上一轮的**全绿**报告——应用仍在失败却显示「✓ 3 通过，✗ 0 失败」。

## N13 并发安装同一工具无锁

- **状态**：未修｜⚠️ 静态审查
- **位置**：`internal/toolchain/install.go:208-250`、`:272-303`
- **问题**：安装状态无进程内或跨进程锁。两个并发 `InstallTool` 对同一 URL 会同时写同一个 part 文件，一方失败会删掉另一方正在用的数据；解压阶段两者都 `os.RemoveAll(root)` 再 `os.Rename`。
- **影响**：「全部更新」进行中再点同一卡片，或同时开两个 launcher 实例，会得到随机失败与「已下载但未激活」的中间状态。

## N14 `schemastery` 闭包注入是假阳性，且 `cp -a` 语义导致嵌套

- **状态**：未修｜✅ 已复核
- **位置**：`apps/desktop-launcher/linglong/prepare-offline.sh:78`、`:83`（另见 `:84-87`）
- **问题**：守卫是 `[ ! -f "$dest/lib/index.js" ]`，而 `@deepseek-ai/schemastery` 的入口是 `lib/index.cjs`（`vendor/schemastery/package.json` 的 `main`）→ 守卫恒真、每次都进入「注入」分支；目标 `lib` 已存在时 `cp -a "$pkgdir/lib" "$dest/lib"` **嵌套复制**。`:84-87` 的 `2>/dev/null || true` 还会吞掉 `package.json`/`bin` 的复制失败。
- **影响**：日志假装在补闭包，**真正缺文件时永远补不上**；实测产物含 `@deepseek-ai/schemastery/lib/lib/` = 184 KB 重复内容。

## N16 `verify-tools.sh` 的一致性校验可静默跳过

- **状态**：未修｜✅ 已复核
- **位置**：`apps/desktop-launcher/linglong/verify-tools.sh:108-143`、`apps/desktop-launcher/linglong/tools.yaml:50-53`（边界声明在注释里）
- **问题**：`if [ -f "$INDEX_JSON" ]` **没有 else**——`index.json` 缺失或改名时，校验与失败判定一起静默消失；缺 `python3` 时只打印 SKIP、不置 `fail=1`。校验也只 `diff` ID 集合，不比对 `version`/`url`/`sha256`。
- **影响**：「界面可安装、实际必失败」的防线形同虚设，且构建照常成功。
- **多版本下的覆盖面（2026-09-14 补充）**：`installable` 每个工具只有一组 `version`/`url`/`sha256`，因此「sha256 含占位符即失败」这条检查只覆盖**推荐版本**：`jdk21` 新加的 `17.0.20.1` 与 `8u504` 即便写成占位符也能通过构建。多版本清单的唯一事实来源是 `index.json`，`tools.yaml` 只在注释里声明这条边界（见 N20–N24）。要恢复覆盖面，需把该段扩展为多版本格式并让脚本逐版本比对。

## S1 supervisor 保留已退出子进程的 `cmd`

- **状态**：未修｜⚠️ 静态审查
- **位置**：`internal/supervisor/supervisor.go:109`/`:206`/`:224`/`:445`、`internal/supervisor/process_unix.go:35-41`
- **问题**：子进程退出后 Wait goroutine 只清 `s.pid`/`state`，从不清 `s.cmd`；`Restart`/`StopHarness` 会对已回收的 PID 执行 `kill(-pid, SIGTERM/SIGKILL)`。
- **影响**：PID 回绕复用时误杀无关进程组。

## S2 Terminal 反向加锁顺序

- **状态**：未修｜⚠️ 静态审查
- **位置**：`internal/terminal/session.go:179-197`、`internal/terminal/manager.go:168-177`
- **问题**：`waitLoop` 持 `s.mu` 时回调 `onStatus`（→ `m.mu.RLock`）；`Manager.List` 持 `m.mu.RLock` 时调 `s.Info`（→ `s.mu.Lock`）。Go `RWMutex` 在有 writer 等待时会阻塞新 reader，三者互等即死锁。
- **影响**：`TerminalList` 目前前端未调用，属潜在死锁。

## S3 X11 服务端返回数据未校验

- **状态**：未修｜✅ 已复核
- **位置**：`internal/clipboard/x11.go:414-418`、`:482-489`
- **问题**：setup 成功回复中 `vendorLen`/`nFormats` 完全来自对端，`off := 32 + pad4(vendorLen) + nFormats*8` 未与 `len(body)` 校验，`body[off:off+4]` 越界即 panic；`readReply` 把 32 位线上长度直接当分配量（`make([]byte, length*4)`，最大约 17 GB），一次回复即可触发 OOM。
- **触发面**：`connectSocket` 的回退顺序含 TCP `127.0.0.1:6000`，且抽象 socket `/tmp/.X11-unix/X0` 在本机 X 不在 `:0`（Wayland 会话、X 在 `:1`、容器路径不同的宿主）时无人绑定，任意本地进程可抢占应答。

## S4 `harness.log` 无轮转、行缓冲无上限

- **状态**：未修｜⚠️ 静态审查
- **位置**：`internal/supervisor/supervisor.go:489-496`、`:518-536`、`:554-568`、`:593-608`
- **问题**：日志以 `O_APPEND` 永久追加、跨重启不轮转不裁剪；`readyScanner`/`failScanner`/`timedWriter` 的行缓冲 `append` 无上限，只在遇到 `\n` 时消费。
- **影响**：长期运行下磁盘与内存缓慢耗尽。

## S5 `ConfigureChildEnv` 非幂等

- **状态**：未修｜⚠️ 静态审查
- **位置**：`internal/appenv/env.go:203-212`（调用点 `main.go:30`、`internal/app/app.go:1105`/`:1176`/`:1190`/`:1201`/`:1233`）
- **问题**：每次都把固定段前置到 `PATH`/`LD_LIBRARY_PATH` 并覆盖，安装/切换/卸载会重复调用，段重复累积且从不收敛。

## S6 项目配置 tool ID 未校验

- **状态**：未修｜⚠️ 静态审查
- **位置**：`internal/toolchain/project.go:34-75`、`internal/toolchain/catalog.go:137-144`/`:205-219`
- **问题**：`.dsh-toolchain.yml` 的 `tools:` 键从不与清单比对，`id` 直接进入 `currentLink(dir, id)`。带 `../` 的 ID 会让 `os.Remove(link)` 删除 `<dir>` 之外的同名文件。
- **影响**：需前端直接调用 `ApplyProjectToolchain`，影响面受限。

## S7 `preview.mjs` 的产物目录与样式路径

- **状态**：未修｜⚠️ 静态审查
- **位置**：`frontend/tools/preview.mjs:386`、`:230-238`、`main.go:22`
- **问题**：默认产物写入 `frontend/.preview/`，而 `//go:embed all:frontend` 会把它嵌进二进制（该目录已在 `.gitignore`，但 `all:` 前缀不看 gitignore）。另外 `buildPreview` 只把 `styles.css` 改写成绝对路径，`vendor/xterm.css` 保持相对路径 → 在 `.preview/preview.html` 下 404，xterm 样式从未生效。

## N20 多版本下「非推荐版本」恒显可更新，且「全部更新」不会切换

- **状态**：未修｜✅ 已复核
- **位置**：`internal/toolchain/catalog.go:487`（`HasUpdate` 判定）、`internal/app/app.go:1081-1111`（`UpdateAllTools`）、`internal/toolchain/install.go:158-163`（激活条件，见 N7）
- **问题**：`HasUpdate` 是 `active != versions[0]` 的字符串比较，而 `versions[0]` 的语义是「推荐版本」而不是「更高版本」。清单在 2026-09-14 首次出现多版本数据后，用户从市场刻意安装并激活 `8u504` 或 `17.0.20.1` 时，卡片会**永久**显示「可更新」。点「全部更新」也纠正不了：`UpdateAllTools` 传空 version（即 `versions[0]`），`InstallTool` 在目标版本已安装时提前返回，而 `Activate` 在生产代码里无人传（N7）——于是既不下载也不切换，却仍提示「已更新 N 个工具，失败 0 个」。
- **影响**：多版本能力的正常用法（项目指定 JDK 8）被界面判成「该更新」；卡片徽标与状态栏的「N 个可更新」长期不收敛，用户按提示操作不会产生任何变化。
- **建议**：把「推荐」与「更高版本」分开表达——`HasUpdate` 改为按版本比较、只在 `active` 低于 `versions[0]` 时置位，或在卡片上区分「推荐版本」与「不是推荐版本」两种措辞。需与 N7 一并处理，否则「全部更新」仍然不激活。
- **2026-09-14 下架 17 后复核**：`17.0.20.1` 已不在清单（见 N25），本条的实例改为「用户刻意安装并激活 `8u504` 时卡片永久显示可更新」——两版本下同样成立，判定逻辑未变。

## N21 `Uninstall` 卸载激活版本后按字母序回退，多版本下会激活错误版本

- **状态**：未修｜✅ 已复核
- **位置**：`internal/toolchain/catalog.go:239-247`（回退选择）、`:166-182`（`ListVersions` 用 `sort.Strings`）
- **问题**：卸载激活版本后取 `vers[len(vers)-1]`，即**字母序最大**的剩余版本。单版本时代这等价于「唯一的那个」；`jdk21` 现有 `8u504`/`17.0.20.1`/`21.0.12.1` 三条，字母序为 `17.0.20.1` < `21.0.12.1` < `8u504`，因此回退会选中 **`8u504`**。
- **影响**：用户卸载当前 JDK 后，`current/jdk21` 与 `~/.dsh-tools/bin` 下的 `java` 等命令**静默**切到更旧的版本；多版本工具越多、标签风格越杂，选错的面越大。
- **证据边界**：结论来自 `ListVersions` 的 `sort.Strings` 与 `vers[len-1]` 逐行阅读；现有 `TestUninstall` 用的是 `1.23.2`/`1.24.0`（字母序与版本序恰好一致），因此**没有用例固定这条错误回退**——补一条用 `8u504`/`17.0.20.1`/`21.0.12.1` 的用例即可复现。
- **建议**：按版本号数值排序（或回退到 `versions[0]`），并补上那条用例。
- **2026-09-14 下架 17 后复核**：清单只剩 `8u504`/`21.0.12.1`，字母序最大仍是 `8u504`，卸载激活版本后的错误回退不变；三条版本的复现用例改为两条即可（见 N25）。

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

- **状态**：未修｜✅ 已复核
- **位置**：`frontend/index.html:90-94`
- **问题**：只有 spinner + 「正在启动...」+ 静态提示。现有进度条仅属工具链安装（`app.js:962-970`）。

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

- **状态**：未修｜⚠️ 静态审查
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

- **状态**：未修｜⚠️ 静态审查
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

- **状态**：已执行（本地清单与文档）｜⏳ 远端索引待重钉｜✅ 已复核
- **位置**：`internal/toolchain/tools/index.json`（`jdk21.versions`、`description`）、`internal/toolchain/remote.go:44`（`defaultIndexURL` 仍钉在 `ee9c181bf6`）、`frontend/tools/preview.mjs:202`（预览 mock）
- **说明**：多版本清单上线当天先收窄版本面——`jdk21` 只保留推荐版本 `21.0.12.1` 与 `8u504`，移除 `17.0.20.1`，`description` 同步改为「可选 8 / 21」。动机是把 N20/N21/N22 三条未修的多版本语义缺陷的暴露面从三版本压到两版本，并为随后修复「更新不切换」（N7）留出更小的改动面。**不是 17 自身有故障**：其下载地址在 2026-09-14 实测 `HTTP/2 302` 可达，清单里的 url/sha256/size 未被改动，本次只是不再提供。
- **影响**：
  - 已装 `jdk21-17.0.20.1` 的机器不受影响——`ListVersions` 扫目录而非查清单，该版本仍可切换与卸载；但「可安装版本」下拉里不再出现 17，且因 N20 未修，激活 17 时卡片仍显示「可更新」。
  - **远端索引尚未生效**：运行时索引取自 `defaultIndexURL` 钉住的提交 `ee9c181bf6`，其中仍含 17。本次只改本地清单，已发版客户端在重钉前仍能看到并安装 17。
  - `test-verify-tools.sh` 只比对工具 ID 集合、`catalog_test.go` 无 17 断言，两者都不受影响；`README.md`/`README.zh.md` 的「当前只有 JDK 8/17/21」与 `preview.mjs` 的预览下拉已同步为两版本。
- **待办**：把承载新 `index.json` 的提交推送后，用 `git rev-parse <commit>:apps/desktop-launcher/internal/toolchain/tools/index.json` 确认 blob 与工作区一致，再改 `defaultIndexURL` 重钉（步骤同 `92d150b3cb`）。
- **建议**：与 N7/N20/N21 的修复一并发布，避免两次重钉索引。

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
3. **N3 / N4 / N7**——供应链来源认证，以及两处「用户可见的静默无效」。
4. **8**（WebKit 依赖链裁剪）——剩余体积里唯一的大块，约 50 MB+。
5. **N22 / N16**（把多版本纳入两道门禁）与 **N20 / N21**（多版本的两处用户可见错误）——JDK 8/21 已在市场（17 已下架，见 N25），这四条是同一批多版本语义的收尾；**N23** 的 ID 改名与一次性迁移可与此一并做。

（原第 3 项 N2 已完成，见附录 A。）

# Agent Note: 工具链市场的 JDK 多版本清单

Status: implemented

[English](2026-09-14-toolchain-market-jdk-multiversion.md) | 中文

## 问题

清单一直是每个工具一个版本。`ToolVersion` 本来就是数组，卡片也早就有版本下拉，后端有 `SetActiveVersion` 与 `Uninstall` 支撑，但没有任何条目带过第二个版本，这条路从未承载产品数据。JDK 是第一个有真实需求的工具：项目分别要求 8、17、21，而沙箱里只能装 21。工具 ID（`jdk21`）本身也编码了「只有 21」，改名会连带 gradle 与 maven 的依赖声明、打包白名单、测试断言，以及每台机器上已装的 `~/.dsh-tools/jdk21-21.0.12.1` 目录。

## 决策

本次只改数据，ID 保持 `jdk21`。`tools/index.json` 里 `name` 改为 `JDK (Temurin)`，`description` 写明可选 8 / 17 / 21，`versions` 增加 17 与 8u504，而 `21.0.12.1` 保持在首位：首个条目就是推荐版本，「可更新」判定与「全部更新」都以它为目标，因此顺序即语义。

版本标签沿用发行标签去掉 build 号的形式：`21.0.12.1`、`17.0.20.1`，8 用 Adoptium 的发行标签 `8u504`。`21.0.12.1` 这一串逐字不变，因为它决定安装目录 `jdk21-21.0.12.1` 与 `current/jdk21` 软链的指向；改动会让已装用户变成「未安装」并留下孤儿目录。

三个版本都声明 `bin_rel: "bin"` 与 `lib_rel: "lib"`，这正是两个归档存放命令与库的位置。每个 url、sha256 与 size 都取自 Adoptium v3 API，并用真实下载的字节复核；21 那条没有重下，因为 API 当前的 21 资产报出的 sha256 与清单里的值相同。

`linglong/tools.yaml` 仍只登记推荐版本，并在注释里写明这条边界：它每个工具只有一个 `version`/`url`/`sha256`，表达不了多出来的版本。

ID 改名（`jdk21` → `jdk`）与随之而来的迁移（`jdk21-*` 转 `jdk-*` 并重建 `current` 软链）留到下次代码改动。

## 新增版本的覆盖情况

`verify-tools.sh` 只检查每个工具那一个 sha256 是否填实，也只 diff `installable` 与 `index.json` 的工具 ID 集合，因此新加的 17 或 8u504 即便写成占位哈希也能通过构建——这正是 `AUDIT.md` N16 记录的缺口。本次改以人工实测补上：两个归档都下载、sha256 与清单比对、解包确认只有一个顶层目录且含 `bin/` 与 `lib/`，并实跑 `bin/java -version`。

`TestE2E_CatalogInstall` 只装 `versions[0]`，所以新增的两个版本不在它的覆盖里。把审计改成遍历版本是下次代码改动的候选项。

## 备选方案

**本次一起把 ID 改成 `jdk`。** 现在迁移面最小——只有一个已装目录要搬——但还要改 gradle 与 maven 的依赖声明、打包白名单、测试断言，并新增迁移代码。延期让本次保持在数据层，代价是将来要迁移一到三个目录。

**不收 JDK 8。** 少一份 103 MB 的归档与一条维护负担，但项目里仍有用 8 的代码，沙箱没有别的路可走。

**给 JDK 8 用 `1.8` 当标签。** 贴合口语，但破坏了 `21.0.12.1` 的发行标签风格；Adoptium 自己的标签是 `8u504`。

**把 `dependencies` 改成按版本声明。** 依赖挂在 `Tool` 上，同一工具的不同版本因此无法各自声明依赖。当前没有这种需求，不为假设的需求扩 schema。

**把 `tools.yaml` 扩展成多版本格式并升级校验脚本。** 那能让打包侧的哈希门禁重新覆盖每个版本，但属于代码改动，延期处理。

## 后果

市场卡片显示「JDK (Temurin)」并列出 8 / 17 / 21：未装时出现版本下拉，已装后可在已装版本之间切换。21 仍是推荐版本，因此已装 21 的用户看不到更新提示。

每个已装版本占一份完整 JDK（归档分别约 103 MB、193 MB、207 MB），同一时刻只有一个生效：`~/.dsh-tools/current/jdk21` 指向它，`bin/` 由它重建。

两处已知缺口随本次一起交付：打包侧的占位哈希校验与端到端审计都只覆盖推荐版本。

两处相邻问题本次未修。`Uninstall` 会激活剩余版本里**字母序**最后的一个，在现在这套标签下会选中 `8u504` 而不是版本号最高的那个。`ToolVersion.LibRel` 只写进 `tool.yml`，没有任何读取点：`ReconcileBinLinks` 无条件探测 `root/lib` 与 `root/lib64`。

索引仍需发布。它的地址固定到提交哈希，所以这条数据改动只能随一次 launcher 发版到达用户——把 `defaultIndexURL` 指向承载新索引的提交；应用内的刷新拉取的还是同一个钉住的提交。

## 测试

人工实测：`jdk8.tar.gz` 为 103542511 字节、sha256 `9c70e102…`，`jdk17.tar.gz` 为 193252603 字节、sha256 `3808d1d1…`，两者与 Adoptium API 报出的 size 与 checksum 一致。解包后顶层目录是 `jdk8u504-b01/` 与 `jdk-17.0.20.1+1/`，各自含 `bin/` 与 `lib/`，`java`、`javac`、`jdb`、`jar` 均可执行，`bin/java -version` 分别输出 `1.8.0_504` 与 `17.0.20.1`。

`go test ./internal/toolchain` 继续通过：它对 `jdk21` 的断言（sha256 已填实、未安装、给出推荐版本）在三个版本下仍成立。

`sh apps/desktop-launcher/linglong/test-verify-tools.sh` 通过，`tools.yaml` 仍可解析，`installable` 的 ID 与 `index.json` 仍一致。

未验证：没有在玲珑容器内实跑；17 与 8 也没有走通 launcher 自己的安装路径——市场唯一的入口是 Wails 绑定，而端到端审计只覆盖 `versions[0]`。

## 关联

- [工具链市场的清单扩容与命令暴露](2026-09-12-toolchain-market-catalog-expansion.zh.md) 拥有清单数据形状，包括 `bin_names` 与本次沿用的 sha256 取证规则。
- [容器内工具链的三层防线](2026-08-19-linglong-container-toolchain.zh.md) 拥有 `installable` 白名单与三层防线。
- [工具链市场的呈现](2026-09-12-desktop-launcher-toolchain-market-presentation.zh.md) 拥有弹框的卡片布局与控件样式，包括版本下拉的外观。

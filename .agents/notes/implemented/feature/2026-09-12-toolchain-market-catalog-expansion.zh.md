# Agent Note: 工具链市场的清单扩容与命令暴露

Status: implemented

[English](2026-09-12-toolchain-market-catalog-expansion.md) | 中文

## 问题

市场清单原有 28 个工具、3 个分类，而分类名与内容对不上：`compiler` 下只有 CMake，而 CMake 是构建系统。`frontend/app.js` 的 `categoryLabel` 早已有 `code-quality` 与 `debug` 两个标签，弹框却只有三个页签，这两类工具既筛不到、也只能在「全部」里翻。

沙箱内没有任何 C/C++ 编译器：`prune-gcc-toolchain.sh` 把 gcc 工具链从随包产物里裁掉，只保留运行时库。agent 遇到需要编译 C 的项目、要构建原生 Node 扩展、或要从源码装 Python 包时无路可走。

安装器的两处行为又挡住了本来合适的归档。`ReconcileBinLinks` 按归档内文件名给软链命名，`provides` 只参与冲突检测，于是二进制名带平台后缀的归档会把后缀暴露成命令（yq 归档内是 `yq_linux_amd64`）；归档里任何带可执行位的文件都会被软链，包括恰为 0755 的安装与辅助脚本（yq 另带 `install-man-page.sh`）。

清单写的是固定地址，而镜像站会轮换版本：Apache dlcdn 上除当前版本外的 maven `bin.tar.gz` 全部返回 404，腐坏只在用户点安装时才暴露。

## 决策

清单现有 43 个工具，分布在 `language-sdk`、`build-tools`、`modern-cli`、`code-quality` 四个分类，弹框为每个分类提供一个页签。

新增 15 个工具，sha256 均为下载实测，项目若发布校验文件则与之一致：

- `build-tools`：ninja 1.13.2、zig 0.16.0、gradle 9.7.1、maven 3.9.16
- `modern-cli`：yq 4.53.6、sqlite 3.53.4、duckdb 1.5.5、pandoc 3.11、just 1.58.0、grpcurl 1.9.4
- `code-quality`：ruff 0.16.7、golangci-lint 2.13.2、actionlint 1.7.12、shellcheck 0.11.0
- `language-sdk`：php 8.5.8

`compiler` 在清单、标签表与页签里统一改名为 `build-tools`，`code-quality` 与 `debug` 补上页签。弹框的分类页签由已加载清单里实际出现的分类生成，而不是写死的列表：远程索引是独立发布的产物，客户端与它的分类集合可能不同版本，写死页签在两个方向都是错的——新客户端配旧索引会得到点不出内容的页签，旧客户端配新索引会把整个分类藏掉。已知分类保持固定顺序，未知分类按 ID 排序排在后面，没有工具的分类不生成页签，选中项所在分类消失时回落到「全部」。前端用例从 `tools/index.json` 读出分类并断言每个都有中文标签，再渲染一份旧结构的清单，把两个错配方向都固定下来。

`ToolVersion.BinNames` 把归档内文件名映射为对外命令名，值为空串则屏蔽该文件。`linkExecutables` 应用映射，并把对外命令名记进 `seen`，`cleanStaleLinks` 因此会保留改名后的软链、清掉被屏蔽的。`ReconcileBinLinks` 是唯一建立这些软链的路径，映射只有一个生效点。

zig 归入 `build-tools`，是沙箱里的 C/C++ 编译路径：`zig cc` 不需要 gcc 工具链即可编译与链接。gradle 与 maven 把 `jdk21` 声明为依赖，安装它们会先装 JDK。两者都不需要 `JAVA_HOME`：启动脚本会退回使用 PATH 上的 `java`，而 `appenv` 已经把它前置，二者都在 `JAVA_HOME` 未设置的情况下用市场装的 JDK 实跑通过。

`TestE2E_CatalogInstall` 端到端审计整份清单：默认跳过，设 `DSH_TC_E2E=1` 后对每个工具在独立目录里真实安装，校验地址可达、归档与清单 sha256 一致、解压布局与 `bin_rel`/`bin_names` 相符、每个声明过的命令都在 `bin/` 下且软链指向存活文件。`DSH_TC_E2E_IDS` 可只跑指定工具。

考察后未纳入的候选：shfmt 只发裸二进制，安装器支持的 tar.gz/zip/tar.xz 都装不了；clang 与 LLVM 见下；act 需要沙箱没有的容器运行时 socket；`dlv` 需要 ptrace，容器内是否可用未经验证，且产品里没有消费它的界面；Ruby、Lua、Perl 官方没有自包含的 Linux 构建。

## 备选方案

**给被屏蔽的文件单独加 `bin_exclude` 字段。** 读起来比「映射值为空」直白，但把同一件事拆成两套机制：归档内同一个文件名既决定对外命令名、又决定是否暴露。空值让这两件事留在同一个字段里。

**接受杂散的 `install-man-page.sh`。** 它只有 405 字节，卡片也不会展示它，但它会作为一个可调用命令落在 PATH 上，以后每个带辅助脚本的归档都会再加一个。

**保留 `compiler` 分类名，只补 zig。** 光有 zig 确实能让名字不算说谎，但 CMake、ninja、gradle、maven 都不是编译器，分类会继续暗示相反的事实。

**把 `build-tools` 与 `compiler` 拆成两个分类。** 一个分类里只有一个编译器，而在排除 clang 之后，第二个分类没有东西可放。

**收 clang 与 LLVM 而不收 zig。** 官方 `x86_64-linux` 包面向 Ubuntu 24.04（glibc 2.39），而容器跑在 `org.deepin.base/25.2.2` 上，其 glibc 是否满足未经验证，且包体约 1 GB。zig 是 53 MB，且只需要容器里已有的那套共享库。

**把 zig 归入 `language-sdk`。** zig 确实是一门语言，但这里要补的缺口是沙箱内的编译能力，而 `build-tools` 才是用户去找它的地方。

**只信上游校验文件，不下载。** 校验文件是上游的一面之词，而安装器信的是清单里的值，所以必须和字节对得上的是清单值。15 个里有 5 个（shellcheck、ninja，以及 sqlite、duckdb、php 的压缩包）根本没有校验文件，无论如何都得下载。

**让审计进入默认测试套件。** 它要下载整份清单约 2 GB 且依赖外网，CI 没有这份预算，因此设为显式开启。

## 后果

清单从 28 项增到 43 项。弹框网格、「全部」页签与状态栏都会显示更多卡片，远程索引文件随之变大。

安装 gradle 或 maven 时，若市场里没有 jdk21，会先拉一份约 190 MB 的 JDK——用户并没有直接要求它。已经通过宿主挂载装了 JDK 的用户仍会拿到市场那份，因为依赖解析只查市场仓库。

`bin_names` 从此是清单约定的一部分：新工具的归档名与命令名不一致时必须声明映射，否则平台后缀名会进 PATH。

maven 的地址指向 `archive.apache.org`，因为 `dlcdn.apache.org` 只保留当前版本。php 的地址指向 `dl.static-php.dev` 的 `common` 频道，该频道不带版本号、会轮换；二者在加入时都可达，之后的轮换只能靠审计发现。

远程索引仍需人工发布：本次只改了仓内的 `tools/index.json` 与 `linglong/tools.yaml`。在索引发布到 `linglong` 分支之前，客户端读到的还是旧清单，内置那份继续充当兜底。这层错配是看得见的：旧索引带的是 `compiler`，也没有 `code-quality` 与 `debug`，所以发布之前这几个页签干脆不出现，只有发布后的索引才会把它们带回来。

页签集合现在跟着索引走，其宽度因而随索引版本变化。按真实样式表在 560px 弹框宽度下实测：四个页签时工具栏本来就是两行（页签加搜索一行、刷新按钮一行），六个页签同样是两行，高度 64px 对 66px。弹框仍固定 86vh，且切换分类不改变页签集合，因此呈现那篇笔记里的高度不变性依然成立。

## 测试

每个新增 sha256 都经下载实测：yq、ruff、golangci-lint、actionlint、just、grpcurl、gradle、zig 与上游校验文件或索引一致；maven 的官方 sha512 与实测 sha512 一致；shellcheck、ninja 以及 sqlite、duckdb、php 的压缩包上游没有校验文件，清单里就是实测值。

`DSH_TC_E2E=1 go test ./internal/toolchain -run TestE2E_CatalogInstall` 把每个新增工具都真实装了一遍并报出各自暴露的命令；yq 的 `bin/` 恰好是声明的那条命令——由 `yq_linux_amd64` 改名而来，`install-man-page.sh` 被屏蔽。gradle 与 maven 的两次运行同时演练了 jdk21 依赖链。这条审计立刻体现了价值：grpcurl 首次运行就因清单 sha256 抄错一个字符而失败，光靠读上游校验文件不会发现。

`go test ./internal/toolchain` 用 `TestReconcileBinLinks_RenamesArchiveBinary` 固定改名与屏蔽行为。`node --test frontend/test-app.cjs` 38 例通过，其中分类用例渲染一份旧结构的清单并断言其页签，同时覆盖选中分类消失后的回落；把按数据推导的页签换回写死列表会让该用例失败。`linglong/verify-tools.sh` 报 `installable` 集合与 `index.json` 一致，`node frontend/tools/preview.mjs verify` 通过，但它不覆盖市场弹框。

未验证：没有在玲珑容器内实跑，新二进制的 glibc 兼容性只经宿主运行背书。

## 关联

- [容器内工具链的三层防线](2026-08-19-linglong-container-toolchain.zh.md) 拥有构建期清单、运行时自检与按需安装三层；本次就地更新了它那份 `installable` 枚举。
- [工具链市场卡片提示容器内运行时可用性](2026-09-10-toolchain-market-runtime-availability-hint.zh.md) 拥有卡片上那一行「容器内已可用」提示。
- [工具链市场的呈现](2026-09-12-desktop-launcher-toolchain-market-presentation.zh.md) 拥有弹框的卡片布局、对比度与空态。

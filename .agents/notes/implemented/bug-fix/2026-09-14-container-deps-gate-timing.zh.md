# Agent Note: 容器依赖校验放在依赖真正存在的时机

Status: implemented

[English](2026-09-14-container-deps-gate-timing.md) | 中文

## 问题

构建的依赖闸门（`linglong/verify-container-deps.sh`，被 `build:` 段第一行调用）要求 `buildext.apt` 声明的每个包——`build_depends` 与 `depends` 一视同仁——都已装进构建容器。带着它的首次真实构建当场失败：8 个 `depends` 包报「没有装上」（fonts-wqy-microhei、git、git-lfs、wget、jq、xxd、zip、xdg-utils），6 个基础层包报 `dpkg --verify` 不一致（libgtk-3-0、libglib2.0-0、python3、curl、unzip、ca-certificates）。任何构建都过不去。

两组都是误报，而第一组的证据仓库里早就写着了：

1. **时机**：ll-builder 只把 `build_depends` 装进构建容器。生成的 `linglong/buildext.sh` 只有三行——写 apt 沙箱配置、`apt update`、`apt -y install libwebkit2gtk-4.1-0`。`depends` 是在 `build:` 段**之后**的 preCommit 合并阶段才安装的，这一点 `verify-tools.sh` 的头部注释与 `linglong.yaml` 的 webkit 注释都已写明。更早的构建日志给出了顺序（`[Start Build]` → `Setting up wget/xdg-utils/git/git-lfs` → `[Install Files]`），而上一版已导出的产物层里带着它们的实体（`bin/git`、`bin/git-lfs`、`bin/wget`、`bin/jq`、`bin/xxd`、`bin/xdg-open`、`bin/zip`）。
2. **继承包的文档**：基座层 `org.deepin.base` 的 `.list` 保留 `/usr/share/doc`、`/usr/share/man` 条目，而实体在镜像制作时已被裁剪（614 个包、`/usr/share/doc` 实体 0 条；`libgtk-3-0:amd64.list` 列了 6 条 doc 路径，一条也不存在）。因此继承自基座的包，`dpkg --verify` 永远不可能为空。

加上这道闸门的那个提交，自己记录了它从未在真实容器里跑过。

## 决策

把校验拆成两半，各自放在依赖真正存在的时机。

`verify-container-deps.sh` 仍留在 `build:` 第一行，只覆盖 `build_depends`：dpkg 状态、已装版本等于 apt 候选、包内无字符设备实体、`/usr` 下无 `*.dpkg-new` 残留，以及干净的 `dpkg --verify`——但放行 `/usr/share/doc` 与 `/usr/share/man`，那两处是基础镜像裁剪掉的。这条例外很窄，且不削弱校验：安装失败会丢掉的载荷落在 `lib/`、`bin/`、`share/` 下，而字符设备那条分支与 `--verify` 彼此独立。

`verify-merged-deps.sh`（新增，宿主侧，由 `build-linglong.sh` 在 `export` 前调用）在合并产物树上覆盖 `depends`：扫描字符设备与 `*.dpkg-new`（N19 在产物里的特征），并要求 `buildext.apt` 声明的每个包被一条规则认领——`tool:<name>`（实体路径取自既有单一事实来源 `tools.yaml`）、`path:<rel>`（本脚本直接断言的实体）、`base:<理由>`（基础镜像提供，按设计不进 `$PREFIX`）、`none:<理由>`（声明了但当前不交付任何实体到产物）。未被认领的包会让构建失败，新依赖因此无法在没有任何断言的情况下进来。

`verify-tools.sh` 的失败在 `build-linglong.sh` 里是警告，而本校验会中止导出：它存在的意义就是把静默的依赖丢失变成响声，接线之前先拿真实合并树量过它的误报面。

## 规则背后的证据

每条 `base:` 与 `none:` 规则都来自对真实层的实体清点，不是按包名猜的。`/etc/ssl/certs/ca-certificates.crt` 在基座层存在、在产物层不存在，所以 `ca-certificates` 由基座提供。`libgtk-3.so.0` 与 `libglib-2.0.so.0` 在三版产物层里都没有，而基座提供它们。任何一层都没有 wqy 字体，`AUDIT.md` N26 把这个问题记为待查。`bin/zip` 三版产物层都有，却不在 `tools.yaml` 里，因此用 `path:` 断言——把它加进 `tools.yaml` 会同时把它加进 launcher 的运行时自检清单（`check.go` 的 `DefaultSpecs`），那是一个本次修复不需要的产品改动。

## 备选方案

**只留一道闸门，在 `build:` 阶段校验所有包。** 那正是失败的状态。没有任何单一位置能同时看见两组：`build_depends` 只在构建容器里，`depends` 只在合并产物树里。

**砍掉 `depends` 那一半，只留容器内校验。** 文件更少，但那样就没有任何东西验证「声明的运行时依赖是否真的进了包」——正是 AUDIT N18 存在的理由——而 `verify-tools.sh` 只覆盖 `tools.yaml` 里列出的工具。

**把产物树校验也做成 `verify-tools.sh` 那样的警告。** 与旧脚本一致，但这恰恰是本次要喊响的静默丢失类问题；长构建日志里的一行警告，正是原问题两轮都没被发现的原因。

**只放行文档路径，其余不动。** 那能修掉十四个失败里的六个，剩下八个 `depends` 的是结构性问题，不是措辞问题。

## 后果

构建重新能过，而且依赖这件事的两半都在各自可观测的地方被校验：容器里管 `build_depends`，产物里管 `depends`。代价是多一个脚本，以及一张需要在新增依赖时扩写的规则表——未认领的包会让构建失败，而不是无声通过。

文档路径的例外意味着「只有文档没装上」的包能通过容器校验。这是有意的：文档不是这道闸门保护的对象，而对继承包来说那项比对本就没有意义。

`AUDIT.md` 的 N18 与 N19 已改写为真实机制与修正后的落点；N26 记录了规则表浮现出来的字体问题。

## 测试

`test-verify-container-deps.sh`（10 项）固定新语义：只要求 `build_depends`、apt 候选版本落后、包未安装、字符设备、`*.dpkg-new` 残留、载荷不一致、仅文档不一致时放行、文档不一致不掩盖载荷不一致，以及真实 `linglong.yaml` 能被解析且其 `depends` 包在该阶段不被要求。

`test-verify-merged-deps.sh`（7 项）固定产物树校验：真实清单 + 齐全产物树通过（同时证明每个声明包都被认领）、`tools.yaml` 工具实体缺失、`path:` 实体缺失、`zip` 缺失、用 `mknod` 造出的字符设备、`*.dpkg-new` 残留，以及未认领的新包。

`verify-merged-deps.sh` 还对上一版成功构建的真实合并树（`~/.cache/linglong-builder/merged/50f29c89…/files`）实跑过：16/16 OK，退出码 0。未验证：两个脚本都没有在本机的真实 ll-builder 调用里跑过（本环境没有 ll-builder），因此下一次构建才是修正后落点的首次端到端运行。

## 关联

- [容器内工具链的三层防线](../feature/2026-08-19-linglong-container-toolchain.zh.md) 拥有 `installable` 白名单与容器侧的三层防线。

# Agent Note: webkit 改从 apt 候选的 .deb 交付

Status: implemented

[English](2026-09-14-webkit-delivery-from-deb.md) | 中文

## 问题

构建容器的 overlay 写入不落盘：`apt` 报告 `Unpacking`/`Setting up libwebkit2gtk-4.1-0 (2.50.4-…)`，而 `/usr/lib/x86_64-linux-gnu/` 下仍是 2026-04-07 的实体（92,804,704 字节，基础层的 WebKitGTK 2.48.5），新文件只以 `c 0,0` 字符设备的形式留下 `*.dpkg-new`（`AUDIT.md` N19）。`build:` 段过去把那个实体从容器复制进包前缀并打补丁——于是每次发布交付的都是 2.48.5，而 `depends` 里写着更新的版本，构建过程对此一言不发。

那份旧实体还连带破坏了一件事：构建器收集库依赖时要把同一个文件复制进 `output/_build`，失败为 `无效的参数`，降级成警告后继续。近期那道把 `failed to copy` 变成构建失败的门禁因此拦下了每一次构建，而它拦下的这次复制，其内容产品并不取自那里。

## 决策

`build:` 改为从 apt 候选的 `.deb` 取 webkit，而不从容器文件系统取：`apt-get download libwebkit2gtk-4.1-0`，断言 `.deb` 唯一，断言从文件名解析出的版本等于 `apt-cache policy` 的候选，`dpkg-deb -x` 解包，断言解出的 `libwebkit2gtk-4.1.so.0.*` 是唯一的普通文件，复制进 `${PREFIX}/lib/x86_64-linux-gnu/`，跑 `patch-webkit-exec-path.sh`，重建两条 soname 软链，并打印交付的版本。这些都是普通文件写入，不经过 overlay，ll-builder 的合并时序丢不掉它们；包内交付的版本跟随容器的 apt 候选，而不是本仓库里写死的一个数字。

「webkit 到底有没有进包」的判据从构建器日志搬到产物。`verify-builder-log.sh` 只放行一种行形态——源是 `libwebkit2gtk-4.1.so.0` 的 `failed to copy`——并打印豁免了几处，其余 `failed to copy` 照旧失败。随后由 `verify-merged-deps.sh` 断言包内 `lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0` 存在、是普通文件（不是字符设备）、且含 exec-path 补丁标记；标记的取值来自 `internal/packaging/webkit-exec-path.txt`，与 launcher 内嵌、补丁脚本读取的是同一份单一来源。

这条豁免是安全的，因为失败的那次复制搬的是**旧**库；正因为它失败，基础层的版本才没能覆盖我们的补丁版。将来若构建器复制成功并把它合并进来，产物里的补丁标记就消失、构建随之失败——N17 说的那种"静默"由此换成一条针对用户真正运行的东西的断言。

## 备选方案

**继续从容器 `/usr` 复制。** 这正是"清单要新版、实际交旧版"的现状。旧实体可读时复制永远成功，于是一切看起来健康，交付的却是错的版本。

**去修 overlay 本身。** 那是构建器与内核的事，在本仓库之外，且没有真实 ll-builder 就无法验证。绕开它才是剩下的选择。

**保留日志门禁，把 `failed to copy` 改回警告。** 一行改动就能解封，但同时也丢掉了 N17 记录的那一类唯一防线：包悄悄沿用了上一层的文件。

**把 `.deb` 解到容器的 `/usr` 里。** 那正是写入不落盘的那条路，解包会显得成功而实际什么都没变。

**在仓库里钉一个 webkit 版本，而不是跟随候选。** 钉版本要有自己的更新流程与 sha256，而容器的 apt 列表本来就是"能装什么"的权威；与候选比对是更便宜的"下载到的正是要的那个"检查。

## 后果

包内携带的是容器 apt 候选所命名的那个版本，构建日志会写明。产物断言覆盖了两个方向：库缺失或被降级，以及合并覆盖掉打过补丁的文件。

代价：每次构建约 25 MB 下载，以及硬依赖候选仍可获取——镜像撤掉它时构建会响亮地失败，而不是交付一个更旧的库。overlay 缺陷本身没有动；这次是绕开它，因此**其它**写入未落盘的依赖，若没有自己的产物断言，依然不可见。

## 测试

`test-verify-builder-log.sh` 覆盖五条路径：干净日志、唯一失败是 webkit 复制的日志（通过并打印豁免处数）、因另一个库而失败的日志（仍失败）、两者并存的日志（仍失败且点明未豁免处数）、日志缺失。带门禁的那次真实构建日志现在报 `共 1 处，豁免 webkit 1 处`，退出码 0。

`test-verify-merged-deps.sh` 覆盖八条路径，其中包括"webkit 在、但缺补丁标记"必须失败。

`build:` 块已从 `linglong.yaml` 抽出并通过 `bash -n`。未验证：这些步骤没有一步在本机的真实 ll-builder 上跑过，下一次构建才是解包与产物断言的首次端到端运行。

## 关联

- [容器依赖校验放在依赖真正存在的时机](2026-09-14-container-deps-gate-timing.zh.md) 按时机拆开了依赖闸门，并在日志层面放行了 webkit 那条复制失败；本条目补上了那次放行缺失的另一半——产物侧断言。
- [容器内工具链的三层防线](../feature/2026-08-19-linglong-container-toolchain.zh.md) 拥有 `installable` 白名单与容器侧的三层防线。

# Agent Note: 在玲珑沙箱内使用宿主应用

Status: implemented

[English](2026-09-20-host-escape-open-in-app.md) | 中文

## 问题

deepin 桌面客户端运行在[玲珑沙箱](2026-08-14-desktop-launcher-linux-linglong.zh.md)内。在沙箱里，「在应用中打开」菜单只有文件管理器，而同一份构建在普通宿主机上能列出用户的编辑器与 IDE：沙箱既没有把宿主的文件系统放进 XDG 搜索路径，其命名空间内的进程也无法执行只存在于宿主上的程序。[宿主工具链挂载](2026-08-19-linglong-container-toolchain.zh.md)让宿主的*命令*可达，却不涉及宿主的*应用*，因此安装打包客户端的用户失去了同一份目录在其他环境都提供的能力。

## 决策

沙箱通过继承进程层上的两个环境事实声明一条可选的宿主逃逸通道：`DSH_HOST_ROOTFS`（宿主根目录的只读挂载点）与 `DSH_HOST_LAUNCH`（在宿主机上启动进程的转发启动器）。`apps/desktop-launcher` 只在宿主根挂载是可读目录、且该启动器能在 `PATH` 上解析时才在 `ConfigureChildEnv` 中注入这对变量，且从不覆盖已存在的取值——因此把 `DSH_HOST_ROOTFS` 设为空值即可关闭该通道。[`@deepseek-ai/dsh-launch-environment`](../../../../packages/util/launch-environment/src/index.ts) 由 `hostEscapeOf()` 把这对变量解析为 `HostEscapeFact`，只读进程层，与 `launchedThroughSsh()` 完全一致：项目或用户的 `.env` 层永远无法打开宿主通道；缺失、为空、相对路径或未知声明都只表示"没有通道"而不是失败，因此不在沙箱内的主机解析行为与从前逐字节相同。

[`@deepseek-ai/dsh-host-open-in-app`](../../../../packages/host/open-in-app/README.zh.md) 只通过新增的 `host-desktop` locator 消费该事实，编辑器与 IDE 的 Linux spec 现在声明了它。只读取每个条目记录的 desktop id——绝不枚举——来源是宿主的 `/usr/local/share`、`/usr/share`、玲珑应用商店导出条目所在的 `/var/lib/linglong/entries/share`，以及与宿主共享的 home。只有当条目 `Exec` 指名的程序在宿主上存在时才提供该候选：系统路径经宿主根挂载验证，共享 home 下的路径直接验证，而裸程序名证明不了任何东西，会被跳过。

每趟解析只做一次探测来决定是否提供该通道：`systemd-run --user --collect --quiet --service-type=exec -- /bin/sh -c 'exec "$0" "$@"' /bin/true`。探测失败即整趟不提供任何宿主条目，因为一个点击就报错的条目比没有这个条目更糟。

宿主启动会把宿主的程序安装为宿主用户管理器的一次性单元：`systemd-run --user --collect --quiet --service-type=exec --expand-environment=no --unit=<唯一名> -- /bin/sh -c 'exec "$0" "$@"' <宿主程序> <workspace>`。这条 argv 由两个性质决定。转发启动器只在沙箱一侧校验外层可执行文件，真正执行的是宿主在自己路径上找到的那个文件，因此桥接必须是两个命名空间都存在的路径——`/bin/sh`——而宿主程序作为 `$0` 传入。`--service-type=exec` 让宿主侧 exec 失败立即导致启动失败，而不是等到看护窗口结束才判定；`--expand-environment=no` 让 workspace 路径里的 `%` 与 `$` 保持字面量；桥接的目标始终是 argv，绝不是 shell 字符串。宿主启动失败会按失效解析上报，让 open 路由只重解析该条目一次——这是沙箱内唯一可用的修复手段；Linux 图标路由会跟随宿主启动所依据的那条 desktop 条目，到相同的宿主数据目录取图标，并在遇到绝对 `Icon=` 路径时回退到宿主根挂载。

## 考虑过的替代方案

**从宿主的目录枚举它的应用。** 否决：读取任意宿主 desktop 条目会提供目录无法验证的启动协议，而目录本身就是一份刻意收敛的白名单。该 locator 只读每个条目记录的 id。

**复用随包的 `xdg-open` 垫片来做宿主打开。** 否决：该垫片转发到宿主自己的文件管理器与浏览器打开器，因此能打开目录却无法启动指定的宿主应用，而菜单的约定是「指定的应用 + 一个 workspace」。

**让桌面启动器自己解析并启动宿主应用。** 否决：那会重复一份目录，还需要第二条通道把结果提供给菜单。注入事实让目录只有一个归属，并让同一份解析代码在沙箱内外都能运行。

**沿用[子进程提供方](../../implemented/architecture/2026-08-28-subprocess-native-containment.zh.md)的 `systemd-run --user --scope` 形态。** 否决：scope 加入的是调用方的命名空间，目标仍留在容器内。只有 service 才能到达宿主。

**另造一个宿主侧辅助服务。** 否决：那意味着大得多的兼容面——宿主安装、套接字协议与它自己的版本管理——而宿主根挂载与用户管理器在所有目标系统上本来就已经存在。

## 后果

打包客户端的用户现在能在同一个菜单里拿到宿主的编辑器与 IDE，同时对宿主没有任何写权限：检测只读两个数据目录加上共享 home，解析会验证它将要执行的程序，什么都不安装。该通道是每台机器一次探测得出的事实，因此宿主用户管理器一旦不可达，就会损失整趟解析的宿主条目直到下次解析；宿主程序在解析与点击之间消失只多付一次重解析；没有 `Icon=` 键的条目保持通用占位图形。

这对声明是一份没有 schema 的跨语言契约：Go 写入这两个变量与启动器取值，TypeScript 读取它们，两侧都在注释里写明这一对。它的信任边界是共享 home——每个用户的 `~/.local/share/applications` 对该用户可写，而 locator 恰好只信任到这一步。

把浏览器与 `xdg-open` 的转发折叠到这条通道、以及为目录增加仅宿主可用的条目，都延后处理；当前的文件管理器条目保持既有转发行为。

## 测试

- 单元测试钉住了 locator 链（经宿主根挂载解析宿主条目、从共享 home 解析每用户条目、按序尝试 id）、各条不提供路径（没有 `Exec`、裸程序名、宿主上无此程序、探测失败）、每趟只探测一次、包含桥接与唯一单元名的完整启动 argv，以及在宿主数据目录中的图标查找。
- REAL-composition 测试经 Loader 启动 WebServer 与本插件，在进程层声明该通道，断言 apps 路由与 open 路由产出的 `systemd-run` argv。
- 在真实的玲珑客户端上（装有 VS Code、Sublime Text 与两个 JetBrains IDE 的 deepin 宿主）解析器列出了这四个宿主应用，且经桥接启动的宿主程序在宿主 `/tmp` 中创建并删除了文件。

## 相关

- [通过 Go + webview_go 实现 Linux 桌面启动器](2026-08-14-desktop-launcher-linux-linglong.zh.md)——该通道所在的沙箱。
- [玲珑容器工具链](2026-08-19-linglong-container-toolchain.zh.md)——面向命令的同族宿主挂载机制。
- [原生所有者收容逃逸的子进程后代](../../implemented/architecture/2026-08-28-subprocess-native-containment.zh.md)——受管子进程为什么用 scope 而不是本通道。
- [为宿主浏览器打开捆绑 xdg-open](../../implemented/bug-fix/2026-08-27-bundle-xdg-open-for-host-browser-opening.zh.md)——本通道尚未替代的转发垫片。
- 设计记录：[玲珑容器内的宿主逃逸 Open In](../../../../docs/superpowers/specs/2026-09-20-host-escape-open-in-app-design.zh.md)。

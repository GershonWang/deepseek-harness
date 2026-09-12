# Agent Note: Desktop entry needs StartupWMClass for dock icon association

Status: implemented

[English](2026-09-12-desktop-launcher-dock-icon-association.md) | 中文

## Problem

在终端里用 `ll-cli run com.deepseek.dsh-desktop` 启动时，launcher 窗口在 Deepin 任务栏上是通用的占位图标；从应用启动器启动同一个窗口，显示的却是随包的 DeepSeek 图标。

对运行中窗口的两项测量解释了差异。它的 `WM_CLASS` 是 `"dsh-desktop-launcher", "Dsh-desktop-launcher"`，而 `_NET_WM_ICON` 不存在——窗口自身不发布任何图标。已安装的条目叫 `com.deepseek.dsh-desktop.desktop`，且没有声明 `StartupWMClass`。

任务栏从窗口找到桌面条目有两条路径。应用启动器会附上"启动自哪个条目"的提示，任务栏直接跟着提示走。终端里的 `ll-cli run` 不带这个提示，只剩任务栏拿 `WM_CLASS` 去比对 `StartupWMClass`——未设置——或者比对条目自己的文件名 `com.deepseek.dsh-desktop`，而它与窗口类名的两半都对不上。关联失败，兜底路径也不可用，因为窗口没有设置图标。

这正是缺陷只在命令行出现的原因：启动器那条路径从来不需要这个字段。

## Decision

桌面条目声明 `StartupWMClass=dsh-desktop-launcher`，即用 `xprop` 实测到的窗口类 instance 字段。同一个源文件同时服务两条打包路径——`linglong/linglong.yaml` 装进玲珑包，`build-deb.sh` 装进 `.deb`——因此一行改动让两种分发都获得关联能力。

取值用 instance 而不是 class 字段（`Dsh-desktop-launcher`）：任务栏实现实践中匹配的是小写的 instance，同类第三方条目也是这么写的（Sublime Text 的窗口是 `"sublime_text", "Sublime_text"`）。

## Alternatives considered

**让 launcher 自己设置窗口图标，使兜底路径可用。** 它能修好所有关联失败的条目，而不只是这一个，而且 launcher 已经知道自己的图标在哪（`packaging.AboutIconPath`）。否决：Wails v2 的 Linux 前端没有窗口图标 API——`pkg/runtime/window.go` 只有标题、尺寸、位置、背景色与主题，没有图标；`window.c` 里那句 `gtk_window_set_icon` 从 Go 侧不可达。要让它可达就得给 Wails 打补丁或自己写 cgo，为一个纯观感的兜底付出太大代价。

**改二进制名去对齐条目，或改条目名去对齐二进制。** 否决：`dsh-desktop-launcher` 是 Makefile、玲珑入口脚本与 `.deb` 打包共同引用的构建产物名，而 `com.deepseek.dsh-desktop` 是沙箱、桌面条目与已发布产物共同依赖的玲珑应用 id。改名的影响远超任务栏图标。

**改由应用代码设置 `WM_CLASS`。** 否决：GTK 从程序名推导窗口类，而窗口由 Wails 创建，覆盖它意味着对这个程序并未创建的窗口发 cgo 调用——比一个声明式字段多出太多活动件。

**不修，改为记录绕过方法。** 否决：`ll-cli run` 正是开发与排查时启动 launcher 的方式，而这时开发者恰恰需要在任务栏里把窗口认出来。

## Consequences

launcher 窗口现在无论以何种方式启动都能关联到自己的桌面条目，任务栏显示 `Icon=dsh-desktop` 而不是占位图标。应用启动器那条路径保持不变——它仍旧走提示关联，只是如今多了一条与其一致的路径。

这项改动引入的约束是一种耦合：`StartupWMClass` 必须等于窗口的 `WM_CLASS` instance，而后者由 GTK 从可执行文件名推导。改了构建出的二进制名却不同步这个字段，命令行启动就会静默地退回占位图标。字段上带了一行注释写明这一点，因为没有别的地方会拦下这种不一致。

## Testing

窗口开着时诊断可复现：

```sh
xprop -name "DeepSeek Harness" WM_CLASS        # "dsh-desktop-launcher", "Dsh-desktop-launcher"
xprop -name "DeepSeek Harness" _NET_WM_ICON    # not found
```

字段可被标准解析器读出（`configparser` 读回的 `StartupWMClass` 为 `dsh-desktop-launcher`），因此条目格式没有写坏。本环境未安装 `desktop-file-validate`，所以没有用 freedesktop 的模式校验器检查过该条目。

在运行中的系统上验证修复不需要重新打包：把条目复制到 `~/.local/share/applications/`、再从终端重启客户端，走的就是任务栏实际执行的那套查找，因为任务栏读的是已安装条目而不是打包源文件。在全新安装的包上确认仍然待办——本次改动没有经过重新构建的 `.uab` 验证。

## Related

[Desktop launcher on Linux/Linglong](../feature/2026-08-14-desktop-launcher-linux-linglong.zh.md) 拥有该条目所属的打包与桌面集成。

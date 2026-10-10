# 以玲珑包分发的 DeepSeek Harness 双客户端

[English](README.md) | 中文

`apps/linyaps/desktop-dual` 把本仓库的两个桌面客户端打进同一个玲珑包，并加一个常驻切换器保证任一时刻只有一个在跑。官方 Electron 客户端来自 [`apps/linyaps/desktop-linglong`](../desktop-linglong)，Go + Wails 薄壳客户端来自 [`apps/linyaps/desktop-launcher`](../desktop-launcher)；切换器是本包安装的唯一入口。

## 这个模块产出什么

一个 `.uab`，命令入口是 `/opt/apps/com.deepseek.dsh-desktop-dual/files/bin/dsh-desktop-switcher`——一个没有窗口的托盘进程。它拉起被选为默认的客户端、在系统托盘上提供切换，并在自己退出时终止客户端的整棵进程组，因此不会留下「客户端还在后台跑、用户却看不到控制入口」的状态。

本包不自带第三份运行时：薄壳版的 harness 树与 Node 来自 launcher 的暂存产物，官方 Electron 客户端自带 Electron 与它自己的 dsh 树。

## 切换器为什么独立成模块

切换器原本是 launcher 二进制的 `--mode=bubble` 模式，画成一个置顶悬浮球。它现在是独立的 Go 模块与独立二进制，原因有两条。

悬浮窗口有一批托盘图标没有的失效模式：它遮挡内容、与全屏应用冲突，持久化的屏幕坐标还会在换显示器之后把球留在屏幕之外——那条路径一旦发生，用户就再也够不到后台客户端的唯一入口。托盘项由桌面面板托管，不存在这类丢失。

托盘进程不需要窗口工具链。`desktop-dual` 因此以 `CGO_ENABLED=0` 构建，只依赖 `github.com/godbus/dbus/v5`；Wails 与 WebKit 依赖留在 launcher 自己那边，两个模块不共享任何 Go 包。

## 切换行为

切换按设计是互斥且破坏性的：切换器先停掉当前客户端的进程组、等它退出，再拉起另一个。被停掉的客户端里正在进行的会话会中断——这是产品选择，不是缺陷。

托盘项通过角标、提示与菜单首项报告当前形态，首项永远写着「另一个」形态的名字。菜单还能设置默认形态（已是默认时置灰）与退出切换器，退出会连带停掉客户端。

切换失败会弹一条桌面通知并把托盘项标记为需要关注，原因同时进入提示文字，因此不会出现无声失败。

## 构建

先在宿主机准备产物，再在玲珑容器内组装。

```sh
sh apps/linyaps/desktop-dual/prepare-offline.sh
ll-builder build -f apps/linyaps/desktop-dual/linglong.yaml
```

`prepare-offline.sh` 调用两个客户端模块各自的准备脚本，然后把切换器构建进 `stage/bin/`。launcher 的暂存产物需要宿主机的 GTK 与 WebKit 开发文件，与薄壳版包的要求一致。

聚合入口执行的是同一份脚本：`sh apps/linyaps/build-linglong.sh dual`。

## 已知边界

切换器以 StatusNotifierItem 注册。会话里没有 `org.kde.StatusNotifierWatcher` 时，它按默认形态启动客户端、记录原因，并每五秒核对一次，桌面外壳回来后入口自动恢复。核对还会把宿主的连接唯一名与注册时记下的那个比对，因此外壳崩溃后被新进程接管时，入口会在五秒内回来，而不是让客户端一直够不到。宿主不在时无法切换客户端。

只有切换器进程在跑时，托盘才提供切换入口。再次启动会立即退出，而不是叠出第二个客户端。

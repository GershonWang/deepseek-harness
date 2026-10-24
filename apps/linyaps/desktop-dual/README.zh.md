# 以玲珑包分发的 DeepSeek Harness 双客户端（薄壳版的升级款）

[English](README.md) | 中文

`apps/linyaps/desktop-dual` 把本仓库的两个桌面客户端打进同一个玲珑包，并加一个常驻切换器保证任一时刻只有一个在跑。这个包**沿用薄壳版的包 ID `com.deepseek.dsh-desktop`**，只把版本号抬高，因此在玲珑应用商店里它是薄壳版的升级，而不是货架上的第二个应用。官方 Electron 客户端来自 [`apps/linyaps/desktop-linglong`](../desktop-linglong)，Go + Wails 薄壳客户端来自 [`apps/linyaps/desktop-launcher`](../desktop-launcher)；切换器是本包安装的唯一入口。

## 这个模块产出什么

本模块从同一份切换器源码产出两个变体。`dual` 打进薄壳客户端与官方 Electron 客户端；`tray` 只打进薄壳客户端，因此不含 Electron 运行时，包体也相应更小，托盘菜单收敛为「显示 / 关于 / 退出」。两者都沿用薄壳版的包 ID `com.deepseek.dsh-desktop`。

切换器提供哪些托盘项，由包里实际有什么决定：它在启动时探测 `<PREFIX>/bin/dsh-desktop-launcher` 与 `<PREFIX>/electron/dsh-desktop-official`，因此变体不需要靠编译开关来选择菜单。

每个变体是一个 `.uab`，命令入口是 `/opt/apps/com.deepseek.dsh-desktop/files/bin/dsh-desktop-switcher`——一个没有窗口的托盘进程。它拉起客户端并给它一个托盘项，在自己退出时终止客户端的整棵进程组，因此不会留下「客户端还在后台跑、用户却看不到控制入口」的状态。

本包不自带第三份运行时：薄壳版的 harness 树与 Node 来自 launcher 的暂存产物，`dual` 里官方 Electron 客户端自带 Electron 与它自己的 dsh 树。两套运行时合起来约 1.8 GB，这是「一个包内可切换」的代价；`tray` 不含 Electron 那一半。切换器唯一额外需要的二进制是 `dsh-desktop-about`——一个画「关于」对话框的 GTK3 小助手，因为切换器本身不带窗口工具链。

## 为什么沿用薄壳版的包 ID

包 ID 就是应用商店里的身份：同一个 ID 配更高的版本号是一次升级，用户装到的仍是同一个应用，桌面图标、固定在启动器或任务栏上的入口、家目录里的工具链与配置全部延续；换一个 ID 会变成第二个应用，用户得重装一遍，这些状态也不继承。

因此本包连桌面入口的文件名（`com.deepseek.dsh-desktop.desktop`）、`Name` 与 `Icon` 都与薄壳版逐字一致，真正变化的只有 `Exec`——它指向切换器，由切换器再按默认形态拉起客户端。包内目录在容器里是只读层（实测挂载选项为 `ro`），运行期无法往包里写任何东西，两种客户端因此都随包提供，不存在「安装后再把另一份拉进包目录」这一步。

## 切换器为什么独立成模块

切换器原本是 launcher 二进制的 `--mode=bubble` 模式，画成一个置顶悬浮球。它现在是独立的 Go 模块与独立二进制，原因有两条。

悬浮窗口有一批托盘图标没有的失效模式：它遮挡内容、与全屏应用冲突，持久化的屏幕坐标还会在换显示器之后把球留在屏幕之外——那条路径一旦发生，用户就再也够不到后台客户端的唯一入口。托盘项由桌面面板托管，不存在这类丢失。

托盘进程不需要窗口工具链。`desktop-dual` 因此以 `CGO_ENABLED=0` 构建，只依赖 `github.com/godbus/dbus/v5`；Wails 与 WebKit 依赖留在 launcher 自己那边，两个模块不共享任何 Go 包。

## 切换行为

切换按设计是互斥且破坏性的：切换器先停掉当前客户端的进程组、等它退出，再拉起另一个。被停掉的客户端里正在进行的会话会中断——这是产品选择，不是缺陷。

托盘项通过角标、提示与菜单首项报告当前形态，首项永远写着「另一个」形态的名字。左键是「显示客户端」而不是切换：没有客户端在跑时启动默认形态，已有客户端在跑时把隐藏的窗口叫回来——薄壳版与官方版都一样——误关的客户端因此一次点击就能叫回来。右键菜单里有切换项、「设置」子菜单（两项都是单选项，当前默认那项带勾选标记，两项都可点）、「关于」与「退出」；退出会连带停掉客户端。

菜单由包里有什么决定，判断依据就是同一个启动探测。`dual` 两种客户端都在，因此有切换项与「设置」子菜单。`tray` 里薄壳客户端是唯一客户端：没有可切换的对象，也没有默认形态可选，两项因此都不出现。

「关于」由随包的 GTK3 助手画出，窗口标题就是「关于」，居中于显示器工作区。分隔线上方是包版本行与两行居中事实：当前形态、包 ID；分隔线下方是四行事实：DSH版本、DSH仓库、玲珑封装作者、玲珑封装仓库。这组事实原本在薄壳版标题栏的关于弹框里，那个弹框已移除；DSH 版本按本包复用的 `${PREFIX}/harness/package.json` 解析（取不到时显示 `unknown`，绝不显示空白）。所有值（含两个仓库地址）都由同一种 GtkLabel 渲染，且刻意不做成可选中文本：可选中标签会选中整行，弹框一打开就是一段高亮，这正是要消除的效果；链接与其它值共用同一种控件，事实网格各行才严格对齐、行高一致。唯一需要原样抄给别人的包 ID 因此在值右侧带一个一键复制按钮（复制成功后按钮短暂显示「已复制」）。弹框没有「关闭」按钮，也没有默认动作：标题栏的 ✕ 与 Esc 就是出口，打开时不给任何控件焦点。两个仓库地址渲染成可点击链接，点击按「宿主 portal → 随包 `xdg-open` → GTK 默认处理器」的顺序尝试：玲珑容器里没有任何本地 URI 处理器（随包 `xdg-open` 缺 `dde-open` 与全部候选浏览器，实测以「no method available」退出 3；GIO 默认处理器返回 `Operation not supported`），而宿主 portal 就在会话总线上、实测能打开本机默认浏览器，因此排在最前。三者中有任意一个打开成功，弹框随即自动关闭——用户要的就是把链接交给浏览器；三步都失败时保留弹框并把原因直接写在里面，不再静默。

失败会弹一条桌面通知并写明是哪一步没成，同时把托盘项标记为需要关注；原因也进入提示文字，因此不会出现无声失败。

## 构建

先在宿主机准备产物，再在玲珑容器内组装。

```sh
sh apps/linyaps/desktop-dual/prepare-offline.sh
ll-builder build -f apps/linyaps/desktop-dual/linglong.yaml
```

`tray` 变体跳过官方 Electron 客户端：`--without-official` 让它不进入 `stage/`，`linglong-tray.yaml` 只组装薄壳客户端与切换器。

```sh
sh apps/linyaps/desktop-dual/prepare-offline.sh --without-official
ll-builder build -f apps/linyaps/desktop-dual/linglong-tray.yaml
```

`prepare-offline.sh` 调用各客户端的准备脚本，然后把切换器与「关于」对话框助手构建进 `stage/bin/`。launcher 的暂存产物需要宿主机的 GTK 与 WebKit 开发文件，与薄壳版包的要求一致；助手用的就是同一套 GTK 开发文件，因此不新增宿主机前置条件。

聚合入口执行的是同一份脚本：`sh apps/linyaps/build-linglong.sh dual` 出双客户端包，`sh apps/linyaps/build-linglong.sh tray` 出只含薄壳客户端的变体。

在 VS Code/Electron 集成终端里直接调用 `ll-builder` 会因 `NoNewPrivs` 无法映射用户命名空间；一键脚本会自动改走宿主透传包装，原因与完整命令见 launcher README 的[打包要点](../desktop-launcher/README.zh.md)。

## 已知边界

切换器以 StatusNotifierItem 注册。会话里没有 `org.kde.StatusNotifierWatcher` 时，它按默认形态启动客户端、记录原因，并每五秒核对一次，桌面外壳回来后入口自动恢复。核对还会把宿主的连接唯一名与注册时记下的那个比对，因此外壳崩溃后被新进程接管时，入口会在五秒内回来，而不是让客户端一直够不到。宿主不在时无法切换客户端。

只有切换器进程在跑时，托盘才提供切换入口。再次启动会立即退出，而不是叠出第二个客户端。

`dual` 与 `tray` 共用包 ID，但不在同一条版本线上：`tray`（0.1.6.1）延续的是薄壳版那条线（0.1.5.4），`dual`（0.2.0.6）仍是同 ID 下的更高版本。因此 `tray` 只构成薄壳版的升级：在已经装过 `dual` 的机器上它是更低版本，商店不会把它当作升级，也就无法覆盖 `dual` 安装。要把一台机器从 `dual` 换成 `tray`，必须先卸载 `dual`。

`dual` 与 `tray` 共用同一份宿主暂存目录 `apps/linyaps/desktop-dual/stage/`，而两个变体要求它装的内容不同。用 `--without-official` 出过 `tray` 包之后，该目录里没有 `stage/electron/`；此时若给 `dual` 加 `--no-prepare` 复用这份暂存，构建会明确报出官方客户端产物缺失并中止，而不是产出一个缺官方客户端的包。重新完整运行一次 `prepare-offline.sh`（不带 `--no-prepare`）即可恢复。

统一身份之前的双客户端包（`com.deepseek.dsh-desktop-dual`）与本包可以装在同一台机器上，但只应保留一个：两者装的是同一个切换器，单实例名刻意保持相同，让它们互相排斥——否则两个切换器会各拉起一个客户端去抢同一个 dsh 端口与 `~/.dsh` 状态。表现是只有先启动的那一个拥有托盘入口。升级测试机时建议先执行 `ll-cli uninstall com.deepseek.dsh-desktop-dual`。

装了切换器的包（`tray` 与 `dual`）里，薄壳版关窗只是隐藏，harness 继续在后台运行；左键或「显示」会取消最小化并把窗口重新显示出来。只有这两个包如此：launcher 仅在带 `DSH_DESKTOP_HIDE_ON_CLOSE=1` 启动时隐藏，而该变量只有切换器会注入，单独出包的 `web` 仍然是关窗即结束。

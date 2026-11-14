# Agent Note: 桌面壳的容器级终端（托盘入口）

Status: implemented

[English](2026-11-15-desktop-shell-terminal.md) | 中文

## 问题

桌面客户端里已经有一套终端：[右侧栏终端](../../../../packages/client/ui-sidebar-terminal/README.zh.md)，它属于 Session，经 Host RPC 启动进程、受 Session 沙箱约束、可被 Agent 接管。它工作得很好，但它的前提是 **Host 已经起来、且当前有选中的 Session**。

托盘的用途恰好覆盖相反的处境：窗口被隐藏起来跑长任务时、Host 启动失败时、插件写坏配置导致工作区打不开时——用户想进容器里看一眼，而这时侧栏终端根本没有可用的宿主。薄壳版（[`apps/linyaps/desktop-launcher`](../../../../apps/linyaps/desktop-launcher/README.zh.md)）的标题栏终端解决的正是这件事：它在客户端进程内直接 fork PTY，与 harness 无关。Electron 版迁到当前形态后，这个入口没有了。

## 决策

在 Electron 壳里新增一个**容器级**终端，作为托盘菜单项，只按需打开。

- **归属**：`apps/desktop/src/terminal-{contract,session,window}.ts` 与 `apps/desktop/src/client/terminal.ts`、`apps/desktop/renderer/terminal.{html,css}`。PTY 由 main 进程的 `node-pty` 直接拉起，不经过 harness、不经过 Host，因此 Host 起不来时它照常可用。
- **形态**：独立原生窗口，不是主窗口内的弹框。Linux 上关窗只隐藏主窗口，弹框此时不可见，而托盘正是窗口隐藏时唯一可点的入口；「关于」与工具链市场窗口已因同一原因独立成窗。
- **单例与复用**：重复点击只聚焦窗口；页面重新上报尺寸时复用仍在跑的会话，只有会话自己退出、或窗口/应用关闭才结束它。页面重载不能杀掉用户正在跑的命令。
- **结束**：先 SIGHUP 再在宽限期后 SIGKILL。交互式 shell 按惯例忽略 SIGTERM，SIGHUP 才是终端关闭的语义信号；宽限期给后台作业收尾。定时器 `unref()`，应用退出不被宽限期拖住。
- **平台**：仅 Linux，与「关于」一致。Windows 保留较短菜单，也没有这条逃生路径的使用先例。

### 环境

子进程环境取自壳已有的登录 shell 读取结果（`login-shell-environment.ts`），并补两处：

- `SHELL` 缺失时补上：玲珑容器里 GUI 启动不携带它，而 `~/.bashrc` 的 dircolors 等工具依赖它判断输出格式，缺失会每次开终端都打印 `dircolors: no SHELL environment variable`。bash 会从账户记录补一个同名变量但并不导出，子进程读不到。
- `TERM` **总是**被覆盖成 `xterm-256color`：这个窗口的渲染端就是 xterm.js，而继承来的值描述的是「谁启动了应用」——从脚本或服务里启动时可能是 `dumb`，那样 vim、less、tmux 会直接拒绝运行。实测本仓库的开发环境继承的正是 `dumb`。

空白值只对 `SHELL` 等价于缺失；其他变量的空串是有意义的设置（例如 `GIT_EDITOR=`），不做剔除。

### 文案与键盘

页面只渲染 main 下发的视图：标题、屏幕无障碍名、状态行全部来自壳语言字典，8 种语言补齐 5 个键。状态行在会话退出或起不来时出现，附一颗「重开」按钮，两者共用同一套控件——对用户来说区别只在文案。

键盘归属照搬薄壳版（[终端呈现](2026-09-12-desktop-launcher-terminal-presentation.zh.md)）的结论：壳自己的复制、粘贴、字号组合键不在页面拦截，其余一切**包括 Esc** 都属于 PTY。Esc 是 vim 退出插入模式、readline 转义序列的前缀。

### 打包

`node-pty` 进 `apps/desktop/package.json` 的 `dependencies`：main 进程 bundle 的外部包白名单就是这份清单，`packagedImportsPlugin` 会让漏登记的裸导入在打包时就失败。原生 `.node` 由既有的 `asarUnpack` 解包规则（`**/*.node`）带到 `app.asar.unpacked`，Electron 的 asar 集成会透明地落到解包副本上。xterm 走第二条 Vite 构建输出到 `lib/terminal/`，不与登录页共享 bundle——登录路径不该为终端付加载代价。

`dsh-app://shell` 路由因此需要一条新分支：壳文档里的 `../lib/...` 引用会解析成 `/lib/...`，那是 `renderer/` 旁的构建产物而不是壳文档。没有这条分支时所有页面资源都 404，而 HTML 本身仍然 200——页面白屏、控制台只有一条资源错误。

## 考虑过的替代方案

**复用右侧栏终端，托盘只负责唤起窗口。** 零新增代码，但侧栏终端是 Session 作用域的：没有 Host、没有选中 Session 就没有终端，恰好与托盘入口要覆盖的处境相反。

**把薄壳版的弹框原样搬进主窗口。** 主窗口隐藏时不可见，而托盘往往正是窗口隐藏时才被使用。

**让页面直接调 `node-pty`。** 沙箱渲染进程拿不到 Node 内建能力，且那会把进程创建能力交给页面。

**继承 `TERM`。** 尊重宿主的设置，但在 `dumb` 下终端不可用，而 `TERM` 在这里描述的是本窗口的渲染端，不是启动环境。

**只做单终端、不做重开按钮。** 更少的零件，但 shell 退出后窗口就废了，用户只能关掉再从托盘开一次。

## 影响

容器里从此有两套终端，边界必须写死：侧栏终端属于 Session，受沙箱约束、可被 Agent 接管；托盘终端属于容器，不经过 harness、没有沙箱与会话语义，只回答「在这台机器上跑命令」。这也意味着托盘终端不受 Session 沙箱限制——它和薄壳版标题栏终端一样，是给用户本人的逃生入口，不是给 Agent 的执行通道。

`node-pty` 因此在应用里多了一份（约 316 KB，只随目标平台的 prebuild），与 dsh 树里那份互不影响。页面在 Linux 上不再有「没有可用终端」的情形。

## 测试

`terminal-session.spec.ts` 在真实 `node-pty` 上跑通 shell（宿主没有原生模块时自跳过）：命令回显、退出码、resize、幂等关闭；环境与 shell 解析的四种组合单独覆盖。`terminal-window.spec.ts` 用注入的假 PTY 覆盖窗口生命周期、会话复用、退出与失败视图、输出只在窗口存活时推送、发送者校验、输入上限与关窗 SIGHUP。`tray.spec.ts` 固定菜单位置与平台差异，`main-startup.spec.ts` 固定 `/lib/` 路由。

真机验证：`apps/desktop` 构建产物在真实 Electron 里加载 `dsh-app://shell/terminal.html`，托盘窗口拉起真实登录 shell，`echo smoke-$((6*7))` 回显 `smoke-42`，提示符配色、括号粘贴模式（`\u001b[?2004h`）与 SIGWINCH 均正常。

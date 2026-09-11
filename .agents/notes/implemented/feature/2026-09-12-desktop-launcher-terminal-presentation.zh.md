# Agent Note: 桌面启动器终端弹框的呈现与键盘面

Status: implemented

[English](2026-09-12-desktop-launcher-terminal-presentation.md) | 中文

## 问题

[xterm 重写](2026-09-06-desktop-launcher-xterm-terminal.zh.md)把启动器自研的输出面板换成了 xterm.js，会话机制是对的，但呈现与键盘面此后没有人再看。由此留下七个用户可见的缺口。

只有 xterm 画布被钉在深色配色上。弹框头部与标签条仍取壳的语义变量，于是浅色系统主题下 `--bg`/`--bg-toolbar` 解析为 `#f5f5f5`/`#e6e6e6`，浅色标签条直接压在 `#1a1a1a` 的画布上：这张卡片看起来是互不相干的两截。

状态点只在激活标签上着色（`.terminal-tab.active .terminal-tab-status`），因此后台运行中的会话与已退出的会话长得一模一样，非零退出码也只作为灰字存在于回滚缓冲里。

卡片固定为 `min(900px, 90vw) × min(600px, 80vh)`，没有任何放大的入口。建会话要等随包字体最多 2s 才 `term.open`，这段时间内容区是一块没有说明的黑块。没有任何按键能关掉弹框。标签只能用鼠标操作，字号固定 14px，视口滚动条保持 WebKitGTK 默认样式——黑画布上的一道亮边。空态是一行灰字，只说了"没有"，没说怎么有。

## 决策

**弹框自带调色板。** `.modal-terminal` 在自己的子树里重定义壳的语义变量（`--bg`、`--bg-panel`、`--bg-toolbar`、`--bg-hover`、`--border`、`--border-strong`、`--fg`、`--fg-strong`、`--fg-dim`、`--ok`、`--danger`、`--danger-text`），`--term-bg` 也从 `.terminal-content` 上那条重复声明搬到这里。激活标签与画布同用 `#1a1a1a`，标签与终端之间不再有接缝。`app.js` 的 `TERMINAL_THEME` 与这段声明互为同一套色的两个副本，各自在注释里点明对方，因为 xterm 的 `theme` 选项只接受字面色值、读不到 CSS 变量。

**标签状态是推导出来的，不是继承来的。** `renderTerminalTabs` 由 `status` 与 `exitCode` 算出 `running` / `exited` / `failed`——`closed` 但 `exitCode` 不是数字的会话按普通退出处理，不猜失败——为每个标签着色（运行中绿、非零退出红、其余中性），并把退出码写进标签的 `title`。

**卡片可最大化与还原。** 头部按钮切换状态，头部双击同样切换；`is-maximized` 设为 `calc(100vw - 24px)` × `calc(100vh - 24px)` 并放开 `max-width`/`max-height`，两套图标由 CSS 切换，按钮不会跳位。状态切换在 `#terminal-card` 上——也就是带 `modal-terminal` 类、被 `.modal-terminal.is-maximized` 选中的那个元素；遮罩 `#terminal-modal` 是另一个元素，只负责居中与压暗，把类打上去屏幕上不会有任何变化。状态挂在卡片上，关闭再打开仍是用户选的尺寸。尺寸不带 CSS 过渡。

**字号是一份共享偏好。** `terminalState.fontSize` 从 `localStorage`（`dsh-desktop.terminal.fontSize`）恢复，夹在 10–20px，作用于每个活动会话以及之后新建的会话，改完必须重新 fit，因为字符单元格随之变化。标签条上是 `A−` / 读数 / `A+`，到达边界置灰；`Ctrl+=`、`Ctrl+-`、`Ctrl+0` 经 xterm 的按键处理做同样的事。

**启动过程有说明。** `createTerminalSession` 在等字体之前先往内容区写「正在启动终端…」；`restoreTerminalContent` 是重新挂回激活会话（或带指路的空态）的唯一出处——关标签、新建标签失败、快捷键路径都走它。

## 键盘归属

弹框与 PTY 都想要按键，归属是明确划分的。

`attachCustomKeyEventHandler` 决定什么能到 PTY：`Ctrl+Shift+C`/`Ctrl+Shift+V`/`Shift+Insert`（复制粘贴）、字号缩放组合、`Ctrl+Shift+T`/`Ctrl+Shift+W`/`Ctrl+Tab` 一律返回 `false`，xterm 因此不会把它们编成控制字符。其余一切，**包括 Esc**，都属于 PTY。

弹框级处理器 `onTerminalKeydown` 在 `initTerminal` 里只绑定一次，`#terminal-modal` 隐藏时立即返回，因此不会从壳的其余部分夺走任何按键。它管三组：

- **Esc** 关闭弹框并把焦点还给 `#btn-terminal`，但只在焦点位于 `#terminal-content` 之外时才生效。xterm 在打开弹窗与每次切换标签时都会聚焦自己的隐藏输入框，所以弹框通常正是焦点在内容区里的状态，而那里的 Esc 属于 PTY：它退出 vim 的插入模式，也是 readline 转义序列的前缀。抢走它会破坏这次重写本就要支持的交互程序。
- **Ctrl+Shift+T** 与 **Ctrl+Shift+W** 经 `newTerminalSession` / `closeTerminalSession` 新建与关闭会话，与标签条按钮是同一处入口。
- **Ctrl+Tab** 与 **Ctrl+Shift+Tab** 经 `cycleTerminalSession` 循环切换，两端回绕。

这三组放在文档级而不是 xterm 的按键处理里，因为它们是与焦点落在弹框哪一处无关的弹框操作，而 xterm 只在它是某个会话的输入通道时才看得到按键。xterm 侧对同名组合返回 `false`，一次按键因此不会既切标签又到达 PTY；`preventDefault` 拦下 webview 自己的默认行为。

## 备选方案

**让终端跟随壳主题，做一套浅色 16 色。** 弹框在两种主题下会是同一个颜色。否决：这会让需要与 `TERMINAL_THEME` 保持同步的调色板翻倍，而终端惯例是自带配色，不随宿主反转。

**最大化到真全屏（`inset: 0`）。** 能给出最大的终端。否决：它会连无边框窗口的自定义标题栏一起盖住，用户在失去标题栏的同时也失去最小化与关闭；留 12px 内缩既保住窗口控制，看起来也只是放大的卡片而不是另一种模式。

**给最大化加过渡动画。** 比现在的瞬切顺滑。否决：xterm 的行列由容器尺寸算出，每一帧动画都要各自 fit 一次；收益只是观感，代价是额外的 resize 竞态。

**可拖拽、可自由缩放的浮动终端窗口。** 最接近真终端，也是用户最初描述的形状。因范围否决：它需要拖拽/缩放状态、边界约束与持久化，还要与模态遮罩的层叠共存，而最大化按钮已经把大部分空间拿回来了。

**只做缩放快捷键，或只做可见控件。** 两种都更少零件。否决：可见的 `A−`/`A+` 才是可发现性的来源，与最大化按钮同理；键盘路径才让人不必把手离开终端；夹在中间的读数是当前字号唯一的可见出处。

**退出提示只留给标签，从回滚缓冲里删掉。** 输出更干净。否决：那行字标出的是用户正在读的输出里会话结束的位置，也会随日志一起被复制走；标签现在为"正在看别的标签"这一情形复述同一事实。

**无条件用 Esc 关窗。** 对模态来说是最简单的规则。否决：它把按键从 PTY 手里夺走，而 vim 的退出插入模式与 readline 的转义前缀都在那里。

**只在备用屏幕缓冲区未激活时才用 Esc 关窗。** 能让 Esc 在普通 shell 里关窗，同时不碰全屏程序。否决：readline 的 vi 模式以及任何把 `ESC` 当前缀的程序同样不在备用缓冲区上，缓冲区并不是判断"用户想让这个键作为输入"的可靠依据。

**标签快捷键只放进 xterm 的按键处理。** 一个处理器，不需要文档监听。否决：xterm 只在自己持有焦点时看得到按键，焦点一旦移到标签条或头部快捷键就失效——而那正是用户伸手去按它的位置。

**字号按会话各存一份，而不是共享偏好。** 每个标签可以有自己的缩放。否决：缩放是使用者的观察条件，不是会话的属性；共享值还意味着新标签就以用户刚选的字号打开。

## 影响

浅色系统主题下终端现在是启动器里唯一的深色面。这是消除割裂的自觉取舍；另一条路是维护第二套调色板。

Esc 关窗的触发机会比一般模态少，因为终端在打开弹窗与每次切换标签时都拿到焦点。焦点停在标签条或头部时该键仍然有效，关闭按钮仍是主要出口；这条守卫换来的是 `ESC` 在 vim、`less`、readline 里继续可用。

最大化状态跨关闭/重开保留，字号跨重启保留，因此两者都是用户设置一次的状态。尺寸是瞬切而非动画，最大化后的卡片仍留在窗口内 12px 处，并不覆盖窗口。

退出提示留在回滚缓冲里，标签以 tooltip 复述它。`restoreTerminalContent` 成为空态的唯一写出点，也是"新建标签失败不会把运行中会话的节点从内容区抹掉"的原因——在此之前失败路径只往控制台打日志，把占位提示留在了屏幕上。

`test-app.cjs` 的测试桩为了覆盖终端路径而长大，不再只是摆设：`El` 增了 `querySelector`/`querySelectorAll`（只走子树，因为 `innerHTML` 在桩里仍是字符串）、`replaceChildren`（同时清掉那个字符串）、`closest`，以及会更新 `document.activeElement` 的 `focus`；文档桩记录监听器并可用带 `preventDefault` 的事件触发 `keydown`；vm 沙箱拿到 `Terminal`、`FitAddon`、`requestAnimationFrame` 以及假 `window` 上的 `localStorage` 桩；Wails 桩拿到四个 `Terminal*` RPC；终端子树按 `index.html` 的层级嵌套搭建（`#terminal-modal` > `#terminal-card` > 头部与正文），不再平铺。嵌套正是让"类打错元素"看得见的原因：当每个元素都直接挂在 `body` 下时，把 `is-maximized` 打在遮罩而不是卡片上，每条断言都会通过。点击标签仍在桩的覆盖范围之外——标签经 `innerHTML` 渲染，断言读的就是那个字符串。

## 测试

`node --test frontend/test-app.cjs` 跑 37 例，其中 8 例覆盖本次改动：会话建立时 xterm 就绪与运行态标签类；输出写回对应会话与退出码语义类；最大化经按钮与头部双击两条路径，含重新 fit、无会话情形，以及"`is-maximized` 必须落在带 `modal-terminal` 的元素上"这条目标断言；字号经按钮与按键处理缩放，含夹紧、新会话继承、偏好恢复、脏数据与 `setItem` 抛异常；启动占位提示及其被会话节点替换；Esc 在焦点位于终端内时放行给 PTY、不在时关窗并归还焦点；标签快捷键含"弹窗隐藏时不接管"、节点搬运、组合键不进 PTY；以及关闭最后一个标签后的空态。

渲染未经验证。本环境没有 `make` 也没有 `webkit2gtk-4.1`，前端没有重新打进内嵌二进制，也没有对着真实窗口核对过：调色板、间距、最大化后的几何、滚动条（WebKit 私有伪元素）、以及 WebKitGTK 是否会把 `Ctrl+Tab` 交给页面，都还需要在开发工作区跑一次 `make build`。这套用例钉住的是行为与 DOM 状态，不是布局——最大化按钮就是这么带着绿灯出厂的，最后由用户而不是用例发现。

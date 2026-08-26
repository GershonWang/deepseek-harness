# 工具链弹框重设计（Toolchain Dialog Redesign）

日期：2026-08-26
状态：已确认（布局方向 B、纯静态改造方案 A、双主题）

## 背景

桌面启动器（apps/desktop-launcher）的「工具链」弹框目前是两张朴素表格 + 两行提示文字堆在 520px 弹框里：
- 「工具链自检」：随包工具（git/python3/node/curl/jq/pnpm）的 工具/版本/状态 三列表；
- 「环境命令 / 工具链」：按需安装清单（jdk21/go/ripgrep）的四列表，外加宿主路径挂载输入区。

用户以玲珑打包版实测后希望优化弹框外观与信息组织。优先级：**C（品牌一致性）> B（信息组织）> D（交互细节）> A（视觉质感）**。

## 目标

1. **品牌一致性**：把当前 VS Code 蓝（#007acc）对齐到 DeepSeek 品牌蓝 #4D6BFE（全仓 DSH 徽章使用的品牌色），深/浅主题同步。
2. **信息组织**：改为「摘要条 + 三张分区卡」的纵向分组流布局：
   - 随包工具（状态列表）
   - 一键安装（可装项 + 安装按钮）
   - 宿主挂载（仅沙箱环境显示）
3. **交互细节**：状态徽章（✓已安装 / 可安装 / 安装中… / ✗缺失 / 重启后生效）、按钮禁用态、焦点环、悬停反馈。
4. **双主题**：沿用现有 prefers-color-scheme 自动切换，深色与浅色都打磨。

## 非目标（Out of scope）

- 展示层零 Go 改动（现有 ToolStatus 字段已覆盖全部展示数据）；唯一的 Go 改动是 `Catalog()` 新增 uv 一键安装项（见下文）。
- 不引入构建链（Vite/Tailwind）或组件库（FAST/Shoelace）——壳 UI 保持静态 HTML/CSS/JS + go:embed 零构建架构。
- 不做图标化（每工具图标不在范围内）。
- 不新增前端测试基建（前端无测试环境，靠 Go 测试 + 手动验证）。

## 文件范围

仅改 apps/desktop-launcher/frontend/ 下三个静态文件：

| 文件 | 改动 |
|---|---|
| index.html | 重写 #tools-modal 弹框结构（摘要条 + 三张卡 + 底部操作） |
| styles.css | 品牌 token（--brand）替换 --accent、新增卡片/徽章/摘要条/焦点环样式、深/浅双主题 |
| app.js | 重写 renderTools()：按新 DOM 结构生成摘要计数、状态徽章、安装按钮、挂载列表；随包工具详情副文本静态映射表 |
| internal/toolchain/catalog.go | Catalog() 新增 uv 一键安装项（Label/Version/URL/SHA256/BinRel） |
| linglong/tools.yaml | installable 段新增 uv（与 catalog.go 同步，verify-tools.sh 校验 sha256 非占位） |

## 弹框结构（HTML 骨架）

```text
工具链（头部：品牌蓝小方标 + 标题 + ✕）
├─ 摘要条（chips）：随包 N/N ✓ · 可安装 N/N ✓ · 挂载 N 项（按需隐藏）
├─ 卡1 随包工具：逐行 状态点 + 名称 + 版本 + 状态徽章
├─ 卡2 一键安装：逐行 名称 + 版本 + 状态徽章 + 安装按钮（按需）
├─ 卡3 宿主挂载（仅沙箱环境显示）：输入行 + 挂载列表 + 移除按钮
└─ 底部：重新检查（次要按钮，右对齐）
```

每个分区卡有独立小标题与计数说明位。弹框宽度由 520px 加宽到 ~560px 以容纳三卡纵向布局（不超过 92vw）。

## 状态映射（对数据诚实，不发明后端不存在的状态）

### 随包工具（ToolStatus.Rows：ToolCheck{Name, OK, Version, Err}）

| 条件 | 状态点 | 徽章 | 版本列 |
|---|---|---|---|
| OK == true | 绿 | ✓ 已安装（ok） | Version |
| OK == false | 红 | ✗ 缺失（danger） | — |

### 一键安装（ToolStatus.Catalog：CatalogStatus{Name, Label, Version, InstalledVersion, State, Pinned}，配合 ToolStatus.Installing）

| 条件 | 徽章 | 按钮 |
|---|---|---|
| State == installed | ✓ 已安装（ok） | 无 |
| 未安装 且 Pinned | 可安装（brand） | 「安装」（可点） |
| 未安装 且 !Pinned | 待配置（warn，对应现有「待配置 sha256」） | 无 |
| Installing == Name | 安装中…（warn） | 「安装中…」禁用 |

版本列显示：已安装时用 InstalledVersion，否则用清单 Version。

### 宿主挂载（ToolStatus.HostTools：HostToolEntry{Name, Source, Target, Mounted}）

| 条件 | 徽章 | 操作 |
|---|---|---|
| Mounted == true | ✓ 生效中（ok） | 移除（danger 次按钮） |
| Mounted == false | 重启后生效（warn） | 移除 |

- 非沙箱环境（Sandboxed == false）：整个挂载卡隐藏，保留现有提示「开发态：宿主命令本就在 PATH，宿主挂载仅玲珑打包环境可用。」；若同时有安装结果 Notice，拼接在其后显示（开发态安装按钮同样可用，安装结果不能被静态提示覆盖）。
- 挂载失败/成功提示沿用现有 host-hint 交互（AddHostTool 返回后插到挂载卡底部）。

### 摘要条

- 随包：`随包 {okCount}/{total} ✓`，全部就绪才用 ok 样式，否则 warn。
- 可安装：`可安装 {installed}/{total} ✓`，全部就绪才用 ok 样式，否则 brand/ins 样式。
- 挂载：`挂载 {n} 项`，仅 Sandboxed 且 HostTools 非空时显示。

### 提示与错误

- ToolStatus.Notice（安装结果等一次性提示）显示在卡2底部，中性 hint 样式。
- 现有 #tools-refresh（重新检查）与 #host-add（挂载）事件绑定保持不变，仅 DOM 结构换新。

## 随包工具详情（行内副文本，方案 A）

「随包工具」卡中，有附带子命令/能力的工具在名称行下方渲染一行小字副文本；无附带的工具不渲染。数据为前端静态映射表（与玲珑打包内容绑定），探测状态仍以 ToolStatus.Rows 为准：

| 工具 | 详情副文本 |
|---|---|
| node | npm · npx · corepack · pnpm |
| python3 | pip · pip3 |
| git | git-lfs |
| curl / jq / xxd / wget / zip / unzip / tar | 不渲染副文本 |

> 备注：uv 未随包（见下节），不会出现在随包详情中；它以「一键安装」可装项呈现。

## uv 按需安装（方式 1，新增）

实测数据（uv 0.12.6，2026-08）：官方 gnu tarball 19.3 MB，解包后 uv 二进制 48.7 MB + uvx 0.3 MB。若随包进 uab 预计增加 20–25 MB 且版本随包锁定（uv 月更频繁）；故选择并入「一键安装」卡，uab 体积 0 增加，与 jdk21/go/ripgrep 同一机制。

Catalog() 新增项：

| 字段 | 值 |
|---|---|
| Name | uv |
| Label | uv |
| Version | 0.12.6 |
| URL | https://github.com/astral-sh/uv/releases/download/0.12.6/uv-x86_64-unknown-linux-gnu.tar.gz |
| SHA256 | 8681d8921e7d520fb368991dcf5f9c1905b80f5bf2a265a0ed085c8d8e342477 |
| BinRel | .（tarball 单顶层目录由 Install 剥离，uv/uvx 位于解包根，LinkBin 会将两者软链进 .dsh-tools/bin） |

linglong/tools.yaml 的 installable 段同步新增同名条目（version/url/sha256），保持与运行时 catalog 一致；verify-tools.sh 的 sha256 非占位校验覆盖它。

## 样式（CSS）

### 品牌 token

```css
:root {
  --brand: #4d6bfe;          /* DeepSeek 品牌蓝（深色主题） */
  --brand-strong: #6e8bff;   /* hover/按压 */
  --brand-text: var(--brand-strong); /* 品牌文字色：深色下用亮蓝满足 4.5:1 对比度 */
  --accent: var(--brand);    /* 既有使用点统一对齐品牌蓝 */
  --radius-lg: 10px;         /* 分区卡圆角 */
}
@media (prefers-color-scheme: light) {
  :root {
    --brand: #2547d0;        /* 浅色下加深保证对比度 */
    --brand-strong: #1b38b8;
    --brand-text: var(--brand);
    /* 删除浅色块遗留的 --accent: #0066b8;，两主题统一继承 --accent: var(--brand) */
  }
}
```

### 组件样式

- **分区卡**：沿用现有 .section 的边框/背景变量体系，新增 .tool-card（--radius-lg、--bg-panel、边距 10px 12px）。
- **摘要 chips**：圆角胶囊，浅色底 + 品牌/绿描边；ok 态用 --ok，warn 态用 --warn。
- **状态徽章 pill**：color-mix(in srgb, var(--ok/warn/danger) 12%, transparent) 底色 + 同色系半透明描边（现有 CSS 已使用 color-mix，见 .btn-danger:hover，WebKit 兼容有先例）。品牌文字（.chip-brand/.pill.brand 的 color）统一用 --brand-text，深色下即 --brand-strong（#6e8bff，对比度 ~4.96:1 达标），浅色下即 --brand（#2547d0）。
- **状态点**：7px 圆形，绿/红/蓝语义色。
- **按钮层级**：主要动作「安装」= brand 实心；次要动作「重新检查」= 幽灵按钮；危险「移除」= 现有 btn-danger。
- **焦点环**：输入框/按钮焦点 outline: 2px solid var(--brand)。
- **弹框宽度**：.modal-card 520px → 560px（工具链弹框专用类 .modal-tools，不影响其它弹框）。

## 验证

1. cd apps/desktop-launcher && go test ./... —— Catalog() 新增 uv 后单元/集成测试零回归。
2. make build 后开发态运行，人工核对弹框：
   - 深/浅主题（系统主题切换）；
   - 全状态：随包 ✓/✗、可安装、安装中（按钮禁用）、挂载 ✓生效中/重启后生效；
   - 挂载输入/移除、重新检查；
   - 非沙箱开发态挂载卡隐藏。
3. 玲珑产物树无需改动（linglong.yaml、buildext 均不涉及弹框）；tools.yaml 仅 installable 段新增 uv（verify-tools.sh 会校验其 sha256 非占位）。
4. uv 一键安装流：弹框「一键安装」卡里点 uv「安装」→ 下载 19.3 MB → 校验 sha256 → 解包 .dsh-tools/current/uv → 软链 uv/uvx 到 .dsh-tools/bin；安装成功后重启应用，容器内 which uv / uv --version 可用。

## 已确认决策记录

- 优先级：C（品牌）> B（信息组织）> D（交互细节）> A（视觉质感）。
- 双主题：A——沿用 prefers-color-scheme 自动切换，两套都调。
- 布局：方向 B「纵向分组流」（视觉伴侣 mockup 选定）。
- 实现路径：方案 A「纯静态改造」：前端 3 个静态文件 + Catalog() 增补 uv 一项 + tools.yaml installable 同步。
- 随包工具详情：方案 A「行内副文本」（node→npm·npx·corepack·pnpm、python3→pip·pip3、git→git-lfs）。
- uv：并入一键安装（方式 1，按需下载，uab 体积 0 增加）。

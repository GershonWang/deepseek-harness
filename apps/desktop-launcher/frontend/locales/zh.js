/* 壳前端中文文案字典：**键全集的真源**。
 *
 * 为什么单独成文件：壳前端是零构建的全局脚本（见 docs/i18n.md 2.1），没有模块
 * 系统可依赖，字典只能挂在全局上按加载顺序组装；同时仓库闸门
 * `verify-client-ui-i18n` 的 `localeOwner()` 按路径含 `/locales/` 认定字典所有者，
 * 把文案集中在本目录才能让「禁止硬编码产品文案」的规则对壳生效。
 *
 * 约定（见 docs/i18n.md 6.2）：
 * - 键为点分命名空间，如 `titlebar.server`；
 * - 值里的 `{name}` 是占位符，由 i18n.js 的 format 替换，整句进字典而不是在
 *   代码里拼词序——英文语序与中文不同；
 * - 本文件是键全集，`en.js` 必须与它同键集，缺键由闸门直接判失败；
 * - 句子中间夹内联 `<code>`（命令、地址）时按 `.before`/`.after` 拆成两个键，
 *   内联元素留在 HTML 里：壳没有富文本机制，仓库既有客户端文案也按兄弟节点拆分
 *   整句（见 docs/i18n.md 6.3）。
 *
 * 已迁入：标题栏、引导页、加载页、状态栏（P1 第①批）；其余分批迁入见
 * docs/i18n.md 第七节。文案只允许出现在本目录。
 */
"use strict";

window.DSH_LOCALES = window.DSH_LOCALES || {};
window.DSH_LOCALES.zh = {
  /* ---------- 标题栏 ---------- */
  "titlebar.server": "服务器",
  "titlebar.tools": "工具链",
  "titlebar.terminal": "终端",
  "titlebar.doctor": "诊断与修复",
  "titlebar.about": "关于",
  "titlebar.minimize": "最小化",
  "titlebar.maximize": "最大化",
  "titlebar.close": "关闭",

  /* ---------- 引导页 ---------- */
  "guidance.intro": "当前没有可用的 harness 会话：容器内服务未运行，外部服务未连接。选择一种方式开始使用（服务启动中时请稍候片刻）。",
  "guidance.container.title": "容器内",
  // 两张卡片的第一步同句，共用一键：同一句话在两处各写一份，改一处就会漏另一处。
  "guidance.step.openServer": "点击右上角「服务器」",
  "guidance.container.keepMode": "保持「容器内」模式",
  "guidance.container.start": "点击「启动」并等待就绪",
  "guidance.remote.title": "连接本机/远端服务",
  "guidance.remote.switchMode": "切换「本机/远端服务」",
  "guidance.remote.fillAddress": "填入服务地址，点击「连接」",
  "guidance.remote.hint.before": "目标 harness 需以",
  "guidance.remote.hint.after": "启动；非本机地址首次连接需确认。",
  "guidance.npx.title": "本机安装并连接（npx）",
  "guidance.npx.step1.before": "在终端运行",
  "guidance.npx.step1.after": "启动本机 harness 服务",
  "guidance.npx.step2.before": "就绪后服务地址与端口会显示在终端（如",
  "guidance.npx.step2.after": "）",
  "guidance.npx.step3": "点「服务器」→ 切「本机/远端服务」→ 填入该地址 →「连接」",
  "guidance.npx.hint.before": "本机回环地址（127.0.0.1/localhost）无需安全确认；需要局域网访问时加",
  "guidance.npx.hint.after": "重新启动。",

  /* ---------- 加载页 ---------- */
  "loading.title": "正在启动...",
  // loading 与 plugins 两个阶段同句：区别只在后者已有真实计数，页面额外显示进度条。
  "loading.hint": "DeepSeek Harness 正在加载插件和服务，请稍候",
  "loading.phase.starting": "正在启动服务进程，请稍候",
  "loading.phase.serving": "插件已就绪，正在启动服务端口",
  "loading.progress": "已加载 {loaded}/{total} 个插件",

  /* ---------- 状态栏 ---------- */
  // 状态栏是「状态词 + 主机端口 + 修饰语」的组装：主机是数据、修饰语是独立的括号
  // 片段，拼接不涉及词序，因此按片段建键，而不是给每种组合各造一句。状态词与
  // 数值/片段之间由代码加一个空格分隔，故「带主机」的两种形态单独成键。
  "status.external": "外部服务",
  "status.externalWithHost": "外部服务 {host}",
  "status.clientFailed": "界面插件加载失败",
  "status.running": "运行中",
  "status.runningWithHost": "运行中 {host}",
  "status.starting": "启动中",
  "status.failed": "启动失败",
  "status.stopped": "已停止",
  "status.suffix.safeMode": "（安全模式）",
  "status.suffix.freshHome": "（全新环境）",
  "status.exitCode": " ({code})",

  /* ---------- 预检页 ---------- */
  "preflight.title": "启动前预检…",
  "preflight.checking": "正在检查运行环境、配置与插件，稍候片刻",
  "preflight.deepRepair": "深度修复（含移除问题插件）",
  "preflight.safeMode": "安全模式启动",
  "preflight.freshHome": "全新环境启动",
  "preflight.skip": "忽略问题，仍然启动",

  /* ---------- 启动失败页 ---------- */
  "failed.title": "启动失败",
  "failed.diagnose": "诊断问题",
  // 与预检页的"安全模式启动"不同句（多了"以"），因此单独成键，不合并。
  "failed.safeMode": "以安全模式启动",
  // 冒号后是日志路径（数据），由 HTML 提供，见 index.html 的 failed-log-hint。
  "failed.logHint": "完整日志:",

  /* ---------- 关于弹框 ---------- */
  "about.title": "关于",
  // 「DSH版本」等标签的用词是既有决定（见 84b2734094：版本行改称 DSH版本 并置于
  // 首行），迁移只改文案来源、不改用词。
  "about.harnessVersion": "DSH版本",
  "about.upstreamRepo": "上游DSH仓库",
  "about.packageVersion": "玲珑包版本",
  "about.packager": "玲珑封装作者",
  "about.repo": "玲珑封装仓库",

  /* ---------- 浏览器预览分支 ---------- */
  "preview.noWails": "未检测到 Wails 运行时（浏览器预览模式）",
};

/* 壳前端英文文案字典：与 zh.js **同键集**。
 *
 * 为什么与 zh.js 分开成文件：两份字典是同一批键的两种语言，语言之间不应互相
 * 引用；加载顺序（index.html 保证）只决定挂到全局的先后，不决定回退关系——
 * 回退关系在 i18n.js 的 lookup 里，缺键一律先回退 zh，再回退键名本身。
 *
 * 现状：**下列值仍是中文占位**，英文文案待维护者提供（决策 D）。逐字复制 zh.js
 * 的目的有两个：键集先与 zh.js 对齐，缺键/多键在评审与闸门里立刻可见；界面在
 * 英文语言下与中文语言下完全一致，不会出现半截英文。英文到位后逐条替换即可，
 * 机制不变，也不要为此改动调用点。见 docs/i18n.md 6.5。
 */
"use strict";

window.DSH_LOCALES = window.DSH_LOCALES || {};
window.DSH_LOCALES.en = {
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
  "loading.hint": "DeepSeek Harness 正在加载插件和服务，请稍候",
  "loading.phase.starting": "正在启动服务进程，请稍候",
  "loading.phase.serving": "插件已就绪，正在启动服务端口",
  "loading.progress": "已加载 {loaded}/{total} 个插件",

  /* ---------- 状态栏 ---------- */
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
  "failed.safeMode": "以安全模式启动",
  "failed.logHint": "完整日志:",

  /* ---------- 关于弹框 ---------- */
  "about.title": "关于",
  "about.harnessVersion": "DSH版本",
  "about.upstreamRepo": "上游DSH仓库",
  "about.packageVersion": "玲珑包版本",
  "about.packager": "玲珑封装作者",
  "about.repo": "玲珑封装仓库",

  /* ---------- 浏览器预览分支 ---------- */
  "preview.noWails": "未检测到 Wails 运行时（浏览器预览模式）",
};

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
  /* ---------- 通用 ---------- */
  // 列表分隔符属于文案：中文用顿号，英文用逗号加空格。放进代码就等于把中文排版
  // 习惯焊死在逻辑里（见 docs/i18n.md 6.2）。
  "common.listSeparator": "、",
  "common.close": "关闭",

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
  // 全新环境的原生确认框：多行提示整句进字典，换行符留在值里。
  "preflight.freshHomeConfirm": "将以全新的运行时环境启动（~/.dsh-fallback）：\n\n· 会话历史、模型设置、第三方插件均不可见\n· API Key 等凭证不迁移，需要重新配置\n· 原始 ~/.dsh 数据原样保留，可随时回到默认环境\n\n确认继续？",


  /* ---------- 启动失败页 ---------- */
  "failed.title": "启动失败",
  "failed.diagnose": "诊断问题",
  // 与预检页的"安全模式启动"不同句（多了"以"），因此单独成键，不合并。
  "failed.safeMode": "以安全模式启动",
  // 冒号后是日志路径（数据），由 HTML 提供，见 index.html 的 failed-log-hint。
  "failed.logHint": "完整日志:",

  /* ---------- 服务器弹框 ---------- */
  "server.title": "服务器",
  "server.mode.label": "连接模式",
  "server.mode.container": "容器内",
  "server.mode.external": "本机/远端服务",
  "server.state.label": "状态",
  "server.address.label": "地址",
  "server.copyAddress": "复制服务地址",
  // 复制成功的即时反馈：只换按钮 title，图标由 app.js 换（见 flashCopyButton）。
  "server.copied": "已复制",
  "server.start": "启动",
  "server.restart": "重启",
  "server.stop": "停止",
  "server.safeMode.start": "以插件安全模式启动",
  "server.safeMode.hint": "跳过后装的第三方插件，保留你的会话和设置。升级后启动失败时可以试试。",
  "server.safeMode.active": "插件安全模式运行中",
  "server.safeMode.exit": "退出安全模式",
  // 括号里是数据目录名，与 Go 侧的默认 home 同名；改数据目录要同时改这里。
  "server.freshHome.active": "全新环境运行中（原 ~/.dsh 数据保留）",
  "server.freshHome.exit": "回到默认环境",
  "server.ext.address": "服务地址",
  "server.ext.connect": "连接",
  "server.ext.disconnect": "断开",

  /* ---------- 工具链市场弹框 ---------- */
  "tools.title": "工具链市场",
  "tools.search": "搜索工具",
  "tools.refreshIndexTitle": "刷新远程索引",
  "tools.refreshIndex": "刷新索引",
  // 计数与目标版本是数据，句子在字典里整句给出。
  "tools.updateBanner.count": "检测到 {count} 个工具可更新",
  "tools.updateBanner.withTargets": "检测到 {count} 个工具可更新：{targets}",
  "tools.updateBanner.title": "一键更新会切到这些版本：{targets}；旧版本保留在磁盘上，可在卡片下拉中切换或卸载",
  "tools.updateAll": "一键更新",
  "tools.gridLabel": "工具列表",
  "tools.builtin": "内置",
  "tools.builtinLabel": "内置工具清单",
  "tools.builtinEmpty": "暂无内置工具信息",
  "tools.builtinInstalled": "已随包内置",
  "tools.builtinMissing": "内置工具缺失",
  "tools.hostsLabel": "宿主导入",
  "tools.hostsTitle": "宿主导入（玲珑沙箱，重启应用生效）",
  "tools.hostScan": "扫描宿主",
  "tools.hostPathPlaceholder": "宿主工具链目录，如 /usr/lib/jvm/java-21",
  "tools.hostNamePlaceholder": "名称(可选)",
  "tools.hostAdd": "挂载",
  "tools.recheck": "重新检查",
  "tools.devNotice": "开发态：宿主命令本就在 PATH，宿主导入仅玲珑打包环境可用。",
  // 分类兜底标签：权威来源是索引里的 category_labels（数据，不经字典），这几个键
  // 只在索引没带标签时生效。
  "tools.category.all": "全部",
  "tools.category.languageSdk": "语言 SDK",
  "tools.category.buildTools": "构建与编译",
  "tools.category.modernCli": "现代 CLI",
  "tools.category.codeQuality": "代码质量",
  "tools.category.debug": "调试",
  "tools.empty": "没有匹配的工具",
  "tools.emptyHint": "换个关键词，或清空当前的分类筛选。",
  "tools.clearFilters": "清空筛选",
  "tools.pill.update": "可更新",
  "tools.pill.installed": "✓ 已安装",
  "tools.pill.installing": "安装中",
  "tools.pill.installable": "可安装",
  "tools.card.updateHint": "可更新到 v{version}：点顶部「一键更新」切过去；旧版本保留在磁盘上，可在下拉中切换或卸载",
  // 运行时可用提示有两个形态：带版本号的那句多一段，因此单独成键，而不是在代码里
  // 拼半句（占位符之间的空格与标点各语言不同）。
  "tools.runtime.available": "容器内已可用：{cmd}（{source}）",
  "tools.runtime.availableVersion": "容器内已可用：{cmd} {version}（{source}）",
  "tools.runtime.title": "该命令由玲珑容器环境提供，市场仓库尚未安装；通过市场安装后将由 ~/.dsh-tools 统一管理，注入 PATH 时优先使用",
  "tools.versionSelect.title": "已装版本选中即切换；未装版本点「安装」",
  "tools.version.current": " · 当前",
  "tools.version.installed": " · 已装",
  "tools.version.installable": " · 可安装",
  "tools.install": "安装",
  "tools.installWithSize": "安装 ({size})",
  "tools.installVersion": "安装 {version}",
  "tools.installing": "安装中…",
  "tools.uninstall": "卸载",
  "tools.uninstallConfirm": "确认卸载?",
  // 市场状态栏与宿主导入区的动态文案。状态栏片段之间用全角间隔号拼接，英文换成
  // 半角「 · 」，因此分隔符也进字典。
  "tools.statusSeparator": "　·　",
  "tools.status.bundled": "随包 {ok}/{total}",
  "tools.status.installed": "已装 {installed}/{total} 个工具",
  "tools.status.totalSize": "总大小 {size}",
  "tools.status.hostMounts": "宿主挂载 {count} 项",
  "tools.status.filtered": "筛选 {count} 个",
  "tools.hostsSummary": "已挂载 {count} 项",
  "tools.hostRemove": "移除",
  "tools.hostMounted": "✓ 生效中",
  "tools.hostPending": "配置已写入 · 重启应用后生效",
  "tools.hostScanEmpty": "未发现可导入的宿主工具链（可在 /opt、/usr/local、~/tools 等放工具目录后重扫）",
  "tools.hostConflict": "与已装重名",
  // 挂载的后果提示：路径是数据，由代码作占位符传入。
  "tools.hostMountWarning": "挂载后沙箱内所有进程都能读取 {path}（只读）；再点一次「确认挂载」生效",
  "tools.hostMountConfirm": "确认挂载?",
  "tools.hostMountFailed": "挂载失败: {error}",
  "tools.hostMountWritten": "已写入挂载配置，请重启应用后生效",
  // 带警告的形态单独成键：警告文本与全角空格的拼接顺序各语言不同。
  "tools.hostMountWrittenWithWarning": "⚠ {warning}　已写入挂载配置，请重启应用后生效",
  "tools.refreshing": "刷新中…",
  "tools.updating": "更新中…",
  "tools.updateFailed": "更新失败: {error}",
  "tools.scanning": "扫描中…",


  /* ---------- 诊断与修复弹框 ---------- */
  "doctor.title": "诊断与修复",
  // 值里的双引号与原界面一致，转义而非换成中文引号：改标点等于改文案。
  "doctor.summary.idle": "点击\"开始诊断\"检查环境、配置、插件和会话数据。",
  "doctor.refresh": "重新诊断",
  "doctor.repairResult": "修复结果",
  "doctor.start": "开始诊断",
  "doctor.summary.running": "正在诊断…",
  // 修复后的复检用另一句：与"又出问题了"的诊断区分开（调用方传的是这个键）。
  "doctor.summary.rechecking": "修复完成，正在复查…",
  "doctor.summary.error": "诊断失败: {error}",
  // 摘要栏由 DOM 节点拼出：计数是数据，措辞与分隔标点各语言不同，括号单独成键是
  // 为了保留"括号默认色、数字按严重级着色"。
  "doctor.summary.total": "共 {total} 项",
  "doctor.summary.leadSeparator": "：",
  "doctor.summary.separator": "，",
  "doctor.summary.ok": "✓ {ok} 通过",
  "doctor.summary.failedCount": "✗ {failed} 失败",
  "doctor.summary.fatalOpen": "（",
  "doctor.summary.fatal": "{fatal} 严重",
  "doctor.summary.fatalClose": "）",
  "doctor.summary.fixable": "{fixable} 项可自动修复",
  "doctor.check.fixableBadge": "可修复 L{level}",
  // 修复级别与 doctor 包的 RepairLevel 语义对齐（1=轻度，2=中度，3=深度）。
  "doctor.plan.mild.title": "轻度修复",
  "doctor.plan.mild.desc": "执行安全、可逆的调整，不修改用户数据。适合环境或配置层面的小问题。",
  "doctor.plan.moderate.title": "中度修复",
  "doctor.plan.moderate.desc": "修改配置或插件列表解决冲突，操作前自动备份、失败自动回滚。适合插件不兼容或配置损坏。",
  "doctor.plan.deep.title": "深度修复",
  "doctor.plan.deep.desc": "删除或重建损坏的数据与状态，无法回滚。适合数据文件损坏等严重问题。",
  "doctor.plan.empty": "本级无待修复项",
  "doctor.plan.recommended": "★ 建议优先执行（覆盖 {count} 项）",
  "doctor.plan.run": "执行{title}（L{level}）",
  "doctor.repair.running": "修复中…",
  "doctor.repair.runningBody": "正在执行修复，请稍候…",
  "doctor.repair.success": "✓ 修复成功",
  "doctor.repair.startingApp": "修复成功，正在启动应用…",
  "doctor.repair.culprit": "启动失败原因：{culprit} 异常",
  "doctor.repair.culpritFixed": "启动失败原因已自动修复",
  "doctor.repair.toastOk": "{reason}，已自动移除/修复并恢复启动。",
  "doctor.repair.toastWarn": "自动启动失败，请稍后手动点「启动」重试。",
  "doctor.repair.failed": "✗ 修复失败",
  "doctor.repair.failedDetail": "修复失败：{error}",

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

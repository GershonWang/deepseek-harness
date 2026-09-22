/* DeepSeek Harness 桌面壳前端逻辑。
 * 通过 Wails 绑定调用 Go 层（window.go.app.App.*），并监听状态事件。 */

"use strict";

const state = { status: null, prevConnectError: "", _stoppedTimer: null, _startupDoctorShown: false, _autoDisabledChecked: false };

// 诊断/修复的运行状态（跨弹窗关闭重开保持）：diagnosisRunning 期间复用同一次
// 检测结果，repairing 期间禁止再次点击修复按钮，lastReport 缓存最近一次诊断
// 结果（诊断完成后再次打开弹窗直接展示，不重复检测）。
const diagnosisState = { running: false, repairing: false, lastCulprit: "", lastReport: null };

// 由 init() 在 Wails 环境赋值为真实诊断函数；浏览器预览分支保持 null，
// applyStatus 的自动弹窗逻辑据此安全跳过（不弹窗、不跑诊断）。
let runDoctor = null;

const $ = (sel) => document.querySelector(sel);

// HTML 转义（模块级：渲染函数与 init 内都使用）。
function escapeHtml(s) {
  return String(s ?? "").replace(/[&<>"']/g, (m) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[m]));
}

// 修复后是否自动启动：复检报告无 Error 且无失败项（诊断全绿）时，说明修复
// 已消除问题，应自动用正常配置重启应用，免去用户手动点击"启动"。
function maybeAutoStartAfterRepair(report) {
  return !!report && report.Error === "" && report.Failed === 0;
}

// 两击确认的武装窗口：超时后自动复位，避免武装状态一直留着——否则用户几秒后
// 的一次普通单击会在没有确认提示的情况下真的执行。
const CONFIRM_ARM_MS = 2500;

// consumeConfirmClick 实现"两击确认"的状态机，返回 true 表示调用方应执行动作。
// 首次点击只把按钮改成确认文案并武装，窗口内再点一次才算确认。卸载工具与宿主
// 挂载共用它：两个动作都会写配置且不易撤销，而按钮只有一行空间说明后果，
// 各写一遍会让"武装 + 超时复位"的细节在调用点之间漂移。
function consumeConfirmClick(button, idleLabel, confirmLabel) {
  if (button.dataset.armed === "1") {
    disarmConfirmClick(button, idleLabel);
    return true;
  }
  button.dataset.armed = "1";
  button.textContent = confirmLabel;
  setTimeout(() => disarmConfirmClick(button, idleLabel), CONFIRM_ARM_MS);
  return false;
}

// disarmConfirmClick 复位按钮的武装状态；未武装时不动，避免超时回调把按钮
// 文案改回空闲态而覆盖调用方刚写入的新文案。
function disarmConfirmClick(button, idleLabel) {
  if (button.dataset.armed !== "1") return;
  button.dataset.armed = "0";
  button.textContent = idleLabel;
}

function api() {
  return window.go.app.App;
}



function radioValue() {
  const el = document.querySelector('input[name="mode"]:checked');
  return el ? el.value : "container";
}

function setRadio(value) {
  const el = document.querySelector('input[name="mode"][value="' + value + '"]');
  if (el) el.checked = true;
}

/* ---------- 外部链接 ---------- */

// target=_blank 在 Wails WebKitGTK 里不生效；统一交给 Wails 运行时
// BrowserOpenURL（随包 xdg-open → 宿主 portal → 本机默认浏览器）。
// 浏览器预览（无 window.runtime）时保持原生行为。
function isHttpUrl(value) {
  return typeof value === "string" && /^https?:\/\//i.test(value);
}

function openExternal(url) {
  if (!isHttpUrl(url)) return;
  if (window.runtime && window.runtime.BrowserOpenURL) window.runtime.BrowserOpenURL(url);
}

// 关于弹框里的外链：选择器 → App.About() 返回的字段名。绑定与填充都从这里
// 取，新增一个仓库地址只需在这里登记一行、在 index.html 放一个同 id 的
// <a target="_blank">，不会出现「显示了但没绑上」（点了没反应）或
// 「绑上了但没填」（点了打开占位 #）这类只差一半的状态。
const ABOUT_LINKS = [
  ["#about-repo", "Repo"],
  ["#about-upstream", "UpstreamRepo"],
];

// 每个外链共用这一份点击逻辑：target=_blank 在 Wails WebKitGTK 里不生效，
// 必须显式转交运行时；浏览器预览（无 window.runtime）时保持原生行为。
function bindExternalAnchor(selector) {
  const link = $(selector);
  link.addEventListener("click", (e) => {
    const url = link.getAttribute("href") || "";
    if (!isHttpUrl(url)) return; // 非 http(s) 保留默认行为
    if (!window.runtime || !window.runtime.BrowserOpenURL) return; // 预览模式
    e.preventDefault();
    openExternal(url);
  });
}

function bindExternalLinks() {
  for (const [selector] of ABOUT_LINKS) bindExternalAnchor(selector);

  // 打包注入的 GUI 链接桥（dsh-link-bridge.js）把 iframe 内的外链点击
  // postMessage 上来（{ dshDesktop: true, type: "open-external", url }），
  // 只用 harness 帧发来的 http(s) 请求，其余一律忽略。
  window.addEventListener("message", (e) => {
    const d = e.data || {};
    if (d.dshDesktop !== true) return;
    const frame = $("#harness");
    if (!frame || !frame.contentWindow || e.source !== frame.contentWindow) return;

    if (d.type === "open-external") {
      if (!isHttpUrl(d.url)) return;
      openExternal(d.url);
      return;
    }

    // iframe 内 harness GUI 的生效语言（桥观察它的 <html lang> 后上报）：壳的
    // 全部文案跟随这一个真源，因此壳不设自己的语言开关。GUI 起来之前壳按
    // navigator 兜底，见 i18n.js 的 init 与 docs/i18n.md 第五节。
    if (d.type === "locale") {
      if (typeof d.id !== "string") return;
      window.DSHI18N.applyFromGui(d.id);
      return;
    }

    // iframe 内的客户端插件树激活失败（桥侦测到 boot 失败并上报）：harness
    // 进程可能完全健康，页面却只剩一张死路页。交给 Go 侧记录并触发自动诊断，
    // 前端由返回的快照切到启动失败页（见 applyStatus 的 ClientFailure 分支）。
    if (d.type === "boot-failed") {
      if (typeof d.message !== "string") return;
      if (window.go && window.go.app && window.go.app.App && window.go.app.App.ReportClientBootFailure) {
        // 失败原因只用于展示，上报失败无需打扰用户：壳二进制若比前端旧，
        // 方法不存在也不会影响页面本身的行为。
        window.go.app.App.ReportClientBootFailure(d.message).then(applyStatus).catch(() => {});
      }
      return;
    }

    // 内嵌 WebKitGTK 的 paste 事件不暴露剪贴板位图，harness 前端在
    // 输入框请求“从剪贴板取图”时走壳进程（宿主侧可直接读 X selection）。
    if (d.type === "clipboard-read-image") {
      const reply = (data) => {
        if (!frame || !frame.contentWindow) return;
        // targetOrigin 用 iframe 的实际 origin，避免 "*" 通配导致消息
        // 被嵌套的第三方 iframe 劫持；接收侧（harness 前端）已通过
        // e.source 判断来源，发送侧再收敛 target 为确切 origin。
        var targetOrigin;
        try {
          targetOrigin = new URL(frame.src).origin;
        } catch (e) {
          targetOrigin = "*";
        }
        frame.contentWindow.postMessage(
          { dshDesktop: true, type: "clipboard-image-result", data: data || "" },
          targetOrigin
        );
      };
      if (window.go && window.go.app && window.go.app.App && window.go.app.App.ReadClipboardImage) {
        window.go.app.App.ReadClipboardImage().then(reply).catch(() => reply(""));
      } else {
        reply("");
      }
    }
  });
}

/* ---------- 加载页启动进度 ---------- */

/**
 * 文案入口：字典在 frontend/locales/ 下，缺键回退 zh、再回退键名本身（见 i18n.js）。
 *
 * 命名刻意避开 `t`：本文件多处把工具链状态快照命名为 `t`（如 renderTools(t)），
 * 同名函数会被那些形参遮蔽——调用点看着正常，实际翻译不到。
 * @param {string} key - 字典键。
 * @param {?Object<string, *>} params - 占位符取值。
 * @returns {string} 当前语言下的文案。
 */
const tr = (key, params) => window.DSHI18N.t(key, params);

// 阶段文案：键是 Go 侧 startupView 的阶段名（internal/app/startup_progress.go），
// 值是字典键。阶段只由可观测事实触发，前端不做任何按时间推进的猜测。
const LOADING_PHASE_HINT = {
  starting: "loading.phase.starting",
  // loading 与 plugins 同句：区别在于后者已有真实计数，页面额外显示进度条与 n/m。
  loading: "loading.hint",
  plugins: "loading.hint",
  serving: "loading.phase.serving",
};

// 加载页进度条的单调渲染状态：分母在启动期会随新行插入小幅增长，比率因此可能
// 回退一两个百分点；进度条按单调不减渲染，避免视觉上的倒退。离开启动态时归零。
const loadingProgress = { ratio: 0 };

/**
 * 渲染加载页的阶段与进度。
 *
 * 数据全部来自 harness 侧的真实上报（阶段判定在 Go 侧 startupView），前端只做
 * 文案与宽度映射：没有上报时不显示进度块，也绝不按时间补一个百分比。
 * @param {?{Phase: string, Loaded: number, Total: number}} v - 启动进度视图；
 *   缺省或零值表示当前不在启动态。
 */
function renderStartup(v) {
  const phase = v && v.Phase ? v.Phase : "";
  // 缺省键覆盖浏览器预览与在途版本（未注入上报插件）两种没有阶段的情况。
  $("#loading-hint").textContent = tr(LOADING_PHASE_HINT[phase] || "loading.hint");

  const showBar = phase === "plugins" || phase === "serving";
  $("#loading-progress").classList.toggle("hidden", !showBar);
  if (!showBar) {
    // 还没开始挂载或已离开启动态：归零，下一轮启动不会带着上一轮的进度。
    loadingProgress.ratio = 0;
    return;
  }

  const ratio = v.Total > 0 ? v.Loaded / v.Total : 0;
  loadingProgress.ratio = Math.max(loadingProgress.ratio, Math.min(1, ratio));
  $("#loading-bar").style.width = (loadingProgress.ratio * 100).toFixed(1) + "%";
  $("#loading-progress-text").textContent = tr("loading.progress", { loaded: v.Loaded, total: v.Total });
}

/* ---------- 状态渲染 ---------- */

// 取消挂起的 stopped 防抖定时器（状态恢复时调用）。
function clearStoppedTimer() {
  if (state._stoppedTimer) {
    clearTimeout(state._stoppedTimer);
    state._stoppedTimer = null;
  }
}

// 主舞台五选一展示：harness iframe / 引导页 / 启动加载页 / 预检页 / 启动失败页。
function showStageOnly(el) {
  for (const id of ["harness", "guidance", "loading-page", "preflight-page", "failed-page"]) {
    const node = document.getElementById(id);
    node.classList.toggle("hidden", node !== el);
  }
}

/**
 * 把服务地址拆成「主机端口」与「?token=…」两段，供分行展示。
 *
 * 令牌固定 43 字符，整串在弹框宽度内放不下一行，浏览器会在任意字符处折断它，
 * 于是令牌被切成两半。按 `?` 拆开后两段都能各自占满一整行。
 * 没有查询串时整串作为主机端口段返回；空地址返回两个空段。
 * @param {string} url - 形如 http://127.0.0.1:3456/?token=… 的服务地址。
 * @returns {{origin: string, token: string}} 主机端口段与查询段（无查询段时为空串）。
 */
function splitAddress(url) {
  if (!url) return { origin: "", token: "" };
  const cut = url.indexOf("?");
  if (cut < 0) return { origin: url, token: "" };
  return { origin: url.slice(0, cut), token: url.slice(cut) };
}

/**
 * 取服务地址的「主机:端口」标签，供状态栏常驻展示。
 *
 * 状态栏是长期可见的纯文本，完整服务地址携带访问 token，截图或共享屏幕即泄露；
 * 完整地址与一键复制留在「服务器」弹框内。解析失败返回空串而不回退到原串，
 * 否则脱敏会被一条畸形地址绕开。
 * @param {string} url - 形如 http://127.0.0.1:3456/?token=… 的服务地址。
 * @returns {string} 主机与端口（如 127.0.0.1:3456）；地址缺失或无法解析时为空串。
 */
function hostLabel(url) {
  if (!url) return "";
  try {
    return new URL(url).host;
  } catch {
    // URL 构造失败只可能来自畸形地址；此处有意丢弃异常，调用方按空标签渲染。
    return "";
  }
}

function applyStatus(s) {
  state.status = s;

  const dot = $("#status-dot");
  const text = $("#status-text");

  if (s.Mode === "external") {
    dot.className = "dot ok";
    const host = hostLabel(s.ExternalURL);
    text.textContent = host ? tr("status.externalWithHost", { host: host }) : tr("status.external");
  } else if (s.ClientFailure) {
    // 进程在跑但界面起不来：状态栏不能报"运行中"，否则与失败页自相矛盾。
    dot.className = "dot danger";
    text.textContent = tr("status.clientFailed") + (s.SafeMode ? tr("status.suffix.safeMode") : "");
  } else if (s.State === "running") {
    dot.className = "dot ok";
    const host = hostLabel(s.URL);
    // 锁与新环境是符号而非文案，不进字典（见 docs/i18n.md 6.2）。
    text.textContent = (host ? tr("status.runningWithHost", { host: host }) : tr("status.running"))
      + (s.SafeMode ? " 🔒" : "") + (s.FreshHome ? " 🆕" : "");
  } else if (s.State === "starting") {
    dot.className = "dot warn";
    text.textContent = tr("status.starting")
      + (s.SafeMode ? tr("status.suffix.safeMode") : "")
      + (s.FreshHome ? tr("status.suffix.freshHome") : "");
  } else if (s.State === "failed") {
    dot.className = "dot danger";
    text.textContent = tr("status.failed")
      + (s.LastExit ? tr("status.exitCode", { code: s.LastExit }) : "");
  } else {
    dot.className = "dot muted";
    text.textContent = tr("status.stopped")
      + (s.LastExit ? tr("status.exitCode", { code: s.LastExit }) : "");
  }

  // 目标：外部已连接 / 容器运行中 -> iframe；启动中 -> 加载页（预检占用时 ->
  // 预检页）；启动失败 -> 失败页（附失败原因）；手动停止（非重试间隙）-> 引导页。
  // 客户端插件加载失败（ClientFailure）必须排在 Target 之前判定：运行态下 Target
  // 恒非空，否则失败页永远轮不到，用户又被送回那张起不来的死路页。
  const frame = $("#harness");
  if (s.ClientFailure) {
    clearStoppedTimer();
    frame.removeAttribute("src");
    $("#failed-reason").textContent = "界面插件加载失败（harness 服务进程仍在运行）\n" + s.ClientFailure;
    showStageOnly($("#failed-page"));
  } else if (s.Target) {
    clearStoppedTimer();
    if (frame.getAttribute("src") !== s.Target) frame.setAttribute("src", s.Target);
    showStageOnly(frame);
  } else {
    // 不展示 iframe 时清掉 src，避免后台继续加载
    frame.removeAttribute("src");
    if (s.State === "starting") {
      clearStoppedTimer();
      // 预检占用舞台：预检进行中或等待用户决策时优先于加载页，
      // 放行（ok/autofixed/skipped/error）后回到正常加载流程。
      const p = s.Preflight;
      if (p && (p.Busy || p.Phase === "running" || p.Phase === "needs-confirm" || p.Phase === "exhausted")) {
        renderPreflight(p);
        showStageOnly($("#preflight-page"));
      } else {
        showStageOnly($("#loading-page"));
      }
    } else if (s.State === "failed") {
      clearStoppedTimer();
      $("#failed-reason").textContent = s.LastExit || "";
      showStageOnly($("#failed-page"));
    } else if (s.State === "stopped") {
      // supervisor 重试期间进程退出后会短暂变回 stopped（500ms~10s）再重新 starting，
      // 延迟 1s 再切引导页，期间状态恢复由后续 applyStatus 的 clearStoppedTimer 取消。
      if (!state._stoppedTimer) {
        state._stoppedTimer = setTimeout(() => {
          state._stoppedTimer = null;
          showStageOnly($("#guidance"));
        }, 1000);
      }
    }
  }

  updateStartupDoctor(s);
  maybeNotifyAutoDisabled(s);
  renderStartup(s.Startup);
  renderServerDialog(s);
}

/* ---------- 启动前预检 ---------- */

// 预检问题 Kind 的徽标文案。
const PREFLIGHT_KIND_LABEL = {
  auto: "已自动修复",
  confirm: "需确认修复",
  none: "无自动方案",
  warn: "提醒",
};

// renderPreflight 渲染预检页内容：按 Phase 切换标题/图标/按钮组。
// 仅在舞台被预检占用（applyStatus 判定）时调用，状态事件驱动重绘。
function renderPreflight(p) {
  const icon = $("#preflight-icon");
  const title = $("#preflight-title");
  const hint = $("#preflight-hint");
  const issues = $("#preflight-issues");
  const repairs = $("#preflight-repairs");
  const actions = $("#preflight-actions");
  const note = $("#preflight-note");

  const repairing = !!p.Busy;
  const decided = p.Phase === "needs-confirm" || p.Phase === "exhausted";
  const exhausted = p.Phase === "exhausted";

  icon.textContent = exhausted ? "🧯" : decided ? "🩺" : "🩺";
  title.textContent = repairing
    ? (exhausted ? "修复执行中…" : "预检修复执行中…")
    : exhausted ? "修复后仍存在问题" : decided ? "预检发现问题" : "启动前预检…";
  hint.textContent = repairing
    ? "正在应用修复并复查，真实插件加载探测最长可能需要一分钟"
    : exhausted
      ? "自动修复已尽力，仍无法保证启动。推荐先试安全模式（保留全部数据），必要时用全新环境（数据隔离，凭证需重新配置）"
      : decided
        ? "低风险修复已自动应用；下列问题需要你确认修复方式，或选择其他启动方式"
        : "正在检查运行环境、配置与插件，稍候片刻";

  // 问题清单
  const list = p.Issues || [];
  if (decided && list.length > 0) {
    issues.innerHTML = "";
    for (const item of list) {
      const row = document.createElement("div");
      row.className = "preflight-issue";
      const badge = document.createElement("span");
      badge.className = "preflight-kind kind-" + (item.Kind || "warn");
      badge.textContent = PREFLIGHT_KIND_LABEL[item.Kind] || item.Kind || "";
      const body = document.createElement("div");
      body.className = "preflight-issue-body";
      const name = document.createElement("div");
      name.className = "preflight-issue-name";
      name.textContent = item.Name || item.ID;
      const msg = document.createElement("div");
      msg.className = "preflight-issue-msg";
      msg.textContent = item.Message + (item.Detail ? " — " + item.Detail : "");
      body.appendChild(name);
      body.appendChild(msg);
      row.appendChild(badge);
      row.appendChild(body);
      issues.appendChild(row);
    }
    issues.classList.remove("hidden");
  } else {
    issues.classList.add("hidden");
  }

  // 已应用的修复摘要（含备份位置，用户可回滚）
  const applied = p.Repairs || [];
  if (applied.length > 0) {
    repairs.textContent = "已应用修复: " + applied.join("；");
    repairs.classList.remove("hidden");
  } else {
    repairs.classList.add("hidden");
  }

  // 按钮组：仅决策态显示；修复中禁用。
  actions.classList.toggle("hidden", !decided);
  $("#btn-preflight-deep-repair").classList.toggle("hidden", exhausted);
  for (const id of ["btn-preflight-deep-repair", "btn-preflight-safe-mode", "btn-preflight-fresh", "btn-preflight-skip"]) {
    $(("#" + id)).disabled = repairing;
  }

  // 备份目录提示
  const backups = p.BackupDirs || [];
  if (backups.length > 0) {
    note.textContent = "修复前的原文件已备份到: " + backups.join("、");
    note.classList.remove("hidden");
  } else {
    note.classList.add("hidden");
  }
}

/* ---------- 启动失败自动诊断 ---------- */

// 失败页的"正在自动诊断问题…"提示行：惰性创建一次，挂在 #failed-reason 之后。
function autoDiagHintEl() {
  let el = $("#auto-diag-hint");
  if (!el) {
    el = document.createElement("div");
    el.id = "auto-diag-hint";
    const reason = $("#failed-reason");
    reason.parentNode.insertBefore(el, reason.nextSibling);
  }
  return el;
}

function setAutoDiagHint(text, diagnosing) {
  const el = autoDiagHintEl();
  el.textContent = text;
  el.classList.toggle("diagnosing", !!diagnosing);
  el.classList.remove("hidden");
}

function hideAutoDiagHint() {
  const el = $("#auto-diag-hint");
  if (el) el.classList.add("hidden");
}

// 自动弹窗提示条：插在 #doctor-summary 上方。runDoctor/renderDoctorReport 会用
// textContent/innerHTML 整体重写 summary，提示条放兄弟节点才能跨渲染保留。
function showDoctorAutoBanner() {
  let banner = $("#doctor-auto-hint");
  if (!banner) {
    banner = document.createElement("div");
    banner.id = "doctor-auto-hint";
    banner.className = "doctor-auto-hint";
    banner.textContent = "检测到启动失败，已为你自动诊断";
    const summary = $("#doctor-summary");
    summary.parentNode.insertBefore(banner, summary);
  }
  banner.classList.remove("hidden");
}

function hideDoctorAutoBanner() {
  const el = $("#doctor-auto-hint");
  if (el) el.classList.add("hidden");
}

// 更新诊断摘要栏：只改写文本 span（保留行内的"重新诊断"按钮不被整体重写冲掉），
// 并控制按钮显隐 —— 诊断中/失败时隐藏，结果就绪时显示。
// 两个入口按内容来源分开：计数摘要由本文件拼接、只含固定字面量与转义输出，走
// setDoctorSummaryHtml；其余文案（诊断错误、占位提示、调用方传入的复查文案）来自
// 报告或调用方，走 setDoctorSummaryText 由 textContent 承担转义。合成一个入口时
// 调用方无法从签名看出自己交的是不是 HTML，历史上正是这样把报告错误文案送进了
// innerHTML。
function setDoctorSummaryHtml(html, showRefresh) {
  const text = $("#doctor-summary-text");
  if (!text) return; // 结构未就绪（预览分支）
  text.innerHTML = html;
  setDoctorRefreshVisible(showRefresh);
}

// setDoctorSummaryText 以纯文本更新摘要栏，任意输入都不会被解析为标记。
function setDoctorSummaryText(textContent, showRefresh) {
  const text = $("#doctor-summary-text");
  if (!text) return; // 结构未就绪（预览分支）
  text.textContent = textContent;
  setDoctorRefreshVisible(showRefresh);
}

// setDoctorRefreshVisible 控制"重新诊断"按钮显隐；摘要栏缺失时一并跳过，
// 使两个摘要入口在预览分支下的行为一致。
function setDoctorRefreshVisible(showRefresh) {
  const btn = $("#doctor-refresh");
  if (btn) btn.classList.toggle("hidden", !showRefresh);
}

// 修复进行中：所有修复卡片按钮禁用并显示"修复中…"（跨弹窗关闭重开保持，
// 因为按钮节点在弹框 DOM 内，hidden 不销毁它们）。
function setRepairButtonsBusy(busy) {
  document.querySelectorAll("[data-repair-level]").forEach((btn) => {
    btn.disabled = busy;
    if (busy) btn.textContent = "修复中…";
  });
}

// 右下角 toast 提醒：修复完成/失败时短暂提示原因与处理方式，几秒后自动消失。
function showRepairToast(text, kind) {
  let toast = $("#repair-toast");
  if (!toast) {
    toast = document.createElement("div");
    toast.id = "repair-toast";
    document.body.appendChild(toast);
  }
  toast.textContent = text;
  toast.className = "repair-toast " + (kind || "ok");
  toast.classList.remove("hidden");
  clearTimeout(toast._timer);
  toast._timer = setTimeout(() => toast.classList.add("hidden"), 6000);
}

/* doctor 自动禁用提示：harness 启动成功后告诉用户"哪些插件被自动禁用了、怎么恢复"。
 *
 * 为什么常驻而不是弹窗：此刻用户已经在用主界面，弹窗会打断手头的操作；但这件事必须
 * 被看到——只把插件悄悄摘掉，用户只会发现插件不见了，不知道是被禁用、更不知道还能
 * 恢复。「知道了」之后写确认标记（Go 侧按包名确认），同一批留痕不再提示。 */
let autoDisabledNotice = null;

/**
 * 展示自动禁用提示条（重复调用只更新内容）。
 * @param {Array<{Bundle: string, Reason: string}>} list - 尚未提示过的禁用留痕。
 */
function showAutoDisabledNotice(list) {
  if (!autoDisabledNotice) {
    const el = document.createElement("div");
    el.id = "auto-disabled-notice";
    const title = document.createElement("div");
    title.className = "auto-disabled-title";
    title.textContent = "已自动禁用不兼容的插件";
    const body = document.createElement("div");
    body.className = "auto-disabled-body";
    const hint = document.createElement("div");
    hint.className = "auto-disabled-hint";
    hint.textContent = "安装与依赖仍然保留：可在「插件」页重新启用，或自行卸载。";
    const ack = document.createElement("button");
    ack.id = "auto-disabled-ack";
    ack.className = "btn";
    ack.textContent = "知道了";
    ack.addEventListener("click", () => {
      // 先收起再确认：确认失败最多让下次启动再提示一次，不该把提示挂在界面上不走。
      // 确认时按展示过的包名回传，展示期间 doctor 新禁用的插件留到下次启动提示。
      el.classList.add("hidden");
      const bundles = autoDisabledNotice.list.map((item) => item.Bundle);
      api().AckAutoDisabled(bundles).catch(() => {});
    });
    el.append(title, body, hint, ack);
    document.body.appendChild(el);
    autoDisabledNotice = { el, body, list: [] };
  }
  autoDisabledNotice.list = list;
  // 原因另有行：doctor 的原因文案本身常带括号，套在包名后面会出现嵌套括号。
  autoDisabledNotice.body.textContent = list
    .map((item) => (item.Reason ? "• " + item.Bundle + "\n  " + item.Reason : "• " + item.Bundle))
    .join("\n");
  autoDisabledNotice.el.classList.remove("hidden");
}

/**
 * harness 启动成功后问一次"有没有被自动禁用的插件"，有就提示。
 *
 * 只在运行态问：进程没起来时用户关心的是启动失败，不是插件去留。每个启动周期问一次
 * （离开运行态时复位），所以同一轮修复只提示一次；新的留痕在下次启动才出现。
 * @param {object} s - 状态快照。
 */
function maybeNotifyAutoDisabled(s) {
  if (s.Mode === "external" || s.State !== "running" || s.ClientFailure) {
    state._autoDisabledChecked = false;
    return;
  }
  if (state._autoDisabledChecked) return;
  state._autoDisabledChecked = true;
  api().PendingAutoDisabled()
    .then((list) => {
      if (Array.isArray(list) && list.length > 0) showAutoDisabledNotice(list);
    })
    // 读取失败按"没有留痕"处理：提示只是知情渠道，不该在启动成功后弹错。
    .catch(() => {});
}

// 修复结果面板：把 `dsh doctor --repair` 的人类可读输出解析为结构化展示。
// 输入形如：
//   Repair level 2 complete.
//     Applied: 1
//     Skipped: 0
//     Backups: /path
//   Applied repairs:
//     ✓ plugin-dynamic-load: 已从 profile bundles 移除...
function renderRepairOutput(raw) {
  const output = $("#doctor-repair-output");
  if (!output) return;
  output.classList.remove("hidden");

  const statusEl = $("#repair-panel-status");
  const bodyEl = $("#repair-panel-body");
  const backupEl = $("#repair-panel-backup");

  const text = String(raw || "");

  // 状态：Applied > 0 → 成功；有 Skipped 且 Applied=0(失败信息) → 部分/失败。
  const appliedMatch = text.match(/Applied:\s*(\d+)/);
  const skippedMatch = text.match(/Skipped:\s*(\d+)/);
  const appliedCount = appliedMatch ? Number(appliedMatch[1]) : 0;
  const skippedCount = skippedMatch ? Number(skippedMatch[1]) : 0;
  const backupsMatch = text.match(/Backups:\s*(.+)/);
  const backupPath = backupsMatch ? backupsMatch[1].trim() : "";

  // 提取执行项："✓ id: message"（Applied repairs 之后）与跳过项 "- id: reason"。
  const appliedRows = [];
  const skippedRows = [];
  const lines = text.split("\n");
  let inApplied = false;
  let inSkipped = false;
  for (const line of lines) {
    const t = line.trim();
    if (/^Applied repairs:/.test(t)) { inApplied = true; inSkipped = false; continue; }
    if (/^Skipped:/.test(t)) { inSkipped = true; inApplied = false; continue; }
    if (/^Repair level/.test(t)) { continue; }
    if (t === "") { continue; }
    if (/^Applied:|^Skipped:|^Backups:/.test(t)) { continue; }
    if (inApplied && /^✓/.test(t)) {
      const msg = t.replace(/^✓\s*/, "").replace(/^[^:]+:\s*/, "");
      appliedRows.push(msg || t);
    } else if (inSkipped && /^-/.test(t)) {
      const msg = t.replace(/^-\s*/, "").replace(/^[^:]+:\s*/, "");
      skippedRows.push(msg || t);
    }
  }

  // 状态徽章 + 摘要行
  if (appliedCount > 0) {
    statusEl.textContent = "✓ 修复成功";
    statusEl.className = "repair-panel-status ok";
  } else if (skippedCount > 0 && appliedCount === 0) {
    statusEl.textContent = "⚠ 未完成";
    statusEl.className = "repair-panel-status error";
  } else {
    statusEl.textContent = "— 无操作";
    statusEl.className = "repair-panel-status";
  }

  const rows = [];
  rows.push(`
    <div class="rp-summary">
      <span class="rp-row"><span class="rp-badge">应用</span>${appliedCount} 项</span>
      <span class="rp-row"><span class="rp-badge">跳过</span>${skippedCount} 项</span>
    </div>`);
  for (const m of appliedRows) {
    rows.push(`<div class="rp-row ok"><span class="rp-badge">已执行</span>${escapeHtml(m)}</div>`);
  }
  for (const m of skippedRows) {
    rows.push(`<div class="rp-row warn"><span class="rp-badge">跳过</span>${escapeHtml(m)}</div>`);
  }
  if (appliedRows.length + skippedRows.length === 0) {
    rows.push(`<div class="rp-row">${escapeHtml(text.trim() || "无输出")}</div>`);
  }
  bodyEl.innerHTML = rows.join("");

  if (backupPath) {
    backupEl.textContent = "备份目录: " + backupPath;
    backupEl.classList.remove("hidden");
  } else {
    backupEl.classList.add("hidden");
  }
}

// 自动诊断状态处理：失败页提示诊断中/完成；进入诊断（StartupDiagnosing 或
// StartupDoctorReady）时打开诊断弹窗。弹窗在诊断一开始就打开并显示
// "正在诊断…"，而不是干等结果才出现，避免用户只看到失败页 spinner 转几十秒。
// state._startupDoctorShown 保证每个失败周期只自动弹窗一次，退出失败态
// （用户手动重启/安全模式）后重置，下一周期可再触发。
// 预览模式（runDoctor 为 null）只记录标记，不弹窗不诊断。
function updateStartupDoctor(s) {
  // 客户端插件加载失败同样是一次启动失败（进程健康而已），必须走同一套自动
  // 诊断：否则界面卡在失败页，用户只能自己去点"诊断问题"。
  if (s.State !== "failed" && !s.ClientFailure) {
    state._startupDoctorShown = false;
    // 退出失败态（用户重启/安全模式/修复后自动启动）：环境可能已变化，
    // 清空诊断缓存，进入下一次失败周期时重新检测而非展示旧结果。
    //
    // 这段必须排在 repairing 判断之前：修复流程最后会用修复后的状态调 applyStatus
    // （自动启动成功时已不在失败态），若那次快照被 repairing 提前 return，标记与
    // 缓存就留在上一轮——第二个失败周期不再自动弹窗，手动诊断还会渲染上一轮的
    // 全绿报告（审计 N12）。
    diagnosisState.lastReport = null;
    hideAutoDiagHint();
    hideDoctorAutoBanner();
    return;
  }

  // 修复进行中：保持弹窗的"修复中…"状态，不因 supervisor 状态抖动（如在重启）
  // 再触发一轮"正在诊断…"的自动弹窗，打断用户看到的修复进度。
  if (diagnosisState.repairing) return;

  if (s.StartupDoctorReady || s.StartupDiagnosing) {
    if (s.StartupDoctorReady) setAutoDiagHint("诊断完成", false);
    else setAutoDiagHint("正在自动诊断问题…", true);
    // 诊断一开始（Diagnosing）或已就绪（Ready）都打开弹窗。
    if (!state._startupDoctorShown) {
      state._startupDoctorShown = true;
      if (runDoctor) {
        openModal("doctor-modal");
        runDoctor();
        showDoctorAutoBanner();
      }
    }
  } else {
    hideAutoDiagHint();
  }
}

/** 复制按钮的默认图标（两个叠加方块），与 index.html 中的初始内容一致。 */
const COPY_ICON = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
/** 复制成功后的对勾图标。 */
const COPIED_ICON = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6L9 17l-5-5"/></svg>';

/** 复制反馈的复原计时器；连续复制时重置，避免前一次的复原抹掉后一次的反馈。 */
let copyResetTimer = null;

/**
 * 把复制按钮切到「已复制」态并在 1.5s 后复原。
 *
 * 反馈落在按钮自身而不是新增提示条：弹框的行高固定，插入文字会让正在发生的
 * 布局跳动，而用户的视线本来就在刚点过的图标上。
 * @param {HTMLElement} btn - 复制按钮；缺省时静默返回。
 */
function flashCopyButton(btn) {
  if (!btn) return;
  btn.innerHTML = COPIED_ICON;
  btn.title = tr("server.copied");
  btn.classList.add("copied");
  if (copyResetTimer) clearTimeout(copyResetTimer);
  copyResetTimer = setTimeout(() => {
    btn.innerHTML = COPY_ICON;
    // 复位值必须与 index.html 上 data-i18n-title 指向同一键：这个按钮的文案是
    // title 属性，复位写错键就会留下一句永远不跟随语言的文案。
    btn.title = tr("server.copyAddress");
    btn.classList.remove("copied");
    copyResetTimer = null;
  }, 1500);
}

/**
 * 复制 harness 服务地址并给出成功反馈。
 *
 * 走 Wails 注入的 `window.runtime.ClipboardSetText`（GTK 实现，容器内可用），
 * 与终端复制同一通道；`navigator.clipboard` 在 WebKit 容器里权限不可靠。
 * 复制失败只记控制台，不弹错误框打断用户手上的操作。
 * @param {string} url - 要复制的地址；空值直接返回（按钮此时应处于隐藏态）。
 */
function copyServerAddress(url) {
  if (!url) return;
  if (!window.runtime || !window.runtime.ClipboardSetText) {
    console.warn("copy server address: ClipboardSetText unavailable");
    return;
  }
  window.runtime.ClipboardSetText(url)
    .then(() => flashCopyButton($("#server-copy")))
    .catch((e) => { console.warn("copy server address failed:", e && e.message); });
}

function renderServerDialog(s) {
  const externalMode = radioValue() === "external";
  $("#container-panel").classList.toggle("hidden", externalMode);
  $("#external-panel").classList.toggle("hidden", !externalMode);

  // 单选只在两个"权威时刻"被强制，平时让用户自由切换（准备连接外部时不停留在外部面板）：
  //  - 外部已连接 -> 外部；
  //  - 连接失败（错误从无到有）-> 回容器，展示容器状态与弹框级错误。
  if (s.Mode === "external") {
    setRadio("external");
  } else if (s.ConnectError && !state.prevConnectError) {
    setRadio("container");
  }
  state.prevConnectError = s.ConnectError;

  // 连接错误在弹框级常显，两种模式都能看到。
  $("#dlg-error").textContent = s.ConnectError || "";

  const stateText = { running: "运行中", starting: "启动中", failed: "启动失败", stopped: "已停止" }[s.State] || "已停止";
  // 状态色沿用底部状态栏的语义：运行=ok、启动中=warn、失败=danger、其余中性。
  // 状态点由 .state-value::before 以 currentColor 画出，不需要额外 DOM 节点。
  const stateClass = { running: "state-ok", starting: "state-warn", failed: "state-danger" }[s.State] || "state-muted";
  const stateEl = $("#server-state");
  stateEl.textContent = stateText;
  stateEl.className = "row-value state-value " + stateClass;

  if (s.State === "running") {
    const addr = splitAddress(s.URL);
    $("#server-addr-origin").textContent = addr.origin;
    $("#server-addr-token").textContent = addr.token;
    $("#server-detail2").textContent = s.PID;
  } else if (s.State === "starting") {
    $("#server-detail1").textContent = "harness 正在启动…";
    $("#server-detail2").textContent = "";
  } else if (s.State === "failed") {
    $("#server-detail1").textContent = s.LastExit || "";
    $("#server-detail2").textContent = "~/.cache/dsh-desktop/harness.log";
  } else {
    $("#server-detail1").textContent = s.LastExit || "";
    $("#server-detail2").textContent = "";
  }

  $("#server-start").disabled = !s.CanStart;
  $("#server-restart").disabled = !s.CanRestart;
  $("#server-stop").disabled = !s.CanStop;
  // 复制按钮只在地址即为服务 URL 时出现：其余状态下这一行显示的是「正在启动…」、
  // 退出原因或日志路径，复制它们没有意义。
  // 地址行的两种呈现互斥：运行态是分两行的服务地址，其余状态是纯文案。等宽与
  // 代码块挂在只承载地址的 #server-address 上，随它一起显隐，因此「正在启动…」
  // 不会被套进一个看似可复制、实则无按钮的输入框样式里。
  const isAddress = s.State === "running";
  $("#server-copy").classList.toggle("hidden", !isAddress);
  $("#server-address").classList.toggle("hidden", !isAddress);
  $("#server-detail1").classList.toggle("hidden", isAddress);

  // 安全模式：失败态显示「以插件安全模式启动」
  const failed = s.State === "failed" || s.State === "stopped";
  $("#safe-mode-row").classList.toggle("hidden", !failed || !!s.SafeMode || !!s.FreshHome);
  // 运行中且为安全模式，显示安全模式标识和退出按钮
  $("#safe-mode-active").classList.toggle("hidden", !s.SafeMode);
  // 全新环境运行标识与退出入口
  $("#fresh-home-active").classList.toggle("hidden", !s.FreshHome);

  $("#ext-connect").disabled = !s.CanConnect;
  $("#ext-disconnect").disabled = !s.CanDisconnect;

  if (s.Mode === "external") {
    // 只报主机端口：输入框里已经是完整地址，状态栏也已收窄到 host:port，
    // 这里再贴一遍完整 URL 会折成三行把弹框顶高，也让 token 多显示一处。
    const host = hostLabel(s.ExternalURL);
    $("#ext-state").textContent = host ? "已连接 " + host : "已连接";
  } else if (s.Busy) {
    $("#ext-state").textContent = "连接中…";
  } else {
    $("#ext-state").textContent = "";
  }
}

/* ---------- 工具链市场 ---------- */

// marketState 缓存最近一次工具链状态，供分类/搜索过滤与卡片渲染。
// progress 记录各工具链安装的实时进度：id → {Phase, Percent, Message}。
// 由 toolchain:progress 事件驱动；done/error 阶段会删除对应条目。支持多工具并发。
const marketState = { category: "all", search: "", catalog: [], categoryLabels: {}, progress: {}, status: null };

// fmtSize 把字节数格式化为 "1.6 MB" 之类的可读文本；0/空返回空串。
function fmtSize(bytes) {
  const n = Number(bytes) || 0;
  if (n <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let v = n, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return (i === 0 ? String(v) : v.toFixed(1)) + " " + units[i];
}

// DEFAULT_CATEGORY_LABELS 是分类标签的兜底表：标签的权威来源是索引里的
// category_labels（随 t.CategoryLabels 下发），这张表只在两种情况下生效——索引没带该
// 分类的标签，以及生效的还是引入 category_labels 之前的旧索引。索引优先，因此新增分类
// 只要在索引里写一行标签，客户端不必跟着发版。
const DEFAULT_CATEGORY_LABELS = {
  "language-sdk": "tools.category.languageSdk", "build-tools": "tools.category.buildTools",
  "modern-cli": "tools.category.modernCli",
  "code-quality": "tools.category.codeQuality", "debug": "tools.category.debug",
};

// categoryLabel 分类 ID → 标签：索引声明优先，其次字典兜底，都没有则原样返回 ID。
// 索引下发的 category_labels 是数据（内容由索引作者写），因此不经字典；只有兜底表
// 里的措辞是客户端的文案，值存的是键。
function categoryLabel(cat) {
  const fromIndex = marketState.categoryLabels[cat];
  if (fromIndex) return fromIndex;
  return DEFAULT_CATEGORY_LABELS[cat] ? tr(DEFAULT_CATEGORY_LABELS[cat]) : cat;
}

// MARKET_CATEGORY_ORDER 只决定已知分类在页签里的先后，不再是页签全集：清单来自独立
// 发布的远程索引，客户端与它的版本必然错配。写死页签会在两种错配下出问题——旧索引
// 配新客户端时点进去是空列表，新索引配旧客户端时整个分类被藏掉。改成按清单里实际出现
// 的分类建页签后，未知分类仍成页签（排在已知分类之后），没有工具的分类不会留下死页签。
const MARKET_CATEGORY_ORDER = ["language-sdk", "build-tools", "modern-cli", "code-quality", "debug"];

// visibleCategories 汇总清单里出现的分类，已知分类按固定顺序在前，未知分类按 ID 排序在后。
function visibleCategories() {
  const present = new Set(marketState.catalog.map((c) => c.Category));
  const known = MARKET_CATEGORY_ORDER.filter((c) => present.has(c));
  const unknown = [...present].filter((c) => !MARKET_CATEGORY_ORDER.includes(c)).sort();
  return known.concat(unknown);
}

// renderMarketTabs 按当前清单重建分类页签。选中项在清单里消失（换了索引版本）时回落到
// "全部"，否则筛选条件会指向一个已不存在的分类，网格永远空着。
function renderMarketTabs() {
  const box = $("#market-tabs");
  box.innerHTML = "";
  const cats = visibleCategories();
  if (marketState.category !== "all" && !cats.includes(marketState.category)) {
    marketState.category = "all";
  }
  const items = [{ cat: "all", label: tr("tools.category.all") }].concat(cats.map((c) => ({ cat: c, label: categoryLabel(c) })));
  for (const item of items) {
    const btn = document.createElement("button");
    btn.className = "market-tab" + (marketState.category === item.cat ? " active" : "");
    btn.dataset.cat = item.cat;
    btn.textContent = item.label;
    // 页签随时可能被重建，逐个绑事件而不是在启动时按选择器绑一次；委托还依赖冒泡，
    // 而这里不需要那层间接。
    btn.addEventListener("click", () => selectMarketCategory(item.cat));
    box.appendChild(btn);
  }
}

// selectMarketCategory 切换分类筛选：更新状态与高亮后重画网格与状态栏。
function selectMarketCategory(cat) {
  marketState.category = cat;
  document.querySelectorAll(".market-tab").forEach((t) =>
    t.classList.toggle("active", t.dataset.cat === cat));
  refreshMarketView();
}

function renderTools(t) {
  marketState.catalog = t.Catalog || [];
  // 分类标签随生效索引下发；先落状态再建页签，页签才拿得到新标签。
  marketState.categoryLabels = t.CategoryLabels || {};
  // 缓存 Rows，供点击"内置"按钮时动态渲染
  marketState.builtinRows = t.Rows || [];
  // 缓存最近一次状态：筛选变化时要重画状态栏（"筛选 N 个"），而那时事件不会再送一份 t。
  marketState.status = t;
  renderMarketTabs();
  renderMarketGrid();
  renderStatusbar(t);
  renderHostTools(t);
  renderUpdateBadge(t);
  $("#toolchain-notice").textContent = hostToolsNotice(t);
}

// hostToolsNotice 返回工具链弹框顶部提示条应显示的文案。
// 提示条只有 renderTools 一处写点：宿主导入区曾自己写一次，紧接着被这里的
// `t.Notice` 无条件覆盖，开发态提示因此在渲染周期里消失。
function hostToolsNotice(t) {
  if (t.Sandboxed) return t.Notice || "";
  const devMsg = tr("tools.devNotice");
  return t.Notice ? devMsg + " " + t.Notice : devMsg;
}

// renderUpdateBadge 更新工具链图标的小红点和弹框内的更新提示条。
// 横幅是唯一有整行空间的地方，因此在这里点明每个工具会更新到哪个版本：
// 卡片徽标只放得下短文案（见 toolCard），而「会切到哪一版」正是用户点更新前要知道的。
function renderUpdateBadge(t) {
  var count = Number(t.UpdateCount) || 0;
  var badge = $("#tools-update-badge");
  if (badge) badge.classList.toggle("hidden", count === 0);
  var banner = $("#market-update-banner");
  var text = $("#market-update-text");
  if (banner && text) {
    if (count > 0) {
      var targets = (t.Catalog || [])
        .filter(function (c) { return c.HasUpdate; })
        .map(function (c) { return c.Name + " → " + c.AvailableVersion; });
      const list = targets.join(tr("common.listSeparator"));
      text.textContent = targets.length
        ? tr("tools.updateBanner.withTargets", { count: count, targets: list })
        : tr("tools.updateBanner.count", { count: count });
      text.title = targets.length ? tr("tools.updateBanner.title", { targets: list }) : "";
      banner.classList.remove("hidden");
    } else {
      banner.classList.add("hidden");
    }
  }
}

// renderBuiltin 渲染内置工具收纳盒（#builtin-panel）：随包工具不可卸载，
// 只展示名称/版本/可用状态。由"内置"按钮点击触发，非自动渲染。
function renderBuiltin() {
  const box = $("#builtin-panel");
  const rows = marketState.builtinRows || [];
  box.innerHTML = "";
  if (!rows || rows.length === 0) {
    const empty = document.createElement("span");
    empty.className = "builtin-empty";
    empty.textContent = tr("tools.builtinEmpty");
    box.textContent = "";
    box.appendChild(empty);
    return;
  }
  for (const r of rows) {
    const chip = document.createElement("span");
    chip.className = "builtin-chip" + (r.State === "installed" ? " ok" : " missing");
    chip.title = r.State === "installed" ? tr("tools.builtinInstalled") : tr("tools.builtinMissing");
    const nm = document.createElement("span");
    nm.className = "builtin-name";
    nm.textContent = r.Name;
    chip.appendChild(nm);
    if (r.Version) {
      const ver = document.createElement("span");
      ver.className = "builtin-ver";
      ver.textContent = r.Version;
      chip.appendChild(ver);
    }
    box.appendChild(chip);
  }
}

// 内置工具按钮 toggle：点击展开/折叠收纳盒
function setupBuiltinToggle() {
  const btn = $("#builtin-toggle");
  const panel = $("#builtin-panel");
  if (!btn || !panel) return;
  btn.addEventListener("click", () => {
    const open = panel.classList.toggle("hidden");
    btn.classList.toggle("active", !open);
    if (!open) renderBuiltin();
  });
}

// 宿主导入 toggle：默认折叠成一行。折叠态由 #hosts-summary 报出已挂载项数，
// 展开后才出现扫描与挂载表单——把底部这块常驻空间还给卡片网格。
function setupHostsToggle() {
  const btn = $("#hosts-toggle");
  const body = $("#hosts-body");
  if (!btn || !body) return;
  // aria-expanded 由 DOM 的折叠态推导，不在 HTML 与 JS 里各记一份状态。
  const sync = () => btn.setAttribute("aria-expanded", body.classList.contains("hidden") ? "false" : "true");
  btn.addEventListener("click", () => {
    body.classList.toggle("hidden");
    sync();
  });
  sync();
}

// renderProgress 处理 toolchain:progress 事件：更新进度表，并定向刷新对应
// 卡片的进度条（不做整网格重渲染，下载回调高频时保持 UI 响应）。
function renderProgress(ev) {
  if (!ev || !ev.ID) return;
  if (ev.Phase === "done" || ev.Phase === "error") {
    delete marketState.progress[ev.ID];
    updateProgressBar(ev.ID, null);
    return;
  }
  marketState.progress[ev.ID] = ev;
  updateProgressBar(ev.ID, ev);
}

// updateProgressBar 定向更新某卡片的状态徽标与进度条；ev 为 null 表示清除进度。
// id 来自 toolchain:progress 事件，由远程索引下发的工具 ID 决定。它不拼进选择器：
// `.tool-card-item[data-tool-id="..."]` 里的引号与反斜杠会让含该字符的 ID 变成另一条
// 选择器，或让这次查询抛错、中断整轮进度刷新。改为在网格子树内逐一比较 dataset，
// 语义不变、不依赖转义规则，也不会命中网格之外的卡片。
function updateProgressBar(id, ev) {
  const grid = $("#market-grid");
  let card = null;
  if (grid) {
    for (const el of grid.querySelectorAll(".tool-card-item")) {
      if (el.dataset.toolId === id) { card = el; break; }
    }
  }
  if (!card) return;
  const bar = card.querySelector(".tool-progress-fill");
  const label = card.querySelector(".tool-progress-label");
  if (bar) bar.style.width = (ev ? ev.Percent || 0 : 0) + "%";
  if (label) label.textContent = ev ? (ev.Percent || 0) + "%" : "";
}

// filteredCatalog 按当前分类 + 搜索词过滤目录。
function filteredCatalog() {
  const q = marketState.search.trim().toLowerCase();
  return marketState.catalog.filter((c) => {
    if (marketState.category !== "all" && c.Category !== marketState.category) return false;
    if (!q) return true;
    const hay = (c.Name + " " + c.Description + " " + (c.Provides || []).join(" ")).toLowerCase();
    return hay.includes(q);
  });
}

// hasFilter 判断当前是否设了分类或搜索条件；空态与状态栏都靠它决定要不要提"筛选"。
function hasFilter() {
  return marketState.category !== "all" || marketState.search.trim() !== "";
}

// emptyState 构造无结果时的网格内容。弹框定高后这里会留出大片空白，一句"没有匹配"
// 不足以收尾：给出下一步（改关键词或清空筛选）。目录本身为空时不给清空按钮——
// 那会让人以为是自己筛掉了什么。
function emptyState() {
  const box = document.createElement("div");
  box.className = "market-empty";
  const text = document.createElement("div");
  text.className = "market-empty-text";
  text.textContent = tr("tools.empty");
  box.appendChild(text);
  if (hasFilter()) {
    const hint = document.createElement("div");
    hint.className = "market-empty-hint";
    hint.textContent = tr("tools.emptyHint");
    const btn = document.createElement("button");
    btn.className = "btn btn-quiet btn-sm";
    btn.textContent = tr("tools.clearFilters");
    btn.addEventListener("click", () => {
      marketState.search = "";
      $("#market-search").value = "";
      selectMarketCategory("all");
    });
    box.append(hint, btn);
  }
  return box;
}

// refreshMarketView 重画网格与状态栏。筛选条件由页签与搜索框改动，两处都要跟着变：
// 状态栏里的"筛选 N 个"若只在 toolchain:status 事件里更新，输入搜索词后就会停在旧值。
function refreshMarketView() {
  renderMarketGrid();
  if (marketState.status) renderStatusbar(marketState.status);
}

// renderMarketGrid 渲染工具卡片网格（分类 + 搜索过滤后）。
function renderMarketGrid() {
  const grid = $("#market-grid");
  grid.innerHTML = "";
  const list = filteredCatalog();
  if (list.length === 0) {
    grid.appendChild(emptyState());
    return;
  }
  for (const c of list) grid.appendChild(toolCard(c));
}

// toolCard 渲染单个工具卡片；已装与未装分支的动作不同。
// installed 是状态标记（描边不再区分已装）；updatable 与 installing 才是需要用户
// 注意、由卡片描边强调的两种状态。
function toolCard(c) {
  const el = document.createElement("div");
  const prog = marketState.progress[c.ID];
  const installing = !!prog && prog.Phase !== "done" && prog.Phase !== "error";
  el.className = "tool-card-item"
    + (c.Installed ? " installed" : "")
    + (c.Installed && c.HasUpdate ? " updatable" : "")
    + (installing ? " installing" : "");
  el.dataset.toolId = c.ID; // 供进度事件定向更新

  const head = document.createElement("div");
  head.className = "tool-card-head";
  const name = document.createElement("span");
  name.className = "tool-card-name";
  name.textContent = c.Name;
  const status = document.createElement("span");
  if (c.Installed && c.HasUpdate) {
    // 徽标保持短文案：卡片仅约 170px 宽，写上「可更新到 v21.0.12.1」会把工具名挤成
    // 省略号。目标版本由横幅（有整行空间）与这里的 title 共同给出。
    status.className = "pill warn";
    status.textContent = tr("tools.pill.update");
    status.title = tr("tools.card.updateHint", { version: c.AvailableVersion });
  } else if (c.Installed) {
    status.className = "pill ok";
    status.textContent = tr("tools.pill.installed");
  } else if (installing) {
    // 徽标只表状态：百分比由进度条与其下方的读数承担，同一个数字不必出现三处
    status.className = "pill warn";
    status.textContent = tr("tools.pill.installing");
  } else {
    status.className = "pill brand";
    status.textContent = tr("tools.pill.installable");
  }
  head.append(name, status);

  const desc = document.createElement("div");
  desc.className = "tool-card-desc";
  desc.textContent = c.Description || "";

  // 运行时提示：市场仓库未装、但容器 PATH 已有同名命令（随包/宿主导入/系统
  // 提供）时标注来源与版本，区分"仓库未安装"与"容器内不可用"两个概念。
  // 由后端 annotateRuntime 组装，前端纯渲染。
  let runtimeHint = null;
  if (!c.Installed && c.RuntimeCmd) {
    runtimeHint = document.createElement("div");
    runtimeHint.className = "tool-card-runtime";
    runtimeHint.textContent = c.RuntimeVersion
      ? tr("tools.runtime.availableVersion", { cmd: c.RuntimeCmd, version: c.RuntimeVersion, source: c.RuntimeSource })
      : tr("tools.runtime.available", { cmd: c.RuntimeCmd, source: c.RuntimeSource });
    runtimeHint.title = tr("tools.runtime.title");
  }

  const meta = document.createElement("div");
  meta.className = "tool-card-meta";
  meta.textContent = [categoryLabel(c.Category), (c.Provides || []).join(" "), fmtSize(c.Size)].filter(Boolean).join(" · ");

  // 安装中的进度条：toolchain:progress 事件按 data-tool-id 定向更新宽度。
  const progress = document.createElement("div");
  progress.className = "tool-progress";
  const fill = document.createElement("div");
  fill.className = "tool-progress-fill";
  fill.style.width = (prog ? prog.Percent || 0 : 0) + "%";
  progress.appendChild(fill);
  const pctLabel = document.createElement("div");
  pctLabel.className = "tool-progress-label";
  pctLabel.textContent = prog ? (prog.Percent || 0) + "%" : "";

  const actions = document.createElement("div");
  actions.className = "tool-card-actions";

  if (c.Installed) {
    // 已装：版本下拉（已装版本 + 清单里尚未安装的版本）+ 安装 / 两击确认卸载。
    // 多版本工具若只能「卸载再重装」换版本，等于把多版本能力藏起来：这里让未装版本
    // 也能在卡片上直接安装，并沿用后端的「安装即激活」语义。
    const installed = c.InstalledVersions || [];
    const sel = document.createElement("select");
    sel.className = "version-select";
    sel.title = tr("tools.versionSelect.title");
    const appendOption = (v, isInstalled) => {
      const opt = document.createElement("option");
      opt.value = v;
      opt.textContent = "v" + v + (v === c.ActiveVersion
        ? tr("tools.version.current")
        : (isInstalled ? tr("tools.version.installed") : tr("tools.version.installable")));
      if (v === c.ActiveVersion) opt.selected = true;
      sel.appendChild(opt);
    };
    for (const v of installed) appendOption(v, true);
    for (const v of c.AvailableVersions || []) {
      if (!installed.includes(v)) appendOption(v, false);
    }
    const isInstalled = () => installed.includes(sel.value);

    const install = document.createElement("button");
    install.className = "btn btn-primary";
    install.textContent = tr("tools.install");
    install.addEventListener("click", () => {
      install.disabled = true;
      install.textContent = tr("tools.installing");
      api().InstallToolVersion(c.ID, sel.value);
    });

    const un = document.createElement("button");
    un.className = "btn btn-danger";
    un.textContent = tr("tools.uninstall");
    un.addEventListener("click", async () => {
      if (!consumeConfirmClick(un, tr("tools.uninstall"), tr("tools.uninstallConfirm"))) return;
      const err = await api().UninstallTool(c.ID, sel.value);
      if (err) { $("#toolchain-notice").textContent = err; }
      api().RefreshTools();
    });

    // 选中的是未装版本时，「卸载」对那个版本无意义（后端会返回不存在），
    // 因此两者互斥显示：安装按钮只为未装版本出现，卸载只为已装版本出现。
    const syncActions = () => {
      const ok = isInstalled();
      install.classList.toggle("hidden", ok);
      un.classList.toggle("hidden", !ok);
      if (!ok) install.textContent = tr("tools.installVersion", { version: sel.value });
    };

    sel.addEventListener("change", async () => {
      syncActions();
      if (!isInstalled()) return; // 未装版本等用户点「安装」
      const err = await api().SetActiveToolVersion(c.ID, sel.value);
      if (err) { $("#toolchain-notice").textContent = err; }
      api().RefreshTools();
    });
    syncActions();
    actions.append(sel, install, un);
  } else {
    // 未装：可选版本（多版本时给下拉，默认推荐）+ 安装按钮。
    const vers = c.AvailableVersions || [];
    let sel = null;
    if (vers.length > 1) {
      sel = document.createElement("select");
      sel.className = "version-select";
      for (const v of vers) {
        const opt = document.createElement("option");
        opt.value = v;
        opt.textContent = "v" + v;
        sel.appendChild(opt);
      }
      actions.appendChild(sel);
    }
    const b = document.createElement("button");
    b.className = "btn btn-primary";
    const size = fmtSize(c.Size);
    b.textContent = installing
      ? tr("tools.installing")
      : (size ? tr("tools.installWithSize", { size: size }) : tr("tools.install"));
    b.disabled = installing;
    b.addEventListener("click", () => {
      b.disabled = true;
      b.textContent = tr("tools.installing");
      api().InstallToolVersion(c.ID, sel ? sel.value : c.AvailableVersion);
    });
    actions.appendChild(b);
  }

  if (installing) {
    el.append(head, desc, meta, progress, pctLabel, actions);
  } else {
    el.append(head, desc, meta, actions);
  }
  // 运行时提示追加在动作区之后（也就是卡片最后一行）。它比其他卡片多占一行，
  // 因此同时给卡片抬高最小高度档次（见 .tool-card-item.has-runtime）。
  if (runtimeHint) {
    el.classList.add("has-runtime");
    el.append(runtimeHint);
  }
  return el;
}

// renderStatusbar 渲染底部状态栏：随包 + 已装 L2 + 总大小 + 宿主挂载数。
function renderStatusbar(t) {
  const sb = $("#market-statusbar");
  const rows = t.Rows || [];
  const ok = rows.filter((r) => r.State === "installed").length;
  const cats = marketState.catalog;
  const installed = cats.filter((c) => c.Installed).length;
  let total = 0;
  for (const c of cats) if (c.Installed) total += Number(c.Size) || 0;
  const parts = [];
  if (rows.length > 0) parts.push(tr("tools.status.bundled", { ok: ok, total: rows.length }));
  parts.push(tr("tools.status.installed", { installed: installed, total: cats.length }));
  if (total > 0) parts.push(tr("tools.status.totalSize", { size: fmtSize(total) }));
  if (t.Sandboxed && (t.HostTools || []).length > 0) {
    parts.push(tr("tools.status.hostMounts", { count: t.HostTools.length }));
  }
  // 定高弹框里筛选后留下的空白需要有交代：报出当前条件命中的工具数。
  if (hasFilter()) parts.push(tr("tools.status.filtered", { count: filteredCatalog().length }));
  sb.textContent = parts.join(tr("tools.statusSeparator"));
}

// renderHostTools 渲染宿主挂载列表与扫描结果；开发态隐藏整个宿主导入区，
// 对应提示条文案见 hostToolsNotice。
function renderHostTools(t) {
  const hostBox = $("#card-hosts");
  if (!t.Sandboxed) {
    hostBox.classList.add("hidden");
    return;
  }
  hostBox.classList.remove("hidden");
  const mounts = t.HostTools || [];
  // 折叠态下唯一可见的一行：报出已挂载项数，用户不必展开就知道有没有配置。
  $("#hosts-summary").textContent = mounts.length ? tr("tools.hostsSummary", { count: mounts.length }) : "";
  const hl = $("#host-list");
  hl.innerHTML = "";
  for (const h of mounts) {
    const row = document.createElement("div");
    row.className = "host-item";
    const rm = document.createElement("button");
    rm.className = "btn btn-danger";
    rm.textContent = tr("tools.hostRemove");
    rm.addEventListener("click", () => api().RemoveHostTool(h.Name));
    const mounted = h.Mounted
      ? "<span class='state-ok'>" + tr("tools.hostMounted") + "</span>"
      : "<span class='state-missing'>" + tr("tools.hostPending") + "</span>";
    row.innerHTML =
      "<span class='selectable host-name'>" + escapeHtml(h.Name) + "</span>" +
      "<span class='hint selectable'>" + escapeHtml(h.Source) + " → " + escapeHtml(h.Target) + "</span>" +
      "<span class='hint'>" + mounted + "</span>";
    row.appendChild(rm);
    hl.appendChild(row);
  }
}

// renderHostScan 渲染宿主导入扫描结果（名称 + 版本 + 挂载按钮 + 冲突标记）。
function renderHostScan(entries) {
  const box = $("#host-scan-list");
  box.innerHTML = "";
  if (!entries || entries.length === 0) {
    box.innerHTML = "<div class='empty'>" + tr("tools.hostScanEmpty") + "</div>";
    return;
  }
  for (const e of entries) {
    const row = document.createElement("div");
    row.className = "host-item";
    const add = document.createElement("button");
    add.className = "btn btn-primary";
    add.textContent = tr("tools.hostAdd");
    add.addEventListener("click", async () => {
      const hint = $("#host-hint");
      if (!consumeConfirmClick(add, tr("tools.hostAdd"), tr("tools.hostMountConfirm"))) {
        // 首次点击：把这次挂载的后果写进提示行（按钮一行放不下）。挂载以 rbind,ro
        // 写进 config.d，生效后沙箱内所有进程都能读到该目录——用户需要知道这一点
        // 才能判断该不该继续。
        hint.className = "hint";
        hint.textContent = tr("tools.hostMountWarning", { path: e.Source });
        return;
      }
      const res = await api().AddHostTool(e.Source, e.Name);
      if (res.Error) {
        hint.className = "error";
        hint.textContent = tr("tools.hostMountFailed", { error: res.Error });
      } else {
        hint.className = "hint";
        hint.textContent = res.Warning
          ? tr("tools.hostMountWrittenWithWarning", { warning: res.Warning })
          : tr("tools.hostMountWritten");
      }
      api().RefreshTools();
    });
    row.innerHTML =
      "<span class='selectable host-name'>" + escapeHtml(e.Name) + "</span>" +
      "<span class='hint selectable'>" + escapeHtml(e.Tool) + (e.Version ? " " + escapeHtml(e.Version) : "") + "</span>" +
      "<span class='hint selectable'>" + escapeHtml(e.Source) + "</span>" +
      (e.Conflict ? "<span class='pill warn'>" + tr("tools.hostConflict") + "</span>" : "");
    row.appendChild(add);
    box.appendChild(row);
  }
}


/* ---------- 弹框 ---------- */

function openModal(id) {
  $("#" + id).classList.remove("hidden");
}

function closeModal(id) {
  $("#" + id).classList.add("hidden");
}

/**
 * 绑定「服务器」弹框里会启动或重启 harness 的动作按钮。
 *
 * 动作受理后关闭弹框回到主舞台：启动/重启的进展由主舞台的加载页与失败页承担，
 * 弹框留着只会挡住用户真正要看的那块界面（弹框是全屏遮罩，主舞台其实已经切走了）。
 * 这里刻意不按返回的快照判断动作是否生效——supervisor 的状态由监护循环异步推进，
 * 紧接着取到的快照可能仍停在 stopped/failed；而按钮在动作不受理时本就是禁用的
 * （CanStart/CanRestart），能点到即代表这次动作已被受理。绑定调用抛错时不关，
 * 错误留在弹框里可见。「停止」不走这里：它是原地操作，用户通常紧接着要再启动
 * 或复制地址。
 * @param {string} selector - 动作按钮的选择器。
 * @param {() => Promise<object>} action - 返回最新状态快照的 Wails 绑定调用。
 */
function bindServerAction(selector, action) {
  $(selector).addEventListener("click", async () => {
    applyStatus(await action());
    closeModal("server-modal");
  });
}

/* ---------- 事件绑定 ---------- */

function bindUI() {
  bindExternalLinks();

  // 自定义标题栏窗口控制（Wails frameless）；浏览器预览时 window.runtime 缺失，安全降级
  $("#win-min").addEventListener("click", () => window.runtime && window.runtime.WindowMinimise && window.runtime.WindowMinimise());
  $("#win-max").addEventListener("click", () => window.runtime && window.runtime.WindowToggleMaximise && window.runtime.WindowToggleMaximise());
  $("#win-close").addEventListener("click", () => window.runtime && window.runtime.Quit && window.runtime.Quit());
  $("#titlebar").addEventListener("dblclick", (e) => {
    if (e.target.closest("button")) return; // 按钮上双击不触发最大化
    if (window.runtime && window.runtime.WindowToggleMaximise) window.runtime.WindowToggleMaximise();
  });

  $("#btn-server").addEventListener("click", () => {
    openModal("server-modal");
    if (state.status) renderServerDialog(state.status);
  });
  $("#btn-tools").addEventListener("click", () => {
    openModal("tools-modal");
    api().RefreshTools();
  });
  $("#btn-about").addEventListener("click", async () => {
    const info = await api().About();
    $("#about-package-version").textContent = info.PackageVersion;
    $("#about-harness-version").textContent = info.HarnessVersion;
    $("#about-packager").textContent = info.Packager;
    for (const [selector, field] of ABOUT_LINKS) {
      const link = $(selector);
      const url = info[field];
      link.textContent = url;
      // 写属性而不是 .href 属性：点击处理读的就是 getAttribute("href")，
      // 两者同源才不会出现「显示有、点了没反应」。
      link.setAttribute("href", url);
    }
    openModal("about-modal");
  });

  document.querySelectorAll("[data-close]").forEach((b) =>
    b.addEventListener("click", () => closeModal(b.dataset.close)));

  document.querySelectorAll('input[name="mode"]').forEach((r) =>
    r.addEventListener("change", () => {
      if (state.status) renderServerDialog(state.status);
    }));

  // 启动/重启语义的动作受理后关弹框回主舞台（见 bindServerAction）；停止留在弹框内。
  bindServerAction("#server-start", () => api().StartServer());
  bindServerAction("#server-restart", () => api().RestartServer());
  bindServerAction("#btn-safe-mode", () => api().StartSafeMode());
  bindServerAction("#btn-exit-safe-mode", () => api().ExitSafeMode());
  bindServerAction("#btn-exit-fresh-home", () => api().ExitFreshHome());
  $("#server-stop").addEventListener("click", async () => {
    applyStatus(await api().StopServer());
  });
  $("#server-copy").addEventListener("click", () => {
    copyServerAddress(state.status && state.status.URL);
  });

  // 预检页操作：深度修复 / 忽略启动 / 安全模式 / 全新环境。
  $("#btn-preflight-deep-repair").addEventListener("click", async () => {
    applyStatus(await api().ConfirmDeepRepair());
  });
  $("#btn-preflight-skip").addEventListener("click", async () => {
    applyStatus(await api().SkipPreflight());
  });
  $("#btn-preflight-safe-mode").addEventListener("click", async () => {
    applyStatus(await api().StartSafeModeLevel("config"));
  });
  $("#btn-preflight-fresh").addEventListener("click", async () => {
    if (!confirm(tr("preflight.freshHomeConfirm"))) {
      return;
    }
    applyStatus(await api().StartFreshHome());
  });
  $("#ext-connect").addEventListener("click", async () => {
    const url = $("#ext-url").value.trim();
    const err = await api().ConnectExternal(url);
    if (err) $("#dlg-error").textContent = err;
  });
  $("#ext-disconnect").addEventListener("click", () => api().DisconnectExternal());

  $("#tools-refresh").addEventListener("click", () => api().RefreshTools());

  // 分类页签由 renderMarketTabs 按清单重建，事件在建立时逐个绑定（见该函数）。

  // 搜索：输入即过滤（防抖可省，目录规模小）。
  $("#market-search").addEventListener("input", () => {
    marketState.search = $("#market-search").value;
    refreshMarketView();
  });

  // 刷新远程索引：异步拉取，完成后推送一次状态。
  $("#market-refresh").addEventListener("click", async () => {
    const btn = $("#market-refresh");
    btn.disabled = true;
    btn.textContent = tr("tools.refreshing");
    await api().RefreshToolIndex();
    btn.disabled = false;
    btn.textContent = tr("tools.refreshIndex");
  });

  // 一键更新所有过时工具
  var updateAllBtn = $("#market-update-all");
  if (updateAllBtn) {
    updateAllBtn.addEventListener("click", async () => {
      if (updateAllBtn.disabled) return;
      updateAllBtn.disabled = true;
      updateAllBtn.textContent = tr("tools.updating");
      var err = await api().UpdateAllTools();
      if (err) {
        $("#toolchain-notice").textContent = tr("tools.updateFailed", { error: err });
        updateAllBtn.disabled = false;
        updateAllBtn.textContent = tr("tools.updateAll");
      }
      // 成功时由 toolchain:status 事件刷新 UI 和按钮状态
    });
  }

  // 宿主导入向导：扫描常见宿主工具链根目录。
  $("#host-scan").addEventListener("click", async () => {
    const btn = $("#host-scan");
    btn.disabled = true;
    btn.textContent = tr("tools.scanning");
    try {
      renderHostScan(await api().ScanHostTools());
    } finally {
      btn.disabled = false;
      btn.textContent = tr("tools.hostScan");
    }
  });

  const hostAdd = $("#host-add");
  hostAdd.addEventListener("click", async () => {
    const src = $("#host-path").value.trim();
    const name = $("#host-name").value.trim();
    if (!src) return;
    const hint = $("#host-hint");
    // 与卸载工具同一套两击确认：挂载以 rbind,ro 写进 config.d，生效后沙箱内所有
    // 进程都能读到该目录，而按钮一行放不下这句后果，首次点击时写进提示行。
    // 确认时按输入框当前值执行——两次点击之间用户仍可修改，改动由他自己作出。
    if (!consumeConfirmClick(hostAdd, tr("tools.hostAdd"), tr("tools.hostMountConfirm"))) {
      hint.className = "hint";
      hint.textContent = tr("tools.hostMountWarning", { path: src });
      return;
    }
    const res = await api().AddHostTool(src, name);
    $("#host-path").value = "";
    $("#host-name").value = "";
    if (res.Error) {
      hint.className = "error";
      hint.textContent = tr("tools.hostMountFailed", { error: res.Error });
    } else {
      hint.className = "hint";
      hint.textContent = res.Warning
        ? tr("tools.hostMountWrittenWithWarning", { warning: res.Warning })
        : tr("tools.hostMountWritten");
    }
    api().RefreshTools();
  });
}

/* ---------- 启动 ---------- */

function init() {
  // 先定语言再渲染：加载页、预检页、失败页都在 GUI 起来之前出现，此时只能按
  // navigator 兜底；GUI 起来后由桥上报的 locale 消息覆盖（见 i18n.js）。
  window.DSHI18N.init();

  bindUI();

  if (!window.go || !window.go.app) {
    // 浏览器直接打开 index.html 的开发预览：无 Wails 运行时，仅展示引导页。
    $("#status-text").textContent = tr("preview.noWails");
    return;
  }

  window.runtime.EventsOn("harness:status", (s) => applyStatus(s));
  window.runtime.EventsOn("toolchain:status", (t) => renderTools(t));
  window.runtime.EventsOn("toolchain:progress", (p) => renderProgress(p));
  // 启动进度单独走事件：条目激活在数秒内产生上百次计数变化，1s 状态轮询只能
  // 采到一两个点，进度条会跳变。两条通道共用同一份视图与渲染函数。
  window.runtime.EventsOn("startup:progress", (v) => renderStartup(v));
  setupBuiltinToggle();
  setupHostsToggle();

  // 初始化终端模块
  initTerminal();

  // 诊断与修复
  $("#btn-doctor").addEventListener("click", () => {
    openModal("doctor-modal");
  });

  // 失败页快捷操作：runDoctor 是模块级绑定，失败页按钮与 applyStatus 的
  // 自动弹窗共用同一实现。
  $("#btn-failed-doctor").addEventListener("click", () => {
    openModal("doctor-modal");
    runDoctor();
  });
  $("#btn-failed-safe-mode").addEventListener("click", async () => {
    applyStatus(await api().StartSafeMode());
  });

  runDoctor = async function (summaryText, force) {
    // 检测已在进行：复用同一次检测（后台自动触发或用户点"诊断问题"后，
    // 弹框被关闭再打开不应二次触发重复检测），返回相同的结果。
    // 检测已在进行：复用同一次检测（后台自动触发或用户点"诊断问题"后，
    // 弹框被关闭再打开不应二次触发重复检测）。force=true（重新诊断、修复后
    // 复检）不能被进行中的检测或缓存短路——必须真正重跑，否则修复前后
    // running/promise 残留会让复检返回旧的失败结果。
    if (!force && diagnosisState.running && diagnosisState.promise) {
      return diagnosisState.promise;
    }
    // 诊断已完成且有结果：直接展示缓存，不重复检测。只有"重新诊断"按钮
    // （force=true）才强制重跑。
    if (!force && diagnosisState.lastReport) {
      setDoctorSummaryText(summaryText || "正在诊断…", false);
      renderDoctorReport(diagnosisState.lastReport);
      return diagnosisState.lastReport;
    }
    diagnosisState.running = true;
    // summaryText 可覆盖默认文案：修复后的复检用"修复完成，正在复查…"，
    // 与"又出问题了"的诊断区分开。
    setDoctorSummaryText(summaryText || "正在诊断…", false);
    $("#doctor-content").classList.add("hidden");
    $("#doctor-start").classList.add("hidden");
    const currentPromise = (async () => {
      try {
        const r = await api().RunDoctor();
        renderDoctorReport(r);
        // 记录失败元凶（供修复后的 toast 说明原因）。
        const firstBad = (r && !r.Error && r.Checks || []).find((c) => !c.OK);
        diagnosisState.lastCulprit = firstBad ? firstBad.Name : "";
        // 缓存结果：成功后再次打开弹窗直接展示（含出错报告，供用户查看）。
        diagnosisState.lastReport = r;
        return r;
      } catch (e) {
        setDoctorSummaryText("诊断失败: " + e.message, false);
        $("#doctor-start").classList.remove("hidden");
        return null;
      }
    })();
    diagnosisState.promise = currentPromise;
    try {
      return await currentPromise;
    } finally {
      // 只在仍是当前诊断时才清理状态：force 重跑会替换 promise，
      // 旧 promise 完成时不能误清新一轮的状态。
      if (diagnosisState.promise === currentPromise) {
        diagnosisState.running = false;
        diagnosisState.promise = null;
      }
    }
  };

  function renderDoctorReport(r) {
    if (r.Error) {
      setDoctorSummaryText("诊断失败: " + r.Error, false);
      $("#doctor-start").classList.remove("hidden");
      $("#doctor-content").classList.add("hidden");
      return;
    }

    // 语义类而非内联色值：内联只能写死一套主题的颜色，浅色主题下会变成白底上的
    // 浅绿浅黄（对比度 1.8–2.5:1）。类名对应的颜色由 styles.css 按主题给出。
    const sevClass = { fatal: "sev-error", error: "sev-error", warning: "sev-warn", info: "sev-info" };
    const statusClass = (ok, sev) => ok ? "sev-ok" : (sevClass[sev] || "sev-muted");

    setDoctorSummaryHtml(
      `<strong>共 ${r.Total} 项</strong>：` +
      `<span class="sev-ok">✓ ${r.OK} 通过</span>，` +
      `<span class="sev-error">✗ ${r.Failed} 失败</span>` +
      (r.Fatal > 0 ? `（<span class="sev-error">${r.Fatal} 严重</span>）` : "") +
      (r.Fixable > 0 ? `，<span class="sev-warn">${r.Fixable} 项可自动修复</span>` : ""),
      true,
    );

    // 安全模式提示：安全模式下第三方插件被跳过，诊断看到的是不完整的安装
    // 状态（可能误报"无第三方插件"并漏掉插件问题），提示用户先退出安全模式。
    const safeModeNotice = state.status && state.status.SafeMode
      ? '<div class="doctor-auto-hint">当前以安全模式运行（已跳过第三方插件），诊断结果不完整。请先退出安全模式再重新诊断。</div>'
      : "";
    // 提示条插在摘要栏上方（不影响"共 N 项 + 重新诊断"行的布局）。
    const hintId = "doctor-safe-hint";
    const existingHint = document.getElementById(hintId);
    if (existingHint) existingHint.remove();
    if (safeModeNotice) {
      const hint = document.createElement("div");
      hint.id = hintId;
      hint.className = "doctor-summary-safe-hint";
      hint.innerHTML = safeModeNotice;
      const box = $("#doctor-summary").parentNode;
      box.insertBefore(hint, $("#doctor-summary"));
    }

    // 清单里每个字符串字段都来自 `dsh doctor --json` 的子进程 stdout，而非
    // dsh-doctor 内置检查的固定输出：任何注册进 doctor 进程的检查都能决定它们，
    // 而壳前端持有全部 Go 绑定，壳内脚本执行等价于拿到这些能力。因此整段拼接
    // 只允许出现固定字面量与 escapeHtml 的输出；Go 侧的计数与 SuggestedLevel
    // 解成 int，不在此列。
    const checksHtml = r.Checks.map((c) => {
      const icon = c.OK ? "✓" : "✗";
      const colorClass = statusClass(c.OK, c.Severity);
      const fixBadge = c.Fixable && !c.OK
        ? `<span class="pill warn" style="margin-left:auto">可修复 L${c.SuggestedLevel}</span>` : "";
      const detail = c.Detail && !c.OK
        ? `<div class="doctor-detail">${escapeHtml(c.Detail)}</div>` : "";
      return `
        <div class="doctor-check-row">
          <span class="doctor-check-icon ${colorClass}">${icon}</span>
          <div class="doctor-check-main">
            <div class="doctor-check-title">
              <span>${escapeHtml(c.Name)}</span>
              <span class="hint" style="margin-left:8px">[${escapeHtml(c.Category)} / ${escapeHtml(c.Severity)}]</span>
              ${fixBadge}
            </div>
            <div class="doctor-check-msg">${escapeHtml(c.Message)}</div>
            ${detail}
          </div>
        </div>`;
    }).join("");

    $("#doctor-checks").innerHTML = checksHtml;
    $("#doctor-content").classList.remove("hidden");
    renderRepairPlans(r);
  }

  // 修复方案元数据：每个级别的名称、范围描述、适用场景、示例。
  // 文案与 doctor 包的 RepairLevel 语义对齐（1=轻度，2=中度，3=深度）。
  const REPAIR_PLAN_META = {
    1: {
      title: "轻度修复",
      desc: "执行安全、可逆的调整，不修改用户数据。适合环境或配置层面的小问题。",
      what: "环境变量提示、设置文件补全、缓存类修正",
    },
    2: {
      title: "中度修复",
      desc: "修改配置或插件列表解决冲突，操作前自动备份、失败自动回滚。适合插件不兼容或配置损坏。",
      what: "禁用损坏的第三方插件、移除失效的配置引用，全程备份可还原",
    },
    3: {
      title: "深度修复",
      desc: "删除或重建损坏的数据与状态，无法回滚。适合数据文件损坏等严重问题。",
      what: "清理损坏的会话记录、重建异常存储",
    },
  };

  // 渲染修复方案区：按诊断结果动态列出每级可修项，并标记最高建议级别。
  function renderRepairPlans(r) {
    const failed = r.Checks.filter((c) => !c.OK);
    // 可自动修复的检查项，按建议级别分组。
    const fixableByLevel = (level) =>
      failed.filter((c) => c.Fixable && c.SuggestedLevel <= level).map((c) => c.Name);
    // 推荐级别：所有可修项中最大的 required 级别；无可修项则不显示。
    const maxLevel = failed.reduce((acc, c) =>
      (c.Fixable && c.SuggestedLevel > acc ? c.SuggestedLevel : acc), 0);

    if (maxLevel === 0) {
      $("#repair-plans").classList.add("hidden");
      return;
    }

    const cards = [1, 2, 3].map((level) => {
      const meta = REPAIR_PLAN_META[level];
      const items = fixableByLevel(level);
      const recommended = level === maxLevel;
      const itemText = items.length > 0
        ? items.slice(0, 4).map((n) => `<span class="repair-plan-item">${escapeHtml(n)}</span>`).join("")
        : `<span class="repair-plan-item">本级无待修复项</span>`;
      const recoBadge = recommended
        ? `<span class="repair-plan-reco">★ 建议优先执行（覆盖 ${items.length} 项）</span>` : "";
      return `
        <div class="repair-plan${recommended ? " recommended" : ""}">
          <div class="repair-plan-head">
            <span class="repair-level-badge">L${level}</span>
            <span class="repair-plan-title">${meta.title}</span>
          </div>
          <div class="repair-plan-desc">${meta.desc}</div>
          <div class="repair-plan-items">${itemText}${recoBadge}</div>
          <button class="btn ${level === 2 ? "btn-warn" : level === 3 ? "btn-danger" : "btn-primary"} repair-plan-btn"
                  data-repair-level="${level}" ${recommended ? "" : "disabled"}>执行${meta.title}（L${level}）</button>
        </div>`;
    }).join("");

    $("#repair-plans").innerHTML = cards;
    $("#repair-plans").classList.remove("hidden");
    // 卡片按钮统一绑定：只允许执行诊断建议的级别（disabled 卡不可点）。
    document.querySelectorAll("[data-repair-level]").forEach((btn) => {
      btn.addEventListener("click", () => runRepair(Number(btn.dataset.repairLevel)));
    });
  }

  $("#doctor-start").addEventListener("click", () => runDoctor("", true));
  // "重新诊断"：忽略缓存，强制重新检测。
  $("#doctor-refresh").addEventListener("click", () => runDoctor("", true));

  async function runRepair(level) {
    // 修复进行中：按钮已禁用，重复点击直接忽略。
    if (diagnosisState.repairing) return;
    diagnosisState.repairing = true;
    setRepairButtonsBusy(true);
    // 修复面板：显示进行中状态
    const statusEl = $("#repair-panel-status");
    const bodyEl = $("#repair-panel-body");
    if (statusEl) { statusEl.textContent = "修复中…"; statusEl.className = "repair-panel-status running"; }
    if (bodyEl) bodyEl.innerHTML = '<div class="rp-row">正在执行修复，请稍候…</div>';
    $("#doctor-repair-output").classList.remove("hidden");
    try {
      const result = await api().RunDoctorRepair(level);
      renderRepairOutput(result);
      // 修复后重新诊断
      const report = await runDoctor("修复完成，正在复查…", true);
      // 诊断全绿 → 自动启动应用：安全模式下先退出安全模式再用正常配置重启，
      // 免去用户手动到服务器弹框里点"启动"。
      if (maybeAutoStartAfterRepair(report)) {
        if (statusEl) { statusEl.textContent = "✓ 修复成功"; statusEl.className = "repair-panel-status ok"; }
        const msg = document.createElement("div");
        msg.className = "rp-row ok";
        msg.textContent = "修复成功，正在启动应用…";
        bodyEl.appendChild(msg);
        try {
          if (state.status && state.status.SafeMode) {
            applyStatus(await api().ExitSafeMode());
          } else {
            applyStatus(await api().StartServer());
          }
          // 启动应用成功 → 关闭诊断弹框，右下角提示原因与处理方式。
          const reason = diagnosisState.lastCulprit
            ? `启动失败原因：${diagnosisState.lastCulprit} 异常`
            : "启动失败原因已自动修复";
          setTimeout(() => {
            closeModal("doctor-modal");
            showRepairToast(`${reason}，已自动移除/修复并恢复启动。`, "ok");
          }, 1500);
        } catch (e) {
          showRepairToast("自动启动失败，请稍后手动点「启动」重试。", "warn");
        }
      }
    } catch (e) {
      if (statusEl) { statusEl.textContent = "✗ 修复失败"; statusEl.className = "repair-panel-status error"; }
      bodyEl.innerHTML = `<div class="rp-row error">${escapeHtml(e.message)}</div>`;
      showRepairToast("修复失败：" + e.message, "warn");
    } finally {
      diagnosisState.repairing = false;
      setRepairButtonsBusy(false);
    }
  }

  api().Status().then((s) => applyStatus(s));
}


/* ---------- 终端模块（xterm.js） ---------- */

/**
 * 字号偏好的取值范围与默认值。上限 20px 是弹框 900px 宽下仍能容纳常用命令行的
 * 边界，下限 10px 保证还能读；默认值与 xterm 创建时的字号一致。
 */
const TERMINAL_FONT_SIZE = { min: 10, max: 20, default: 14 };

/** 字号偏好的存储键：全会话共用一份，写在 localStorage 里跨启动保留。 */
const TERMINAL_FONT_SIZE_KEY = "dsh-desktop.terminal.fontSize";

/**
 * 终端状态管理：每个会话持有一个独立的 xterm 实例与挂载节点。
 * 切换标签时整体搬运 DOM 节点（而非销毁重建），保留各会话的滚动历史、
 * 光标位置与进程交互状态 —— 与桌面终端多标签行为一致。
 */
const terminalState = {
  sessions: {},   // sessionId -> { id, title, status, exitCode, term, fitAddon, holder }
  activeId: null, // 当前激活的会话 ID
  fontSize: TERMINAL_FONT_SIZE.default, // 由 initTerminal 从偏好存储恢复
};

/**
 * xterm 主题色：styles.css 的 .modal-terminal 用同一组色值画 xterm 画布之外
 * 那一层（头部、标签栏、留白），两处改动必须同步。xterm 的 theme 选项只接受
 * 具体色值（不解析 CSS 变量），因此在此静态映射。
 */
const TERMINAL_THEME = {
  background: "#1a1a1a",
  foreground: "#d4d4d4",
  cursor: "#d4d4d4",
  cursorAccent: "#1a1a1a",
  selectionBackground: "#3b4252",
  black: "#000000",
  red: "#f14c4c",
  green: "#23d18b",
  yellow: "#e2e210",
  blue: "#3b8eea",
  magenta: "#bc3fbc",
  cyan: "#29b7d3",
  white: "#e5e5e5",
  brightBlack: "#666666",
  brightRed: "#f14c4c",
  brightGreen: "#23d18b",
  brightYellow: "#f5f543",
  brightBlue: "#3b8eea",
  brightMagenta: "#bc3fbc",
  brightCyan: "#29b7d3",
  brightWhite: "#ffffff",
};

/** 把字号夹进可调范围：按钮、快捷键与记忆里的脏数据共用这一处收敛。 */
function clampTerminalFontSize(size) {
  return Math.min(TERMINAL_FONT_SIZE.max, Math.max(TERMINAL_FONT_SIZE.min, size));
}

/**
 * 读取记忆的终端字号。
 * WebKit 在禁用存储（隐私模式、无盘容器）时读写都会抛，字号只是便利项，
 * 读写失败一律退回默认值，不能让它影响终端可用性。
 * @returns {number} 夹进可调范围的字号
 */
function loadTerminalFontSize() {
  try {
    const raw = window.localStorage ? window.localStorage.getItem(TERMINAL_FONT_SIZE_KEY) : null;
    const size = Number.parseInt(raw, 10);
    if (Number.isInteger(size)) return clampTerminalFontSize(size);
  } catch (e) {
    // 读不到就是没记住，退默认值
  }
  return TERMINAL_FONT_SIZE.default;
}

/**
 * 刷新字号读数的文本与两端按钮的可用状态：到达边界时按钮置灰，
 * 让"再点也没用"这件事在界面上可见，而不是点了没反应。
 * @param {number} size - 当前字号
 */
function renderTerminalFontSize(size) {
  const label = $("#terminal-font-size");
  if (label) label.textContent = String(size);
  const dec = $("#terminal-font-dec");
  if (dec) dec.disabled = size <= TERMINAL_FONT_SIZE.min;
  const inc = $("#terminal-font-inc");
  if (inc) inc.disabled = size >= TERMINAL_FONT_SIZE.max;
}

/**
 * 设定终端字号并记住选择。所有会话共用同一偏好，新建会话也按它创建。
 * 字号变了字符单元格尺寸随之变化，必须重新 fit，否则行列与 PTY 侧对不上，
 * 全屏程序会按错误尺寸重绘。
 * @param {number} size - 目标字号，越界值会被夹进可调范围
 */
function setTerminalFontSize(size) {
  const next = clampTerminalFontSize(size);
  terminalState.fontSize = next;
  for (const session of Object.values(terminalState.sessions)) {
    session.term.options.fontSize = next;
  }
  fitActiveTerminal();
  renderTerminalFontSize(next);
  try {
    if (window.localStorage) window.localStorage.setItem(TERMINAL_FONT_SIZE_KEY, String(next));
  } catch (e) {
    // 写不进去只影响"下次打开还记得"，本次缩放照常生效
  }
}

/**
 * 把当前激活会话的选中文本复制到系统剪贴板。
 * 走 Wails 注入的 window.runtime.ClipboardSetText（GTK 实现，容器内可用）。
 */
function terminalCopySelection() {
  const session = terminalState.sessions[terminalState.activeId];
  if (!session || !session.term.hasSelection()) return;
  const text = session.term.getSelection();
  session.term.clearSelection();
  if (window.runtime && window.runtime.ClipboardSetText) {
    window.runtime.ClipboardSetText(text).catch((e) => {
      console.warn("terminal copy failed:", e && e.message);
    });
  }
}

/**
 * 把系统剪贴板文本粘贴进当前激活会话的 PTY。
 * 粘贴内容作为输入直接写入终端（与手工键入等价），换行符原样传递，
 * 由 shell 自己决定如何执行 —— 避免粘贴多行命令时被意外批量执行
 * 的问题留给 shell 的 bracketed-paste 机制处理（bash 默认开启）。
 */
async function terminalPaste() {
  const session = terminalState.sessions[terminalState.activeId];
  if (!session || session.status !== "running") return;
  if (!window.runtime || !window.runtime.ClipboardGetText) return;
  try {
    const text = await window.runtime.ClipboardGetText();
    if (text) await api().TerminalWrite(terminalState.activeId, text);
  } catch (e) {
    console.warn("terminal paste failed:", e && e.message);
  }
}

/**
 * 创建新的终端会话：先挂载 xterm 实例并 fit 出真实行列，
 * 再以该尺寸启动 PTY，保证首屏布局与 shell 的 $COLUMNS/$LINES 一致。
 * @param {string} [title] - 会话标题（缺省自动编号）
 * @returns {Promise<string>} 会话 ID
 */
async function createTerminalSession(title) {
  const content = $("#terminal-content");
  if (!content) throw new Error("终端容器不存在");
  if (typeof Terminal === "undefined" || typeof FitAddon === "undefined") {
    throw new Error("xterm.js 未加载（vendor/xterm.js）");
  }

  // 每个会话独立的包装节点：term.open 只允许调用一次，
  // 之后通过移动该节点在标签间切换
  const holder = document.createElement("div");
  holder.className = "terminal-holder";

  // 字体就绪前先占位：下面最多等 2s，这段内容区若是空的，用户看到的就是一块
  // 没有任何说明的黑屏，分不清是在启动还是卡住了。占位节点在 open 前被替换。
  content.innerHTML = '<div class="terminal-loading">正在启动终端…</div>';

  // 等随包字体就绪再创建实例：xterm 在 open 时测量字符单元格，
  // 字体晚到会测出与实际渲染不一致的行列尺寸。加载失败或超时
  // 只回退系统等宽字体，绝不阻塞终端创建。
  try {
    const base = "14px 'JetBrains Mono'";
    await Promise.race([
      Promise.all([
        document.fonts.load("500 " + base),
        document.fonts.load("italic 500 " + base),
        document.fonts.load("bold " + base),
        document.fonts.load("italic bold " + base),
      ]),
      new Promise((resolve) => setTimeout(resolve, 2000)),
    ]);
  } catch (error) {
    // DOM stub 测试环境没有 document.fonts（真实 webview 异常同理）：
    // 仅影响字形，按系统回退字体渲染，不中断建会话流程。
  }

  const term = new Terminal({
    theme: TERMINAL_THEME,
    // JetBrains Mono 由 styles.css 的 @font-face 随包提供，不依赖系统
    // fontconfig；其余项仅在 webview 字体加载失败时兜底。原栈里的
    // Cascadia Code/Menlo/Consolas 在 deepin 上永远缺失，只添空查，已删。
    fontFamily: '"JetBrains Mono", "Noto Sans Mono", "DejaVu Sans Mono", "Liberation Mono", monospace',
    // 字号取全会话共享的偏好：缩放后新建的会话与已有会话保持同一尺寸
    fontSize: terminalState.fontSize,
    fontWeight: 500,
    lineHeight: 1.25,
    letterSpacing: 0,
    cursorBlink: true,
    scrollback: 5000,
  });
  const fitAddon = new FitAddon.FitAddon();
  term.loadAddon(fitAddon);

  // 先挂到容器再 open + fit：xterm 需要真实布局尺寸才能测出字符单元格大小
  content.replaceChildren(holder);
  term.open(holder);
  fitAddon.fit();

  const sessionId = await api().TerminalStart("", "", term.cols, term.rows);
  terminalState.sessions[sessionId] = {
    id: sessionId,
    title: title || "终端 " + (Object.keys(terminalState.sessions).length + 1),
    status: "running",
    exitCode: null,
    term,
    fitAddon,
    holder,
  };

  // 按键逐字符直达 PTY：bash readline 实时回显，与真终端一致。
  // 控制字符（Ctrl+C=\x03、Tab=\t、方向键转义序列等）由 xterm 按
  // 终端协议编码，无需前端逐个拦截。
  term.onData((data) => {
    api().TerminalWrite(sessionId, data).catch((e) => {
      console.warn("terminal write failed:", e && e.message);
    });
  });

  // fit 后行列变化 → 同步 PTY 尺寸（SIGWINCH），保证 vim/top 等全屏程序
  // 的重绘正确
  term.onResize(({ cols, rows }) => {
    api().TerminalResize(sessionId, cols, rows).catch(() => {
      // 会话可能刚好已关闭，忽略
    });
  });

  // 终端区快捷键：Ctrl+Shift+C 复制 / Ctrl+Shift+V、Shift+Insert 粘贴
  // （Linux 终端标准约定）。返回 false 阻止 xterm 把该键继续当输入编码。
  term.attachCustomKeyEventHandler((ev) => {
    if (ev.type !== "keydown") return true;
    if (ev.ctrlKey && ev.shiftKey && ev.code === "KeyC") {
      terminalCopySelection();
      return false;
    }
    if ((ev.ctrlKey && ev.shiftKey && ev.code === "KeyV") ||
        (ev.shiftKey && ev.code === "Insert")) {
      terminalPaste();
      return false;
    }
    // 字号缩放：Ctrl+= / Ctrl+- / Ctrl+0（回到默认），与桌面终端约定一致。
    // 只认不带 Shift/Alt 的纯 Ctrl 组合，Ctrl+Shift+... 留给 shell 与上面的复制粘贴。
    if (ev.ctrlKey && !ev.shiftKey && !ev.altKey) {
      if (ev.code === "Equal" || ev.code === "NumpadAdd") {
        setTerminalFontSize(terminalState.fontSize + 1);
        return false;
      }
      if (ev.code === "Minus" || ev.code === "NumpadSubtract") {
        setTerminalFontSize(terminalState.fontSize - 1);
        return false;
      }
      if (ev.code === "Digit0" || ev.code === "Numpad0") {
        setTerminalFontSize(TERMINAL_FONT_SIZE.default);
        return false;
      }
    }
    // 标签快捷键由文档级监听处理（onTerminalKeydown）：这里返回 false 只是
    // 阻止 xterm 把 Ctrl+Shift+T/W 编成控制字符送进 PTY，事件照常冒泡。
    if (ev.ctrlKey && ev.shiftKey && (ev.code === "KeyT" || ev.code === "KeyW")) return false;
    if (ev.ctrlKey && ev.code === "Tab") return false;
    return true;
  });

  term.focus();
  return sessionId;
}

/**
 * 按容器当前尺寸重算激活会话的行列，并把结果经 onResize 同步给 PTY。
 * 容器隐藏时（弹窗未打开）fit 读到 0 尺寸会自行跳过，因此调用点无需判可见性；
 * 但布局刚变化的调用点必须先等一帧，否则 fit 出来的是旧尺寸。
 */
function fitActiveTerminal() {
  const session = terminalState.sessions[terminalState.activeId];
  if (!session) return;
  try { session.fitAddon.fit(); } catch (e) { /* 布局未就绪时忽略 */ }
}

/**
 * 切换终端弹框的最大化与还原。
 * 状态挂在卡片 #terminal-card 上（不是遮罩 #terminal-modal）：CSS 的规则是
 * .modal-terminal.is-maximized，只有卡片同时带 modal-terminal 这个类，
 * 打在遮罩上不会有任何视觉变化。
 * 尺寸切换后必须重新 fit：xterm 的行列由容器尺寸算出，不重算的话全屏程序
 * （vim/top）仍按旧行列重绘。关闭弹窗不重置——下次打开仍是用户上次选的尺寸。
 */
function toggleTerminalMaximized() {
  const card = $("#terminal-card");
  const btn = $("#terminal-max");
  if (!card) return;
  const maximized = card.classList.toggle("is-maximized");
  if (btn) {
    // 按钮语义随状态翻转：tooltip 与读屏标签都要跟着变，图标由 CSS 按状态切换
    const label = maximized ? "还原" : "最大化";
    btn.setAttribute("title", label);
    btn.setAttribute("aria-label", label);
  }
  // 等一帧让浏览器结算新尺寸，再让 xterm 按新尺寸重算行列
  requestAnimationFrame(fitActiveTerminal);
}

/**
 * 切换到指定会话：搬运对应 holder 节点到容器并聚焦。
 * 容器尺寸可能因标签栏换行而变化，切换后重新 fit 一次。
 * @param {string} sessionId - 目标会话 ID
 */
function switchTerminalSession(sessionId) {
  const session = terminalState.sessions[sessionId];
  const content = $("#terminal-content");
  if (!session || !content) return;

  terminalState.activeId = sessionId;
  content.replaceChildren(session.holder);
  fitActiveTerminal();
  session.term.focus();
  renderTerminalTabs();
}

/**
 * 渲染终端标签页列表。
 * 终态由 status/exitCode 决定：运行中实心点，退出后空心圈，非零退出码用危险色。
 * 多标签下失败会话要能在标签栏直接认出来，否则得逐个翻回它的输出找退出码。
 */
function renderTerminalTabs() {
  const tabsEl = $("#terminal-tabs");
  if (!tabsEl) return;
  const sessions = Object.values(terminalState.sessions);

  tabsEl.innerHTML = sessions
    .map((s) => {
      const isActive = s.id === terminalState.activeId;
      // 后端只保证 closed 状态带 exitCode 字段，取不到数值时按普通退出处理
      const hasCode = typeof s.exitCode === "number";
      const state = s.status === "running" ? "running"
        : hasCode && s.exitCode !== 0 ? "failed" : "exited";
      const statusDot = state === "running" ? "●" : "○";
      const exitSuffix = state === "running"
        ? ""
        : "（已退出" + (hasCode ? "，退出码 " + s.exitCode : "") + "）";
      return `
        <div class="terminal-tab terminal-tab-${state} ${isActive ? "active" : ""}" data-id="${s.id}" title="${escapeHtml(s.title + exitSuffix)}">
          <span class="terminal-tab-status">${statusDot}</span>
          <span class="terminal-tab-title">${escapeHtml(s.title)}</span>
          <button class="terminal-tab-close" data-close="${s.id}" title="关闭">×</button>
        </div>
      `;
    })
    .join("");

  tabsEl.querySelectorAll(".terminal-tab").forEach((tab) => {
    tab.addEventListener("click", (e) => {
      if (e.target.classList.contains("terminal-tab-close")) return;
      switchTerminalSession(tab.dataset.id);
    });
  });

  tabsEl.querySelectorAll(".terminal-tab-close").forEach((btn) => {
    btn.addEventListener("click", async (e) => {
      e.stopPropagation();
      await closeTerminalSession(btn.dataset.close);
    });
  });
}

/**
 * 把激活会话（没有会话时是空态）挂回内容区。
 * 关标签、以及新会话创建失败后都要走这里：占位提示可能已经把内容区换掉，
 * 不还原的话内容区会一直停在「正在启动终端…」，而其余会话其实还活着。
 * 空态文案也只有这一个出处。
 */
function restoreTerminalContent() {
  const content = $("#terminal-content");
  if (!content) return;
  const session = terminalState.sessions[terminalState.activeId];
  if (!session) {
    // 空态顺带指路：关掉最后一个标签后，下一步是点标签栏的 +（或 Ctrl+Shift+T）
    content.innerHTML = '<div class="terminal-empty">'
      + "<span>没有打开的终端</span>"
      + '<span class="terminal-empty-hint">点标签栏的 + 新建一个（Ctrl+Shift+T）</span>'
      + "</div>";
    return;
  }
  content.replaceChildren(session.holder);
  fitActiveTerminal();
  session.term.focus();
}

/**
 * 关闭指定会话：通知后端结束 PTY，销毁 xterm 实例并释放其内存
 * （scrollback 缓存可能较大），最后修正激活标签。
 * @param {string} sessionId - 要关闭的会话 ID
 */
async function closeTerminalSession(sessionId) {
  const session = terminalState.sessions[sessionId];
  if (!session) return;

  try {
    await api().TerminalClose(sessionId);
  } catch (e) {
    // 后端会话可能已随 shell 退出而清理，忽略
  }

  session.term.dispose();
  delete terminalState.sessions[sessionId];

  if (terminalState.activeId === sessionId) {
    const remaining = Object.keys(terminalState.sessions);
    terminalState.activeId = remaining.length > 0 ? remaining[remaining.length - 1] : null;
    restoreTerminalContent();
  }

  renderTerminalTabs();
}

/**
 * 新建会话并激活它。失败时还原内容区，不连累已有会话。
 * 「新建标签」按钮与 Ctrl+Shift+T 共用这一处实现。
 */
async function newTerminalSession() {
  try {
    const id = await createTerminalSession();
    terminalState.activeId = id;
    renderTerminalTabs();
  } catch (e) {
    console.error("create terminal failed:", e.message);
    // 内容区此刻停在新会话的占位提示上，把原会话搬回来
    restoreTerminalContent();
  }
}

/**
 * 按标签顺序循环切换会话，两端回绕。只有一个会话时不做无谓的搬运。
 * @param {number} direction - 1 向后，-1 向前
 */
function cycleTerminalSession(direction) {
  const ids = Object.keys(terminalState.sessions);
  if (ids.length < 2) return;
  const current = ids.indexOf(terminalState.activeId);
  const next = (current + direction + ids.length) % ids.length;
  switchTerminalSession(ids[next]);
}

/**
 * 终端弹窗的文档级快捷键；弹窗没打开时不接管任何按键。
 * 走文档级监听而非 xterm 的键处理：标签操作与焦点落在弹窗哪一处无关，
 * 而 xterm 只在它是某个会话的输入通道时才看得到按键。xterm 侧对同名组合
 * 返回 false，避免一次按键既切标签又被编成控制字符送进 PTY。
 * @param {KeyboardEvent} e - 键盘事件
 */
function onTerminalKeydown(e) {
  const modal = $("#terminal-modal");
  if (!modal || modal.classList.contains("hidden")) return;

  if (e.key === "Escape") {
    // 终端持有焦点时 Esc 属于 PTY（vim 退出插入模式、readline 的转义前缀），
    // 弹窗不能抢；只在焦点不在内容区时才关，并把焦点还给工具栏按钮。
    if (document.activeElement && document.activeElement.closest("#terminal-content")) return;
    closeModal("terminal-modal");
    const btn = $("#btn-terminal");
    if (btn) btn.focus();
    return;
  }

  if (e.ctrlKey && e.shiftKey && e.code === "KeyT") {
    e.preventDefault();
    newTerminalSession();
    return;
  }
  if (e.ctrlKey && e.shiftKey && e.code === "KeyW") {
    e.preventDefault();
    if (terminalState.activeId) closeTerminalSession(terminalState.activeId);
    return;
  }
  if (e.ctrlKey && e.code === "Tab") {
    e.preventDefault();
    cycleTerminalSession(e.shiftKey ? -1 : 1);
  }
}

/**
 * 打开终端弹窗：确保至少一个会话存在并聚焦。
 * 已有会话时仅重新挂载激活标签（弹窗可能经历了隐藏-重开）。
 */
async function openTerminalModal() {
  openModal("terminal-modal");

  const content = $("#terminal-content");
  if (!content) return;

  if (Object.keys(terminalState.sessions).length === 0) {
    try {
      const id = await createTerminalSession();
      terminalState.activeId = id;
      renderTerminalTabs();
    } catch (e) {
      content.innerHTML =
        '<div class="terminal-error">启动终端失败：' + escapeHtml(e.message) + "</div>";
      return;
    }
  } else if (terminalState.activeId) {
    const session = terminalState.sessions[terminalState.activeId];
    content.replaceChildren(session.holder);
    // 弹窗刚显示，等一帧布局稳定后再 fit + 聚焦
    requestAnimationFrame(() => {
      fitActiveTerminal();
      session.term.focus();
    });
  }
}

/**
 * 初始化终端模块：按钮、右键复制粘贴、事件监听。
 * 右键语义（经典终端约定）：有选中文本 → 复制并清除选区；
 * 无选区 → 粘贴剪贴板内容到 PTY。
 */
function initTerminal() {
  const btnTerminal = $("#btn-terminal");
  if (btnTerminal) {
    btnTerminal.addEventListener("click", openTerminalModal);
  }

  const btnNew = $("#terminal-new");
  if (btnNew) {
    btnNew.addEventListener("click", newTerminalSession);
  }

  // 字号缩放：按钮与 Ctrl± 快捷键共用 setTerminalFontSize 一处实现；
  // 先恢复记忆的字号，再按它刷新读数与两端按钮的可用状态。
  terminalState.fontSize = loadTerminalFontSize();
  renderTerminalFontSize(terminalState.fontSize);
  const btnFontDec = $("#terminal-font-dec");
  if (btnFontDec) {
    btnFontDec.addEventListener("click", () => setTerminalFontSize(terminalState.fontSize - 1));
  }
  const btnFontInc = $("#terminal-font-inc");
  if (btnFontInc) {
    btnFontInc.addEventListener("click", () => setTerminalFontSize(terminalState.fontSize + 1));
  }

  const content = $("#terminal-content");
  if (content) {
    content.addEventListener("contextmenu", async (e) => {
      e.preventDefault();
      const session = terminalState.sessions[terminalState.activeId];
      if (!session) return;
      if (session.term.hasSelection()) {
        terminalCopySelection();
      } else {
        await terminalPaste();
      }
    });
  }

  const btnMax = $("#terminal-max");
  if (btnMax) {
    btnMax.addEventListener("click", toggleTerminalMaximized);
  }

  const head = $("#terminal-head");
  if (head) {
    // 双击头部切换最大化：与自定义标题栏的双击习惯一致；头部按钮上双击不触发
    head.addEventListener("dblclick", (e) => {
      if (e.target.closest("button")) return;
      toggleTerminalMaximized();
    });
  }

  // 弹窗级快捷键统一由 onTerminalKeydown 处理（Esc 与标签快捷键共用一处入口）
  document.addEventListener("keydown", onTerminalKeydown);

  // 窗口尺寸变化 → 去抖后 fit 激活会话；其余会话在切回标签时再 fit
  let resizeTimer = null;
  window.addEventListener("resize", () => {
    if (!terminalState.activeId) return;
    if (resizeTimer) clearTimeout(resizeTimer);
    resizeTimer = setTimeout(fitActiveTerminal, 200);
  });

  // PTY 输出 → 写入对应会话的 xterm 实例（含非激活会话，
  // 后台标签的输出持续累积，切回时不丢内容）
  if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn("terminal:output", (payload) => {
      const event = payload.detail || payload;
      const session = terminalState.sessions[event.sessionId];
      if (session) {
        session.term.write(event.data);
      }
    });

    window.runtime.EventsOn("terminal:status", (payload) => {
      const event = payload.detail || payload;
      const session = terminalState.sessions[event.sessionId];
      if (session) {
        session.status = event.status;
        session.exitCode = event.exitCode;
        if (event.status === "closed") {
          session.term.write(
            "\r\n\x1b[90m[进程已退出，退出码: " + event.exitCode + "]\x1b[0m\r\n"
          );
        }
        renderTerminalTabs();
      }
    });
  }
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", init);
} else {
  init();
}

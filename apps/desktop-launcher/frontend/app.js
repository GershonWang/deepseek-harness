/* DeepSeek Harness 桌面壳前端逻辑。
 * 通过 Wails 绑定调用 Go 层（window.go.app.App.*），并监听状态事件。 */

"use strict";

const state = { status: null, prevConnectError: "", _stoppedTimer: null, _startupDoctorShown: false };

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

function bindExternalLinks() {
  $("#about-repo").addEventListener("click", (e) => {
    const url = $("#about-repo").getAttribute("href") || "";
    if (!isHttpUrl(url)) return; // 非 http(s) 保留默认行为
    if (!window.runtime || !window.runtime.BrowserOpenURL) return; // 预览模式
    e.preventDefault();
    openExternal(url);
  });

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
    text.textContent = host ? "外部服务 " + host : "外部服务";
  } else if (s.State === "running") {
    dot.className = "dot ok";
    const host = hostLabel(s.URL);
    text.textContent = "运行中" + (host ? " " + host : "") + (s.SafeMode ? " 🔒" : "") + (s.FreshHome ? " 🆕" : "");
  } else if (s.State === "starting") {
    dot.className = "dot warn";
    text.textContent = "启动中" + (s.SafeMode ? "（安全模式）" : "") + (s.FreshHome ? "（全新环境）" : "");
  } else if (s.State === "failed") {
    dot.className = "dot danger";
    text.textContent = "启动失败" + (s.LastExit ? " (" + s.LastExit + ")" : "");
  } else {
    dot.className = "dot muted";
    text.textContent = "已停止" + (s.LastExit ? " (" + s.LastExit + ")" : "");
  }

  // 目标：外部已连接 / 容器运行中 -> iframe；启动中 -> 加载页（预检占用时 ->
  // 预检页）；启动失败 -> 失败页（附失败原因）；手动停止（非重试间隙）-> 引导页。
  const frame = $("#harness");
  if (s.Target) {
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
function setDoctorSummary(htmlOrText, showRefresh) {
  const text = $("#doctor-summary-text");
  if (!text) return; // 结构未就绪（预览分支）
  text.innerHTML = htmlOrText;
  const btn = $("#doctor-refresh");
  btn.classList.toggle("hidden", !showRefresh);
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
  // 修复进行中：保持弹窗的"修复中…"状态，不响应任何状态事件去自动弹窗/
  // 重置标记 —— 修复期间 supervisor 状态抖动（如在重启）不能触发又一轮
  // "正在诊断…"的自动弹窗，打断用户看到的修复进度。
  if (diagnosisState.repairing) {
    if (s.State !== "failed") hideAutoDiagHint();
    return;
  }

  if (s.State !== "failed") {
    state._startupDoctorShown = false;
    // 退出失败态（用户重启/安全模式/修复后自动启动）：环境可能已变化，
    // 清空诊断缓存，进入下一次失败周期时重新检测而非展示旧结果。
    diagnosisState.lastReport = null;
    hideAutoDiagHint();
    hideDoctorAutoBanner();
    return;
  }

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
  btn.title = "已复制";
  btn.classList.add("copied");
  if (copyResetTimer) clearTimeout(copyResetTimer);
  copyResetTimer = setTimeout(() => {
    btn.innerHTML = COPY_ICON;
    btn.title = "复制服务地址";
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
    $("#ext-state").textContent = "已连接\n外部地址: " + s.ExternalURL;
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
const marketState = { category: "all", search: "", catalog: [], progress: {} };

// fmtSize 把字节数格式化为 "1.6 MB" 之类的可读文本；0/空返回空串。
function fmtSize(bytes) {
  const n = Number(bytes) || 0;
  if (n <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let v = n, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return (i === 0 ? String(v) : v.toFixed(1)) + " " + units[i];
}

// categoryLabel 分类 ID → 中文标签；未知分类原样返回。
function categoryLabel(cat) {
  return ({ "language-sdk": "语言 SDK", "compiler": "编译器", "modern-cli": "现代 CLI", "code-quality": "代码质量", "debug": "调试" })[cat] || cat;
}

function renderTools(t) {
  marketState.catalog = t.Catalog || [];
  // 缓存 Rows，供点击"内置"按钮时动态渲染
  marketState.builtinRows = t.Rows || [];
  renderMarketGrid();
  renderStatusbar(t);
  renderHostTools(t);
  renderUpdateBadge(t);
  $("#toolchain-notice").textContent = t.Notice || "";
}

// renderUpdateBadge 更新工具链图标的小红点和弹框内的更新提示条。
function renderUpdateBadge(t) {
  var count = Number(t.UpdateCount) || 0;
  var badge = $("#tools-update-badge");
  if (badge) badge.classList.toggle("hidden", count === 0);
  var banner = $("#market-update-banner");
  var text = $("#market-update-text");
  if (banner && text) {
    if (count > 0) {
      text.textContent = "检测到 " + count + " 个工具可更新";
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
    box.innerHTML = '<span class="builtin-empty">暂无内置工具信息</span>';
    return;
  }
  for (const r of rows) {
    const chip = document.createElement("span");
    chip.className = "builtin-chip" + (r.State === "installed" ? " ok" : " missing");
    chip.title = r.State === "installed" ? "已随包内置" : "内置工具缺失";
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
function updateProgressBar(id, ev) {
  const card = document.querySelector('.tool-card-item[data-tool-id="' + id + '"]');
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

// renderMarketGrid 渲染工具卡片网格（分类 + 搜索过滤后）。
function renderMarketGrid() {
  const grid = $("#market-grid");
  grid.innerHTML = "";
  const list = filteredCatalog();
  if (list.length === 0) {
    grid.innerHTML = "<div class='empty'>没有匹配的工具</div>";
    return;
  }
  for (const c of list) grid.appendChild(toolCard(c));
}

// toolCard 渲染单个工具卡片；已装与未装分支的动作不同。
function toolCard(c) {
  const el = document.createElement("div");
  el.className = "tool-card-item" + (c.Installed ? " installed" : "");
  el.dataset.toolId = c.ID; // 供进度事件定向更新
  const prog = marketState.progress[c.ID];
  const installing = !!prog && prog.Phase !== "done" && prog.Phase !== "error";

  const head = document.createElement("div");
  head.className = "tool-card-head";
  const name = document.createElement("span");
  name.className = "tool-card-name";
  name.textContent = c.Name;
  const status = document.createElement("span");
  if (c.Installed && c.HasUpdate) {
    status.className = "pill warn";
    status.textContent = "可更新";
  } else if (c.Installed) {
    status.className = "pill ok";
    status.textContent = "✓ 已安装";
  } else if (installing) {
    status.className = "pill warn";
    status.textContent = "安装中 " + (prog.Percent || 0) + "%";
  } else {
    status.className = "pill brand";
    status.textContent = "可安装";
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
    runtimeHint.textContent = "容器内已可用：" + c.RuntimeCmd
      + (c.RuntimeVersion ? " " + c.RuntimeVersion : "")
      + "（" + c.RuntimeSource + "）";
    runtimeHint.title = "该命令由玲珑容器环境提供，市场仓库尚未安装；通过市场安装后将由 ~/.dsh-tools 统一管理，注入 PATH 时优先使用";
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
    // 已装：版本切换下拉（只列已装版本）+ 两击确认卸载。
    const sel = document.createElement("select");
    sel.className = "version-select";
    sel.title = "切换激活版本";
    for (const v of c.InstalledVersions || []) {
      const opt = document.createElement("option");
      opt.value = v;
      opt.textContent = "v" + v + (v === c.ActiveVersion ? " · 当前" : "");
      if (v === c.ActiveVersion) opt.selected = true;
      sel.appendChild(opt);
    }
    sel.addEventListener("change", async () => {
      const err = await api().SetActiveToolVersion(c.ID, sel.value);
      if (err) { $("#toolchain-notice").textContent = err; }
      api().RefreshTools();
    });
    actions.appendChild(sel);

    const un = document.createElement("button");
    un.className = "btn btn-danger";
    un.textContent = "卸载";
    un.addEventListener("click", async () => {
      if (un.dataset.armed !== "1") {
        un.dataset.armed = "1";
        un.textContent = "确认卸载?";
        setTimeout(() => { if (un.dataset.armed === "1") { un.dataset.armed = "0"; un.textContent = "卸载"; } }, 2500);
        return;
      }
      const err = await api().UninstallTool(c.ID, sel.value);
      if (err) { $("#toolchain-notice").textContent = err; }
      api().RefreshTools();
    });
    actions.appendChild(un);
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
    b.textContent = installing ? "安装中…" : "安装" + (fmtSize(c.Size) ? " (" + fmtSize(c.Size) + ")" : "");
    b.disabled = installing;
    b.addEventListener("click", () => {
      b.disabled = true;
      b.textContent = "安装中…";
      api().InstallToolVersion(c.ID, sel ? sel.value : c.AvailableVersion);
    });
    actions.appendChild(b);
  }

  if (installing) {
    el.append(head, desc, meta, progress, pctLabel, actions);
  } else {
    el.append(head, desc, meta, actions);
  }
  // 运行时提示固定插在 meta 之后、动作区之前（append 顺序即 DOM 顺序）。
  if (runtimeHint) el.append(runtimeHint);
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
  if (rows.length > 0) parts.push("随包 " + ok + "/" + rows.length);
  parts.push("已装 " + installed + "/" + cats.length + " 个工具");
  if (total > 0) parts.push("总大小 " + fmtSize(total));
  if (t.Sandboxed && (t.HostTools || []).length > 0) parts.push("宿主挂载 " + t.HostTools.length + " 项");
  sb.textContent = parts.join("　·　");
}

// renderHostTools 渲染宿主挂载列表与扫描结果；开发态隐藏整个宿主导入区。
function renderHostTools(t) {
  const hostBox = $("#card-hosts");
  if (!t.Sandboxed) {
    hostBox.classList.add("hidden");
    const devMsg = "开发态：宿主命令本就在 PATH，宿主导入仅玲珑打包环境可用。";
    $("#toolchain-notice").textContent = t.Notice ? devMsg + " " + t.Notice : devMsg;
    return;
  }
  hostBox.classList.remove("hidden");
  const hl = $("#host-list");
  hl.innerHTML = "";
  for (const h of t.HostTools || []) {
    const row = document.createElement("div");
    row.className = "host-item";
    const rm = document.createElement("button");
    rm.className = "btn btn-danger";
    rm.textContent = "移除";
    rm.addEventListener("click", () => api().RemoveHostTool(h.Name));
    const mounted = h.Mounted
      ? "<span class='state-ok'>✓ 生效中</span>"
      : "<span class='state-missing'>配置已写入 · 重启应用后生效</span>";
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
    box.innerHTML = "<div class='empty'>未发现可导入的宿主工具链（可在 /opt、/usr/local、~/tools 等放工具目录后重扫）</div>";
    return;
  }
  for (const e of entries) {
    const row = document.createElement("div");
    row.className = "host-item";
    const add = document.createElement("button");
    add.className = "btn btn-primary";
    add.textContent = "挂载";
    add.addEventListener("click", async () => {
      const res = await api().AddHostTool(e.Source, e.Name);
      const hint = $("#host-hint");
      if (res.Error) { hint.className = "error"; hint.textContent = "挂载失败: " + res.Error; }
      else { hint.className = "hint"; hint.textContent = (res.Warning ? "⚠ " + res.Warning + "　" : "") + "已写入挂载配置，请重启应用后生效"; }
      api().RefreshTools();
    });
    row.innerHTML =
      "<span class='selectable host-name'>" + escapeHtml(e.Name) + "</span>" +
      "<span class='hint selectable'>" + escapeHtml(e.Tool) + (e.Version ? " " + escapeHtml(e.Version) : "") + "</span>" +
      "<span class='hint selectable'>" + escapeHtml(e.Source) + "</span>" +
      (e.Conflict ? "<span class='pill warn'>与已装重名</span>" : "");
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
    $("#about-version").textContent = "harness " + info.HarnessVersion + "   客户端 " + info.PackageVersion;
    $("#about-repo").textContent = info.Repo;
    $("#about-repo").href = info.Repo;
    openModal("about-modal");
  });

  document.querySelectorAll("[data-close]").forEach((b) =>
    b.addEventListener("click", () => closeModal(b.dataset.close)));

  document.querySelectorAll('input[name="mode"]').forEach((r) =>
    r.addEventListener("change", () => {
      if (state.status) renderServerDialog(state.status);
    }));

  $("#server-start").addEventListener("click", async () => {
    applyStatus(await api().StartServer());
  });
  $("#server-restart").addEventListener("click", async () => {
    applyStatus(await api().RestartServer());
  });
  $("#server-stop").addEventListener("click", async () => {
    applyStatus(await api().StopServer());
  });
  $("#server-copy").addEventListener("click", () => {
    copyServerAddress(state.status && state.status.URL);
  });
  $("#btn-safe-mode").addEventListener("click", async () => {
    applyStatus(await api().StartSafeMode());
  });
  $("#btn-exit-safe-mode").addEventListener("click", async () => {
    applyStatus(await api().ExitSafeMode());
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
    if (!confirm("将以全新的运行时环境启动（~/.dsh-fallback）：\n\n· 会话历史、模型设置、第三方插件均不可见\n· API Key 等凭证不迁移，需要重新配置\n· 原始 ~/.dsh 数据原样保留，可随时回到默认环境\n\n确认继续？")) {
      return;
    }
    applyStatus(await api().StartFreshHome());
  });
  $("#btn-exit-fresh-home").addEventListener("click", async () => {
    applyStatus(await api().ExitFreshHome());
  });

  $("#ext-connect").addEventListener("click", async () => {
    const url = $("#ext-url").value.trim();
    const err = await api().ConnectExternal(url);
    if (err) $("#dlg-error").textContent = err;
  });
  $("#ext-disconnect").addEventListener("click", () => api().DisconnectExternal());

  $("#tools-refresh").addEventListener("click", () => api().RefreshTools());

  // 分类页签：切换后高亮并重渲染网格。
  document.querySelectorAll("#market-tabs .market-tab").forEach((tab) => {
    tab.addEventListener("click", () => {
      document.querySelectorAll("#market-tabs .market-tab").forEach((x) => x.classList.remove("active"));
      tab.classList.add("active");
      marketState.category = tab.dataset.cat || "all";
      renderMarketGrid();
    });
  });

  // 搜索：输入即过滤（防抖可省，目录规模小）。
  $("#market-search").addEventListener("input", () => {
    marketState.search = $("#market-search").value;
    renderMarketGrid();
  });

  // 刷新远程索引：异步拉取，完成后推送一次状态。
  $("#market-refresh").addEventListener("click", async () => {
    const btn = $("#market-refresh");
    btn.disabled = true;
    btn.textContent = "刷新中…";
    await api().RefreshToolIndex();
    btn.disabled = false;
    btn.textContent = "刷新索引";
  });

  // 一键更新所有过时工具
  var updateAllBtn = $("#market-update-all");
  if (updateAllBtn) {
    updateAllBtn.addEventListener("click", async () => {
      if (updateAllBtn.disabled) return;
      updateAllBtn.disabled = true;
      updateAllBtn.textContent = "更新中…";
      var err = await api().UpdateAllTools();
      if (err) {
        $("#toolchain-notice").textContent = "更新失败: " + err;
        updateAllBtn.disabled = false;
        updateAllBtn.textContent = "一键更新";
      }
      // 成功时由 toolchain:status 事件刷新 UI 和按钮状态
    });
  }

  // 宿主导入向导：扫描常见宿主工具链根目录。
  $("#host-scan").addEventListener("click", async () => {
    const btn = $("#host-scan");
    btn.disabled = true;
    btn.textContent = "扫描中…";
    try {
      renderHostScan(await api().ScanHostTools());
    } finally {
      btn.disabled = false;
      btn.textContent = "扫描宿主";
    }
  });

  $("#host-add").addEventListener("click", async () => {
    const src = $("#host-path").value.trim();
    const name = $("#host-name").value.trim();
    if (!src) return;
    const res = await api().AddHostTool(src, name);
    $("#host-path").value = "";
    $("#host-name").value = "";
    const hint = $("#host-hint");
    if (res.Error) {
      hint.className = "error";
      hint.textContent = "挂载失败: " + res.Error;
    } else {
      hint.className = "hint";
      hint.textContent = (res.Warning ? "⚠ " + res.Warning + "　" : "") + "已写入挂载配置，请重启应用后生效";
    }
    api().RefreshTools();
  });
}

/* ---------- 启动 ---------- */

function init() {
  bindUI();

  if (!window.go || !window.go.app) {
    // 浏览器直接打开 index.html 的开发预览：无 Wails 运行时，仅展示引导页。
    $("#status-text").textContent = "未检测到 Wails 运行时（浏览器预览模式）";
    return;
  }

  window.runtime.EventsOn("harness:status", (s) => applyStatus(s));
  window.runtime.EventsOn("toolchain:status", (t) => renderTools(t));
  window.runtime.EventsOn("toolchain:progress", (p) => renderProgress(p));
  setupBuiltinToggle();

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
      setDoctorSummary(summaryText || "正在诊断…", false);
      renderDoctorReport(diagnosisState.lastReport);
      return diagnosisState.lastReport;
    }
    diagnosisState.running = true;
    // summaryText 可覆盖默认文案：修复后的复检用"修复完成，正在复查…"，
    // 与"又出问题了"的诊断区分开。
    setDoctorSummary(summaryText || "正在诊断…", false);
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
        setDoctorSummary("诊断失败: " + e.message, false);
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
      setDoctorSummary("诊断失败: " + r.Error, false);
      $("#doctor-start").classList.remove("hidden");
      $("#doctor-content").classList.add("hidden");
      return;
    }

    // 语义类而非内联色值：内联只能写死一套主题的颜色，浅色主题下会变成白底上的
    // 浅绿浅黄（对比度 1.8–2.5:1）。类名对应的颜色由 styles.css 按主题给出。
    const sevClass = { fatal: "sev-error", error: "sev-error", warning: "sev-warn", info: "sev-info" };
    const statusClass = (ok, sev) => ok ? "sev-ok" : (sevClass[sev] || "sev-muted");

    setDoctorSummary(
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
              <span class="hint" style="margin-left:8px">[${c.Category} / ${c.Severity}]</span>
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
 * 尺寸切换后必须重新 fit：xterm 的行列由容器尺寸算出，不重算的话全屏程序
 * （vim/top）仍按旧行列重绘。最大化状态挂在卡片上，关闭弹窗不重置——下次打开
 * 仍是用户上次选择的尺寸。
 */
function toggleTerminalMaximized() {
  const card = $("#terminal-modal");
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
    const content = $("#terminal-content");
    if (content) {
      if (terminalState.activeId) {
        content.replaceChildren(terminalState.sessions[terminalState.activeId].holder);
        terminalState.sessions[terminalState.activeId].term.focus();
      } else {
        content.innerHTML = '<div class="terminal-empty">没有打开的终端</div>';
      }
    }
  }

  renderTerminalTabs();
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
    btnNew.addEventListener("click", async () => {
      try {
        const id = await createTerminalSession();
        terminalState.activeId = id;
        renderTerminalTabs();
      } catch (e) {
        console.error("create terminal failed:", e.message);
      }
    });
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

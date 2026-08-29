/* DeepSeek Harness 桌面壳前端逻辑。
 * 通过 Wails 绑定调用 Go 层（window.go.app.App.*），并监听状态事件。 */

"use strict";

const state = { status: null, prevConnectError: "", _stoppedTimer: null, _startupDoctorShown: false };

// 诊断/修复的运行状态（跨弹窗关闭重开保持）：diagnosisRunning 期间复用同一次
// 检测结果，repairing 期间禁止再次点击修复按钮。
const diagnosisState = { running: false, repairing: false, lastCulprit: "" };

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

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
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
        frame.contentWindow.postMessage(
          { dshDesktop: true, type: "clipboard-image-result", data: data || "" },
          "*"
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

// 主舞台四选一展示：harness iframe / 引导页 / 启动加载页 / 启动失败页。
function showStageOnly(el) {
  for (const id of ["harness", "guidance", "loading-page", "failed-page"]) {
    const node = document.getElementById(id);
    node.classList.toggle("hidden", node !== el);
  }
}

function applyStatus(s) {
  state.status = s;

  const dot = $("#status-dot");
  const text = $("#status-text");

  if (s.Mode === "external") {
    dot.className = "dot ok";
    text.textContent = "外部服务 " + (s.ExternalURL || "");
  } else if (s.State === "running") {
    dot.className = "dot ok";
    text.textContent = "运行中 " + s.URL + (s.SafeMode ? " 🔒" : "");
  } else if (s.State === "starting") {
    dot.className = "dot warn";
    text.textContent = "启动中" + (s.SafeMode ? "（安全模式）" : "");
  } else if (s.State === "failed") {
    dot.className = "dot danger";
    text.textContent = "启动失败" + (s.LastExit ? " (" + s.LastExit + ")" : "");
  } else {
    dot.className = "dot muted";
    text.textContent = "已停止" + (s.LastExit ? " (" + s.LastExit + ")" : "");
  }

  // 目标：外部已连接 / 容器运行中 -> iframe；启动中 -> 加载页；
  // 启动失败 -> 失败页（附失败原因）；手动停止（非重试间隙）-> 引导页。
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
      showStageOnly($("#loading-page"));
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

// 自动诊断状态处理：失败页提示诊断中/完成；StartupDoctorReady 首次变 true 时
// 自动打开诊断弹窗并运行一次诊断。state._startupDoctorShown 保证每个失败周期
// 只自动弹窗一次，退出失败态（用户手动重启/安全模式）后重置，下一周期可再触发。
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
    hideAutoDiagHint();
    hideDoctorAutoBanner();
    return;
  }

  if (s.StartupDoctorReady) {
    setAutoDiagHint("诊断完成", false);
    if (!state._startupDoctorShown) {
      state._startupDoctorShown = true;
      if (runDoctor) {
        openModal("doctor-modal");
        runDoctor();
        showDoctorAutoBanner();
      }
    }
  } else if (s.StartupDiagnosing) {
    setAutoDiagHint("正在自动诊断问题…", true);
  } else {
    hideAutoDiagHint();
  }
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
  $("#server-state").textContent = stateText;

  if (s.State === "running") {
    $("#server-detail1").textContent = s.URL;
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
  $("#server-stop").disabled = !s.CanStop;

  // 安全模式：失败态显示「以插件安全模式启动」
  const failed = s.State === "failed" || s.State === "stopped";
  $("#safe-mode-row").classList.toggle("hidden", !failed || !!s.SafeMode);
  // 运行中且为安全模式，显示安全模式标识和退出按钮
  $("#safe-mode-active").classList.toggle("hidden", !s.SafeMode);

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

/* ---------- 工具 / 凭据 ---------- */

const TOOL_DETAILS = {
  node: "npm · npx · corepack · pnpm",
  python3: "pip · pip3",
  git: "git-lfs",
};

function pill(cls, text) {
  return "<span class='pill " + cls + "'>" + esc(text) + "</span>";
}

function dot(cls) {
  return "<span class='state-dot " + cls + "'></span>";
}

function renderTools(t) {
  // ------ 摘要条 ------
  const sum = $("#tool-summary");
  sum.innerHTML = "";
  const rows = t.Rows || [];
  const cats = t.Catalog || [];
  const chips = [];
  if (rows.length > 0) {
    const ok = rows.filter((r) => r.State === "installed").length;
    const all = ok === rows.length;
    chips.push("<span class='chip " + (all ? "chip-ok" : "chip-warn") + "'>随包 " + ok + "/" + rows.length + " ✓</span>");
  }
  if (cats.length > 0) {
    const installed = cats.filter((c) => c.State === "installed").length;
    const all = installed === cats.length;
    chips.push("<span class='chip " + (all ? "chip-ok" : "chip-brand") + "'>可安装 " + installed + "/" + cats.length + " ✓</span>");
  }
  if (t.Sandboxed && (t.HostTools || []).length > 0) {
    chips.push("<span class='chip'>挂载 " + t.HostTools.length + " 项</span>");
  }
  sum.innerHTML = chips.join("");

  // ------ 随包工具 ------
  const bl = $("#bundled-list");
  bl.innerHTML = "";
  if (rows.length === 0) bl.innerHTML = "<div class='empty'>无结果</div>";
  for (const row of rows) {
    const ok = row.State === "installed";
    const el = document.createElement("div");
    el.className = "tool-row";
    el.innerHTML =
      (ok ? dot("ok") : dot("missing")) +
      "<span class='tool-name'>" + esc(row.Name) + "</span>" +
      "<span class='tool-version'>" + (ok ? esc(row.Version) : "—") + "</span>" +
      (ok ? pill("ok", "✓ 已安装") : pill("danger", "✗ 缺失"));
    bl.appendChild(el);
    if (TOOL_DETAILS[row.Name]) {
      const d = document.createElement("div");
      d.className = "tool-detail";
      d.textContent = TOOL_DETAILS[row.Name];
      bl.appendChild(d);
    }
  }

  // ------ 一键安装 ------
  const cl = $("#catalog-list");
  cl.innerHTML = "";
  if (cats.length === 0) cl.innerHTML = "<div class='empty'>无结果</div>";
  for (const c of cats) {
    const installed = c.State === "installed";
    const installing = !installed && t.Installing === c.Name;
    let statusPill = "";
    if (installed) statusPill = pill("ok", "✓ 已安装");
    else if (installing) statusPill = pill("warn", "安装中…");
    else if (c.Pinned) statusPill = pill("brand", "可安装");
    else statusPill = pill("warn", "待配置");
    const version = installed ? (c.InstalledVersion || c.Version) : c.Version;
    const el = document.createElement("div");
    el.className = "tool-row";
    el.innerHTML =
      (installed ? dot("ok") : dot("brand")) +
      "<span class='tool-name'>" + esc(c.Label) + "</span>" +
      "<span class='tool-version'>" + esc(version) + "</span>" +
      statusPill;
    if (!installed && c.Pinned) {
      const b = document.createElement("button");
      b.className = "btn btn-primary";
      b.textContent = installing ? "安装中…" : "安装";
      b.disabled = installing;
      b.addEventListener("click", () => {
        b.disabled = true;
        b.textContent = "安装中…";
        api().InstallToolchain(c.Name);
      });
      el.appendChild(b);
    }
    cl.appendChild(el);
  }
  $("#toolchain-notice").textContent = t.Notice || "";

  // ------ 宿主挂载 ------
  const hostBox = $("#card-hosts");
  if (!t.Sandboxed) {
    hostBox.classList.add("hidden");
    const devMsg = "开发态：宿主命令本就在 PATH，宿主挂载仅玲珑打包环境可用。";
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
      "<span class='selectable host-name'>" + esc(h.Name) + "</span>" +
      "<span class='hint selectable'>" + esc(h.Source) + " → " + esc(h.Target) + "</span>" +
      "<span class='hint'>" + mounted + "</span>";
    row.appendChild(rm);
    hl.appendChild(row);
  }
  const hint = $("#host-hint");
  hint.textContent =
    "挂载为只读（工具箱需自写安装目录时不可用）；非家目录路径在部分系统环境可能挂载失败，" +
    "建议优先用上方一键安装或把工具放入家目录后再挂载；改动需重启应用生效。";
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
  $("#server-stop").addEventListener("click", async () => {
    applyStatus(await api().StopServer());
  });
  $("#btn-safe-mode").addEventListener("click", async () => {
    applyStatus(await api().StartSafeMode());
  });
  $("#btn-exit-safe-mode").addEventListener("click", async () => {
    applyStatus(await api().ExitSafeMode());
  });

  $("#ext-connect").addEventListener("click", async () => {
    const url = $("#ext-url").value.trim();
    const err = await api().ConnectExternal(url);
    if (err) $("#dlg-error").textContent = err;
  });
  $("#ext-disconnect").addEventListener("click", () => api().DisconnectExternal());

  $("#tools-refresh").addEventListener("click", () => api().RefreshTools());

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

  runDoctor = async function (summaryText) {
    // 检测已在进行：复用同一次检测（后台自动触发或用户点"诊断问题"后，
    // 弹框被关闭再打开不应二次触发重复检测），返回相同的结果。
    if (diagnosisState.running && diagnosisState.promise) {
      return diagnosisState.promise;
    }
    diagnosisState.running = true;
    // summaryText 可覆盖默认文案：修复后的复检用"修复完成，正在复查…"，
    // 与"又出问题了"的诊断区分开。
    setDoctorSummary(summaryText || "正在诊断…", false);
    $("#doctor-content").classList.add("hidden");
    $("#doctor-start").classList.add("hidden");
    diagnosisState.promise = (async () => {
      try {
        const r = await api().RunDoctor();
        renderDoctorReport(r);
        // 记录失败元凶（供修复后的 toast 说明原因）。
        const firstBad = (r && !r.Error && r.Checks || []).find((c) => !c.OK);
        diagnosisState.lastCulprit = firstBad ? firstBad.Name : "";
        return r;
      } catch (e) {
        setDoctorSummary("诊断失败: " + e.message, false);
        $("#doctor-start").classList.remove("hidden");
        return null;
      }
    })();
    try {
      return await diagnosisState.promise;
    } finally {
      diagnosisState.running = false;
      diagnosisState.promise = null;
    }
  };

  function renderDoctorReport(r) {
    if (r.Error) {
      setDoctorSummary("诊断失败: " + r.Error, false);
      $("#doctor-start").classList.remove("hidden");
      $("#doctor-content").classList.add("hidden");
      return;
    }

    const sevColor = { fatal: "#f48771", error: "#f48771", warning: "#cca700", info: "#75beff" };
    const statusColor = (ok, sev) => ok ? "#89d185" : (sevColor[sev] || "#ccc");

    setDoctorSummary(
      `<strong>共 ${r.Total} 项</strong>：` +
      `<span style="color:#89d185">✓ ${r.OK} 通过</span>，` +
      `<span style="color:#f48771">✗ ${r.Failed} 失败</span>` +
      (r.Fatal > 0 ? `（<span style="color:#f48771">${r.Fatal} 严重</span>）` : "") +
      (r.Fixable > 0 ? `，<span style="color:#cca700">${r.Fixable} 项可自动修复</span>` : ""),
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
      const color = statusColor(c.OK, c.Severity);
      const fixBadge = c.Fixable && !c.OK
        ? `<span class="pill warn" style="margin-left:auto">可修复 L${c.SuggestedLevel}</span>` : "";
      const detail = c.Detail && !c.OK
        ? `<div class="doctor-detail">${escapeHtml(c.Detail)}</div>` : "";
      return `
        <div class="doctor-check-row">
          <span class="doctor-check-icon" style="color:${color}">${icon}</span>
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

  $("#doctor-start").addEventListener("click", runDoctor);
  $("#doctor-refresh").addEventListener("click", runDoctor);

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
      const report = await runDoctor("修复完成，正在复查…");
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

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", init);
} else {
  init();
}
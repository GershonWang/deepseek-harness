/* DeepSeek Harness 桌面壳前端逻辑。
 * 通过 Wails 绑定调用 Go 层（window.go.app.App.*），并监听状态事件。 */

"use strict";

const state = { status: null, prevConnectError: "", _stoppedTimer: null, _startupDoctorShown: false };

// 由 init() 在 Wails 环境赋值为真实诊断函数；浏览器预览分支保持 null，
// applyStatus 的自动弹窗逻辑据此安全跳过（不弹窗、不跑诊断）。
let runDoctor = null;

const $ = (sel) => document.querySelector(sel);

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

function setAutoDiagHint(text) {
  const el = autoDiagHintEl();
  el.textContent = text;
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

// 自动诊断状态处理：失败页提示诊断中/完成；StartupDoctorReady 首次变 true 时
// 自动打开诊断弹窗并运行一次诊断。state._startupDoctorShown 保证每个失败周期
// 只自动弹窗一次，退出失败态（用户手动重启/安全模式）后重置，下一周期可再触发。
// 预览模式（runDoctor 为 null）只记录标记，不弹窗不诊断。
function updateStartupDoctor(s) {
  if (s.State !== "failed") {
    state._startupDoctorShown = false;
    hideAutoDiagHint();
    hideDoctorAutoBanner();
    return;
  }

  if (s.StartupDoctorReady) {
    setAutoDiagHint("诊断完成");
    if (!state._startupDoctorShown) {
      state._startupDoctorShown = true;
      if (runDoctor) {
        openModal("doctor-modal");
        runDoctor();
        showDoctorAutoBanner();
      }
    }
  } else if (s.StartupDiagnosing) {
    setAutoDiagHint("正在自动诊断问题…");
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

  runDoctor = async function () {
    $("#doctor-summary").textContent = "正在诊断…";
    $("#doctor-content").classList.add("hidden");
    $("#doctor-start").classList.add("hidden");
    try {
      const r = await api().RunDoctor();
      renderDoctorReport(r);
    } catch (e) {
      $("#doctor-summary").textContent = "诊断失败: " + e.message;
      $("#doctor-start").classList.remove("hidden");
    }
  };

  function renderDoctorReport(r) {
    if (r.Error) {
      $("#doctor-summary").textContent = "诊断失败: " + r.Error;
      $("#doctor-start").classList.remove("hidden");
      $("#doctor-content").classList.add("hidden");
      return;
    }

    const sevColor = { fatal: "#f48771", error: "#f48771", warning: "#cca700", info: "#75beff" };
    const statusColor = (ok, sev) => ok ? "#89d185" : (sevColor[sev] || "#ccc");

    $("#doctor-summary").innerHTML =
      `<strong>共 ${r.Total} 项</strong>：` +
      `<span style="color:#89d185">✓ ${r.OK} 通过</span>，` +
      `<span style="color:#f48771">✗ ${r.Failed} 失败</span>` +
      (r.Fatal > 0 ? `（<span style="color:#f48771">${r.Fatal} 严重</span>）` : "") +
      (r.Fixable > 0 ? `，<span style="color:#cca700">${r.Fixable} 项可自动修复</span>` : "");

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
  }

  function escapeHtml(s) {
    return String(s ?? "").replace(/[&<>"']/g, (m) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[m]));
  }

  $("#doctor-start").addEventListener("click", runDoctor);
  $("#doctor-refresh").addEventListener("click", runDoctor);

  async function runRepair(level) {
    $("#doctor-repair-output").classList.remove("hidden");
    $("#doctor-repair-output").textContent = "正在修复…";
    try {
      const result = await api().RunDoctorRepair(level);
      $("#doctor-repair-output").textContent = result;
      // 修复后重新诊断
      await runDoctor();
    } catch (e) {
      $("#doctor-repair-output").textContent = "修复失败: " + e.message;
    }
  }

  $("#doctor-repair-1").addEventListener("click", () => runRepair(1));
  $("#doctor-repair-2").addEventListener("click", () => runRepair(2));

  api().Status().then((s) => applyStatus(s));
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", init);
} else {
  init();
}
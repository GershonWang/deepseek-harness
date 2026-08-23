/* DeepSeek Harness 桌面壳前端逻辑。
 * 通过 Wails 绑定调用 Go 层（window.go.app.App.*），并监听状态事件。 */

"use strict";

const state = { status: null, prevConnectError: "" };

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

/* ---------- 状态渲染 ---------- */

function applyStatus(s) {
  state.status = s;

  const dot = $("#status-dot");
  const text = $("#status-text");

  if (s.Mode === "external") {
    dot.className = "dot ok";
    text.textContent = "外部服务 " + (s.ExternalURL || "");
  } else if (s.State === "running") {
    dot.className = "dot ok";
    text.textContent = "运行中 " + s.URL;
  } else if (s.State === "starting") {
    dot.className = "dot warn";
    text.textContent = "启动中";
  } else if (s.State === "failed") {
    dot.className = "dot danger";
    text.textContent = "启动失败" + (s.LastExit ? " (" + s.LastExit + ")" : "");
  } else {
    dot.className = "dot muted";
    text.textContent = "已停止" + (s.LastExit ? " (" + s.LastExit + ")" : "");
  }

  // 目标：外部已连接 -> 外部 URL；容器运行中 -> 容器 URL；否则引导页。
  const frame = $("#harness");
  const guide = $("#guidance");
  if (s.Target) {
    if (frame.getAttribute("src") !== s.Target) frame.setAttribute("src", s.Target);
    frame.classList.remove("hidden");
    guide.classList.add("hidden");
  } else {
    frame.classList.add("hidden");
    frame.removeAttribute("src");
    guide.classList.remove("hidden");
  }

  renderServerDialog(s);
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
    $("#server-detail1").textContent = "地址: " + s.URL;
    $("#server-detail2").textContent = "PID: " + s.PID;
  } else if (s.State === "starting") {
    $("#server-detail1").textContent = "harness 正在启动…";
    $("#server-detail2").textContent = "";
  } else if (s.State === "failed") {
    $("#server-detail1").textContent = "原因: " + (s.LastExit || "");
    $("#server-detail2").textContent = "日志: ~/.cache/dsh-desktop/harness.log";
  } else {
    $("#server-detail1").textContent = "上次退出: " + (s.LastExit || "");
    $("#server-detail2").textContent = "";
  }

  $("#server-start").disabled = !s.CanStart;
  $("#server-stop").disabled = !s.CanStop;

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

function renderTools(t) {
  const tbody = $("#tools-table tbody");
  tbody.innerHTML = "";
  if (!t.Rows || t.Rows.length === 0) {
    tbody.innerHTML = "<tr><td colspan='3'>无结果</td></tr>";
    return;
  }
  for (const row of t.Rows) {
    const tr = document.createElement("tr");
    const ok = row.State === "installed";
    tr.innerHTML =
      "<td>" + esc(row.Name) + "</td>" +
      "<td>" + esc(row.Version) + "</td>" +
      "<td class='" + (ok ? "state-ok" : "state-missing") + "'>" + (ok ? "✓ 已安装" : "✗ 缺失") + "</td>";
    tbody.appendChild(tr);
  }
  $("#tools-install").textContent =
    "已安装: " + (t.Installed || "无") + "    可安装(启动器): " + (t.Installable || "");

  // 内置工具链一键安装清单
  const ctb = $("#catalog-table tbody");
  ctb.innerHTML = "";
  for (const c of t.Catalog || []) {
    const tr = document.createElement("tr");
    const installed = c.State === "installed";
    const statusText = installed
      ? "✓ " + (c.InstalledVersion || "已安装")
      : (c.Pinned ? "可安装" : "待配置 sha256");
    tr.innerHTML =
      "<td>" + esc(c.Label) + "</td>" +
      "<td>" + esc(c.Version) + "</td>" +
      "<td class='" + (installed ? "state-ok" : "") + "'>" + esc(statusText) + "</td>";
    const tdBtn = document.createElement("td");
    if (!installed && c.Pinned) {
      const b = document.createElement("button");
      b.className = "btn";
      b.textContent = "安装";
      b.addEventListener("click", () => {
        b.disabled = true;
        b.textContent = "安装中…";
        api().InstallToolchain(c.Name);
      });
      tdBtn.appendChild(b);
    }
    tr.appendChild(tdBtn);
    ctb.appendChild(tr);
  }
  $("#toolchain-notice").textContent = t.Notice || "";

  // 宿主命令挂载（仅沙箱环境显示）
  const hostBox = $("#hosttools-box");
  if (!t.Sandboxed) {
    hostBox.classList.add("hidden");
    $("#toolchain-notice").textContent = "开发态：宿主命令本就在 PATH，宿主挂载仅玲珑打包环境可用。";
  } else {
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
      row.innerHTML =
        "<span class='selectable host-name'>" + esc(h.Name) + "</span>" +
        "<span class='hint selectable'>" + esc(h.Source) + " → " + esc(h.Target) + "</span>";
      row.appendChild(rm);
      hl.appendChild(row);
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

  api().Status().then((s) => applyStatus(s));
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", init);
} else {
  init();
}
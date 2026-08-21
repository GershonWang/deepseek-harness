/* DeepSeek Harness 桌面壳前端逻辑。
 * 通过 Wails 绑定调用 Go 层（window.go.app.App.*），并监听状态事件。 */

"use strict";

const state = { status: null };

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

  // 已连接外部时，单选按钮强制镜像外部模式（Go 侧状态是唯一权威）。
  if (s.Mode === "external" && !externalMode) setRadio("external");

  const stateText = { running: "运行中", starting: "启动中", stopped: "已停止" }[s.State] || "已停止";
  $("#server-state").textContent = stateText;

  if (s.State === "running") {
    $("#server-detail1").textContent = "地址: " + s.URL;
    $("#server-detail2").textContent = "PID: " + s.PID;
  } else if (s.State === "starting") {
    $("#server-detail1").textContent = "harness 正在启动…";
    $("#server-detail2").textContent = "";
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
  $("#ext-error").textContent = s.ConnectError || "";
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

  const cred = t.CredSaved
    ? "✓ 已保存 (" + t.CredUser + ")\n存储位置: " + t.CredPath
    : "未保存\n存储位置: " + t.CredPath;
  $("#cred-status").textContent = cred;
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
  $("#btn-server").addEventListener("click", () => {
    openModal("server-modal");
    if (state.status) renderServerDialog(state.status);
  });
  $("#btn-settings").addEventListener("click", () => {
    openModal("settings-modal");
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
    if (err) $("#ext-error").textContent = err;
  });
  $("#ext-disconnect").addEventListener("click", () => api().DisconnectExternal());

  $("#tools-refresh").addEventListener("click", () => api().RefreshTools());

  $("#cred-save").addEventListener("click", async () => {
    const err = await api().SaveCredentials($("#cred-user").value.trim(), $("#cred-token").value);
    if (err) $("#cred-status").textContent = "保存失败: " + err;
    $("#cred-token").value = "";
  });
  $("#cred-clear").addEventListener("click", () => api().ClearCredentials());
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
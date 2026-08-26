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
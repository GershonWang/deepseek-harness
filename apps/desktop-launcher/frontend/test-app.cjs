/* DOM-stub 仿真测试：验证 app.js 的「启动失败自动诊断」前端逻辑。
 * 在 vm 隔离上下文里加载 app.js（严格模式脚本，不经过 Node 模块系统），
 * 用最小 DOM stub + Wails 桩驱动 harness:status 事件，断言：
 *   - StartupDiagnosing=true 时失败页显示"正在自动诊断问题…"
 *   - StartupDoctorReady=true 时自动打开诊断弹窗、调用 runDoctor、摘要上方出现提示条
 *   - 同周期重复事件不重复弹窗/重复诊断；退出 failed 后标记重置，下一周期可再触发
 *   - 浏览器预览（无 window.go）分支自动弹窗逻辑安全跳过
 *   - 现有 #btn-failed-doctor 手动入口仍可用
 * 运行：node --test frontend/test-app.cjs（工作目录 apps/desktop-launcher）
 *
 * 注意：init() 末尾的 api().Status() 在微任务里落地首个状态，用例在驱动事件前
 * 先 await flush() 使其结算，避免初始状态与后续事件竞态（真实运行中首个快照
 * 先于任何事件到达）。 */
"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const APP_CODE = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");

/* ---------- 最小 DOM stub ---------- */

class ClassList {
  constructor() {
    this._set = new Set();
  }
  add(...names) {
    for (const n of names) this._set.add(n);
  }
  remove(...names) {
    for (const n of names) this._set.delete(n);
  }
  contains(name) {
    return this._set.has(name);
  }
  toggle(name, force) {
    const on = force === undefined ? !this._set.has(name) : !!force;
    if (on) this._set.add(name);
    else this._set.delete(name);
    return on;
  }
}

class El {
  constructor(tag) {
    this.tagName = String(tag).toUpperCase();
    this.id = "";
    this.className = "";
    this._classList = new ClassList();
    this._text = "";
    this._html = "";
    this.children = [];
    this.parentNode = null;
    this.attrs = {};
    this.dataset = {};
    this.events = {};
    this.value = "";
    this.disabled = false;
    this.checked = false;
  }
  get classList() {
    return this._classList;
  }
  get textContent() {
    return this._text;
  }
  set textContent(v) {
    this._text = String(v);
    this.children = [];
  }
  get innerHTML() {
    return this._html;
  }
  set innerHTML(v) {
    this._html = String(v);
    this.children = [];
  }
  get nextSibling() {
    if (!this.parentNode) return null;
    const i = this.parentNode.children.indexOf(this);
    return this.parentNode.children[i + 1] || null;
  }
  setAttribute(k, v) {
    this.attrs[k] = v;
  }
  getAttribute(k) {
    return k in this.attrs ? this.attrs[k] : null;
  }
  removeAttribute(k) {
    delete this.attrs[k];
  }
  addEventListener(type, fn) {
    (this.events[type] ||= []).push(fn);
  }
  fire(type, arg) {
    for (const fn of this.events[type] || []) fn({ target: this, data: arg });
  }
  appendChild(child) {
    if (child.parentNode) child.parentNode.removeChild(child);
    child.parentNode = this;
    this.children.push(child);
    return child;
  }
  removeChild(child) {
    const i = this.children.indexOf(child);
    if (i >= 0) {
      this.children.splice(i, 1);
      child.parentNode = null;
    }
    return child;
  }
  insertBefore(child, ref) {
    if (child.parentNode) child.parentNode.removeChild(child);
    child.parentNode = this;
    const i = this.children.indexOf(ref);
    if (i < 0) this.children.push(child);
    else this.children.splice(i, 0, child);
    return child;
  }
}

/* 覆盖 app.js 用到的选择器：`#id`、`input[name="mode"]:checked`、
 * `input[name="mode"][value="x"]`、`[data-close]`。 */
function matchesSelector(el, sel) {
  sel = sel.trim();
  const byId = sel.match(/^#([\w-]+)/);
  if (byId) return el.id === byId[1];
  if (sel === "[data-close]") return "close" in el.dataset;
  if (sel.startsWith("[") && sel.endsWith("]")) {
    const inner = sel.slice(1, -1);
    if (inner.includes("=")) {
      const m = inner.match(/^([^=]+)="?([^"]*)"?$/);
      return m ? el.attrs[m[1]] === m[2] : false;
    }
    return inner in el.attrs || inner in el.dataset;
  }
  const attrSel = sel.match(/^(\w+)((?:\[[^=\]]+="[^"]*"\])+)(:checked)?$/);
  if (attrSel) {
    const [, tag, attrsStr, pseudo] = attrSel;
    const attrs = [...attrsStr.matchAll(/\[([^=\]]+)="([^"]*)"\]/g)];
    const base =
      el.tagName === tag.toUpperCase() &&
      attrs.every(([ , k, v]) => el.attrs[k] === v);
    if (pseudo === ":checked") return base && el.checked;
    return base;
  }
  return false;
}

function makeDocument() {
  const registry = [];
  const document = {
    readyState: "complete",
    addEventListener() {},
    createElement(tag) {
      const el = new El(tag);
      registry.push(el);
      return el;
    },
    getElementById(id) {
      return registry.find((el) => el.id === id) || null;
    },
    querySelector(sel) {
      return registry.find((el) => matchesSelector(el, sel)) || null;
    },
    querySelectorAll(sel) {
      return registry.filter((el) => matchesSelector(el, sel));
    },
  };
  document.body = document.createElement("body");
  return { document, registry };
}

/* 构建 app.js 引用的全部元素（扁平挂 body 下，选择器与父链无关），
 * 初始 hidden 类与 index.html 一致。 */
function buildHtml(document) {
  const ids = [
    "status-dot", "status-text",
    "harness", "guidance", "loading-page", "failed-page",
    "failed-reason", "btn-failed-doctor", "btn-failed-safe-mode", "failed-log-hint",
    "server-modal", "tools-modal", "about-modal", "doctor-modal",
    "container-panel", "external-panel", "dlg-error", "server-state",
    "server-detail1", "server-detail2", "server-start", "server-stop",
    "safe-mode-row", "safe-mode-active", "ext-connect", "ext-disconnect", "ext-state",
    "tool-summary", "bundled-list", "catalog-list", "toolchain-notice",
    "card-hosts", "host-list", "host-hint",
    "about-repo", "about-version", "win-min", "win-max", "win-close", "titlebar",
    "btn-server", "btn-tools", "btn-about", "btn-doctor",
    "doctor-summary", "doctor-content", "doctor-checks", "doctor-start",
    "doctor-refresh", "doctor-repair-1", "doctor-repair-2", "doctor-repair-output",
    "ext-url", "tools-refresh", "host-add", "host-path", "host-name",
    "btn-safe-mode", "btn-exit-safe-mode",
  ];
  for (const id of ids) {
    const el = document.createElement("div");
    el.id = id;
    document.body.appendChild(el);
  }
  // 与 index.html 一致的初始 hidden 态
  for (const id of [
    "harness", "loading-page", "failed-page",
    "server-modal", "tools-modal", "about-modal", "doctor-modal",
    "doctor-content", "doctor-repair-output",
    "safe-mode-row", "safe-mode-active", "external-panel",
  ]) {
    document.getElementById(id).classList.add("hidden");
  }

  const radioContainer = document.createElement("input");
  radioContainer.attrs.name = "mode";
  radioContainer.attrs.value = "container";
  radioContainer.value = "container";
  radioContainer.checked = true;
  document.body.appendChild(radioContainer);
  const radioExternal = document.createElement("input");
  radioExternal.attrs.name = "mode";
  radioExternal.attrs.value = "external";
  radioExternal.value = "external";
  document.body.appendChild(radioExternal);

  for (const modal of ["server-modal", "tools-modal", "about-modal", "doctor-modal"]) {
    const b = document.createElement("button");
    b.dataset.close = modal;
    document.body.appendChild(b);
  }
}

/* ---------- Wails / 运行时桩 ---------- */

function fakeReport() {
  return {
    Error: "",
    Total: 3,
    OK: 2,
    Failed: 1,
    Fatal: 0,
    Fixable: 1,
    Checks: [
      { ID: "c1", Name: "环境检查", Category: "env", Severity: "info", OK: true, Message: "OK", Detail: "", Fixable: false, SuggestedLevel: 0 },
      { ID: "c2", Name: "插件加载", Category: "plugin", Severity: "error", OK: false, Message: "加载失败", Detail: "detail", Fixable: true, SuggestedLevel: 2 },
      { ID: "c3", Name: "会话数据", Category: "session", Severity: "info", OK: true, Message: "正常", Detail: "", Fixable: false, SuggestedLevel: 0 },
    ],
  };
}

function baseStatus(over) {
  return {
    Mode: "container",
    State: "stopped",
    URL: "",
    PID: "",
    LastExit: "",
    ExternalURL: "",
    ConnectError: "",
    Target: "",
    Busy: false,
    StartupDiagnosing: false,
    StartupDoctorReady: false,
    CanStart: true,
    CanStop: false,
    CanConnect: true,
    CanDisconnect: false,
    SafeMode: false,
    ...over,
  };
}

function makeWails(runCalls) {
  const events = {};
  const app = {
    RunDoctor: async () => {
      runCalls.push("run");
      return fakeReport();
    },
    Status: async () => baseStatus(),
    StartServer: async () => baseStatus(),
    StopServer: async () => baseStatus(),
    StartSafeMode: async () => baseStatus(),
    ExitSafeMode: async () => baseStatus(),
    ConnectExternal: async () => "",
    DisconnectExternal: async () => baseStatus(),
    RefreshTools: async () => ({}),
    InstallToolchain: async () => ({}),
    RemoveHostTool: async () => ({}),
    AddHostTool: async () => ({}),
    About: async () => ({}),
    ReadClipboardImage: async () => "",
  };
  return {
    events,
    window: {
      go: { app: { App: app } },
      runtime: {
        EventsOn: (name, cb) => {
          events[name] = cb;
        },
        BrowserOpenURL() {},
        WindowMinimise() {},
        WindowToggleMaximise() {},
        Quit() {},
      },
      addEventListener() {},
    },
  };
}

/* 在独立 vm 上下文加载 app.js 并运行 init()；返回驱动句柄。
 * 末尾追加一行把模块级 applyStatus 暴露到 sandbox，供预览分支直接调用。 */
function loadApp({ hasWails = true } = {}) {
  const runCalls = [];
  const { document, registry } = makeDocument();
  buildHtml(document);
  const { window, events } = hasWails
    ? makeWails(runCalls)
    : { window: { addEventListener() {} }, events: {} };

  const sandbox = {
    console,
    setTimeout,
    clearTimeout,
    Promise,
    window,
    document,
    navigator: {},
  };
  vm.createContext(sandbox);
  const code = hasWails
    ? APP_CODE
    : APP_CODE + "\n;globalThis.__testApplyStatus = applyStatus;";
  vm.runInContext(code, sandbox, { filename: "app.js" });

  return {
    sandbox,
    document,
    registry,
    runCalls,
    status: (s) => {
      assert.equal(typeof events["harness:status"], "function",
        "harness:status 事件未注册（需 Wails 环境）");
      events["harness:status"](s);
    },
  };
}

const flush = () => new Promise((r) => setImmediate(r));

/* ---------- 用例 ---------- */

test("StartupDiagnosing 时失败页显示诊断中提示，不弹窗", async () => {
  const h = loadApp();
  await flush(); // 结算 init() 首个 Status() 快照，避免与事件竞态
  h.status(baseStatus({
    State: "failed",
    LastExit: "exit 1",
    StartupDiagnosing: true,
  }));

  const failedPage = h.document.getElementById("failed-page");
  assert.equal(failedPage.classList.contains("hidden"), false, "失败页应可见");
  assert.equal(h.document.getElementById("failed-reason").textContent, "exit 1");

  const hint = h.document.getElementById("auto-diag-hint");
  assert.ok(hint, "#auto-diag-hint 应已惰性创建");
  assert.equal(hint.classList.contains("hidden"), false);
  assert.equal(hint.textContent, "正在自动诊断问题…");

  assert.equal(
    h.document.getElementById("doctor-modal").classList.contains("hidden"),
    true, "诊断中不应弹窗");
  assert.equal(h.runCalls.length, 0, "诊断中不应调用 RunDoctor");
});

test("StartupDoctorReady 自动弹窗并运行诊断，同周期只触发一次", async () => {
  const h = loadApp();
  await flush();
  h.status(baseStatus({ State: "failed", StartupDiagnosing: true }));
  h.status(baseStatus({ State: "failed", StartupDoctorReady: true }));
  await flush();

  const modal = h.document.getElementById("doctor-modal");
  assert.equal(modal.classList.contains("hidden"), false, "应自动打开诊断弹窗");
  assert.equal(h.runCalls.length, 1, "应自动运行一次诊断");

  const banner = h.document.getElementById("doctor-auto-hint");
  assert.ok(banner, "摘要上方应有自动诊断提示条");
  assert.equal(banner.classList.contains("hidden"), false);
  assert.equal(banner.textContent, "检测到启动失败，已为你自动诊断");

  const summary = h.document.getElementById("doctor-summary");
  assert.match(summary.innerHTML, /共 3 项/, "摘要应渲染出诊断报告");
  assert.equal(
    h.document.getElementById("doctor-content").classList.contains("hidden"),
    false, "报告内容区应可见");
  assert.equal(
    h.document.getElementById("auto-diag-hint").textContent,
    "诊断完成");

  // 同周期重复 ready 事件：不重复弹窗/诊断/提示条
  h.status(baseStatus({ State: "failed", StartupDoctorReady: true }));
  await flush();
  assert.equal(h.runCalls.length, 1, "同周期不应重复运行诊断");
  assert.equal(h.document.querySelectorAll("#doctor-auto-hint").length, 1,
    "提示条不应重复创建");
});

test("退出 failed 后标记重置，新失败周期可再次自动弹窗", async () => {
  const h = loadApp();
  await flush();
  h.status(baseStatus({ State: "failed", StartupDoctorReady: true }));
  await flush();
  assert.equal(h.runCalls.length, 1);

  // 退出失败态（手动重启/安全模式）→ 提示与提示条收起，标记重置
  h.status(baseStatus({ State: "starting" }));
  assert.equal(
    h.document.getElementById("auto-diag-hint").classList.contains("hidden"),
    true, "退出失败态后失败页提示应隐藏");
  assert.equal(
    h.document.getElementById("doctor-auto-hint").classList.contains("hidden"),
    true, "退出失败态后提示条应隐藏");

  // 新失败周期：诊断中 → 就绪，再次自动弹窗
  h.status(baseStatus({ State: "failed", StartupDiagnosing: true }));
  assert.equal(
    h.document.getElementById("auto-diag-hint").textContent,
    "正在自动诊断问题…");
  h.status(baseStatus({ State: "failed", StartupDoctorReady: true }));
  await flush();
  assert.equal(h.runCalls.length, 2, "新失败周期应再次自动诊断");
  assert.equal(
    h.document.getElementById("doctor-modal").classList.contains("hidden"),
    false, "新周期应再次自动弹窗");
});

test("浏览器预览分支（无 Wails）：自动弹窗逻辑安全跳过", () => {
  const h = loadApp({ hasWails: false });
  const applyStatus = h.sandbox.__testApplyStatus;
  assert.equal(typeof applyStatus, "function", "预览分支 applyStatus 应可调用");

  applyStatus(baseStatus({
    State: "failed",
    StartupDiagnosing: true,
    StartupDoctorReady: true,
  }));

  assert.equal(
    h.document.getElementById("doctor-modal").classList.contains("hidden"),
    true, "预览分支不应弹窗");
  assert.equal(h.document.getElementById("doctor-auto-hint"), null,
    "预览分支不应创建提示条");
  assert.equal(h.runCalls.length, 0);
  // 失败页提示仍按状态渲染，不抛异常
  assert.ok(h.document.getElementById("auto-diag-hint"));
});

test("失败页「诊断问题」按钮仍可手动打开弹窗并运行诊断", async () => {
  const h = loadApp();
  await flush();
  await h.document.getElementById("btn-failed-doctor").fire("click");
  await flush();

  assert.equal(
    h.document.getElementById("doctor-modal").classList.contains("hidden"),
    false, "手动点击应打开诊断弹窗");
  assert.equal(h.runCalls.length, 1, "手动点击应运行诊断");
});
/* DOM-stub 仿真测试：验证 app.js 的「启动失败自动诊断」前端逻辑。
 * 在 vm 隔离上下文里加载 app.js（严格模式脚本，不经过 Node 模块系统），
 * 用最小 DOM stub + Wails 桩驱动 harness:status 事件，断言：
 *   - StartupDiagnosing=true 时失败页显示"正在自动诊断问题…"
 *   - StartupDoctorReady=true 时自动打开诊断弹窗、调用 runDoctor、摘要上方出现提示条
 *   - 同周期重复事件不重复弹窗/重复诊断；退出 failed 后标记重置，下一周期可再触发
 *   - 浏览器预览（无 window.go）分支自动弹窗逻辑安全跳过
 *   - 现有 #btn-failed-doctor 手动入口仍可用
 *   - 终端：会话建立走真实按钮路径，标签状态按运行/退出/非零退出码取语义类
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
    // style 用普通对象承接内联样式赋值（进度条 width 等），不需要完整 CSSOM。
    this.style = {};
  }
  get classList() {
    return this._classList;
  }
  // className 与 classList 同步（与真实 DOM 一致：设置整个 className 字符串
  // 会重建 classList 内容）。
  get className() {
    return [...this._classList._set].join(" ");
  }
  set className(v) {
    this._classList._set.clear();
    for (const c of String(v || "").split(/\s+/)) {
      if (c) this._classList._set.add(c);
    }
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
  remove() {
    if (this.parentNode) this.parentNode.removeChild(this);
  }
  // 真实 DOM 的 focus() 会把 activeElement 挪到自己身上；Esc 的归属判断
  // （焦点是否在终端内容区）依赖这一点，故这里如实模拟。
  focus() {
    if (this.ownerDocument) this.ownerDocument.activeElement = this;
  }
  // 双击处理器用 closest("button") 排除按钮上的双击（真实 DOM 同名 API）。
  closest(sel) {
    let node = this;
    while (node) {
      if (matchesSelector(node, sel)) return node;
      node = node.parentNode;
    }
    return null;
  }
  // 与真实 DOM 一致的 append：按参数顺序追加到末尾（toolCard 渲染用到）。
  append(...nodes) {
    for (const n of nodes) this.appendChild(n);
  }
  // app.js 用 replaceChildren 在两个容器间搬运终端节点（真实 DOM 同名 API）。
  // innerHTML/textContent 在本 stub 里是独立字符串，替换子节点时要一并清掉，
  // 否则换回真实会话后仍读得到上一次写入的占位提示文本。
  replaceChildren(...nodes) {
    this.children = [];
    this._html = "";
    this._text = "";
    for (const n of nodes) this.appendChild(n);
  }
  // 元素级查询走子树：renderTerminalTabs 设置 innerHTML 后要在标签容器里挂监听。
  // innerHTML 在本 stub 里只是字符串，不产生子节点，因此标签查询结果为空 ——
  // 标签相关断言读 innerHTML 文本，点击标签的行为不在 stub 覆盖范围内。
  querySelector(sel) {
    return this.querySelectorAll(sel)[0] || null;
  }
  querySelectorAll(sel) {
    const found = [];
    const walk = (node) => {
      for (const child of node.children) {
        if (matchesSelector(child, sel)) found.push(child);
        walk(child);
      }
    };
    walk(this);
    return found;
  }
}

/* 覆盖 app.js 用到的选择器：`#id`、`.class`、裸标签名（closest("button")）、
 * `input[name="mode"]:checked`、`input[name="mode"][value="x"]`、`[data-close]`。 */
function matchesSelector(el, sel) {
  sel = sel.trim();
  const byId = sel.match(/^#([\w-]+)/);
  if (byId) return el.id === byId[1];
  const byClass = sel.match(/^\.([\w-]+)/);
  if (byClass) return el.classList.contains(byClass[1]);
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
  // 裸标签名（closest("button") 这类）按 tagName 匹配。
  if (/^[a-z]+$/.test(sel)) return el.tagName === sel.toUpperCase();
  return false;
}

function makeDocument() {
  const registry = [];
  const listeners = {};
  const document = {
    readyState: "complete",
    activeElement: null,
    // 文档级键盘监听：Esc 关终端弹窗靠这条通道，用 fire("keydown", ev) 驱动。
    addEventListener(type, fn) {
      (listeners[type] ||= []).push(fn);
    },
    fire(type, event) {
      for (const fn of listeners[type] || []) fn(event);
    },
    createElement(tag) {
      const el = new El(tag);
      // 元素 focus() 要能更新 activeElement（Esc 的归属判断依赖它）
      el.ownerDocument = document;
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
    "harness", "guidance", "loading-page", "failed-page", "preflight-page",
    "failed-reason", "btn-failed-doctor", "btn-failed-safe-mode", "failed-log-hint",
    "preflight-icon", "preflight-title", "preflight-hint", "preflight-repairs",
    "preflight-issues", "preflight-actions", "preflight-note",
    "btn-preflight-deep-repair", "btn-preflight-safe-mode", "btn-preflight-fresh", "btn-preflight-skip",
    "server-modal", "tools-modal", "about-modal", "doctor-modal",
    "container-panel", "external-panel", "dlg-error", "server-state",
    "server-detail1", "server-detail2", "server-start", "server-restart", "server-stop", "server-copy",
    "server-address", "server-addr-origin", "server-addr-token",
    "safe-mode-row", "safe-mode-active", "ext-connect", "ext-disconnect", "ext-state",
    "fresh-home-active", "btn-exit-fresh-home",
    "tool-summary", "bundled-list", "catalog-list", "toolchain-notice",
    "card-hosts", "host-list", "host-hint",
    "about-repo", "about-version", "win-min", "win-max", "win-close", "titlebar",
    "btn-server", "btn-tools", "btn-about", "btn-doctor",
    "doctor-content", "doctor-checks", "doctor-start",
    "repair-plans", "doctor-repair-output",
    "repair-panel-status", "repair-panel-body", "repair-panel-backup",
    "ext-url", "tools-refresh", "host-add", "host-path", "host-name",
    "btn-safe-mode", "btn-exit-safe-mode",
    /* 工具市场 / 内置工具：bindUI 静态绑定（无判空）的元素必须存在 */
    "market-search", "market-refresh", "host-scan", "host-scan-list",
    "market-grid", "market-statusbar", "builtin-toggle", "builtin-panel",
    "repair-toast",
    /* 终端：initTerminal 判空引用，补齐以贴近真实 DOM */
    "btn-terminal", "terminal-new", "terminal-tabs", "terminal-content",
    "terminal-modal", "terminal-head", "terminal-max",
    "terminal-font-dec", "terminal-font-size", "terminal-font-inc",
  ];
  for (const id of ids) {
    const el = document.createElement("div");
    el.id = id;
    document.body.appendChild(el);
  }
  // 诊断摘要栏：行内含文本 span 与"重新诊断"按钮（按钮默认隐藏）
  const summary = document.createElement("div");
  summary.id = "doctor-summary";
  summary.classList.add("doctor-summary");
  const summaryText = document.createElement("span");
  summaryText.id = "doctor-summary-text";
  summaryText.className = "doctor-summary-text";
  summary.appendChild(summaryText);
  const refreshBtn = document.createElement("button");
  refreshBtn.id = "doctor-refresh";
  refreshBtn.classList.add("hidden");
  summary.appendChild(refreshBtn);
  document.body.appendChild(summary);
  // 与 index.html 一致的初始 hidden 态
  for (const id of [
    "harness", "loading-page", "failed-page", "preflight-page",
    "preflight-repairs", "preflight-issues", "preflight-actions", "preflight-note",
    "server-modal", "tools-modal", "about-modal", "doctor-modal", "terminal-modal",
    "doctor-content", "doctor-repair-output",
    "safe-mode-row", "safe-mode-active", "external-panel", "server-address",
    "fresh-home-active",
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

  for (const modal of ["server-modal", "tools-modal", "about-modal", "doctor-modal", "terminal-modal"]) {
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

/* localStorage 桩：终端字号偏好写在这里，用例据此断言记忆行为。
 * 直接暴露底层 Map，用例也能预置脏数据来验证兜底分支。 */
function makeStorage() {
  const store = new Map();
  return {
    store,
    getItem: (k) => (store.has(k) ? store.get(k) : null),
    setItem: (k, v) => { store.set(k, String(v)); },
    removeItem: (k) => { store.delete(k); },
  };
}

function makeWails(runCalls, overrides = {}) {
  const events = {};
  let terminalSeq = 0;
  const app = {
    RunDoctor: overrides.RunDoctor ?? (async () => {
      runCalls.push("run");
      return fakeReport();
    }),
    Status: async () => baseStatus(),
    StartServer: overrides.StartServer ?? (async () => {
      runCalls.push("start");
      return baseStatus();
    }),
    RestartServer: overrides.RestartServer ?? (async () => {
      runCalls.push("restart");
      return baseStatus();
    }),
    StopServer: async () => baseStatus(),
    StartSafeMode: async () => baseStatus(),
    ExitSafeMode: overrides.ExitSafeMode ?? (async () => {
      runCalls.push("exit-safe");
      return baseStatus();
    }),
    ConnectExternal: async () => "",
    DisconnectExternal: async () => baseStatus(),
    RefreshTools: async () => ({}),
    InstallToolchain: async () => ({}),
    RemoveHostTool: async () => ({}),
    AddHostTool: async () => ({}),
    About: async () => ({}),
    ReadClipboardImage: async () => "",
    // 终端 PTY 通道：id 递增便于断言会话隔离，其余调用记入 runCalls。
    TerminalStart: async () => "pty-" + (++terminalSeq),
    TerminalWrite: async () => {},
    TerminalResize: async () => {},
    TerminalClose: async () => {},
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
        // 复制服务地址走这条通道（与终端复制同一实现）；文本记入 runCalls 供断言。
        ClipboardSetText: async (text) => { runCalls.push(`clipboard:${text}`); },
      },
      addEventListener() {},
      localStorage: overrides.localStorage ?? makeStorage(),
    },
  };
}

/* xterm 桩：xterm.js 是随包 vendor 的 UMD 构建，真实例 open 需要真实布局尺寸
 * 才能测出字符单元格，桩 DOM 给不出。这里实现 app.js 用到的最小成员并记录调用，
 * 让终端用例能走完「点新建 → 开会话 → 事件回写 → 渲染标签」整条路径。 */
function makeXtermStub() {
  const instances = [];
  class FakeTerminal {
    constructor(options) {
      this.options = options;
      this.cols = 80;
      this.rows = 24;
      this.opened = false;
      this.disposed = false;
      this.addons = [];
      this.written = [];
      this.dataHandlers = [];
      this.resizeHandlers = [];
      this.keyHandlers = [];
      this.selection = "";
      instances.push(this);
    }
    // 记录挂载节点：标签切换靠搬运它证明切到了正确的会话
    open(node) { this.opened = true; this.holder = node; }
    loadAddon(addon) { this.addons.push(addon); }
    onData(fn) { this.dataHandlers.push(fn); }
    onResize(fn) { this.resizeHandlers.push(fn); }
    attachCustomKeyEventHandler(fn) { this.keyHandlers.push(fn); }
    focus() {}
    dispose() { this.disposed = true; }
    write(data) { this.written.push(data); }
    hasSelection() { return this.selection !== ""; }
    getSelection() { return this.selection; }
    clearSelection() { this.selection = ""; }
  }
  class FakeFitAddon {
    constructor() { this.fitCount = 0; }
    fit() { this.fitCount += 1; }
  }
  return { instances, Terminal: FakeTerminal, FitAddon: { FitAddon: FakeFitAddon } };
}

/* 在独立 vm 上下文加载 app.js 并运行 init()；返回驱动句柄。
 * 末尾追加一行把模块级 applyStatus 暴露到 sandbox，供预览分支直接调用。 */
function loadApp({ hasWails = true, overrides = {} } = {}) {
  const runCalls = [];
  const { document, registry } = makeDocument();
  buildHtml(document);
  const xterm = makeXtermStub();
  const { window, events } = hasWails
    ? makeWails(runCalls, overrides)
    : { window: { addEventListener() {} }, events: {} };

  const sandbox = {
    console,
    setTimeout,
    clearTimeout,
    Promise,
    // app.js 的状态栏脱敏用 URL 解析主机端口；vm 上下文默认不带这个 Web API，
    // 缺了它 hostLabel 会走解析失败的兜底分支，脱敏行为就测不到了。
    URL,
    // 终端：xterm 由 index.html 的 vendor 脚本注入全局，vm 上下文没有脚本加载，
    // 用桩代替；requestAnimationFrame 同理缺失，而弹窗重开与布局稳定后的 fit
    // 都依赖它，缺了会抛 ReferenceError。
    Terminal: xterm.Terminal,
    FitAddon: xterm.FitAddon,
    requestAnimationFrame: (fn) => setTimeout(() => fn(0), 0),
    window,
    document,
    navigator: {},
  };
  vm.createContext(sandbox);
  // 加一行暴露模块级绑定供测试直接调用（函数声明提升，运行前已定义）
  const code = APP_CODE + "\n;globalThis.__testMaybeAutoStart = maybeAutoStartAfterRepair;"
    + "\n;globalThis.__testRenderRepairOutput = renderRepairOutput;"
    + "\n;globalThis.__testRenderTools = renderTools;"
    + "\n;globalThis.__testRunDoctorForce = function (t) { return runDoctor(t || '', true); };"
    + "\n;globalThis.__testSwitchTerminal = switchTerminalSession;"
    + "\n;globalThis.__testCloseTerminal = closeTerminalSession;"
    + (hasWails ? "" : "\n;globalThis.__testApplyStatus = applyStatus;");
  vm.runInContext(code, sandbox, { filename: "app.js" });

  return {
    sandbox,
    document,
    registry,
    runCalls,
    overrides,
    terminals: xterm.instances,
    storage: window.localStorage,
    status: (s) => {
      assert.equal(typeof events["harness:status"], "function",
        "harness:status 事件未注册（需 Wails 环境）");
      events["harness:status"](s);
    },
    /* 驱动一条后端终端事件。EventsOn 的桩把回调存进 events，名称与 index.html
     * 脚本注册的一致（terminal:output / terminal:status）。 */
    terminalEvent: (name, payload) => {
      assert.equal(typeof events[name], "function", name + " 事件未注册");
      events[name](payload);
    },
    /* 文档级键盘事件（Esc 关弹窗走这条通道）。 */
    /* 文档级键盘事件（Esc 与标签快捷键走这条通道）。事件对象按真实
     * KeyboardEvent 补上 preventDefault，用例可据 defaultPrevented 断言
     * 默认行为确实被拦下。 */
    keydown: (event) => {
      const ev = {
        defaultPrevented: false,
        preventDefault() { ev.defaultPrevented = true; },
        ...event,
      };
      document.fire("keydown", ev);
      return ev;
    },
    /* 走真实按钮路径建一个会话（点击 #terminal-new），返回其 xterm 桩。
     * 创建过程有多个 await，flush 两次让 TerminalStart 结算并渲染完标签。 */
    newTerminal: async () => {
      document.getElementById("terminal-new").fire("click");
      await flush();
      await flush();
      return xterm.instances[xterm.instances.length - 1];
    },
  };
}

const flush = () => new Promise((r) => setImmediate(r));

/* 沙箱里的 requestAnimationFrame 映射到 setTimeout(0)，与 setImmediate 的先后
 * 顺序不确定，需要等布局回调结算的断言用这个固定短等待。 */
const settle = () => new Promise((r) => setTimeout(r, 5));

/* ---------- 用例 ---------- */

test("StartupDiagnosing 时失败页提示诊断中并立即打开弹窗", async () => {
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

  // 诊断一开始就要打开弹窗（显示"正在诊断…"），而不是干等结果才出现。
  assert.equal(
    h.document.getElementById("doctor-modal").classList.contains("hidden"),
    false, "诊断中应立即打开弹窗");
  assert.equal(h.runCalls.length, 1, "诊断中应调用 RunDoctor");
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

  assert.match(
    h.document.getElementById("doctor-summary-text").innerHTML,
    /共 3 项/, "摘要应渲染出诊断报告");
  assert.equal(
    h.document.getElementById("doctor-refresh").classList.contains("hidden"),
    false, "诊断就绪后应显示重新诊断按钮");
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

  // 修复方案区：mock 报告有一项 fixable(SuggestedLevel=2)，应渲染 L2 推荐卡
  const plans = h.document.getElementById("repair-plans");
  assert.equal(plans.classList.contains("hidden"), false, "有可修项时修复方案区应可见");
  assert.match(plans.innerHTML, /轻度修复/, "L1 卡片应有说明");
  assert.match(plans.innerHTML, /中度修复/, "L2 卡片应有说明");
  assert.match(plans.innerHTML, /建议优先执行/, "应有推荐级别标记");
  assert.match(plans.innerHTML, /插件加载/, "L2 卡片应列出可修检查项");
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

test("maybeAutoStartAfterRepair：全绿报告触发自动启动，非全绿不触发", () => {
  const h = loadApp();
  const fn = h.sandbox.__testMaybeAutoStart;
  assert.equal(typeof fn, "function", "应暴露 maybeAutoStartAfterRepair");

  // 全绿：无 Error、无失败 → 自动启动
  assert.equal(fn({ Error: "", Failed: 0, Checks: [] }), true);
  // 仍有失败项 → 不自动启动（等用户决定）
  assert.equal(fn({ Error: "", Failed: 1, Checks: [] }), false);
  // 诊断命令本身失败 → 不自动启动
  assert.equal(fn({ Error: "exit status 1", Failed: 0, Checks: [] }), false);
  // 空报告（诊断抛出）→ 不自动启动
  assert.equal(fn(null), false);
  assert.equal(fn(undefined), false);
});

test("诊断进行中再次触发 runDoctor 复用同一次检测，不重复调用", async () => {
  // RunDoctor 返回受控 promise：证明第二次 runDoctor 等待同一个结果。
  let apiCalls = 0;
  let resolveRun;
  const gate = new Promise((res) => { resolveRun = res; });
  const h = loadApp({
    overrides: {
      RunDoctor: async () => {
        apiCalls += 1;
        await gate;
        return fakeReport();
      },
    },
  });
  await flush();
  // 触发第一次诊断
  await h.document.getElementById("btn-failed-doctor").fire("click");
  await flush();
  // 检测未完成时再次触发（模拟：自动检测未完成，用户关掉弹窗再点诊断按钮）
  await h.document.getElementById("btn-failed-doctor").fire("click");
  await flush();
  assert.equal(apiCalls, 1, "进行中的检测应被复用，不应二次调用 RunDoctor");
  // 放行第一次检测：两次调用者都拿到结果
  resolveRun();
  await flush();
  await flush();
});

test("诊断完成后关闭再开弹窗：直接复用结果，不重新诊断", async () => {
  const h = loadApp();
  await flush();
  // 首次诊断（会自动触发一次 RunDoctor）
  h.status(baseStatus({ State: "failed", LastExit: "exit 1", StartupDiagnosing: true }));
  await flush();
  assert.equal(h.runCalls.length, 1, "首次诊断应跑一次");
  // 模拟诊断完成事件（并已打开过弹窗）
  h.status(baseStatus({ State: "failed", StartupDoctorReady: true }));
  await flush();

  // 关闭弹窗，再点失败页"诊断问题"：应直接展示缓存结果，不重新检测。
  // 关闭弹窗（模拟用户点 ×），再点失败页"诊断问题"：应直接展示缓存结果。
  h.document.getElementById("doctor-modal").classList.add("hidden");
  await h.document.getElementById("btn-failed-doctor").fire("click");
  await flush();
  assert.equal(h.runCalls.length, 1, "复用缓存后不应再次调用 RunDoctor");
  assert.equal(
    h.document.getElementById("doctor-modal").classList.contains("hidden"),
    false, "弹窗应重新打开");
  // 摘要应展示诊断结果而非"正在诊断…"
  assert.match(
    h.document.getElementById("doctor-summary-text").innerHTML,
    /共 3 项/, "应直接展示诊断结果");
});

test("force 诊断穿透缓存与进行中状态（重新诊断/修复复检必须真正重跑）", async () => {
  const h = loadApp();
  await flush();
  // 第一次诊断（非 force）完成并缓存。
  h.status(baseStatus({ State: "failed", LastExit: "exit 1", StartupDiagnosing: true }));
  await flush();
  assert.equal(h.runCalls.length, 1, "首次诊断跑一次");

  // 用暴露的 force 包装：必须重新调用后端，忽略缓存。
  await h.sandbox.__testRunDoctorForce("复查");
  await flush();
  assert.equal(h.runCalls.length, 2, "force 诊断应重新调用 RunDoctor（忽略缓存）");
  assert.equal(
    h.document.getElementById("doctor-summary-text").innerHTML.includes("共 3 项"),
    true, "复检结果仍正常渲染");
});

test("诊断报告用语义类着色，不把主题色值写进内联样式", async () => {
  // 内联色只能写死一套主题：过去写的是深色主题的值，浅色主题下就成了白底上的
  // 浅绿浅黄（对比度 1.8–2.5:1）。颜色必须由 CSS 按主题给出，JS 只给语义类。
  const h = loadApp();
  await flush();
  h.status(baseStatus({ State: "failed", LastExit: "exit 1", StartupDiagnosing: true }));
  await flush();

  const summary = h.document.getElementById("doctor-summary-text").innerHTML;
  assert.equal(/style="[^"]*color\s*:/iu.test(summary), false, "摘要不得内联颜色");
  assert.match(summary, /class="sev-ok"/u, "通过计数应带 ok 语义类");
  assert.match(summary, /class="sev-error"/u, "失败计数应带 error 语义类");

  const checks = h.document.getElementById("doctor-checks").innerHTML;
  assert.equal(/style="[^"]*color\s*:/iu.test(checks), false, "诊断清单不得内联颜色");
  assert.match(checks, /doctor-check-icon sev-error/u, "失败项图标应带 error 语义类");
  assert.match(checks, /doctor-check-icon sev-ok/u, "通过项图标应带 ok 语义类");
});

test("renderRepairOutput 把 CLI 输出解析为结构化面板", () => {
  const h = loadApp();
  const fn = h.sandbox.__testRenderRepairOutput;
  assert.equal(typeof fn, "function", "应暴露 renderRepairOutput");

  const cliOutput = [
    "Repair level 2 complete.",
    "  Applied: 1",
    "  Skipped: 0",
    "  Backups: /home/u/.dsh/backups/doctor-123",
    "",
    "Applied repairs:",
    "  ✓ plugin-dynamic-load: 已从 profile bundles 移除导致加载失败的插件：test-bad（原 manifest 已备份）",
  ].join("\n");
  fn(cliOutput);

  const status = h.document.getElementById("repair-panel-status");
  assert.equal(status.textContent, "✓ 修复成功");
  assert.ok(status.classList.contains("ok"));
  const body = h.document.getElementById("repair-panel-body");
  assert.match(body.innerHTML, /应用/g, "应显示应用计数");
  assert.match(body.innerHTML, /已从 profile bundles 移除/, "应显示执行项消息");
  const backup = h.document.getElementById("repair-panel-backup");
  assert.equal(backup.classList.contains("hidden"), false);
  assert.match(backup.textContent, /doctor-123/);

  // 失败输出：Applied 0，Skipped 1 → 未完成
  fn([
    "Repair level 2 complete.",
    "  Applied: 0",
    "  Skipped: 1",
    "Skipped:",
    "  - plugin-dynamic-load: 已移除 6 个插件仍无法加载，已还原 manifest",
  ].join("\n"));
  assert.equal(status.textContent, "⚠ 未完成");
  assert.ok(status.classList.contains("error"));
  assert.match(body.innerHTML, /已移除 6 个插件/, "应显示跳过原因");
});

/* ---------- 工具链市场卡片：容器内运行时提示 ---------- */

// fakeTools 构造一次 renderTools 的输入：node 未装但容器内有随包命令，
// deno 未装且容器内无命令，go 已装（仓库语义优先，不应出现运行时提示）。
function fakeTools() {
  return {
    Rows: [{ Name: "node", State: "installed", Version: "24.9.0" }],
    Catalog: [
      { ID: "node", Name: "Node.js", Category: "language-sdk", Description: "JavaScript 运行时",
        Provides: ["node", "npm"], Installed: false, AvailableVersion: "24.13.0",
        AvailableVersions: ["24.13.0"], Size: 55300000,
        RuntimeCmd: "node", RuntimeVersion: "24.9.0", RuntimeSource: "随包" },
      { ID: "deno", Name: "Deno", Category: "language-sdk", Description: "安全的 JavaScript/TypeScript 运行时",
        Provides: ["deno"], Installed: false, AvailableVersion: "2.1.4",
        AvailableVersions: ["2.1.4"], Size: 39700000 },
      { ID: "go", Name: "Go", Category: "language-sdk", Description: "Go 编译工具链",
        Provides: ["go", "gofmt"], Installed: true, ActiveVersion: "1.23.2",
        AvailableVersions: ["1.23.2"], Size: 70000000 },
    ],
    Sandboxed: true, HostTools: [], UpdateCount: 0, Notice: "",
  };
}

test("市场卡片：仓库未装但容器内已有命令时提示来源，已装/无命令不提示", () => {
  const h = loadApp();
  const render = h.sandbox.__testRenderTools;
  assert.equal(typeof render, "function", "应暴露 renderTools");

  render(fakeTools());

  const cards = h.document.querySelectorAll(".tool-card-item");
  assert.equal(cards.length, 3, "应渲染 3 张工具卡片");

  // node 卡：未装 + RuntimeCmd 命中 → 显示"容器内已可用"提示行（含版本与来源）
  const hints = h.document.querySelectorAll(".tool-card-runtime");
  assert.equal(hints.length, 1, "只有 node 卡应有运行时提示");
  assert.equal(hints[0].textContent, "容器内已可用：node 24.9.0（随包）");
  assert.ok(hints[0].title, "提示行应带悬浮说明");

  // deno 卡：未装且无 RuntimeCmd → 不提示
  // go 卡：已装 → 仓库状态（已安装）优先，不提示
  // （stub 的 querySelector 不限定子树，状态徽标用全局顺序断言：
  //   三张卡的 pill 依次为 node=可安装、deno=可安装、go=✓ 已安装）
  const nodeCard = cards[0];
  assert.equal(nodeCard.dataset.toolId, "node");
  const denoCard = cards[1];
  const goCard = cards[2];
  for (const card of [denoCard, goCard]) {
    const hasHint = card.children.some((ch) => ch.classList.contains("tool-card-runtime"));
    assert.equal(hasHint, false, "无命中的卡片不应出现运行时提示");
  }
  const pills = h.document.querySelectorAll(".pill");
  assert.equal(pills.length, 3, "每张卡一个状态徽标");
  assert.equal(pills[0].textContent, "可安装");
  assert.equal(pills[1].textContent, "可安装");
  assert.match(pills[2].textContent, /已安装/);
});

test("市场卡片：运行时提示在版本探测失败时省略版本段", () => {
  const h = loadApp();
  const tools = fakeTools();
  tools.Catalog[0].RuntimeVersion = "";
  h.sandbox.__testRenderTools(tools);

  const hints = h.document.querySelectorAll(".tool-card-runtime");
  assert.equal(hints.length, 1);
  assert.equal(hints[0].textContent, "容器内已可用：node（随包）");
});
test("预检 needs-confirm：舞台切到预检页并渲染问题清单与操作按钮", () => {
  const h = loadApp();
  h.status(baseStatus({
    State: "starting",
    Preflight: {
      Phase: "needs-confirm",
      Busy: false,
      Error: "",
      Issues: [
        { ID: "plugin-dynamic-load", Name: "插件运行时兼容性", Severity: "fatal", Message: "插件 X 导致启动失败", Detail: "", Fixable: true, Level: 2, Kind: "confirm" },
        { ID: "env-bootstrap-env", Name: ".env 变量", Severity: "fatal", Message: "已注释违规行", Detail: "", Fixable: true, Level: 1, Kind: "auto" },
      ],
      Repairs: ["env-bootstrap-env: 已注释"],
      BackupDirs: ["/home/u/.dsh/backups/doctor-1"],
    },
  }));

  const page = h.document.getElementById("preflight-page");
  assert.equal(page.classList.contains("hidden"), false, "预检页应可见");
  const loading = h.document.getElementById("loading-page");
  assert.equal(loading.classList.contains("hidden"), true, "加载页应隐藏");

  assert.equal(h.document.getElementById("preflight-title").textContent, "预检发现问题");
  const issues = h.document.querySelectorAll(".preflight-issue");
  assert.equal(issues.length, 2, "应渲染两条问题");
  const kinds = h.document.querySelectorAll(".preflight-kind");
  assert.equal(kinds[0].textContent, "需确认修复");
  assert.equal(kinds[1].textContent, "已自动修复");

  const actions = h.document.getElementById("preflight-actions");
  assert.equal(actions.classList.contains("hidden"), false, "操作按钮应可见");
  const repairs = h.document.getElementById("preflight-repairs");
  assert.match(repairs.textContent, /已应用修复/);
});

test("预检 running：舞台显示预检页但不显示操作按钮", () => {
  const h = loadApp();
  h.status(baseStatus({
    State: "starting",
    Preflight: { Phase: "running", Busy: false, Error: "", Issues: [], Repairs: [], BackupDirs: [] },
  }));

  const page = h.document.getElementById("preflight-page");
  assert.equal(page.classList.contains("hidden"), false, "预检页应可见");
  const actions = h.document.getElementById("preflight-actions");
  assert.equal(actions.classList.contains("hidden"), true, "预检中不应显示操作按钮");
});

test("预检放行后（ok）回到加载页", () => {
  const h = loadApp();
  h.status(baseStatus({
    State: "starting",
    Preflight: { Phase: "ok", Busy: false, Error: "", Issues: [], Repairs: [], BackupDirs: [] },
  }));

  const page = h.document.getElementById("preflight-page");
  assert.equal(page.classList.contains("hidden"), true, "预检页应隐藏");
  const loading = h.document.getElementById("loading-page");
  assert.equal(loading.classList.contains("hidden"), false, "应回到加载页");
});

test("freshHome 运行中：服务器弹框显示全新环境标识", () => {
  const h = loadApp();
  h.status(baseStatus({
    State: "running",
    Target: "http://127.0.0.1:1",
    FreshHome: true,
  }));
  const badge = h.document.getElementById("fresh-home-active");
  assert.equal(badge.classList.contains("hidden"), false, "全新环境标识应可见");
});

test("服务器弹框「重启」按钮在运行态调用 RestartServer", async () => {
  // dsh-market 的一键重启已被 launcher 的监护声明禁用（两个重启者会争同一个
  // --port），插件变更后走这个入口，可用性由状态里的 CanRestart 决定。
  const h = loadApp();
  h.status(baseStatus({ State: "running", CanStart: false, CanStop: true, CanRestart: true }));
  const btn = h.document.getElementById("server-restart");
  assert.ok(btn, "服务器弹框应有重启按钮");
  assert.equal(btn.disabled, false, "运行态重启按钮应可用");

  await btn.fire("click");
  await flush();
  assert.ok(h.runCalls.includes("restart"), "点击重启应调用 RestartServer");
});

test("停止态禁用「重启」按钮", () => {
  const h = loadApp();
  h.status(baseStatus({ State: "stopped", CanRestart: false }));
  assert.equal(h.document.getElementById("server-restart").disabled, true,
    "停止态由「启动」承担，重启应不可用");
});

test("服务器弹框复制按钮：运行态复制完整地址并反馈已复制", async () => {
  const h = loadApp();
  await flush();
  const url = "http://127.0.0.1:1/?token=abc";
  h.status(baseStatus({ State: "running", CanStart: false, CanStop: true, URL: url }));

  const btn = h.document.getElementById("server-copy");
  assert.equal(btn.classList.contains("hidden"), false, "运行态应显示复制按钮");
  assert.equal(h.document.getElementById("server-addr-origin").textContent, "http://127.0.0.1:1/",
    "地址第一行应是主机端口");
  assert.equal(h.document.getElementById("server-addr-token").textContent, "?token=abc",
    "地址第二行应是查询段（令牌固定 43 字符，整串一行放不下）");

  await btn.fire("click");
  await flush();
  assert.ok(h.runCalls.includes(`clipboard:${url}`),
    "点击应把完整地址写入剪贴板（含 token，与界面显示一致）");
  assert.ok(btn.classList.contains("copied"), "复制成功后按钮应进入已复制态");
});

test("非运行态隐藏复制按钮", () => {
  const h = loadApp();
  h.status(baseStatus({ State: "stopped" }));
  assert.equal(h.document.getElementById("server-copy").classList.contains("hidden"), true,
    "停止态地址行显示的是退出原因，复制按钮应隐藏");
});

test("地址没有查询串时整串落在第一行", () => {
  const h = loadApp();
  h.status(baseStatus({ State: "running", URL: "http://127.0.0.1:3456/" }));

  assert.equal(h.document.getElementById("server-addr-origin").textContent, "http://127.0.0.1:3456/");
  assert.equal(h.document.getElementById("server-addr-token").textContent, "",
    "无查询串时第二行为空，不应凭空补出 token 前缀");
});

test("服务器弹框状态行按状态带语义色，地址与文案两种呈现互斥", () => {
  const h = loadApp();
  h.status(baseStatus({ State: "running", URL: "http://127.0.0.1:1/?token=abc" }));

  const state = h.document.getElementById("server-state");
  const note = h.document.getElementById("server-detail1");
  const addr = h.document.getElementById("server-address");
  assert.equal(state.textContent, "运行中");
  assert.ok(state.classList.contains("state-ok"), "运行态状态应为 ok 语义色");
  assert.equal(addr.classList.contains("hidden"), false, "运行态应显示分行的服务地址");
  assert.equal(note.classList.contains("hidden"), true, "运行态不应同时显示地址文案行");

  h.status(baseStatus({ State: "starting" }));
  assert.ok(state.classList.contains("state-warn"), "启动中应为 warn 语义色");
  assert.equal(state.classList.contains("state-ok"), false, "语义色不应叠加");
  assert.equal(note.textContent, "harness 正在启动…", "启动中该行应是启动提示");
  assert.equal(addr.classList.contains("hidden"), true,
    "「正在启动…」不是可复制地址，不该套代码块样式");
});

test("状态栏只显示主机端口，不带服务地址里的访问 token", () => {
  const h = loadApp();
  const token = "s3cr3t-token";

  h.status(baseStatus({ State: "running", URL: `http://127.0.0.1:3456/?token=${token}` }));
  let text = h.document.getElementById("status-text").textContent;
  assert.equal(text, "运行中 127.0.0.1:3456", "状态栏应只显示主机与端口");
  assert.equal(text.includes(token), false, "状态栏不得出现 token");

  h.status(baseStatus({ Mode: "external", ExternalURL: `http://10.0.0.5:3456/?token=${token}` }));
  text = h.document.getElementById("status-text").textContent;
  assert.equal(text, "外部服务 10.0.0.5:3456");
  assert.equal(text.includes(token), false, "外部模式同样不得出现 token");
});

/* ---------- 终端用例 ---------- */

test("新建终端会话：xterm 就绪、标签渲染为运行态", async () => {
  const h = loadApp();
  await flush();
  const term = await h.newTerminal();

  assert.equal(term.opened, true, "会话启动前必须先 open xterm");
  assert.equal(term.addons.length, 1, "应装载 fit 插件");
  assert.ok(term.addons[0].fitCount >= 1, "open 后应 fit 出真实行列再启动 PTY");
  assert.ok(term.options.fontFamily.includes("JetBrains Mono"), "终端应使用随包等宽字体");

  const tabs = h.document.getElementById("terminal-tabs").innerHTML;
  assert.match(tabs, /class="terminal-tab terminal-tab-running active"/,
    "首个会话应为激活标签且带运行中语义类");
  assert.match(tabs, /终端 1/, "标签标题应按会话数自动编号");
});

test("终端输出写回对应会话，退出后标签按退出码取语义类", async () => {
  const h = loadApp();
  await flush();
  const term = await h.newTerminal();

  h.terminalEvent("terminal:output", { sessionId: "pty-1", data: "hello" });
  assert.deepEqual(term.written, ["hello"], "输出只写入自己的会话实例");

  h.terminalEvent("terminal:status", { sessionId: "pty-1", status: "closed", exitCode: 1 });
  let tabs = h.document.getElementById("terminal-tabs").innerHTML;
  assert.match(tabs, /terminal-tab-failed/, "非零退出码应为 failed 语义类");
  assert.match(tabs, /（已退出，退出码 1）/, "标签提示应带上退出码");
  assert.match(term.written.join(""), /退出码: 1/, "滚动区的退出提示按约定保留");

  h.terminalEvent("terminal:status", { sessionId: "pty-1", status: "closed", exitCode: 0 });
  tabs = h.document.getElementById("terminal-tabs").innerHTML;
  assert.match(tabs, /terminal-tab-exited/, "正常退出应为 exited 语义类");
  assert.equal(tabs.includes("terminal-tab-failed"), false, "正常退出不得带失败类");
  assert.match(tabs, /（已退出，退出码 0）/, "正常退出同样标注退出码");
});

test("终端弹框最大化与还原：按钮与双击头部都能切换，切换后重算行列", async () => {
  const h = loadApp();
  await flush();
  const term = await h.newTerminal();
  const modal = h.document.getElementById("terminal-modal");
  const btn = h.document.getElementById("terminal-max");
  const fitsBefore = term.addons[0].fitCount;

  btn.fire("click");
  assert.equal(modal.classList.contains("is-maximized"), true, "点按钮应进入最大化");
  assert.equal(btn.getAttribute("title"), "还原", "按钮 tooltip 应翻转为还原");
  await settle();
  assert.ok(term.addons[0].fitCount > fitsBefore,
    "尺寸变化后要重新 fit，否则全屏程序按旧行列重绘");

  btn.fire("click");
  assert.equal(modal.classList.contains("is-maximized"), false, "再点一次应还原");
  assert.equal(btn.getAttribute("title"), "最大化", "还原后按钮语义回到最大化");

  h.document.getElementById("terminal-head").fire("dblclick");
  assert.equal(modal.classList.contains("is-maximized"), true, "双击头部应最大化");

  // 按钮上的双击冒泡到头部：目标元素命中 closest("button")，不得切换状态
  h.document.getElementById("terminal-max").fire("dblclick");
  assert.equal(modal.classList.contains("is-maximized"), true, "按钮上的双击不得切换");
});

test("没有会话时最大化只切尺寸，不因缺会话抛错", () => {
  const h = loadApp();
  const modal = h.document.getElementById("terminal-modal");
  h.document.getElementById("terminal-max").fire("click");
  assert.equal(modal.classList.contains("is-maximized"), true, "无会话也应能最大化");
});

test("终端字号：按钮与 Ctrl± 快捷键缩放，越界夹紧且新会话继承", async () => {
  const h = loadApp();
  await flush();
  const term = await h.newTerminal();
  const inc = h.document.getElementById("terminal-font-inc");
  const dec = h.document.getElementById("terminal-font-dec");
  const readout = h.document.getElementById("terminal-font-size");

  assert.equal(term.options.fontSize, 14, "默认字号应为 14");
  assert.equal(readout.textContent, "14", "读数应显示当前字号");
  assert.equal(dec.disabled, false, "默认值不在下界，缩小按钮可用");

  inc.fire("click");
  assert.equal(term.options.fontSize, 15, "点 A+ 应放大 1px");
  assert.equal(readout.textContent, "15", "读数应跟着更新");
  assert.equal(h.storage.store.get("dsh-desktop.terminal.fontSize"), "15",
    "缩放后应写入偏好，下次打开还记住");

  dec.fire("click");
  dec.fire("click");
  assert.equal(term.options.fontSize, 13, "点 A− 应缩小");

  // 快捷键走 xterm 的自定义键处理：返回 false 才不会把该键当输入编码
  const onKey = term.keyHandlers[0];
  assert.equal(typeof onKey, "function", "会话应注册按键处理");
  assert.equal(onKey({ type: "keydown", ctrlKey: true, code: "Equal" }), false,
    "Ctrl+= 应被拦截");
  assert.equal(term.options.fontSize, 14, "Ctrl+= 放大 1px");
  onKey({ type: "keydown", ctrlKey: true, code: "Digit0" });
  assert.equal(term.options.fontSize, 14, "Ctrl+0 回到默认字号");
  assert.equal(onKey({ type: "keydown", ctrlKey: true, shiftKey: true, code: "Equal" }), true,
    "带 Shift 的组合不归字号管，应放行给 shell");

  // 边界：一直缩小到下限后夹紧，按钮置灰，继续点不再变小
  for (let i = 0; i < 20; i += 1) dec.fire("click");
  assert.equal(term.options.fontSize, 10, "字号应夹在下限 10px");
  assert.equal(dec.disabled, true, "到达下限后缩小按钮应置灰");
  assert.equal(readout.textContent, "10");

  // 新会话继承当前字号，不回到默认值
  const second = await h.newTerminal();
  assert.equal(second.options.fontSize, 10, "新建会话应继承当前字号");
  assert.equal(term.options.fontSize, 10, "已有会话不受新会话影响");
});

test("终端字号偏好：合法值恢复，脏数据回默认，写失败不影响缩放", async () => {
  const seeded = makeStorage();
  seeded.setItem("dsh-desktop.terminal.fontSize", "18");
  const h = loadApp({ overrides: { localStorage: seeded } });
  await flush();
  assert.equal(h.document.getElementById("terminal-font-size").textContent, "18",
    "合法偏好应恢复");
  const term = await h.newTerminal();
  assert.equal(term.options.fontSize, 18, "恢复的字号应作用于新建会话");
  assert.equal(h.document.getElementById("terminal-font-inc").disabled, false);

  const dirty = makeStorage();
  dirty.setItem("dsh-desktop.terminal.fontSize", "abc");
  const h2 = loadApp({ overrides: { localStorage: dirty } });
  await flush();
  assert.equal(h2.document.getElementById("terminal-font-size").textContent, "14",
    "非数字偏好应退回默认值");

  // 写失败的存储（隐私模式/配额）不得让缩放本身失败
  const broken = { getItem: () => null, setItem: () => { throw new Error("QuotaExceeded"); } };
  const h3 = loadApp({ overrides: { localStorage: broken } });
  await flush();
  const term3 = await h3.newTerminal();
  h3.document.getElementById("terminal-font-inc").fire("click");
  assert.equal(term3.options.fontSize, 15, "写不进偏好也要完成本次缩放");
});

test("终端启动等待字体期间显示占位提示，字体就绪后换成真实会话", async () => {
  const h = loadApp();
  await flush();
  // 字体加载慢/缺失时内容区会空着十几毫秒到 2s，占位提示让这段等待可见
  const pending = [];
  h.document.fonts = { load: () => new Promise((resolve) => pending.push(resolve)) };

  h.document.getElementById("terminal-new").fire("click");
  const content = h.document.getElementById("terminal-content");
  assert.match(content.innerHTML, /正在启动终端/, "点新建后应立即可见占位提示");
  assert.equal(pending.length, 4, "四个字重都参与字体就绪判断");

  pending.forEach((resolve) => resolve());
  await flush();
  await flush();
  assert.equal(content.innerHTML, "", "真实会话挂载后占位提示应被替换掉");
  assert.equal(content.children.length, 1, "内容区应挂上唯一的会话节点");
  assert.equal(content.children[0].className, "terminal-holder", "挂载的是 xterm 容器");
  assert.equal(h.terminals[0].opened, true, "字体就绪后应继续建会话");
});

test("Esc：终端持有焦点时放行给 PTY，焦点在工具栏时关窗并把焦点还回按钮", async () => {
  const h = loadApp();
  await flush();
  h.document.getElementById("btn-terminal").fire("click");
  await flush();
  await flush();
  const modal = h.document.getElementById("terminal-modal");
  assert.equal(modal.classList.contains("hidden"), false, "点工具栏按钮应打开终端弹窗");

  // 终端持有焦点：vim 的退出插入模式、readline 的转义前缀都靠 Esc，弹窗不得抢
  h.document.activeElement = h.document.getElementById("terminal-content");
  h.keydown({ key: "Escape" });
  assert.equal(modal.classList.contains("hidden"), false, "终端持有焦点时 Esc 不得关窗");

  h.keydown({ key: "a" });
  h.document.activeElement = h.document.getElementById("terminal-tabs");
  h.keydown({ key: "Escape" });
  assert.equal(modal.classList.contains("hidden"), true, "焦点不在终端时 Esc 应关窗");
  assert.equal(h.document.activeElement, h.document.getElementById("btn-terminal"),
    "关窗后焦点应回到工具栏按钮");

  h.keydown({ key: "Escape" });
  assert.equal(modal.classList.contains("hidden"), true, "弹窗已关时 Esc 不应有副作用");
});

test("终端标签快捷键：Ctrl+Shift+T 新建、Ctrl+Tab 循环、Ctrl+Shift+W 关闭", async () => {
  const h = loadApp();
  await flush();
  const modal = h.document.getElementById("terminal-modal");
  const content = h.document.getElementById("terminal-content");
  const tabs = () => h.document.getElementById("terminal-tabs").innerHTML;
  const tabCount = () => tabs().match(/data-id=/g).length;

  // 弹窗没打开时不接管按键：快捷键只在终端界面可见时生效
  h.keydown({ key: "T", code: "KeyT", ctrlKey: true, shiftKey: true });
  await flush();
  assert.equal(h.terminals.length, 0, "弹窗未打开时不应新建会话");

  h.document.getElementById("btn-terminal").fire("click");
  await flush();
  await flush();
  assert.equal(tabCount(), 1, "打开弹窗应建立首个会话");

  assert.equal(h.keydown({ key: "T", code: "KeyT", ctrlKey: true, shiftKey: true }).defaultPrevented,
    true, "标签快捷键要拦下 webview 的默认行为");
  await flush();
  await flush();
  assert.equal(tabCount(), 2, "Ctrl+Shift+T 应新建标签");
  assert.match(tabs(), /terminal-tab-running active" data-id="pty-2"/, "新标签应成为激活标签");
  assert.equal(content.children[0], h.terminals[1].holder, "内容区应换成新会话的节点");

  h.keydown({ key: "Tab", code: "Tab", ctrlKey: true });
  assert.match(tabs(), /active" data-id="pty-1"/, "Ctrl+Tab 应切到下一个标签（到尾部回绕）");
  assert.equal(content.children[0], h.terminals[0].holder, "切换标签应搬运对应会话的节点");

  h.keydown({ key: "Tab", code: "Tab", ctrlKey: true, shiftKey: true });
  assert.match(tabs(), /active" data-id="pty-2"/, "Ctrl+Shift+Tab 应反向切换");

  h.keydown({ key: "W", code: "KeyW", ctrlKey: true, shiftKey: true });
  await flush();
  assert.equal(tabCount(), 1, "Ctrl+Shift+W 应关闭当前标签");
  assert.equal(h.terminals[1].disposed, true, "关闭的会话应释放 xterm 实例");

  // 这些组合不能被 xterm 编成控制字符送进 PTY
  const onKey = h.terminals[0].keyHandlers[0];
  assert.equal(onKey({ type: "keydown", ctrlKey: true, shiftKey: true, code: "KeyT" }), false,
    "Ctrl+Shift+T 不得进 PTY");
  assert.equal(onKey({ type: "keydown", ctrlKey: true, code: "Tab" }), false,
    "Ctrl+Tab 不得进 PTY");
});

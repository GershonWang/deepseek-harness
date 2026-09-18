/* dsh-link-bridge.js 的 DOM 桩测试：验证注入桥的两条职责。
 *
 * 桥由打包流程注入到桌面版 GUI 的 dist，在真实的 Wails WebKitGTK 里只与
 * window.parent / document / console / MutationObserver 打交道，因此这里用
 * vm 上下文加最小桩加载它，断言它往上发的消息：
 *   - 外链转发：target="_blank" 的 HTTP(S) 点击 → { type: "open-external" }，
 *     非 http(s)、已 preventDefault、非左键、带 download 的都不转发；
 *   - 启动失败上报：console.error 收到 "web boot:" 文案，或引导页出现
 *     "Failed to load plugins" → { type: "boot-failed" }，只上报一次。
 * 运行：node --test linglong/test-link-bridge.cjs（工作目录 apps/desktop-launcher）
 *
 * 为什么用桩而不是真实浏览器：桥的判据都落在它自己发出的消息上，桩能精确控制
 * "哪一路先到"，而真实 WebKit 里 boot 失败难以按需复现；上游文案变化时先在这里
 * 暴露，再去真机回归。 */

"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const BRIDGE_CODE = fs.readFileSync(path.join(__dirname, "dsh-link-bridge.js"), "utf8");

/* 最小元素桩：桥只用 closest / hasAttribute / getAttribute。 */
class FakeElement {
  constructor(href, attrs = {}) {
    this.href = href;
    this.attrs = attrs;
  }
  closest(selector) {
    return selector === 'a[target="_blank"]' && this.href !== undefined ? this : null;
  }
  hasAttribute(name) {
    return this.attrs[name] === true;
  }
  getAttribute(name) {
    return name === "href" ? this.href : null;
  }
}

/* MutationObserver 桩：记录实例，用例按需触发回调（真实浏览器里由 DOM 变化触发）。 */
function makeObservers() {
  const instances = [];
  class FakeMutationObserver {
    constructor(callback) {
      this.callback = callback;
      this.disconnected = false;
      this.observed = null;
      instances.push(this);
    }
    observe(target) {
      this.observed = target;
    }
    disconnect() {
      this.disconnected = true;
    }
    trigger() {
      if (!this.disconnected) this.callback();
    }
  }
  return { instances, FakeMutationObserver };
}

/* 引导页 / 挂载点桩：querySelector 只认引导页标记，与真实 DOM 的用法一致。 */
function makeRoot() {
  return {
    bootPage: null,
    querySelector(selector) {
      return selector === "[data-dsh-boot]" ? this.bootPage : null;
    },
  };
}

/**
 * 在 vm 上下文里加载一次桥。
 * @param {object} options - inIframe：是否被 iframe 内嵌；ancestorOrigins：父窗口 origin 列表（null 表示浏览器不支持该 API）。
 * @returns 驱动句柄：messages、docListeners、root、observers、console 桩。
 */
function loadBridge({ inIframe = true, ancestorOrigins = ["http://127.0.0.1:8080"] } = {}) {
  const messages = [];
  const docListeners = {};
  const root = makeRoot();
  const { instances, FakeMutationObserver } = makeObservers();

  // WebKitGTK 支持 location.ancestorOrigins；不支持时该属性整个不存在。
  const location = ancestorOrigins === null ? {} : { ancestorOrigins };
  const window = {
    location,
    postMessage(message, targetOrigin) {
      messages.push({ message, targetOrigin });
    },
  };
  // 桥把消息发给 window.parent；被 iframe 内嵌时父窗口才是壳，独立打开时是它自己。
  window.parent = inIframe ? { postMessage: window.postMessage } : window;

  const document = {
    body: root,
    getElementById(id) {
      return id === "root" ? root : null;
    },
    addEventListener(type, fn) {
      (docListeners[type] ||= []).push(fn);
    },
  };

  const consoleStub = { calls: [], error(...args) { this.calls.push(args); } };
  const sandbox = {
    window,
    document,
    URL,
    Element: FakeElement,
    MutationObserver: FakeMutationObserver,
    console: consoleStub,
  };
  vm.createContext(sandbox);
  vm.runInContext(BRIDGE_CODE, sandbox, { filename: "dsh-link-bridge.js" });

  return { messages, docListeners, root, observers: instances, console: consoleStub };
}

/** 造一次左键点击事件；返回的事件对象可断言 defaultPrevented。 */
function clickEvent(target, over = {}) {
  const event = {
    defaultPrevented: false,
    button: 0,
    target,
    preventDefault() { event.defaultPrevented = true; },
    ...over,
  };
  return event;
}

/* vm 上下文里的对象有自己的原型，deepEqual 会因原型不同而失败；桥发出的都是
 * JSON 结构，按 JSON 归一后再比较。 */
function sentMessages(h) {
  return JSON.parse(JSON.stringify(h.messages));
}

test("未被 iframe 内嵌时不注册任何监听", () => {
  const h = loadBridge({ inIframe: false });

  assert.equal(h.docListeners.click, undefined, "独立打开页面不应接管外链点击");
  assert.equal(h.observers.length, 0, "独立打开页面不应观察引导页");
});

test("外链转发：只有 HTTP(S) 的 target=_blank 左键点击才转交壳", () => {
  const h = loadBridge();
  const fire = h.docListeners.click[0];
  const anchor = new FakeElement("https://example.com/guide");
  const event = clickEvent(anchor);

  fire(event);
  assert.equal(event.defaultPrevented, true, "桥应拦下默认导航");
  assert.deepEqual(sentMessages(h), [{
    message: { dshDesktop: true, type: "open-external", url: "https://example.com/guide" },
    targetOrigin: "http://127.0.0.1:8080",
  }]);

  // 非 http(s)、已处理过、非左键、带 download 的都在桥内被忽略。
  fire(clickEvent(new FakeElement("mailto:a@b.c")));
  fire(clickEvent(anchor, { defaultPrevented: true }));
  fire(clickEvent(anchor, { button: 1 }));
  fire(clickEvent(new FakeElement("https://example.com/a.zip", { download: true })));
  assert.equal(h.messages.length, 1, "以上点击都不应转交壳");
});

test("console.error 里的 web boot 失败上报，且不吞掉原始日志", () => {
  const h = loadBridge();
  const error = new Error("web boot: 1 entry did not activate\n@michengai/dsh-archive-manager: import failed");

  h.console.error(error);

  assert.deepEqual(sentMessages(h), [{
    message: {
      dshDesktop: true,
      type: "boot-failed",
      message: "web boot: 1 entry did not activate\n@michengai/dsh-archive-manager: import failed",
    },
    targetOrigin: "http://127.0.0.1:8080",
  }]);
  // 只取 message 不取 stack：失败页要的是"哪几个插件没激活"。
  assert.equal(h.console.calls.length, 1, "原始 console.error 必须照常调用");
  assert.equal(h.console.calls[0][0], error, "原始 console.error 应收到同一个错误对象");
});

test("非 boot 的 console.error 不上报，boot 失败也只上报一次", () => {
  const h = loadBridge();

  h.console.error(new Error("network hiccup"));
  assert.deepEqual(sentMessages(h), [], "普通错误不应被当成启动失败");

  h.console.error(new Error("web boot: 2 entries did not activate"));
  h.console.error(new Error("web boot: window.__ModuleLoader__ bootstrap facade is missing"));
  assert.equal(h.messages.length, 1, "同一页面只上报一次启动失败");
  assert.equal(h.messages[0].message.message, "web boot: 2 entries did not activate");
});

test("DOM 兜底：引导页出现失败文案时上报，加载中不报", () => {
  const h = loadBridge();
  const bootPage = { textContent: "HARNESS Loading plugins…" };
  h.root.bootPage = bootPage;

  h.observers[0].trigger();
  assert.deepEqual(sentMessages(h), [], "加载中的引导页不应上报");

  // 上游若改走别的失败通道（没有 console.error），文案本身就是判据。
  bootPage.textContent = "HARNESS Failed to load plugins @michengai/dsh-archive-manager";
  h.observers[0].trigger();
  assert.equal(h.messages.length, 1, "失败文案应触发上报");
  assert.equal(h.messages[0].message.message, "客户端插件加载失败（未捕获到具体原因）");

  // 已上报后继续变化不再重复发送，且观察器自行断开（应用运行期不再有回调）。
  h.observers[0].trigger();
  assert.equal(h.messages.length, 1);
  assert.equal(h.observers[0].disconnected, true, "上报后应停止观察");
});

test("应用挂载成功（引导页节点消失）后停止观察，不再上报", () => {
  const h = loadBridge();
  h.root.bootPage = { textContent: "HARNESS Loading plugins…" };
  h.observers[0].trigger();

  // mountApp 用 hydrate 接管挂载点：引导页节点被移除，观察窗口到此结束。
  h.root.bootPage = null;
  h.observers[0].trigger();

  assert.equal(h.observers[0].disconnected, true, "引导页消失后应断开观察");
  assert.deepEqual(sentMessages(h), [], "正常启动不应上报任何消息");
});

test("父窗口不支持 ancestorOrigins 时回退为 *", () => {
  const h = loadBridge({ ancestorOrigins: null });

  h.console.error(new Error("web boot: 1 entry did not activate"));

  assert.equal(h.messages[0].targetOrigin, "*");
});

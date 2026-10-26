/* dsh-link-bridge.js 的 DOM 桩测试：验证注入桥的五条职责。
 *
 * 桥由打包流程注入到桌面版 GUI 的 dist，在真实的 Wails WebKitGTK 里只与
 * window.parent / document / console / MutationObserver / setInterval 打交道，
 * 因此这里用 vm 上下文加最小桩加载它，断言它往上发的消息：
 *   - 外链转发：target="_blank" 的 HTTP(S) 点击、以及 HTTP(S) 目标的
 *     window.open → { type: "open-external" }；非 http(s)、已 preventDefault、
 *     非左键、带 download 的都不转发，非 http(s) 的 window.open 原样透传；
 *   - 启动失败上报：console.error 收到 "web boot:" 文案，或引导页出现
 *     "Failed to load plugins" → { type: "boot-failed" }，只上报一次；
 *   - 语言上报：<html lang> 变化 → { type: "locale", id }，未变化不重复上报，
 *     应用挂载完成时补报一次（服务端默认 lang 恰好等于生效语言时属性不会变化，
 *     只靠属性观察会漏掉这种组合）；
 *   - 内容存活性：挂载点连续为空、或引导页长时间不消失 → { type: "boot-stalled" }，
 *     只上报一次；正常挂载（有内容、无引导页）不上报；
 *   - 会话恢复与上报：把壳注入的会话 id 写回客户端存储（只在客户端自己没有选择
 *     时），并按周期把当前选择 → { type: "current-session", sessionId }，同一个值
 *     不重复上报，清空后上报 null。
 * 运行：node --test linglong/test-link-bridge.cjs（工作目录 apps/linyaps/desktop-launcher）
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

/* 引导页 / 挂载点桩：querySelector 只认引导页标记，与真实 DOM 的用法一致。
 * childElementCount 默认 1：正常态挂载点里总有内容（引导页或应用 DOM）。 */
function makeRoot() {
  return {
    bootPage: null,
    childElementCount: 1,
    querySelector(selector) {
      return selector === "[data-dsh-boot]" ? this.bootPage : null;
    },
  };
}

/**
 * 在 vm 上下文里加载一次桥。
 * @param {object} options - inIframe：是否被 iframe 内嵌；ancestorOrigins：父窗口 origin 列表（null 表示浏览器不支持该 API）；storage：客户端 localStorage 的初始内容；restoreId：壳经索引注入送进来的会话 id；storageBroken：localStorage 抛异常（被拒/私密模式）。
 * @returns 驱动句柄：messages、docListeners、root、window、observers、console 桩、storage、nativeOpens（真正落到原生 window.open 的调用）。
 */
function loadBridge({
  inIframe = true,
  ancestorOrigins = ["http://127.0.0.1:8080"],
  storage = {},
  restoreId = undefined,
  storageBroken = false,
} = {}) {
  const messages = [];
  const docListeners = {};
  const nativeOpens = [];
  const root = makeRoot();
  const { instances, FakeMutationObserver } = makeObservers();

  // WebKitGTK 支持 location.ancestorOrigins；不支持时该属性整个不存在。
  const location = ancestorOrigins === null ? {} : { ancestorOrigins };
  // localStorage 桩：桥读写的只有客户端的选择键（见"当前会话的恢复与上报"一节）。
  // storageBroken 模拟存储被拒：读写都抛，桥必须当作"没有存储"继续工作。
  const backing = new Map(Object.entries(storage));
  const localStorage = {
    getItem(key) {
      if (storageBroken) throw new Error("storage denied");
      return backing.has(key) ? backing.get(key) : null;
    },
    setItem(key, value) {
      if (storageBroken) throw new Error("storage denied");
      backing.set(key, value);
    },
  };
  const window = {
    location,
    localStorage,
    postMessage(message, targetOrigin) {
      messages.push({ message, targetOrigin });
    },
    // 原生 window.open 的替身：桥会先保存再替换它（见 dsh-link-bridge.js 职责一
    // 的路径二），这里记录"哪些调用真的落到了 WebKit"以及返回值。
    open(url, name, features) {
      nativeOpens.push({ url, name, features });
      return { stubWindow: true };
    },
  };
  if (restoreId !== undefined) window.__DSH_DESKTOP_RESTORE_SESSION__ = restoreId;
  // 桥把消息发给 window.parent；被 iframe 内嵌时父窗口才是壳，独立打开时是它自己。
  window.parent = inIframe ? { postMessage: window.postMessage } : window;

  const document = {
    body: root,
    // 语言真源：桥读 <html lang> 并上报（见 dsh-link-bridge.js 的"语言上报"一节）。
    // 服务端 index.html 的默认值是 en，与真实 dist 一致。
    documentElement: { lang: "en" },
    getElementById(id) {
      return id === "root" ? root : null;
    },
    addEventListener(type, fn) {
      (docListeners[type] ||= []).push(fn);
    },
  };

  const consoleStub = { calls: [], error(...args) { this.calls.push(args); } };
  // 周期检查的定时器桩：桥用 setInterval 做内容存活性检查（真实浏览器里由计时器
  // 驱动），这里只记录回调，用例按 tick 数精确推进，不等待真实时间。
  const timers = [];
  const sandbox = {
    window,
    document,
    URL,
    Element: FakeElement,
    MutationObserver: FakeMutationObserver,
    console: consoleStub,
    setInterval(fn, ms) {
      timers.push({ fn, ms, cleared: false });
      return timers.length - 1;
    },
    clearInterval(id) {
      if (timers[id]) timers[id].cleared = true;
    },
  };
  vm.createContext(sandbox);
  vm.runInContext(BRIDGE_CODE, sandbox, { filename: "dsh-link-bridge.js" });

  /** 推进一次存活性检查；已清除的定时器不再触发。 */
  function tick() {
    for (const timer of timers) if (!timer.cleared) timer.fn();
  }

  return {
    messages, docListeners, root, document, window, observers: instances,
    console: consoleStub, timers, tick, storage: backing, nativeOpens,
  };
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
  // window.open 同理：普通浏览器打开新标签是它自己的行为，桥只补 WebKit 壳的缺口。
  h.window.open("https://example.com/doc");
  assert.equal(h.nativeOpens.length, 1, "独立打开页面应保留原生 window.open");
  assert.deepEqual(sentMessages(h), [], "独立打开页面不应向壳发消息");
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

test("外链转发：window.open 的 HTTP(S) 目标转交壳，其它目标原样透传", () => {
  const h = loadBridge();

  // 客户端的普通左键路径（MarkdownAnchor）：preventDefault 之后 window.open。
  // WebKit 不创建新浏览上下文，这次调用必须被桥接住，而不是落回原实现。
  assert.equal(
    h.window.open("https://example.com/doc", "_blank", "noopener,noreferrer"),
    null,
    "外链没有真实窗口可返回",
  );
  assert.deepEqual(sentMessages(h), [{
    message: { dshDesktop: true, type: "open-external", url: "https://example.com/doc" },
    targetOrigin: "http://127.0.0.1:8080",
  }]);
  assert.deepEqual(h.nativeOpens, [], "外链不应落到 WebKit 的原生实现（它会把调用吞掉）");

  // URL 对象同样按外链处理，String() 之后仍能判定协议。
  h.window.open(new URL("https://example.com/object"));
  assert.equal(h.messages.length, 2, "URL 对象也应转交壳");

  // 非 http(s)：账号登录预开的 about:blank 必须仍由原实现处理并保留返回值，
  // 登录弹窗靠这个返回值上的 location.replace 导航。
  const popup = h.window.open("about:blank", "_blank");
  assert.deepEqual(h.nativeOpens, [{ url: "about:blank", name: "_blank", features: undefined }]);
  assert.deepEqual(popup, { stubWindow: true }, "透传调用必须返回原实现的返回值");
  assert.equal(h.messages.length, 2, "非外链不应转交壳");
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

test("应用挂载成功（引导页节点消失）后补报语言并停止观察", () => {
  const h = loadBridge();
  h.root.bootPage = { textContent: "HARNESS Loading plugins…" };
  h.observers[0].trigger();

  // mountApp 用 hydrate 接管挂载点：引导页节点被移除，观察窗口到此结束。
  h.root.bootPage = null;
  h.observers[0].trigger();

  assert.equal(h.observers[0].disconnected, true, "引导页消失后应断开观察");
  // 挂载完成即补报一次语言：服务端默认 lang 恰好等于生效语言时（中文系统 +
  // 用户在 GUI 里选英文）属性自始至终没变，只靠属性观察会漏掉这种组合。
  assert.deepEqual(sentMessages(h), [{
    message: { dshDesktop: true, type: "locale", id: "en" },
    targetOrigin: "http://127.0.0.1:8080",
  }]);
});

test("语言上报：<html lang> 变化即上报，未变化不重复上报", () => {
  const h = loadBridge();
  assert.equal(h.observers.length, 2, "应注册引导页观察与语言观察两个观察器");
  const langObserver = h.observers[1];

  // 真实浏览器里观察回调只在属性变化时触发，桩用显式 trigger 表示这一次变化。
  h.document.documentElement.lang = "zh-CN";
  langObserver.trigger();
  assert.equal(h.messages.length, 1, "语言变化应上报");
  assert.deepEqual(sentMessages(h)[0], {
    message: { dshDesktop: true, type: "locale", id: "zh-CN" },
    targetOrigin: "http://127.0.0.1:8080",
  });

  langObserver.trigger(); // 语言未变却再次回调
  assert.equal(h.messages.length, 1, "语言未变不应重复上报");

  // 切回英文同样要上报：壳必须跟着切回去，不能只在首次生效。
  h.document.documentElement.lang = "en";
  langObserver.trigger();
  assert.equal(h.messages.length, 2, "切回英文也应上报");
  assert.equal(h.messages[1].message.id, "en");
});

test("语言上报：取不到 <html lang> 时不发消息", () => {
  const h = loadBridge();
  h.document.documentElement.lang = "";
  h.observers[1].trigger();

  assert.deepEqual(sentMessages(h), [], "空语言不应上报");
});

test("父窗口不支持 ancestorOrigins 时回退为 *", () => {
  const h = loadBridge({ ancestorOrigins: null });

  h.console.error(new Error("web boot: 1 entry did not activate"));

  assert.equal(h.messages[0].targetOrigin, "*");
});

/* ---------- 内容存活性（职责四） ---------- */

test("内容存活性：挂载点连续为空 → 上报 boot-stalled 且只上报一次", () => {
  const h = loadBridge();
  h.root.childElementCount = 0;

  h.tick();
  assert.deepEqual(sentMessages(h), [], "单次空挂载点还可能是渲染间隙，不应立即上报");

  h.tick();
  const sent = sentMessages(h);
  assert.equal(sent.length, 1, "连续两次空挂载点应上报一次");
  assert.equal(sent[0].message.type, "boot-stalled");
  assert.equal(sent[0].message.dshDesktop, true);
  assert.match(sent[0].message.message, /内容为空/);

  h.tick();
  h.tick();
  assert.equal(sentMessages(h).length, 1, "已上报后不再重复");
});

test("内容存活性：引导页长时间不消失 → 上报 boot-stalled", () => {
  const h = loadBridge();
  h.root.bootPage = { textContent: "Loading plugins…" };

  for (let i = 0; i < 5; i++) h.tick();
  assert.deepEqual(sentMessages(h), [], "未到阈值不上报");

  h.tick();
  const sent = sentMessages(h);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].message.type, "boot-stalled");
  assert.match(sent[0].message.message, /引导页仍在/);
});

test("内容存活性：正常挂载（有内容、无引导页）不上报", () => {
  const h = loadBridge();

  for (let i = 0; i < 12; i++) h.tick();

  assert.deepEqual(sentMessages(h), [], "有应用内容时不应上报");
});

test("内容存活性：空挂载点恢复后计数复位", () => {
  const h = loadBridge();
  h.root.childElementCount = 0;
  h.tick();

  h.root.childElementCount = 1;
  h.tick();
  h.root.childElementCount = 0;
  h.tick();

  assert.deepEqual(sentMessages(h), [], "中断过的空计数不应累计到阈值");
});

test("会话恢复：客户端没有选择时把壳记住的会话写回存储并上报", () => {
  const h = loadBridge({ restoreId: "session-remembered" });

  assert.deepEqual(
    JSON.parse(h.storage.get("dsh.sessions.current")),
    { sessionId: "session-remembered" },
    "壳记住的会话应写回客户端的选择键",
  );
  assert.deepEqual(sentMessages(h), [{
    message: { dshDesktop: true, type: "current-session", sessionId: "session-remembered" },
    targetOrigin: "http://127.0.0.1:8080",
  }]);
});

test("会话恢复：客户端自己有选择时不覆盖", () => {
  const h = loadBridge({
    restoreId: "session-remembered",
    storage: { "dsh.sessions.current": JSON.stringify({ sessionId: "client-choice" }) },
  });

  assert.deepEqual(
    JSON.parse(h.storage.get("dsh.sessions.current")),
    { sessionId: "client-choice" },
    "客户端记着的那份才是权威，壳的记忆更旧",
  );
  assert.equal(h.messages.length, 1);
  assert.equal(h.messages[0].message.sessionId, "client-choice");
});

test("会话上报：选择变化各上报一次，清空后上报 null", () => {
  const h = loadBridge({ storage: { "dsh.sessions.current": JSON.stringify({ sessionId: "first" }) } });
  assert.equal(h.messages.length, 1, "启动时的当前选择上报一次");
  assert.equal(h.messages[0].message.sessionId, "first");

  h.tick();
  assert.equal(h.messages.length, 1, "选择未变不重复上报");

  h.storage.set("dsh.sessions.current", JSON.stringify({ sessionId: "second" }));
  h.tick();
  assert.equal(h.messages.length, 2);
  assert.equal(h.messages[1].message.sessionId, "second");

  h.storage.set("dsh.sessions.current", JSON.stringify({}));
  h.tick();
  assert.equal(h.messages.length, 3);
  assert.equal(h.messages[2].message.sessionId, null, "清空后上报 null，壳据此清除记忆");
});

test("会话恢复：存储被拒时不崩、不写，也不上报空选择", () => {
  const h = loadBridge({ restoreId: "session-remembered", storageBroken: true });

  assert.equal(h.storage.size, 0, "被拒的存储不应被写入");
  assert.deepEqual(sentMessages(h), [], "读不到选择时不上报，避免把壳的记忆清掉");
  h.tick();
  assert.deepEqual(sentMessages(h), []);
});

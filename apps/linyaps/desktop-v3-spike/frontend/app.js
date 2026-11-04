/**
 * 文件职责：spike 的壳脚本，刻意写成「客户端现有壳的样子」——
 * 只用 v2 注入的 window.go / window.runtime，不 import 任何模块。
 *
 * 为什么这么写：迁移的判定标准不是「新代码能跑」，而是「旧写法的代码能不能原样跑」。
 * 客户端 2894 行静态壳全部是这种形态；本文件如果能在 v3 上工作，就说明迁移可以靠
 * 一层适配层完成（见 v2shim.js），而不必重写壳。
 */

/** 事件是否被收到的观察结果，跨多次采集共享。 */
const seen = { early: false, announce: false };

/** Go 侧事实（由 spike:go / spike:announce 事件带回）。 */
let goFacts = null;

/** 记录经 v2 全局调通的调用，证明适配层确实被使用（而不是恰好没报错）。 */
const legacy = {
  goCalls: new Set(),
  runtimeCalls: new Set(),
  startupState: "(未调用)",
};

eventsSubscribe();
void run();

/** 经 window.go.app.App.* 调用 Go 方法，并记录方法名。 */
function goCall(name, ...args) {
  legacy.goCalls.add(name);
  return window.go.app.App[name](...args);
}

/** 经 window.runtime.* 调用运行时能力，并记录成员名。 */
function runtimeCall(name, ...args) {
  legacy.runtimeCalls.add(name);
  return window.runtime[name](...args);
}

/** 向后端广播事件（同样走 v2 全局）。 */
function emit(name, data) {
  legacy.runtimeCalls.add("EventsEmit");
  return window.runtime.EventsEmit(name, data);
}

/** 订阅 Go 侧事件。窗口就绪前后都可能到达，因此订阅必须最早执行。 */
function eventsSubscribe() {
  runtimeCall("EventsOn", "spike:early", () => {
    seen.early = true;
  });
  runtimeCall("EventsOn", "spike:announce", (data) => {
    seen.announce = true;
    if (data) {
      goFacts = data;
    }
    // announce 常常比页面脚本更早触发，收到后立刻补发一次事实。
    void publish();
  });
  runtimeCall("EventsOn", "spike:go", (data) => {
    if (data) {
      goFacts = data;
    }
  });
}

/**
 * 主流程：拉启动状态 → 验证全局调用 → 回传事实 → 权限探测。
 *
 * 「拉启动状态」是 v3 下必须新增的一步：就绪前的事件会被丢弃（本 spike 已实测），
 * 客户端现在靠早期事件推进启动进度条，迁移时应改为加载后主动拉一次。
 */
async function run() {
  try {
    legacy.startupState = await goCall("StartupState");
  } catch (err) {
    legacy.startupState = `error:${String(err)}`;
  }

  try {
    const reply = await goCall("Ping", "spike");
    await emit("spike:binding", { ok: true, reply: String(reply), called: "window.go.app.App.Ping" });
  } catch (err) {
    await emit("spike:binding", { ok: false, error: String(err), called: "window.go.app.App.Ping" });
  }

  // 壳里真实用到的另外三个 Go 方法，一并走一遍调用链。
  void goCall("ReadClipboardImage").catch(() => {});
  void goCall("RememberSession", "spike-session").catch(() => {});
  void goCall("ReportClientBootFailure", "").catch(() => {});

  await publish();
  wireTitlebar();
  wireMic();

  // 等权限策略/钩子就位再发请求（挂载在主循环上异步进行）。
  await new Promise((resolve) => setTimeout(resolve, 2500));
  const notification = await requestNotificationPermission();
  const mic = await probeUserMedia({ audio: true });
  const camera = await probeUserMedia({ video: true });
  await emit("spike:probes", { mic, camera, notification });
  renderProbes(mic, camera, notification);
}

/** 采集一次页面事实并回传。可重复调用，报告会被覆盖为最新快照。 */
async function publish() {
  const facts = collect();
  await emit("spike:frontend", facts);
  render(facts);
}

/** 收集页面侧事实。 */
function collect() {
  const storage = localStorageProbe();
  return {
    href: location.href,
    origin: location.origin,
    userAgent: navigator.userAgent,
    localStorageOK: storage.ok,
    localStorageDetail: storage.detail,
    runtimeGlobal: !!window.wails,
    classicGlobals: !!(window.go || window.runtime),
    earlyEventSeen: seen.early,
    announceSeen: seen.announce,
    draggableValue: draggableValue(),
    screen: `${screen.width}x${screen.height}@${window.devicePixelRatio}`,
    legacy: {
      shimInstalled: !!window.__dshShimInstalled,
      goCalls: [...legacy.goCalls].sort(),
      runtimeCalls: [...legacy.runtimeCalls].sort(),
      startupState: legacy.startupState,
    },
  };
}

/**
 * 在壳自己的 origin 下做一次 localStorage 读写。
 *
 * 为什么要专门验证：客户端的语言、字体、终端字号都按 origin 存在 localStorage 里，
 * v2 的壳 origin 是 wails://wails；v3 实测为 wails://localhost，这些值会读不到。
 */
function localStorageProbe() {
  const key = "dsh-v3-spike";
  try {
    localStorage.setItem(key, "1");
    const value = localStorage.getItem(key);
    localStorage.removeItem(key);
    return value === "1" ? { ok: true, detail: "" } : { ok: false, detail: `写后读到 ${value}` };
  } catch (err) {
    return { ok: false, detail: String(err) };
  }
}

/** 读取标题栏 --wails-draggable 的计算值。 */
function draggableValue() {
  const bar = document.querySelector("#titlebar");
  if (!bar) {
    return "(缺少 #titlebar)";
  }
  return getComputedStyle(bar).getPropertyValue("--wails-draggable").trim() || "(空)";
}

/** 接上自绘标题栏的窗口控制按钮（走 v2 全局）。 */
function wireTitlebar() {
  const bind = (id, name) => {
    const node = document.querySelector(id);
    if (!node) {
      return;
    }
    node.addEventListener("click", () => {
      Promise.resolve(runtimeCall(name)).catch(() => {});
    });
  };
  bind("#btn-min", "WindowMinimise");
  bind("#btn-max", "WindowToggleMaximise");
  bind("#btn-close", "Quit");
}

/** 接上手动麦克风重试按钮。 */
function wireMic() {
  const node = document.querySelector("#btn-mic");
  if (!node) {
    return;
  }
  node.addEventListener("click", () => {
    void probeUserMedia({ audio: true }).then((result) => {
      const label = document.querySelector("#mic-result");
      if (label) {
        label.textContent = result.ok ? "麦克风可用（已放行）" : `失败：${result.error}`;
        label.className = result.ok ? "ok" : "fail";
      }
    });
  });
}

/** 把三项探测结果渲染到页面上。 */
function renderProbes(mic, camera, notification) {
  const label = document.querySelector("#mic-result");
  if (!label) {
    return;
  }
  label.textContent = `麦克风:${mic.ok ? "放行" : "拒绝"} · 摄像头:${camera.ok ? "放行" : "拒绝"} · 通知:${notification}`;
  label.className = mic.ok && !camera.ok ? "ok" : "fail";
}

/** 请求通知权限：只为触发一次 permission-request，作为不依赖硬件的对照组。 */
async function requestNotificationPermission() {
  if (!window.Notification || !Notification.requestPermission) {
    return "unavailable";
  }
  try {
    return await Notification.requestPermission();
  } catch (err) {
    return `error:${String(err)}`;
  }
}

/**
 * 采集一次媒体并立刻释放。
 *
 * 用音频验证放行、用视频验证拒绝：客户端策略就是「只允许麦克风」。
 */
async function probeUserMedia(constraints) {
  if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
    return { ok: false, error: "navigator.mediaDevices.getUserMedia 不存在" };
  }
  const timeout = new Promise((resolve) => {
    setTimeout(() => resolve({ ok: false, error: "getUserMedia 超时 10s" }), 10000);
  });
  const attempt = (async () => {
    try {
      const stream = await navigator.mediaDevices.getUserMedia(constraints);
      for (const track of stream.getTracks()) {
        track.stop();
      }
      return { ok: true, error: "" };
    } catch (err) {
      return { ok: false, error: String(err) };
    }
  })();
  return Promise.race([attempt, timeout]);
}

/** 把事实渲染成表格，便于真机上一眼确认结果。 */
function render(facts) {
  const rows = [
    ["绑定调用", goFacts ? "已回传" : "等待 Go 侧", true],
    ["v2 适配层", facts.legacy.shimInstalled ? "已安装" : "未安装", facts.legacy.shimInstalled],
    ["Go 调用", facts.legacy.goCalls.join(", ") || "(无)", facts.legacy.goCalls.length > 0],
    ["runtime 调用", facts.legacy.runtimeCalls.join(", ") || "(无)", facts.legacy.runtimeCalls.length > 0],
    ["启动状态", facts.legacy.startupState, facts.legacy.startupState !== "(未调用)"],
    ["页面 origin", facts.origin, facts.origin === "wails://localhost"],
    ["localStorage", facts.localStorageOK ? "可读写" : facts.localStorageDetail, facts.localStorageOK],
    ["window.wails", String(facts.runtimeGlobal), facts.runtimeGlobal],
    ["window.go / window.runtime", String(facts.classicGlobals), facts.classicGlobals],
    ["早期事件（就绪前）", String(facts.earlyEventSeen), true],
    ["就绪事件", String(facts.announceSeen), facts.announceSeen],
    ["--wails-draggable", facts.draggableValue, facts.draggableValue === "drag"],
    ["屏幕", facts.screen, true],
  ];
  if (goFacts) {
    rows.push(["后端", `${goFacts.backend} · ${goFacts.stackVersion}`, true]);
  }

  const tbody = document.querySelector("#facts tbody");
  if (!tbody) {
    return;
  }
  tbody.replaceChildren(
    ...rows.map(([key, value, ok]) => {
      const tr = document.createElement("tr");
      const th = document.createElement("th");
      th.textContent = key;
      const td = document.createElement("td");
      td.textContent = String(value);
      td.className = ok ? "ok" : "fail";
      tr.append(th, td);
      return tr;
    }),
  );

  const path = document.querySelector("#report-path");
  if (path && goFacts && goFacts.reportPath) {
    path.textContent = `报告路径：${goFacts.reportPath}`;
  }
}

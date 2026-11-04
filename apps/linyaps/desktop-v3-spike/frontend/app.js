/**
 * 文件职责：spike 壳页面的事实采集脚本。
 *
 * 为什么是不带打包器的纯 ES module：桌面客户端的壳是一层静态 HTML/CSS/JS，仓库里没有
 * Node 工具链，v2 靠 Wails 注入的 window.go / window.runtime 全局工作。v3 官方模板默认走
 * Vite + @wailsio/runtime，本脚本要证明「不引入构建步骤也能用 v3 runtime」，这正是壳能否
 * 低成本迁移到 v3 的前提。
 *
 * 采集结果通过事件回传 Go 侧落盘：绑定调用（Call.ByName）本身是待验证对象，若用它承载
 * 报告，一旦名字解析失败就什么证据都留不下。
 */

// 副作用导入：runtime 模块被求值时才安装拖拽、右键菜单等初始化逻辑。
// 少了这一行，无边框窗口的 --wails-draggable 不会生效。
import "/wails/runtime.js";
import { Call, Events, Window } from "/wails/runtime.js";

/**
 * 待验证的绑定全限定名，必须与 Go 侧 probe.BindingFQN() 一致（报告里同时记录两边）。
 * 名字形态是「包导入路径.类型名.方法名」，不是 v2 的 main.App。
 */
const BINDING = "github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-v3-spike/internal/probe.Service.Ping";

/** 事件是否被收到的观察结果，跨多次采集共享。 */
const seen = { early: false, announce: false };

/** Go 侧事实（由 spike:go / spike:announce 事件带回）。 */
let goFacts = null;

eventsSubscribe();
void run();

/** 订阅 Go 侧事件。窗口就绪前后都可能到达，因此订阅必须在页面加载的第一时间完成。 */
function eventsSubscribe() {
  Events.On("spike:early", () => {
    seen.early = true;
  });
  Events.On("spike:announce", (event) => {
    seen.announce = true;
    if (event && event.data) {
      goFacts = event.data;
    }
    // announce 常常比页面脚本更早触发，收到后立刻补发一次事实，避免报告停留在旧快照上。
    void publish();
  });
  Events.On("spike:go", (event) => {
    if (event && event.data) {
      goFacts = event.data;
    }
  });
}

/**
 * 主流程：握手 → 验证绑定 → 回传事实。
 *
 * 顺序有意如此：绑定验证放在前面，这样即使后续采集抛错，绑定这条结论也已经回传。
 */
async function run() {
  await Events.Emit("spike:hello", { at: Date.now() });
  await Events.Emit("spike:binding", await probeBinding());
  await publish();
  wireTitlebar();
  wireMic();
  // 页面加载后立即尝试一次音频采集：这会触发 WebKit 的 permission-request 信号，
  // 从而验证 GTK4 侧的权限钩子是否挂上并放行（结果同时写进 Go 侧报告）。
  await requestMic();
}

/** 接上手动重试按钮：没有设备或首次被拒时需要人工再触发一次。 */
function wireMic() {
  const node = document.querySelector("#btn-mic");
  if (!node) {
    return;
  }
  node.addEventListener("click", () => {
    void requestMic();
  });
}

/** 请求一次麦克风采集，并把结果回传 Go 侧。 */
async function requestMic() {
  const result = await probeMic();
  const label = document.querySelector("#mic-result");
  if (label) {
    label.textContent = result.ok ? "麦克风可用（已放行）" : `失败：${result.error}`;
    label.className = result.ok ? "ok" : "fail";
  }
  await Events.Emit("spike:mic", result);
  return result;
}

/**
 * 采集一次音频并立刻释放。
 *
 * 用音频而非视频：客户端的真实需求就是语音输入，且它正是权限钩子要放行的范围；
 * 摄像头请求按策略应被拒绝，留给手工验证，不在自动流程里制造噪声。
 */
async function probeMic() {
  if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
    return { ok: false, error: "navigator.mediaDevices.getUserMedia 不存在" };
  }
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    for (const track of stream.getTracks()) {
      track.stop();
    }
    return { ok: true, error: "" };
  } catch (err) {
    return { ok: false, error: String(err) };
  }
}

/** 调用一次绑定方法并归一化结果。 */
async function probeBinding() {
  try {
    const reply = await Call.ByName(BINDING, "spike");
    return { ok: true, reply: String(reply), called: BINDING };
  } catch (err) {
    return { ok: false, error: String(err), called: BINDING };
  }
}

/** 采集一次页面事实并回传。可重复调用，报告会被覆盖为最新快照。 */
async function publish() {
  const facts = collect();
  await Events.Emit("spike:frontend", facts);
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
  };
}

/**
 * 在壳自己的 origin 下做一次 localStorage 读写。
 *
 * 为什么要专门验证：客户端的语言、字体、终端字号都按 origin 存在 localStorage 里，
 * v2 的壳 origin 是 wails://wails；若 v3 换了 origin，这些设置会在升级后静默丢失。
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

/** 读取标题栏 --wails-draggable 的计算值，确认该属性没有被样式覆盖或丢弃。 */
function draggableValue() {
  const bar = document.querySelector("#titlebar");
  if (!bar) {
    return "(缺少 #titlebar)";
  }
  return getComputedStyle(bar).getPropertyValue("--wails-draggable").trim() || "(空)";
}

/** 接上自绘标题栏的窗口控制按钮。 */
function wireTitlebar() {
  const bind = (id, action) => {
    const node = document.querySelector(id);
    if (!node) {
      return;
    }
    node.addEventListener("click", () => {
      // 浏览器预览或 runtime 未就绪时静默降级：这些按钮不值得为一次失败打断采集。
      Promise.resolve(action()).catch(() => {});
    });
  };
  bind("#btn-min", () => Window.Minimise());
  bind("#btn-max", () => Window.ToggleMaximise());
  bind("#btn-close", () => Window.Close());
}

/** 把事实渲染成表格，便于真机上一眼确认结果。 */
function render(facts) {
  const rows = [
    ["绑定调用", goFacts ? "已回传" : "等待 Go 侧", true],
    ["页面 origin", facts.origin, facts.origin === "wails://wails"],
    ["页面 href", facts.href, true],
    ["localStorage", facts.localStorageOK ? "可读写" : facts.localStorageDetail, facts.localStorageOK],
    ["window.wails", String(facts.runtimeGlobal), facts.runtimeGlobal],
    ["window.go / window.runtime", String(facts.classicGlobals), !facts.classicGlobals],
    ["早期事件（就绪前）", String(facts.earlyEventSeen), true],
    ["就绪事件", String(facts.announceSeen), facts.announceSeen],
    ["--wails-draggable", facts.draggableValue, facts.draggableValue === "drag"],
    ["屏幕", facts.screen, true],
    ["UserAgent", facts.userAgent, true],
  ];
  if (goFacts) {
    rows.push(["后端", `${goFacts.backend} · ${goFacts.stackVersion}`, true]);
  }

  const tbody = document.querySelector("#facts tbody");
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

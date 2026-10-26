/* dsh 桌面壳桥：外链转发 + 客户端启动失败上报 + 语言上报 + GUI 内容存活性上报。
 *
 * 由打包流程（inject-link-bridge.sh）注入到桌面版 harness Web GUI 的 dist，
 * 与页面同源加载，并先于 <head> 里的 module 入口执行（module 脚本延迟）。
 * 仅在被 iframe 内嵌时生效；独立打开页面时保持原生行为。
 *
 * 协议与启动器侧保持一致：{ dshDesktop: true, type: "open-external", url }。
 *
 * 职责一：外链转发。Wails WebKitGTK 不创建新浏览上下文，iframe 内
 * target="_blank" 的外链点击与脚本发起的 window.open 都会被吞掉，因此两条路径
 * 都要兜住：原生默认动作由点击监听接管；而客户端对普通左键是先 preventDefault
 * 再 window.open（见 packages/client/ui-primitives 的 MarkdownAnchor），默认动作
 * 根本不会发生，只监听点击收不到这类操作，所以还要在客户端入口之前替换
 * window.open。两者都把 URL 通过 postMessage 交给宿主壳
 * （apps/linyaps/desktop-launcher/frontend/app.js 监听），由它经
 * window.runtime.BrowserOpenURL → 随包 xdg-open → 宿主 portal → 本机默认浏览器打开。
 * 非 http(s) 目标（如账号登录预开的 about:blank）原样透传给 WebKit 自己的实现。
 *
 * 职责二：客户端启动失败上报。宿主对非必需条目的激活失败只告警，harness 进程
 * 因此可能完全健康，失败只发生在浏览器侧的插件树上——窗口里只剩一张
 * "Failed to load plugins" 死路页，壳却以为一切正常，用户看不到诊断与安全模式
 * 入口。本桥侦测该状态后以 { dshDesktop: true, type: "boot-failed", message }
 * 上报，由壳改显自己的启动失败页并触发自动诊断。两条通道只上报一次：
 *   1) 包装 console.error：客户端 boot 内核在 catch 里 console.error(原因)
 *      （packages/client/web/src/boot.ts），是唯一能拿到完整原因的通道；
 *      其文案固定以 "web boot:" 开头，正常启动的进度日志不走 console.error，
 *      因此不会误判。
 *   2) 观察 DOM 兜底：失败页根节点带 data-dsh-boot（boot-page.ts），一旦出现
 *      且文案为 "Failed to load plugins" 即上报，覆盖失败不经过 console.error
 *      的上游改动。代价是对上游这两处文案有依赖，两处同时改才会同时失效。
 *
 * 职责三：语言上报。壳（启动器）的全部文案跟随 harness GUI 的生效语言，而 GUI 的
 * 生效语言由它自己的 <html lang> 表达（客户端 locale 服务在每次语言快照变化时同步
 * 该属性），因此以 { dshDesktop: true, type: "locale", id } 上报该值即可，壳不需要
 * 读 settings.yaml、也不需要在 GUI 里再开一个入口。真源选型的取舍见
 * apps/linyaps/desktop-launcher/docs/i18n.md 第五节。
 *
 * 职责四：GUI 内容存活性上报。白屏的另一种形态是"文档还在、客户端内容整个不见了"
 * ——客户端模块入口没跑起来（挂载点一直为空），或应用挂载后被清空。此时壳只看得到
 * iframe 还在，没有任何判据，用户只能靠重启 harness 恢复。本桥按周期检查挂载点还有
 * 没有内容，确认消失后以 { dshDesktop: true, type: "boot-stalled", message } 上报，
 * 由壳重载 iframe（不重启 harness）。与职责二的边界：职责二判"上游自己报了失败"，
 * 这里判"上游没有报错、内容只是不出现"。
 *
 * 职责五：当前会话的恢复与上报。客户端把「上次打开的会话」记在 localStorage，而内嵌
 * iframe 的这份存储退出后不落盘（见 README 的渲染层一节），客户端自己记不住这件事，
 * 每次启动都只能开一个空会话。本桥启动时把壳送进来的
 * `__DSH_DESKTOP_RESTORE_SESSION__`（索引注入，见 internal/appenv/restore_session.go）
 * 写回存储键——先于客户端入口，因为它延迟执行——并按周期把当前选择以
 * { dshDesktop: true, type: "current-session", sessionId } 上报，由壳存进 config.json。
 */
(function () {
  "use strict";
  if (window.parent === window) return;

  // targetOrigin 优先使用父窗口真实 origin（WebKitGTK 支持
  // location.ancestorOrigins），仅在不支持时回退 "*"；
  // 接收侧（app.js）已通过 e.source === frame.contentWindow
  // 做二次校验，确保消息只来自预期的 iframe。
  function targetOrigin() {
    return (window.location.ancestorOrigins && window.location.ancestorOrigins[0])
      ? window.location.ancestorOrigins[0]
      : "*";
  }

  function isHttpUrl(value) {
    try {
      var protocol = new URL(value).protocol;
      return protocol === "http:" || protocol === "https:";
    } catch (err) {
      return false;
    }
  }

  /** 把一条 http(s) 外链交给壳打开；调用方负责判明它确实是外链。 */
  function forwardExternal(href) {
    window.parent.postMessage({ dshDesktop: true, type: "open-external", url: href }, targetOrigin());
  }

  // 路径一：原生默认动作。Ctrl/⌘+点击等客户端没有接管的点击会走到这里。
  document.addEventListener("click", function (event) {
    if (event.defaultPrevented || event.button !== 0) return;
    var node = event.target;
    var anchor = node instanceof Element
      ? node.closest('a[target="_blank"]')
      : (node && node.parentElement ? node.parentElement.closest('a[target="_blank"]') : null);
    if (anchor === null || anchor.hasAttribute("download")) return;
    var href = anchor.getAttribute("href");
    if (href === null || !isHttpUrl(href)) return;
    event.preventDefault();
    forwardExternal(href);
  }, false);

  // 路径二：脚本打开新窗口。客户端对普通左键先 preventDefault 再 window.open，
  // 上面那条监听因此看不到它；而 Wails 的 WebKitGTK 不创建新浏览上下文，这次
  // 调用会被静默丢弃，链接看起来毫无反应。替换 window.open 是唯一能覆盖这条路径
  // 的位置：客户端模块入口延迟执行，本桥是普通脚本，先于它运行。
  //
  // 为什么 http(s) 返回 null：壳是异步打开的，不存在可返回的真实窗口；现有调用点
  // 都忽略该返回值，真正依赖返回值与 location 的是登录弹窗那条，而它的目标是
  // about:blank（非 http(s)），仍走原实现。
  var nativeOpen = typeof window.open === "function" ? window.open : null;
  window.open = function (url, name, features) {
    var href = "";
    try {
      href = url == null ? "" : String(url);
    } catch (err) {
      // 形如 Symbol 的不可字符串化目标：按"不是外链"处理，交给原实现。
      href = "";
    }
    if (isHttpUrl(href)) {
      forwardExternal(href);
      return null;
    }
    return nativeOpen === null ? null : nativeOpen.call(window, url, name, features);
  };

  /* ---------- 客户端启动失败上报 ---------- */

  // 本次页面生命周期内只上报一次：客户端可能重试，重复上报对壳没有意义。
  var reported = false;
  // 最近一次捕获到的 boot 失败原因；DOM 兜底通道用它作为具体原因。
  var bootError = "";

  /** 上报启动失败；已上报过则忽略。message 与已捕获原因都为空时用兜底文案。 */
  function reportBootFailure(message) {
    if (reported) return;
    reported = true;
    window.parent.postMessage({
      dshDesktop: true,
      type: "boot-failed",
      message: message || bootError || "客户端插件加载失败（未捕获到具体原因）",
    }, targetOrigin());
  }

  var originalConsoleError = console.error;
  console.error = function () {
    var parts = [];
    for (var i = 0; i < arguments.length; i++) {
      var arg = arguments[i];
      // 只取 message 不取 stack：失败页要告诉用户"哪几个插件没激活"，
      // JS 调用栈在这里只会挤掉真正有用的信息。判据用鸭子类型而非
      // instanceof Error：跨 realm 造出来的错误（iframe、worker）同样识别。
      var hasMessage = arg !== null && typeof arg === "object" && typeof arg.message === "string";
      parts.push(hasMessage ? arg.message : String(arg));
    }
    var text = parts.join(" ");
    if (text.indexOf("web boot:") !== -1) reportBootFailure(text);
    originalConsoleError.apply(console, arguments);
  };

  // DOM 兜底通道：引导页根节点带 data-dsh-boot（boot-page.ts:36），激活失败时节点
  // 内出现 "Failed to load plugins"（boot-page.ts:92）。应用挂载成功后 mountApp 用
  // hydrate 接管并移除该节点（ui-renderer/src/client/index.ts:72），因此这里以"节点
  // 消失"作为观察终点——观察窗口只覆盖 boot 阶段，应用运行期不再有回调开销。
  var container = document.getElementById("root") || document.body;
  var seenBootPage = false;
  var bootWatcher = new MutationObserver(function () {
    var page = container.querySelector("[data-dsh-boot]");
    if (page === null) {
      if (seenBootPage) {
        // 应用挂载完成，locale 服务此时已激活：补报一次语言。详见下方"语言上报"
        // 一节——函数声明在本作用域内提升，此处调用不受定义位置影响。
        reportLocale();
        bootWatcher.disconnect();
      }
      return;
    }
    seenBootPage = true;
    if (page.textContent !== null && page.textContent.indexOf("Failed to load plugins") !== -1) {
      reportBootFailure(bootError);
      bootWatcher.disconnect();
    }
  });
  bootWatcher.observe(container, { childList: true, subtree: true });

  /* ---------- GUI 内容存活性 ---------- */

  // 判据只有两条，且都只在"正常态不可能出现"时成立：挂载点连续为空；引导页存在超过
  // 阈值仍未消失。因此失效方向是"不报"（上游改了挂载点 id 或引导页标记），不会误伤
  // 正常界面。周期检查只读 childElementCount 与一个选择器，不挂 MutationObserver，
  // 运行期开销可忽略。
  var LIVENESS_TICK_MS = 5000;
  var LIVENESS_EMPTY_STRIKES = 2; // 连续两次空容器（≈10s）才算内容消失
  var LIVENESS_BOOT_STALL_TICKS = 6; // 引导页存在 ≈30s 仍未挂载

  var stalledReported = false;
  var emptyStrikes = 0;
  var bootPageTicks = 0;

  /** 上报"内容消失"；本次页面生命周期内只上报一次，之后停止周期检查。 */
  function reportBootStalled(message) {
    if (stalledReported) return;
    stalledReported = true;
    clearInterval(livenessTimer);
    window.parent.postMessage({ dshDesktop: true, type: "boot-stalled", message: message }, targetOrigin());
  }

  var livenessTimer = setInterval(function () {
    if (stalledReported) return;
    if (container === null) {
      // 桥可能在 <body> 之前执行：当时取不到挂载点，这一轮补一次解析。
      container = document.getElementById("root") || document.body;
      return;
    }
    if (container.childElementCount === 0) {
      emptyStrikes += 1;
      if (emptyStrikes >= LIVENESS_EMPTY_STRIKES) {
        reportBootStalled("客户端界面内容为空（挂载点里没有内容）");
      }
      return;
    }
    emptyStrikes = 0;
    if (container.querySelector("[data-dsh-boot]") !== null) {
      bootPageTicks += 1;
      if (bootPageTicks >= LIVENESS_BOOT_STALL_TICKS) {
        reportBootStalled(
          "客户端界面在 " + String((LIVENESS_BOOT_STALL_TICKS * LIVENESS_TICK_MS) / 1000) + " 秒内未完成挂载（引导页仍在）"
        );
      }
    } else {
      bootPageTicks = 0;
    }
  }, LIVENESS_TICK_MS);

  /* ---------- 语言上报 ---------- */

  // 已上报过的语言；重复上报对壳没有意义，也避免观察回调里反复 postMessage。
  var reportedLang = null;

  /** 上报当前文档语言；取不到或与上次相同则忽略。 */
  function reportLocale() {
    var lang = document.documentElement.lang;
    if (lang === "" || lang === reportedLang) return;
    reportedLang = lang;
    window.parent.postMessage({ dshDesktop: true, type: "locale", id: lang }, targetOrigin());
  }

  // 通道一：<html lang> 变化。覆盖"用户在 GUI 设置里切换语言"。
  //
  // 只有这一条会漏掉一种组合：服务端 index.html 的默认 lang 恰好等于生效语言
  // （中文系统 + 用户在 GUI 里选英文），此时属性自始至终没变过，观察回调不会触发，
  // 壳就会一直用系统语言。因此再由引导页消失时的 reportLocale()（通道二，见上）
  // 在应用挂载后补报一次——那一刻 locale 服务必然已激活。
  new MutationObserver(reportLocale).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["lang"],
  });

  /* ---------- 当前会话的恢复与上报 ---------- */

  // 为什么要由壳记住这个 id：客户端把「上次打开的会话」写在 localStorage 里，而这
  // 份存储在内嵌 iframe 中按第三方存储对待，退出后不落盘（见 README 的渲染层一节）。
  // 客户端于是每次启动都读不到上次的选择，只能开一个空会话。壳把 id 记在自己的
  // config.json 里，下次启动经索引注入以 __DSH_DESKTOP_RESTORE_SESSION__ 送进来，
  // 这里在客户端启动前写回存储键，客户端便照常恢复。
  //
  // 客户端私有的键名是这条通路的代价：它一旦改名，恢复与上报都会静默失效，只剩下
  // 客户端自己的回退（打开该工作区最近的非空会话），不会更糟。
  var SELECTION_KEY = "dsh.sessions.current";

  /** 读取客户端记录的当前会话 id；取不到或形状不对时返回 null。 */
  function readSelection() {
    try {
      var raw = window.localStorage.getItem(SELECTION_KEY);
      if (raw === null) return null;
      var parsed = JSON.parse(raw);
      var id = parsed !== null && typeof parsed === "object" ? parsed.sessionId : null;
      return typeof id === "string" && id !== "" ? id : null;
    } catch (err) {
      return null;
    }
  }

  // 写回壳记住的 id，只在客户端自己没有选择时填进去：在存储仍然可用的环境里
  // （普通浏览器、官方 Electron 客户端），客户端记着的那份才是权威，壳的记忆最多
  // 比它旧 2 秒，覆盖它只会倒退。
  var restoreId = window.__DSH_DESKTOP_RESTORE_SESSION__;
  if (typeof restoreId === "string" && restoreId !== "" && readSelection() === null) {
    try {
      window.localStorage.setItem(SELECTION_KEY, JSON.stringify({ sessionId: restoreId }));
    } catch (err) {
      // 忽略：恢复失败不该影响页面本身。
    }
  }

  // 上报当前会话：壳据此更新自己的记忆。
  //
  // 用轮询而不是监听 storage 事件：同一文档内的 setItem 不触发 storage 事件，而写
  // 这个键的正是同一个文档里的客户端。选择变化是用户动作，2 秒的粒度足够。
  var SESSION_TICK_MS = 2000;
  var reportedSession = null;

  /** 会话选择变化时上报一次；null 表示当前没有选中的会话。 */
  function reportCurrentSession() {
    var id = readSelection();
    if (id === reportedSession) return;
    reportedSession = id;
    window.parent.postMessage({ dshDesktop: true, type: "current-session", sessionId: id }, targetOrigin());
  }

  setInterval(reportCurrentSession, SESSION_TICK_MS);
  reportCurrentSession();
})();

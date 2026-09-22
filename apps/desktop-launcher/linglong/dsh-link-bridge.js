/* dsh 桌面壳桥：外链转发 + 客户端启动失败上报 + 语言上报。
 *
 * 由打包流程（inject-link-bridge.sh）注入到桌面版 harness Web GUI 的 dist，
 * 与页面同源加载，并先于 <head> 里的 module 入口执行（module 脚本延迟）。
 * 仅在被 iframe 内嵌时生效；独立打开页面时保持原生行为。
 *
 * 协议与启动器侧保持一致：{ dshDesktop: true, type: "open-external", url }。
 *
 * 职责一：外链转发。Wails WebKitGTK 不创建新浏览上下文，iframe 内
 * target="_blank" 的外链点击会被吞掉；本桥把这类点击的 URL 通过 postMessage
 * 交给宿主壳（apps/desktop-launcher/frontend/app.js 监听），由它经
 * window.runtime.BrowserOpenURL → 随包 xdg-open → 宿主 portal → 本机默认浏览器打开。
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
 * apps/desktop-launcher/docs/i18n.md 第五节。
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
    window.parent.postMessage({ dshDesktop: true, type: "open-external", url: href }, targetOrigin());
  }, false);

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
})();

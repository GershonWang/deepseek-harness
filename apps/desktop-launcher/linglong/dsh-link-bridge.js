/* dsh 桌面壳外部链接桥。
 *
 * 由打包流程（inject-link-bridge.sh）注入到桌面版 harness Web GUI 的 dist，
 * 与页面同源加载。Wails WebKitGTK 不创建新浏览上下文，iframe 内
 * target="_blank" 的外链点击会被吞掉；本桥把这类点击的 URL 通过
 * postMessage 交给宿主壳（apps/desktop-launcher/frontend/app.js 监听），
 * 由它经 window.runtime.BrowserOpenURL → 随包 xdg-open → 宿主 portal →
 * 本机默认浏览器打开。
 *
 * 协议与启动器侧保持一致：{ dshDesktop: true, type: "open-external", url }。
 * 仅在被 iframe 内嵌时生效；独立打开页面时保持原生 target="_blank" 行为。
 */
(function () {
  "use strict";
  if (window.parent === window) return;

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
    window.parent.postMessage({ dshDesktop: true, type: "open-external", url: href }, "*");
  }, false);
})();
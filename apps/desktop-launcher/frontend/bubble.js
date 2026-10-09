/* 切换器悬浮球的前端逻辑：轮询 Go 侧状态、单击切换形态、右键开菜单、拖拽移动。
 *
 * 为什么用轮询而不是事件推送：客户端可能在切换器之外结束（用户直接关掉客户端窗口），
 * 事件推送需要在 Go 侧挂子进程回收钩子再跨窗口广播；而这里的状态量极小，固定间隔
 * 轮询更简单，也不会漏掉任何一种变化。
 *
 * 拖拽为什么手写而不用 CSS 的 --wails-draggable：那个属性把整块区域交给窗口管理器，
 * 拖拽期间点击事件不再派发，于是「单击切换」直接失效。这里自己实现，并用位移阈值把
 * 一次拖拽与一次点击区分开。
 *
 * 本文件不含产品文案：文案一律来自 locales/ 字典（仓库闸门按路径认定字典所有者）。
 */
"use strict";

(function () {
  /** 状态轮询间隔：客户端退出到悬浮球反映出来，最多差这一拍。 */
  var POLL_MS = 1000;
  /** 悬浮球窗口的边长，与 main.go 的 bubbleSize 一致（球 64 + 四周 12 的阴影留白）。 */
  var BUBBLE_SIZE = 88;
  /** 菜单或错误展开时的窗口尺寸：要容下最长的一条菜单项（min-width 168），
   *  并给球与阴影留出空间。 */
  var EXPANDED = { width: 232, height: 236 };
  /** 位移超过这个像素数才算拖拽；低于它按点击处理，避免手抖把单击吃掉。 */
  var DRAG_THRESHOLD = 4;

  var bubbleEl = document.getElementById("bubble");
  var menuEl = document.getElementById("menu");
  var errorEl = document.getElementById("error");
  var expanded = false;
  /** 当前这一次按住的状态；未按住时为 null。winX/winY 在异步取回窗口位置前为 null。 */
  var drag = null;
  /** 刚刚结束的是一次拖拽：紧接着的那次 click 不该被当成切换。 */
  var justDragged = false;
  var positionRestored = false;

  /**
   * 取 Wails 绑定。命名规则是 <Go 包名>.<结构体名>，本结构体在 internal/switchboard
   * 包，因此是 window.go.switchboard.Switchboard——不是 main.Switchboard（薄壳客户端
   * 用的是 window.go.app.App，同一条规则）。绑定在页面加载后才注入，取不到时返回
   * undefined，调用方按「尚未就绪」处理，而不是抛异常把整个事件处理器打断。
   * @returns {object|undefined} 绑定对象。
   */
  function bindings() {
    return window.go && window.go.switchboard && window.go.switchboard.Switchboard;
  }

  /** 形态的可读名，供 aria-label 使用。
   * @param {string} mode - Go 侧返回的形态标识。
   * @returns {string} 当前语言下的形态名。
   */
  function modeLabel(mode) {
    if (mode === "shell") return DSHI18N.t("bubble.modeShell");
    if (mode === "official") return DSHI18N.t("bubble.modeOfficial");
    return DSHI18N.t("bubble.noClient");
  }

  /** 展开或收回窗口。菜单与错误提示都放不进 72×72，必须借窗口尺寸。
   * @param {boolean} want - 是否展开。
   */
  function setExpanded(want) {
    if (expanded === want) return;
    expanded = want;
    window.runtime.WindowSetSize(want ? EXPANDED.width : BUBBLE_SIZE, want ? EXPANDED.height : BUBBLE_SIZE);
  }

  /** 用一份 Go 侧状态快照重绘悬浮球。
   * @param {{mode: string, defaultMode: string, switching: boolean, error: string}} status - 状态快照。
   */
  function render(status) {
    document.body.dataset.mode = status.mode || "";
    document.body.dataset.switching = status.switching ? "1" : "0";
    // aria-label 带上当前形态，读屏用户也能听出跑的是哪个客户端。
    bubbleEl.setAttribute("aria-label", DSHI18N.t("bubble.switch") + "：" + modeLabel(status.mode));
    if (status.error) {
      // Go 侧给的是技术原因（英文诊断），用户可见的提示框架在这里本地化。
      errorEl.textContent = DSHI18N.t("bubble.switchFailed") + "：" + status.error;
      errorEl.hidden = false;
      // 错误提示排在球下方，72×72 的窗口装不下；不展开就等于没有提示。
      if (menuEl.hidden) setExpanded(true);
    } else {
      errorEl.hidden = true;
      if (menuEl.hidden) setExpanded(false);
    }
  }

  /** 还原上次拖拽留下的位置。只做一次：之后位置由本次拖拽维护，重复设置会让
   *  用户刚拖到的地方被旧值拽回去。 */
  function restorePosition() {
    var api = bindings();
    if (!api) return;
    api.BubblePosition().then(function (pos) {
      if (pos && pos.set) window.runtime.WindowSetPosition(pos.x, pos.y);
    }).catch(function () {});
  }

  /** 拉取一次状态；失败（例如 Go 侧还没就绪）时保持上一帧，不闪回默认值。 */
  function poll() {
    var api = bindings();
    if (!api) return;
    if (!positionRestored) {
      positionRestored = true;
      restorePosition();
    }
    api.Status().then(render).catch(function () {});
  }

  function openMenu() {
    menuEl.hidden = false;
    setExpanded(true);
  }

  function closeMenu() {
    menuEl.hidden = true;
    setExpanded(false);
  }

  // 拖拽：按下时先记鼠标位置，窗口位置异步取回后再开始跟随，避免用过期坐标把球拽飞。
  bubbleEl.addEventListener("mousedown", function (event) {
    if (event.button !== 0) return;
    drag = { mouseX: event.screenX, mouseY: event.screenY, winX: null, winY: null, moved: false };
    window.runtime.WindowGetPosition().then(function (pos) {
      if (!drag) return;
      drag.winX = pos.x;
      drag.winY = pos.y;
    }).catch(function () {});
  });

  document.addEventListener("mousemove", function (event) {
    if (!drag || drag.winX === null) return;
    var dx = event.screenX - drag.mouseX;
    var dy = event.screenY - drag.mouseY;
    if (!drag.moved && Math.abs(dx) + Math.abs(dy) < DRAG_THRESHOLD) return;
    drag.moved = true;
    window.runtime.WindowSetPosition(drag.winX + dx, drag.winY + dy);
  });

  document.addEventListener("mouseup", function () {
    if (!drag) return;
    var wasDragged = drag.moved;
    drag = null;
    justDragged = wasDragged;
    if (!wasDragged) return;
    // 落定后再记一次真实位置：拖拽过程中的坐标是本地推算的，以窗口自己的读数为准。
    window.runtime.WindowGetPosition().then(function (pos) {
      var api = bindings();
      if (api) api.SaveBubblePosition(pos.x, pos.y).catch(function () {});
    }).catch(function () {});
  });

  bubbleEl.addEventListener("click", function () {
    // 拖拽结束的那次 click 不是切换意图。
    if (justDragged) {
      justDragged = false;
      return;
    }
    var api = bindings();
    if (!api) return;
    // 切换进行中再点会排队成第二次切换，这里直接忽略。
    if (document.body.dataset.switching === "1") return;
    api.Switch().catch(function () {});
  });

  bubbleEl.addEventListener("contextmenu", function (event) {
    event.preventDefault();
    if (menuEl.hidden) openMenu();
    else closeMenu();
  });

  menuEl.addEventListener("click", function (event) {
    var button = event.target.closest("button");
    if (!button) return;
    var api = bindings();
    if (button.dataset.default) {
      if (api) api.SetDefault(button.dataset.default).catch(function () {});
      closeMenu();
    } else if (button.dataset.action === "quit") {
      window.runtime.Quit();
    }
  });

  // 点击别处收起菜单：菜单占着窗口的大部分，不收起来会挡住球本身。
  document.addEventListener("click", function (event) {
    if (menuEl.hidden) return;
    if (menuEl.contains(event.target) || event.target === bubbleEl) return;
    closeMenu();
  });

  DSHI18N.init();
  poll();
  setInterval(poll, POLL_MS);
})();

/* 壳前端国际化运行时：locale 解析、字典查找、DOM 回填与 Go 侧同步。
 *
 * 真源是 iframe 内 harness GUI 的 `<html lang>`：harness 客户端 locale 服务在每次
 * 语言快照变化时同步该属性，由 linglong/dsh-link-bridge.js 经既有 postMessage 桥
 * 上报；GUI 尚未起来时（加载页 / 预检页 / 失败页）按 navigator 兜底，再退回 en。
 * 真源为什么这样选、三条候选路径的取舍，见 docs/i18n.md 第五节。
 *
 * 本文件**不含任何产品文案**：文案只允许出现在 locales/ 目录下，否则闸门会拒绝。
 * 加载顺序由 index.html 保证：locales/zh.js → locales/en.js → i18n.js → app.js。
 *
 * 为什么用 IIFE：app.js 同样是全局脚本，顶层声明会共享同一个全局作用域；这里
 * 只把 DSHI18N 这一个门面暴露出去，避免与 app.js 的模块级名字相互污染。
 */
"use strict";

(function () {
  /** 内置语言：与 harness 客户端 locale 插件及 Electron 壳（apps/desktop）一致。 */
  var LOCALE_IDS = ["zh", "en"];

  /** 兜底语言：无任何可用语言信息时使用（对齐客户端的 FALLBACK_LOCALE）。 */
  var FALLBACK_ID = "en";

  /** 文档语言标签：与客户端 syncDocumentLanguage 的映射保持一致（zh → zh-CN）。 */
  var DOCUMENT_LANG = { zh: "zh-CN", en: "en" };

  /** 属性钩子：`data-i18n-<suffix>` 决定把文案回填到哪个属性。 */
  var ATTRIBUTE_HOOKS = ["title", "placeholder", "aria-label", "alt"];

  var currentId = FALLBACK_ID;
  var changeListeners = [];

  /** 字典表；locales/*.js 未加载时为空表，缺键按「回退 zh → 键名」处理。 */
  function dictionaries() {
    return window.DSH_LOCALES || {};
  }

  /**
   * 把任意语言标签归一化为内置语言 id。
   *
   * 主语言子标签之外的部分（地区、编码、修饰）一律丢弃：壳只有 zh/en 两套字典，
   * `zh-Hans`、`zh_CN.UTF-8` 与 `zh` 对壳是同一件事。识别不了的语言（如三方语言包
   * 的 `ja`）返回空串，由调用方决定回退，而不是在这里假装它等于某个内置语言。
   * @param {?string} raw - 形如 `zh-CN`、`zh_CN.UTF-8`、`en` 的标签。
   * @returns {string} 内置语言 id；无法识别时为空串。
   */
  function normalize(raw) {
    if (typeof raw !== "string") return "";
    var primary = raw.trim().toLowerCase().split(/[-_.@]/)[0];
    return LOCALE_IDS.indexOf(primary) >= 0 ? primary : "";
  }

  /**
   * 按浏览器语言解析初始 locale。
   *
   * 遍历顺序与客户端 detectBrowserLocale 相同（languages 优先、language 兜底），
   * 使壳在 GUI 起来之前的语言与 GUI 自己的兜底结果一致。缺语言信息（非浏览器
   * 运行、测试桩）时回退 en。
   * @returns {string} 内置语言 id。
   */
  function fromNavigator() {
    var nav = navigator || {};
    var tags = (nav.languages || []).concat(nav.language ? [nav.language] : []);
    for (var i = 0; i < tags.length; i++) {
      var id = normalize(tags[i]);
      if (id) return id;
    }
    return FALLBACK_ID;
  }

  /**
   * 按当前 locale 查表。
   *
   * 回退链是「当前语言 → zh → 键名本身」：zh 是键全集真源，缺键时显示键名而不是
   * 空串，让漏配在界面上直接可见（与客户端 lookup 的失败表现一致）。
   * @param {string} key - 点分命名空间的键。
   * @returns {string} 文案或键名本身。
   */
  function lookup(key) {
    var dicts = dictionaries();
    var own = dicts[currentId];
    if (own && Object.prototype.hasOwnProperty.call(own, key)) return own[key];
    var zh = dicts.zh;
    if (zh && Object.prototype.hasOwnProperty.call(zh, key)) return zh[key];
    return key;
  }

  /**
   * 替换 `{name}` 占位符。
   *
   * 未提供的占位符原样保留（而不是替换成空串）：漏传参数时界面上留下 `{name}`，
   * 比静默少一段文字更容易被发现。
   * @param {string} text - 含占位符的文案。
   * @param {?Object<string, *>} params - 占位符取值。
   * @returns {string} 替换后的文案。
   */
  function format(text, params) {
    if (!params) return text;
    return text.replace(/\{([^{}]+)\}/g, function (whole, name) {
      return Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : whole;
    });
  }

  /**
   * 翻译一个键。
   * @param {string} key - 点分命名空间的键。
   * @param {?Object<string, *>} params - 占位符取值。
   * @returns {string} 当前语言下的文案。
   */
  function t(key, params) {
    return format(lookup(key), params);
  }

  /** 把 `<html lang>` 指向当前语言，与客户端 syncDocumentLanguage 同规则。 */
  function syncDocumentLanguage() {
    document.documentElement.lang = DOCUMENT_LANG[currentId] || currentId;
  }

  /**
   * 回填静态文案：`data-i18n` 写元素文本，`data-i18n-<属性>` 写同名属性。
   *
   * index.html 里不留中文兜底文案（单一真源，避免字典与 HTML 两份漂移），因此
   * 这一步是静态文案唯一的来源；P1 起 index.html 的元素只保留钩子。
   * @param {?Document} root - 回填范围；缺省为整个文档。
   */
  function fill(root) {
    if (!root || typeof root.querySelectorAll !== "function") return;
    var texts = root.querySelectorAll("[data-i18n]");
    for (var i = 0; i < texts.length; i++) {
      texts[i].textContent = t(texts[i].getAttribute("data-i18n"));
    }
    for (var h = 0; h < ATTRIBUTE_HOOKS.length; h++) {
      var hook = ATTRIBUTE_HOOKS[h];
      var selector = "[data-i18n-" + hook + "]";
      var tagged = root.querySelectorAll(selector);
      for (var j = 0; j < tagged.length; j++) {
        tagged[j].setAttribute(hook, t(tagged[j].getAttribute("data-i18n-" + hook)));
      }
    }
  }

  /** 把当前语言回推给 Go：Go 侧文案在渲染前需要知道语言（见 docs/i18n.md 5.5）。 */
  function pushToGo() {
    var app = window.go && window.go.app && window.go.app.App;
    if (!app || typeof app.SetLocale !== "function") return;
    var pending;
    try {
      pending = app.SetLocale(currentId);
    } catch (err) {
      // 壳二进制可能比前端旧（方法不存在）时同步失败，不影响页面本身的行为。
      return;
    }
    if (pending && typeof pending.catch === "function") {
      // 同步失败无需打扰用户：语言已经在前端生效，Go 侧退回自己的兜底值。
      pending.catch(function () {});
    }
  }

  /** 通知订阅者语言已变；单个订阅者抛错不能拖累其余订阅者。 */
  function notify() {
    for (var i = 0; i < changeListeners.length; i++) {
      try {
        changeListeners[i](currentId);
      } catch (err) {
        console.error("i18n listener crashed:", err);
      }
    }
  }

  /**
   * 应用一个语言。
   *
   * 识别不了的语言一律落到 en（与仓库约定一致：无已注册语言时用英文），而不是
   * 保持上一个语言——保持上一个会让"切到三方语言包"看起来像没生效。
   * @param {?string} id - 语言标签，可带地区与编码后缀。
   */
  function apply(id) {
    var next = normalize(id) || FALLBACK_ID;
    var changed = next !== currentId;
    currentId = next;
    syncDocumentLanguage();
    fill(document);
    pushToGo();
    if (changed) notify();
  }

  window.DSHI18N = {
    /** 按浏览器语言完成首次应用；由 app.js 的 init 在首次渲染前调用。 */
    init: function () {
      apply(fromNavigator());
    },
    /** 应用 GUI 上报的语言；由 app.js 的消息监听器调用。 */
    applyFromGui: function (id) {
      apply(id);
    },
    /** 翻译一个键。 */
    t: t,
    /** 当前语言 id。 */
    current: function () {
      return currentId;
    },
    /** 语言标签归一化（导出供测试与后续复用）。 */
    normalize: normalize,
    /** 订阅语言变化，供动态渲染在切换后重绘。 */
    onChange: function (fn) {
      changeListeners.push(fn);
    },
  };
})();

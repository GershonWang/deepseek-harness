/* i18n.js 的 DOM 桩测试：验证壳前端的语言解析、字典回退、DOM 回填与 Go 侧回推。
 *
 * 为什么单独一个文件而不是塞进 test-app.cjs：i18n.js 是独立的运行时模块，判据都
 * 落在它自己的门面（window.DSHI18N）上；test-app.cjs 关心的是 app.js 的界面行为，
 * 两者的桩需求不同（前者只需 documentElement 与几个带钩子的元素）。
 *
 * 桩只实现 i18n.js 真正用到的 API：document.documentElement.lang、
 * document.querySelectorAll，以及元素上的 textContent / getAttribute /
 * setAttribute。字典按真实方式注册——直接写 window.DSH_LOCALES，与 locales/*.js
 * 的加载结果同一条路径，因此这里测到的回退链就是线上跑的那条。
 *
 * 运行：node --test frontend/test-i18n.cjs（工作目录 apps/desktop-launcher）
 */

"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const ZH_CODE = fs.readFileSync(path.join(__dirname, "locales", "zh.js"), "utf8");
const EN_CODE = fs.readFileSync(path.join(__dirname, "locales", "en.js"), "utf8");
const I18N_CODE = fs.readFileSync(path.join(__dirname, "i18n.js"), "utf8");

/* 最小元素桩：i18n.js 只按属性选择器取元素，再读写文本或同名属性。 */
class StubElement {
  constructor(attrs = {}) {
    this.attrs = { ...attrs };
    this.textContent = "";
  }
  getAttribute(name) {
    return name in this.attrs ? this.attrs[name] : null;
  }
  setAttribute(name, value) {
    this.attrs[name] = value;
  }
}

/* 最小文档桩：i18n.js 用 documentElement.lang 表达文档语言，用 querySelectorAll
 * 按 data-i18n* 钩子取元素。选择器只支持 `[attr]` 这一种形态——多支持一种就多
 * 一份与真实 DOM 不一致的风险，而 i18n.js 只会用这一种。 */
function makeDocument(elements = []) {
  return {
    documentElement: { lang: "" },
    querySelectorAll(selector) {
      const attr = selector.slice(1, -1);
      return elements.filter((el) => attr in el.attrs);
    },
  };
}

/* 在独立 vm 上下文加载三份真实脚本，返回句柄。 */
function loadI18n({ navigator = {}, go, elements = [], dictionaries } = {}) {
  const document = makeDocument(elements);
  const localeCalls = [];
  const consoleErrors = [];
  const window = { addEventListener() {} };
  if (go !== false) {
    window.go = {
      app: {
        App: {
          SetLocale(id) {
            localeCalls.push(id);
            return Promise.resolve();
          },
        },
      },
    };
  }
  const sandbox = {
    // 只桩 error：订阅者抛错时 i18n.js 会记录一行，测试断言它被记录而不是让
    // 噪声混进用例输出。
    console: { error: (...args) => consoleErrors.push(args) },
    document,
    navigator,
    window,
  };
  vm.createContext(sandbox);
  vm.runInContext(`${ZH_CODE}\n${EN_CODE}\n${I18N_CODE}`, sandbox, { filename: "i18n.js" });

  // 字典按真实路径注册：locales/*.js 就是这么写 window.DSH_LOCALES 的。
  if (dictionaries) {
    for (const [id, dict] of Object.entries(dictionaries)) {
      window.DSH_LOCALES[id] = { ...(window.DSH_LOCALES[id] || {}), ...dict };
    }
  }

  return { i18n: window.DSHI18N, document, localeCalls, consoleErrors, window, elements };
}

test("无语言信息时回退 en 并同步文档语言", () => {
  const h = loadI18n();
  h.i18n.init();
  assert.equal(h.i18n.current(), "en");
  assert.equal(h.document.documentElement.lang, "en");
});

test("按 navigator.languages 顺序取第一个内置语言", () => {
  const h = loadI18n({ navigator: { languages: ["fr-FR", "zh-CN", "en"] } });
  h.i18n.init();
  assert.equal(h.i18n.current(), "zh");
  // 文档语言用 zh-CN，与 harness 客户端 syncDocumentLanguage 的映射一致。
  assert.equal(h.document.documentElement.lang, "zh-CN");
});

test("languages 为空时看 navigator.language", () => {
  const h = loadI18n({ navigator: { languages: [], language: "zh_CN.UTF-8" } });
  h.i18n.init();
  assert.equal(h.i18n.current(), "zh");
});

test("GUI 上报覆盖初始兜底值", () => {
  const h = loadI18n({ navigator: { languages: ["en-US"] } });
  h.i18n.init();
  assert.equal(h.i18n.current(), "en");
  h.i18n.applyFromGui("zh-CN");
  assert.equal(h.i18n.current(), "zh");
  assert.equal(h.document.documentElement.lang, "zh-CN");
});

test("未注册语言落到 en 而不是保持上一个语言", () => {
  const h = loadI18n({ navigator: { languages: ["zh-CN"] } });
  h.i18n.init();
  assert.equal(h.i18n.current(), "zh");
  // 三方语言包（如 ja）没有字典：落到 en，界面才会与"不支持"这件事一致。
  h.i18n.applyFromGui("ja-JP");
  assert.equal(h.i18n.current(), "en");
});

test("normalize 丢弃地区、编码与修饰后缀", () => {
  const h = loadI18n();
  const cases = [
    ["zh", "zh"],
    ["zh-CN", "zh"],
    ["zh_CN.UTF-8", "zh"],
    ["zh-Hans-CN", "zh"],
    ["  ZH  ", "zh"],
    ["en_US", "en"],
    ["ja", ""],
    ["C", ""],
    ["", ""],
    [undefined, ""],
    [null, ""],
  ];
  for (const [raw, want] of cases) {
    assert.equal(h.i18n.normalize(raw), want, `normalize(${JSON.stringify(raw)})`);
  }
});

test("t 取当前语言、缺键回退 zh、再缺返回键名", () => {
  const h = loadI18n({
    navigator: { languages: ["en-US"] },
    dictionaries: {
      zh: { "demo.greet": "你好 {name}", "demo.onlyzh": "仅中文" },
      en: { "demo.greet": "hello {name}" },
    },
  });
  h.i18n.init();
  assert.equal(h.i18n.t("demo.greet", { name: "world" }), "hello world");
  // en 缺键回退 zh：P1 迁移期英文未到位时界面不会变成空白。
  assert.equal(h.i18n.t("demo.onlyzh"), "仅中文");
  // 都缺则显示键名，让漏配在界面上直接可见。
  assert.equal(h.i18n.t("demo.missing"), "demo.missing");

  h.i18n.applyFromGui("zh");
  assert.equal(h.i18n.t("demo.greet", { name: "世界" }), "你好 世界");
});

test("占位符未提供时原样保留", () => {
  const h = loadI18n({ dictionaries: { zh: { "demo.pair": "{a} 与 {b}" }, en: {} } });
  h.i18n.init();
  assert.equal(h.i18n.t("demo.pair", { a: "甲" }), "甲 与 {b}");
  assert.equal(h.i18n.t("demo.pair"), "{a} 与 {b}");
});

test("回填 data-i18n 与 data-i18n-<属性> 钩子", () => {
  const text = new StubElement({ "data-i18n": "demo.title" });
  const titled = new StubElement({ "data-i18n-title": "demo.tip" });
  const placeholder = new StubElement({ "data-i18n-placeholder": "demo.ph" });
  const aria = new StubElement({ "data-i18n-aria-label": "demo.aria" });
  const alt = new StubElement({ "data-i18n-alt": "demo.alt" });
  const h = loadI18n({
    navigator: { languages: ["zh-CN"] },
    elements: [text, titled, placeholder, aria, alt],
    dictionaries: {
      zh: {
        "demo.title": "标题",
        "demo.tip": "提示",
        "demo.ph": "占位",
        "demo.aria": "无障碍名",
        "demo.alt": "替代文本",
      },
      en: { "demo.title": "Title" },
    },
  });
  h.i18n.init();
  assert.equal(text.textContent, "标题");
  assert.equal(titled.getAttribute("title"), "提示");
  assert.equal(placeholder.getAttribute("placeholder"), "占位");
  assert.equal(aria.getAttribute("aria-label"), "无障碍名");
  assert.equal(alt.getAttribute("alt"), "替代文本");

  // 切换语言后同一批钩子必须被重新回填，否则切换只对新出现的元素生效。
  h.i18n.applyFromGui("en");
  assert.equal(text.textContent, "Title");
  // en 仍缺的键继续回退 zh，而不是被清空。
  assert.equal(titled.getAttribute("title"), "提示");
});

test("onChange 只在语言真正变化时触发", () => {
  const seen = [];
  const h = loadI18n({ navigator: { languages: ["zh-CN"] } });
  h.i18n.onChange((id) => seen.push(id));
  h.i18n.init(); // zh：与初始值 en 不同 → 触发
  h.i18n.applyFromGui("zh-CN"); // 归一化后仍是 zh → 不触发
  h.i18n.applyFromGui("en"); // 变化 → 触发
  assert.deepEqual(seen, ["zh", "en"]);
});

test("订阅者抛错不影响其余订阅者", () => {
  const seen = [];
  const h = loadI18n({ navigator: { languages: ["zh-CN"] } });
  h.i18n.onChange(() => {
    throw new Error("boom");
  });
  h.i18n.onChange((id) => seen.push(id));
  h.i18n.init();
  assert.deepEqual(seen, ["zh"]);
  assert.equal(h.consoleErrors.length, 1);
});

test("把生效语言回推给 Go，参数为归一化后的 id", () => {
  const h = loadI18n({ navigator: { languages: ["zh-CN"] } });
  h.i18n.init();
  h.i18n.applyFromGui("zh_CN.UTF-8");
  h.i18n.applyFromGui("en-US");
  assert.deepEqual(h.localeCalls, ["zh", "zh", "en"]);
});

test("没有 Wails 运行时时（浏览器预览）不抛错", () => {
  const h = loadI18n({ go: false, navigator: { languages: ["zh-CN"] } });
  h.i18n.init();
  assert.equal(h.i18n.current(), "zh");
});

/* 字典完整性单独一例、不依赖桩：它比对的是两份字典文件本身，因此新增键却忘了
 * 写英文时报错点直接落在键名上，而不是等到界面上看到中文才发现。 */
test("字典完整性：en 与 zh 同键集、同占位符，且没有漏翻的值", () => {
  const box = { window: {} };
  vm.createContext(box);
  vm.runInContext(`${ZH_CODE}\n${EN_CODE}`, box, { filename: "locales" });
  const zh = box.window.DSH_LOCALES.zh;
  const en = box.window.DSH_LOCALES.en;

  assert.deepEqual(Object.keys(en).sort(), Object.keys(zh).sort(), "en 与 zh 必须同键集");

  const placeholders = (text) => (text.match(/\{[a-zA-Z][a-zA-Z0-9]*\}/gu) || []).sort();
  // 唯一允许两种语言同形的条目：它只包一个纯数字占位符，没有可翻译的词。
  const IDENTICAL_ALLOWED = new Set(["status.exitCode"]);
  for (const key of Object.keys(zh)) {
    assert.ok(en[key] && en[key].length > 0, `${key} 缺英文值`);
    assert.deepEqual(placeholders(en[key]), placeholders(zh[key]), `${key} 的占位符与中文不一致`);
    if (!IDENTICAL_ALLOWED.has(key)) {
      assert.notEqual(en[key], zh[key], `${key} 的英文值与中文逐字相同，等于漏翻`);
    }
  }
});

/* 字典值会经 tr() 直接插进若干处 innerHTML（检查项徽标、修复方案卡片、宿主导入行），
 * 值里出现标记就等于把字典当模板使：这里把"值不含 HTML 标记"钉成不变量，新增文案时
 * 一旦顺手写下 <b> 之类的强调标记，报错点落在键名上。 */
test("字典值不含 HTML 标记：多处 tr() 结果直接进 innerHTML", () => {
  const box = { window: {} };
  vm.createContext(box);
  vm.runInContext(`${ZH_CODE}\n${EN_CODE}`, box, { filename: "locales" });
  const values = { ...box.window.DSH_LOCALES.zh, ...box.window.DSH_LOCALES.en };
  for (const [key, value] of Object.entries(values)) {
    assert.equal(/<[A-Za-z/!]/u.test(value), false, `${key} 的值含 HTML 标记：${value}`);
  }
});

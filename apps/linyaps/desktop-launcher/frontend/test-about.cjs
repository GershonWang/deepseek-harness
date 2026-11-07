/* 独立「关于」窗口的静态一致性测试：HTML 的钩子、脚本引用的 id 与字典键必须对得上。
 *
 * 为什么需要它：about.html 与 about.js 都不是编译期检查的对象——HTML 少一个 id、
 * JS 多拼一个 id、字典漏一条键，运行期只会表现为空白行或静默失效；而独立窗口
 * 关掉主界面也能打开，这类退化很难在真机上被及时发现。桩测试覆盖主壳（test-app.cjs），
 * 这里覆盖独立页面这一侧。
 *
 * 运行：node --test frontend/test-about.cjs（工作目录 apps/linyaps/desktop-launcher）
 */
"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const HERE = __dirname;
const HTML = fs.readFileSync(path.join(HERE, "about.html"), "utf8");
const ABOUT_JS = fs.readFileSync(path.join(HERE, "about.js"), "utf8");
const V2SHIM_JS = fs.readFileSync(path.join(HERE, "v2shim.js"), "utf8");

/**
 * 在隔离上下文加载字典脚本，返回它注册的字典。
 * @param {string} name - 语言 id（zh/en），对应 locales/<name>.js。
 * @returns {Object<string, string>} 键值字典。
 */
function loadLocale(name) {
  const code = fs.readFileSync(path.join(HERE, "locales", name + ".js"), "utf8");
  const window = {};
  vm.runInNewContext(code, { window });
  return window.DSH_LOCALES[name];
}

/** @returns {Set<string>} about.html 里声明的全部元素 id。 */
function htmlIds() {
  return new Set([...HTML.matchAll(/\bid="([^"]+)"/g)].map((m) => m[1]));
}

/** @returns {string[]} about.js 通过 byId 引用的元素 id。 */
function jsIds() {
  return [...ABOUT_JS.matchAll(/byId\("([^"]+)"\)/g)].map((m) => m[1]);
}

/** @returns {string[]} about.html 里声明的全部 i18n 键（含属性钩子）。 */
function i18nKeys() {
  return [...HTML.matchAll(/data-i18n(?:-[a-z-]+)?="([^"]+)"/g)].map((m) => m[1]);
}

/**
 * 抽出脚本里的服务全限定名。
 * @param {string} code - 脚本源码。
 * @returns {string} SERVICE 常量的值。
 */
function serviceName(code) {
  const match = code.match(/const SERVICE = "([^"]+)"/);
  assert.ok(match, "脚本必须声明 SERVICE 常量");
  return match[1];
}

test("about.js 引用的 id 都存在于 about.html", () => {
  const ids = htmlIds();
  const missing = jsIds().filter((id) => !ids.has(id));
  assert.deepEqual(missing, [], "about.js 引用了 about.html 中不存在的 id：页面会静默空白");
});

test("about.html 的 i18n 键在两种字典里都存在", () => {
  const zh = loadLocale("zh");
  const en = loadLocale("en");
  const missingZh = i18nKeys().filter((key) => !(key in zh));
  const missingEn = i18nKeys().filter((key) => !(key in en));
  assert.deepEqual(missingZh, [], "about.html 引用了 zh 字典里不存在的键");
  assert.deepEqual(missingEn, [], "about.html 引用了 en 字典里不存在的键");
});

test("about.js 与 v2shim.js 使用同一个 Go 服务名", () => {
  assert.equal(serviceName(ABOUT_JS), serviceName(V2SHIM_JS),
    "两处服务名必须一致：绑定的 Call.ByName 前缀不同会直接调用失败");
});

test("about.html 按 i18n 运行时顺序加载脚本", () => {
  const order = ["locales/zh.js", "locales/en.js", "i18n.js", "about.js"];
  // 按 <script src> 定位，而不是裸文件名：文件注释里也会提到 i18n.js，裸查会命中注释。
  const positions = order.map((name) => HTML.indexOf(`src="${name}"`));
  for (let i = 0; i < positions.length; i++) {
    assert.ok(positions[i] >= 0, "about.html 缺少脚本 " + order[i]);
    if (i > 0) {
      assert.ok(positions[i] > positions[i - 1],
        "脚本顺序错误：" + order[i] + " 必须在 " + order[i - 1] + " 之后");
    }
  }
});

test("about.js 不再依赖壳内 about:open 事件通道", () => {
  assert.equal(ABOUT_JS.includes("about:open"), false,
    "独立窗口应经 AboutData 拉取数据，而不是回到旧的 about:open 事件");
});

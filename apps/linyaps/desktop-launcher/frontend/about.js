/**
 * 文件职责：独立「关于」窗口的渲染与交互。
 *
 * 为什么单独一个页面：关于必须能在主窗口关窗隐藏或最小化时独立显示，壳内模态框
 * 做不到（它是主窗口 DOM 的一部分）。数据由 Go 侧 AboutData 提供（版本号只有 Go
 * 侧知道），文案走 locales 字典，链接与复制走与主壳相同的 Wails 运行时通道。
 */
import { Browser, Call, Clipboard, Window } from "/wails/runtime.js";

/** Go 侧服务全限定名，与 frontend/v2shim.js 的 SERVICE 保持一致。 */
const SERVICE = "github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/app.App";

/** 复制反馈的保持时长，与壳内 flashCopyButton 的节奏一致。 */
const COPY_FEEDBACK_MS = 1200;

/**
 * 按 id 取元素；页面结构固定，取不到说明 HTML 与脚本漂移，直接报错比静默跳过好。
 * @param {string} id - 元素 id。
 * @returns {HTMLElement} 对应元素。
 */
function byId(id) {
  return document.getElementById(id);
}

/**
 * 写入文本；空值统一显示为空串，避免出现 "undefined"。
 * @param {string} id - 元素 id。
 * @param {?string} value - 文本内容。
 */
function setText(id, value) {
  byId(id).textContent = value == null ? "" : String(value);
}

/**
 * 填充仓库链接的展示文本与跳转地址。
 * @param {string} id - 链接元素 id。
 * @param {?string} url - 仓库地址。
 */
function setLink(id, url) {
  const link = byId(id);
  link.textContent = url == null ? "" : String(url);
  link.dataset.url = url == null ? "" : String(url);
}

/**
 * 用 Go 侧载荷填充页面。
 * @param {?object} info - AboutPayload；字段缺失时按空串展示，不抛错。
 */
function fill(info) {
  const data = info || {};
  setText("about-name", data.product);
  setText("about-version", data.version);
  setText("about-package-id", data.packageId);
  setText("about-harness-version", data.harnessVersion);
  setText("about-packager", data.packager);
  setLink("about-upstream", data.upstreamRepo);
  setLink("about-fork", data.forkRepo);
  byId("about-form").textContent = window.DSHI18N.t("about.form", {
    form: window.DSHI18N.t("about.form.thin"),
  });
}

/** 复制包 ID：与主壳的复制按钮同一通道（GTK 实现的 ClipboardSetText）。 */
function bindCopy() {
  const button = byId("about-copy");
  button.addEventListener("click", async () => {
    const id = byId("about-package-id").textContent;
    if (!id) return;
    try {
      await Clipboard.SetText(id);
    } catch (error) {
      console.warn("copy package id failed:", error && error.message);
      return;
    }
    const original = window.DSHI18N.t("about.copyPackageId");
    button.title = window.DSHI18N.t("common.copied");
    setTimeout(() => {
      button.title = original;
    }, COPY_FEEDBACK_MS);
  });
}

/**
 * 仓库链接交给系统浏览器：WebKit 容器里 target=_blank 不生效，统一走 BrowserOpenURL。
 * 不关闭窗口——独立窗口留着，用户看完可以自己关。
 */
function bindLinks() {
  document.querySelectorAll(".about-link").forEach((link) => {
    link.addEventListener("click", (event) => {
      event.preventDefault();
      const url = link.dataset.url;
      if (!url) return;
      Browser.OpenURL(url).catch((error) => {
        console.warn("open repository failed:", error && error.message);
      });
    });
  });
}

/** Esc 关闭本窗口：与系统标题栏的关闭按钮同一条路径。 */
function bindEscape() {
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") Window.Close();
  });
}

/**
 * 隐藏 WebKit 内建的右键菜单：与壳页面（app.js）和 GUI 注入桥（dsh-link-bridge.js）
 * 同一处理，避免关于窗口里出现 Back/Forward/Reload 这类浏览器导航菜单。
 */
function bindContextMenu() {
  document.addEventListener("contextmenu", (event) => event.preventDefault(), true);
}

/** 拉取数据并渲染；Go 侧方法不可用时显示错误行，而不是留一屏空白。 */
async function load() {
  window.DSHI18N.init();
  bindCopy();
  bindLinks();
  bindEscape();
  bindContextMenu();
  try {
    const [info, locale] = await Promise.all([
      Call.ByName(`${SERVICE}.AboutData`),
      Call.ByName(`${SERVICE}.GetLocale`),
    ]);
    window.DSHI18N.applyFromGui(locale);
    fill(info);
  } catch (error) {
    // 控制台日志与壳内其余诊断一样用英文：它不是界面文案，但前端 i18n 门禁对
    // 字符串字面量里的中文零容忍（产品文案只能出现在 locales/ 下）。
    console.error("about: failed to load data:", error);
    byId("about-error").classList.remove("hidden");
  }
}

load();

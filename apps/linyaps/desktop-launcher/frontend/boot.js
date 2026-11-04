/**
 * 文件职责：壳脚本的引导入口——先装 v2 全局适配层与壳设置桥接，再以普通脚本方式加载
 * 壳脚本。
 *
 * 为什么用动态插入普通脚本，而不是把 app.js 改成 ES module：壳是普通脚本，依赖非严格
 * 模式语义，并与前面的 vendor/i18n 普通脚本共享全局；改成 module 会改变执行时机与严格
 * 模式行为。动态插入保持原语义，同时保证「适配层先于壳」这个顺序。
 */
import "./v2shim.js";

/** 壳自己管理的 localStorage 键前缀。 */
const SHELL_KEY_PREFIX = "dsh-desktop.";

/**
 * 把壳设置的读写接到 Go 侧配置。
 *
 * 为什么需要：v3 的壳 origin 从 `wails://wails` 变成 `wails://localhost`，而
 * localStorage 按 origin 隔离——旧 origin 里的语言、终端字号在新版本里读不到，
 * 表现为用户设置被静默重置。迁移动作是把这些键存到 Go 侧配置（AppConfig），
 * 壳里既有的 localStorage 调用一行都不用改。
 *
 * 只接管 `dsh-desktop.` 前缀：xterm 等第三方库可能也用 localStorage，越权接管会改变
 * 它们的行为。
 */
async function installShellSettings() {
  const app = globalThis.go && globalThis.go.app && globalThis.go.app.App;
  if (!app || typeof app.ShellSettingsJSON !== "function") {
    return; // 浏览器预览：保持原生 localStorage
  }

  let settings = {};
  try {
    const raw = await app.ShellSettingsJSON();
    settings = raw ? JSON.parse(raw) : {};
  } catch {
    settings = {};
  }

  const managed = (key) => typeof key === "string" && key.startsWith(SHELL_KEY_PREFIX);
  const nativeGet = Storage.prototype.getItem;
  const nativeSet = Storage.prototype.setItem;
  const nativeRemove = Storage.prototype.removeItem;
  const isLocal = function (storage) {
    return storage === window.localStorage;
  };

  Storage.prototype.getItem = function (key) {
    if (isLocal(this) && managed(key) && Object.prototype.hasOwnProperty.call(settings, key)) {
      return settings[key];
    }
    return nativeGet.call(this, key);
  };
  Storage.prototype.setItem = function (key, value) {
    if (isLocal(this) && managed(key)) {
      settings[key] = String(value);
      Promise.resolve(app.SaveShellSetting(String(key), String(value))).catch(() => {});
      return;
    }
    return nativeSet.call(this, key, value);
  };
  Storage.prototype.removeItem = function (key) {
    if (isLocal(this) && managed(key)) {
      delete settings[key];
      Promise.resolve(app.SaveShellSetting(String(key), "")).catch(() => {});
      return;
    }
    return nativeRemove.call(this, key);
  };
}

// 必须先完成设置注入再加载壳：i18n.js 的继承者在首次渲染前就要读到语言。
await installShellSettings();

const shell = document.createElement("script");
shell.src = "./app.js";
shell.onload = () => {
  // 壳的监听器此刻已注册完毕。v3 会丢弃页面就绪前发出的事件（spike 已实测），
  // 因此在这里补拉一次当前启动进度，让加载页拿到真实阶段而不是停在初始态。
  const app = globalThis.go && globalThis.go.app && globalThis.go.app.App;
  if (app && typeof app.ReplayStartupProgress === "function") {
    Promise.resolve(app.ReplayStartupProgress()).catch(() => {});
  }
};
document.body.appendChild(shell);

/**
 * 文件职责：把 Wails v3 的 ES module 运行时适配回 v2 注入的全局
 * （`window.go` / `window.runtime`），让既有的静态壳脚本原样运行。
 *
 * 为什么需要它：壳是 2894 行普通脚本，全部调用走 `window.go.app.App.*` 与
 * `window.runtime.*`；v3 不再注入这两个全局。若逐个改调用点，迁移就变成重写。
 *
 * 方法清单是对壳脚本的静态清点结果（`api().*` 与 `App.*` 两种调用形态），
 * 不是把绑定全量搬一遍：多列一个不存在的方法会让壳里的 `typeof app.X === "function"`
 * 守卫失效，少列一个则调用在运行期才失败。
 */
import { Browser, Call, Clipboard, Events, Window } from "/wails/runtime.js";

/** 与 Go 侧 app.App 的全限定名一致（包路径 + 类型名 + 方法名）。 */
const SERVICE = "github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/app.App";

/** 壳实际调用的 Go 方法。 */
const APP_METHODS = [
  "AckAutoDisabled",
  "AddHostTool",
  "ConfirmDeepRepair",
  "ConnectExternal",
  "DisconnectExternal",
  "ExitFreshHome",
  "ExitSafeMode",
  "InstallToolVersion",
  "PendingAutoDisabled",
  "ReadClipboardImage",
  "RefreshToolIndex",
  "RefreshTools",
  "RememberSession",
  "RemoveHostTool",
  "ReportClientBootFailure",
  "RestartServer",
  "RunDoctor",
  "RunDoctorRepair",
  "ScanHostTools",
  "SetActiveToolVersion",
  "SkipPreflight",
  "StartFreshHome",
  "StartSafeMode",
  "StartSafeModeLevel",
  "StartServer",
  "Status",
  "StopServer",
  "TerminalClose",
  "TerminalResize",
  "TerminalStart",
  "TerminalWrite",
  "UninstallTool",
];

/**
 * 生成一个经 Call.ByName 调用的 Go 方法包装。
 *
 * 用 ByName 而不是生成 bindings：静态壳没有打包器，ByName 让适配层保持零构建步骤。
 * @param {string} name - Go 方法名。
 * @returns {(...args: any[]) => Promise<any>} 代理方法。
 */
function goMethod(name) {
  return (...args) => Call.ByName(`${SERVICE}.${name}`, ...args);
}

const appBridge = {};
for (const name of APP_METHODS) {
  appBridge[name] = goMethod(name);
}

// 补发启动进度：壳加载完成后由 boot.js 调用（v3 丢弃就绪前事件）。
appBridge.ReplayStartupProgress = goMethod("ReplayStartupProgress");
// 壳设置的读写入口：壳自身不直接调用，由 boot.js 在加载时接管 localStorage。
appBridge.ShellSettingsJSON = goMethod("ShellSettingsJSON");
appBridge.SaveShellSetting = goMethod("SaveShellSetting");

globalThis.go = { app: { App: appBridge } };

/**
 * v2 的 window.runtime 等价物。
 *
 * EventsOn 的回调形态不同：v2 是 ...data 变参，v3 是单个事件对象；壳的监听器都只接收
 * 一个载荷，因此统一成 cb(data)。
 */
globalThis.runtime = {
  EventsOn: (name, callback) => Events.On(name, (event) => callback(event && event.data)),
  EventsOff: (...names) => Events.Off(...names),
  EventsEmit: (name, data) => Events.Emit(name, data),
  BrowserOpenURL: (url) => Browser.OpenURL(url),
  ClipboardSetText: (text) => Clipboard.SetText(text),
  ClipboardGetText: () => Clipboard.Text(),
  WindowMinimise: () => Window.Minimise(),
  WindowToggleMaximise: () => Window.ToggleMaximise(),
  Quit: () => Window.Close(),
};

/** 供壳与 boot 自检：适配层已安装完毕。 */
globalThis.__dshShimInstalled = true;

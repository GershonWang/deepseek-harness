/**
 * 文件职责：把 Wails v3 的 ES module 运行时适配成 v2 注入的全局对象
 * （`window.go` / `window.runtime`）。
 *
 * 为什么需要它：客户端壳是 2894 行静态脚本，全部调用都走 `window.go.app.App.*`
 * 与 `window.runtime.*`；v3 不再注入这两个全局。若每个调用点都要改，迁移就变成
 * 重写；有了这一层，壳源码可以不动。
 *
 * 为什么用动态 import 而不是普通 <script>：module 脚本是异步执行的，直接把壳脚本
 * 放在后面仍可能先于适配层运行；壳里又有「没有 window.go 就当浏览器预览」的分支，
 * 顺序错了会被误判成非客户端环境。因此由 boot.js 先 import 本文件、再 import 壳脚本。
 *
 * 方法清单来自对客户端 app.js 的实际清点，不是「把 v2 API 全量搬一遍」：
 *   window.go.app.App: ReadClipboardImage / RememberSession / ReportClientBootFailure
 *   window.runtime:    BrowserOpenURL / ClipboardGetText / ClipboardSetText /
 *                      EventsOn / Quit / WindowMinimise / WindowToggleMaximise
 */
import { Browser, Call, Clipboard, Events, Window } from "/wails/runtime.js";

/** 与 Go 侧 probe.Service 的全限定名一致（包路径 + 类型名 + 方法名）。 */
const SERVICE = "github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-v3-spike/internal/probe.Service";

/**
 * 生成一个经 Call.ByName 调用的 Go 方法包装。
 *
 * 用 ByName 而不是生成 bindings：静态壳没有打包器，ByName 让适配层保持零构建步骤，
 * 也让「Go 方法改名」在调用时立刻暴露成明确错误，而不是运行时静默失败。
 */
function goMethod(name) {
  return (...args) => Call.ByName(`${SERVICE}.${name}`, ...args);
}

/** v2 的 window.go.app.App 等价物。 */
globalThis.go = {
  app: {
    App: {
      Ping: goMethod("Ping"),
      StartupState: goMethod("StartupState"),
      ReadClipboardImage: goMethod("ReadClipboardImage"),
      RememberSession: goMethod("RememberSession"),
      ReportClientBootFailure: goMethod("ReportClientBootFailure"),
      LoadShellSettings: goMethod("LoadShellSettings"),
      SaveShellSettings: goMethod("SaveShellSettings"),
    },
  },
};

/**
 * v2 的 window.runtime 等价物。
 *
 * EventsOn 的回调形态不同：v2 是 ...data 变参，v3 是单个事件对象。这里统一成
 * `cb(data)`——客户端壳的监听器都只接收一个载荷，按这个形态适配改动面最小。
 */
globalThis.runtime = {
  EventsOn: (name, callback) => Events.On(name, (event) => callback(event?.data)),
  EventsOff: (...names) => Events.Off(...names),
  EventsEmit: (name, data) => Events.Emit(name, data),
  BrowserOpenURL: (url) => Browser.OpenURL(url),
  ClipboardSetText: (text) => Clipboard.SetText(text),
  ClipboardGetText: () => Clipboard.Text(),
  WindowMinimise: () => Window.Minimise(),
  WindowToggleMaximise: () => Window.ToggleMaximise(),
  Quit: () => Window.Close(),
};

/** 供壳脚本自检：适配层已先于它安装完毕。 */
globalThis.__dshShimInstalled = true;

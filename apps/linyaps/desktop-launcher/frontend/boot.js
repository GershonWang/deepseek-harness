/**
 * 文件职责：壳脚本的引导入口——先装 v2 全局适配层，再以普通脚本方式加载壳脚本。
 *
 * 为什么用动态插入普通脚本，而不是把 app.js 改成 ES module：壳是普通脚本，依赖
 * 非严格模式语义，并与前面的 vendor/i18n 普通脚本共享全局；改成 module 会改变执行
 * 时机与严格模式行为。动态插入保持原语义，同时保证「适配层先于壳」这个顺序。
 */
import "./v2shim.js";

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

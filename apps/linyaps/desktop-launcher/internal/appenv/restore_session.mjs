/**
 * 会话恢复插件：由 dsh-desktop-launcher 在每次 spawn harness 时经 overlay 注入，
 * 把壳记住的「上次打开的会话」作为全局量送进 Web GUI 的首屏 HTML。
 *
 * 为什么需要它：GUI 把「上次打开的会话」写在 localStorage 里，而这份存储在内嵌
 * iframe 中按第三方存储对待，退出后不落盘（见 apps/linyaps/desktop-launcher/
 * README.zh.md 的渲染层一节）。客户端于是每次启动都读不到上次的选择。壳把会话 id
 * 记在自己的 config.json 里，这里把它送进页面，由同源的链接桥
 * （linglong/dsh-link-bridge.js）在客户端启动前写回存储键，客户端便照常恢复。
 *
 * 改动前必须遵守的约束：
 * - 不 import 任何模块：本文件由壳写入 ~/.cache/dsh-desktop/，从那里解析不到安装
 *   目录或仓库里的裸包名；一旦 import，这条 entry 会加载失败并中止启动。
 * - 绝不向调用方抛异常：缺少注入只是让客户端退回它自己的回退策略（打开该工作区
 *   最近的非空会话），而 entry 加载失败是 fail-loud 的启动失败。
 * - 全局量名是与链接桥的契约，改名必须同步改桥；桥读不到就静默不恢复。
 */

/** 插件名：出现在 harness 的插件树与诊断输出里，需能一眼看出归属。 */
export const name = 'dsh-desktop-restore-session'

/** 注入的全局量名：链接桥按此读取要恢复的会话。 */
const GLOBAL = '__DSH_DESKTOP_RESTORE_SESSION__'

/** 诊断前缀：只在自身出错时写一行，便于在 harness 日志里分辨来源。 */
const PREFIX = 'dsh-desktop: restore-session '

/**
 * 注册索引注入行：把会话 id 交给服务端渲染的首屏 HTML。
 *
 * 注入行在页面头部渲染，客户端入口是 module 脚本（延迟执行），因此桥一定先于
 * 客户端读到它。
 * @param {object} ctx - cordis 上下文（由 loader 注入）。
 * @param {object} [config] - 本 entry 的 config，形如 `{ sessionId: "session-…" }`；
 *   壳还没有记住任何会话时该行根本不会被写入 overlay（见 restore_session.go）。
 */
export function apply(ctx, config) {
  try {
    const sessionId = config !== null && typeof config === 'object' && typeof config.sessionId === 'string'
      ? config.sessionId
      : ''
    if (sessionId === '') return
    ctx.on('webserver/index-inject', (table) => {
      table.push({ kind: 'global', name: GLOBAL, value: sessionId })
    })
  } catch (error) {
    process.stderr.write(`${PREFIX}unavailable ${describe(error)}\n`)
  }
}

/**
 * 把错误值转成一行可读文本（错误可能是任意抛出值，不假设它一定是 Error）。
 * @param {unknown} error - 捕获到的抛出值。
 * @returns {string} 一行诊断文本。
 */
function describe(error) {
  if (error instanceof Error) return error.message
  return String(error)
}

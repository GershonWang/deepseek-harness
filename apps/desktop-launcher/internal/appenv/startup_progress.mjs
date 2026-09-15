/**
 * 启动进度上报插件：由 dsh-desktop-launcher 在每次 spawn harness 时经 overlay
 * 注入，把「harness 启动到第几步」这一事实变成壳可解析的 stderr 行。
 *
 * 为什么需要它：加载页在 harness 就绪前没有任何可显示的事实——壳从子进程
 * stdout/stderr 只能拿到「进程已拉起」与「就绪行」两个信号，而这段等待的绝大
 * 部分消耗在插件逐条激活上。这里按 loader 已激活的条目数上报真实计数，壳解析
 * 后渲染确定进度；没有它，加载页只能显示一个转圈，或者按时间编造阶段。
 *
 * 改动前必须遵守的约束：
 * - 不 import 任何模块：本文件由壳写入 ~/.cache/dsh-desktop/，从那里解析不到
 *   安装目录或仓库里的裸包名；一旦 import，这条 entry 会加载失败并中止启动。
 * - 绝不向调用方抛异常：entry 加载失败是 fail-loud 的启动失败，而进度上报只是
 *   体验增强；apply 整体兜底，自身出错只写一行诊断，把启动让给 harness。
 * - stderr 前缀 `dsh-desktop: startup ` 是与壳的契约，解析器在
 *   internal/supervisor/supervisor.go 的 startupProgressPattern；格式改动必须
 *   同步改壳，否则加载页静默退回粗粒度阶段。
 */

/** 插件名：出现在 harness 的插件树与诊断输出里，需能一眼看出归属。 */
export const name = 'dsh-desktop-startup-progress'

/** stderr 上报前缀：与壳侧解析器一一对应。 */
const PREFIX = 'dsh-desktop: startup '

/**
 * 激活上报器：播种已存在的条目、订阅后续条目，每次激活完成上报一次计数。
 * @param {object} ctx - cordis 上下文（由 loader 注入）。
 */
export function apply(ctx) {
  try {
    start(ctx)
  } catch (error) {
    // 兜底说明：这里是桌面壳的体验增强，任何自身错误都不允许升级成 harness
    // 启动失败；写一行带前缀的诊断，壳侧解析不到进度就退回粗粒度阶段。
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

/**
 * 统计并上报启动进度。
 *
 * 分母取 loader 当前已知且真正会挂载的条目（group 只是容器、disabled 的条目不
 * 挂载，两者都不计入），分母在启动期基本稳定；分子是已 settle 的条目数。
 * fiber.await() 在条目激活完成或激活失败时 settle——两种结果都表示这条不再阻塞
 * 启动，所以都计一次，计数因此单调不减。
 * @param {object} ctx - cordis 上下文。
 */
function start(ctx) {
  const loader = ctx.loader
  if (loader === undefined || loader === null) return

  /** 已登记的条目 id：internal/plugin 在构造与销毁两个方向都会发射，需去重。 */
  const tracked = new Set()
  let settled = 0

  /** 真正会挂载的条目（排除 group 容器与 disabled 条目）。 */
  const mountable = () =>
    [...loader.entries()].filter((entry) => !entry.options?.group && !entry.disabled)

  /** 向 stderr 写一次进度行；分母为 0 时不写，避免壳拿到无意义的 0/0。 */
  const report = () => {
    const total = mountable().length
    if (total === 0) return
    process.stderr.write(`${PREFIX}${settled}/${total}\n`)
  }

  /** 登记一个新建的 fiber，并在它 settle 时计数。 */
  const watch = (fiber) => {
    const entry = fiber?.entry
    if (entry === undefined || entry === null) return
    if (entry.options?.group || entry.disabled) return
    const id = entry.id
    if (id === undefined || tracked.has(id)) return
    tracked.add(id)
    const done = () => {
      settled += 1
      report()
    }
    Promise.resolve(fiber.await()).then(done, done)
  }

  // 播种：激活时已存在 fiber 的条目（它们可能在订阅之前就构造完了）。
  for (const entry of loader.entries()) if (entry.fiber) watch(entry.fiber)
  ctx.on('internal/plugin', watch)
  report()
}

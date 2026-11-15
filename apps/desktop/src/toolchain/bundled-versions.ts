/**
 * 探测随包工具的真实版本号。
 *
 * 随包工具的版本不写死在代码里：它由基础运行时和打包镜像决定，写死一个数字迟早
 * 与实际交付的副本不符，而市场卡片上"随包 python3 3.12.14"这类标注正需要这个
 * 真实值。因此这里按命令名跑一次 `--version` 并解析第一行。
 *
 * 探测失败不是错误：容器里某个命令缺失、或它的 `--version` 输出格式不认识，都
 * 只让清单少一个版本号，界面照常列出该工具。探测结果在进程内缓存，工具链窗口
 * 每次重绘都要组装视图，不能每次都起子进程。
 *
 * @module @deepseek-ai/dsh-desktop/toolchain/bundled-versions
 */

import { execFile } from 'node:child_process'
import { promisify } from 'node:util'

const run = promisify(execFile)

/** 单次探测的上限：一个健康命令立即返回，超时说明它卡住了，不该拖住窗口。 */
const PROBE_TIMEOUT_MS = 3000

/** 已探测过的结果，按命令名缓存；条目存在即表示探测过，`version` 为空串表示读不到。 */
const cache = new Map<string, CachedVersion>()

/**
 * 从 `--version` 输出里取出一个版本号。
 *
 * 各家的排版差别很大（`git version 2.43.0`、`jq-1.7.1`、`curl 8.5.0 (x86_64...)`），
 * 但都以版本号为主体，因此取第一个形如 `N.N` 或 `N.N.N` 的片段。取不到就返回
 * 空串，而不是把整行当成版本号贴到界面上。
 * @param output - 命令的标准输出或标准错误。
 * @returns 版本号，读不出时为空串。
 */
export function parseVersionOutput(output: string): string {
  const match = /(\d+\.\d+(?:\.\d+)?)/u.exec(output)
  return match?.[1] ?? ''
}

/**
 * 读取一个命令的版本号，结果在进程内缓存。
 *
 * 有些命令把版本写到标准错误（老版本 `java -version` 就是这样），所以失败路径也
 * 尝试解析一次；仍然读不到就记为空串，后续调用直接命中缓存，不会反复起子进程。
 * @param command - 要执行的命令名。
 * @returns 版本号，读不出时为空串。
 */
export async function bundledCommandVersion(command: string): Promise<string> {
  const cached = cache.get(command)
  if (cached !== undefined) return cached.version
  const version = await probe(command)
  cache.set(command, { version })
  return version
}

/** 缓存条目：探测过就存在，`version` 为空串表示读不到版本。 */
interface CachedVersion {
  readonly version: string
}

/**
 * 真正执行一次探测。
 * @param command - 要执行的命令名。
 * @returns 版本号，读不出时为空串。
 */
async function probe(command: string): Promise<string> {
  try {
    const { stdout } = await run(command, ['--version'], {
      timeout: PROBE_TIMEOUT_MS,
      // 输出只用来取版本号，留一点余量即可，不必把整个 help 文本读进内存。
      maxBuffer: 64 * 1024,
      windowsHide: true,
    })
    return parseVersionOutput(stdout)
  } catch (error) {
    // 非零退出或超时都可能仍带有可解析的版本行，按错误对象里的输出重试一次。
    const output = error as { readonly stdout?: string; readonly stderr?: string }
    return parseVersionOutput(`${output.stdout ?? ''}\n${output.stderr ?? ''}`)
  }
}

/** 清空缓存；仅测试使用，避免用例之间互相影响。 */
export function resetBundledVersionCache(): void {
  cache.clear()
}

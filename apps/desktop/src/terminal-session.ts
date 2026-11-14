/**
 * 容器级终端会话：一个 node-pty 伪终端与它启动的交互式 shell。
 *
 * 与右侧栏终端（`api-terminal-controller` + `subprocess`）的分工：后者属于 Session，
 * 走 Host RPC、受沙箱约束、可被 Agent 接管；这里是**容器级**的逃生入口，只回答
 * 「在这台机器的容器里跑命令」，因此不经过 harness、不随 Host 起落、也没有会话语义。
 * 代价是它不享受沙箱，与薄壳版标题栏终端一致。
 *
 * @module @deepseek-ai/dsh-desktop/terminal-session
 */

import { createRequire } from 'node:module'
import { userInfo } from 'node:os'
import type { IPty, IPtyForkOptions } from 'node-pty'

/**
 * 拉起 PTY 的函数。
 *
 * 抽成注入点而不是直接调 `node-pty`：原生模块只有在真的打开终端时才加载，单元
 * 测试因此不需要编译好的 `.node`，也能断言传给 node-pty 的每一份参数。
 */
export type DesktopTerminalSpawn = (file: string, args: string[], options: IPtyForkOptions) => IPty

/** 兜底 TERM：缺了它很多程序显示异常（less、tmux 直接拒绝启动）。 */
const FALLBACK_TERM = 'xterm-256color'

/** 兜底 shell：连账户记录都读不到时用它。 */
const FALLBACK_SHELL = '/bin/sh'

/**
 * SIGHUP 与 SIGKILL 之间的宽限期。
 *
 * 交互式 shell 按惯例忽略 SIGTERM，收到也只会耗满宽限期；SIGHUP 才是「终端关闭」
 * 的语义信号，shell 收到后会自行向作业转发。宽限期给后台作业收尾的时间。
 */
const TERMINATION_GRACE_MS = 3_000

/**
 * 按需加载 node-pty。
 *
 * `createRequire` 相对打包后的 `lib/main.js` 解析，命中的是 `app.asar/node_modules`
 * 里随包的那一份；原生 `.node` 由 electron-builder 解包到 `app.asar.unpacked`，
 * Electron 的 asar 集成会透明地落到解包副本上。
 *
 * @returns node-pty 模块。
 * @throws 原生模块缺失或与 ABI 不匹配时抛出原始错误。
 */
export function loadNodePty(): typeof import('node-pty') {
  return createRequire(import.meta.url)('node-pty') as typeof import('node-pty')
}

/**
 * 解析终端要用的 shell。
 *
 * 顺序：登录 shell 读取到的 `$SHELL` → 账户记录里的登录 shell → `/bin/sh`。不用
 * `process.env.SHELL` 作首选，因为从图形界面启动时它通常缺失，而账户记录才是
 * 用户实际配置的那个 shell。
 *
 * @param environment - 已合并登录 shell 变量的环境。
 * @param accountShell - 账户记录里的登录 shell，读不到时省略。
 * @returns 可执行的 shell 与其参数（交互式由 PTY 本身保证，不需要 `-i`）。
 */
export function resolveTerminalShell(
  environment: NodeJS.ProcessEnv,
  accountShell?: string,
): { path: string; args: string[] } {
  for (const candidate of [environment.SHELL, accountShell]) {
    if (candidate !== undefined && candidate !== '') return { path: candidate, args: [] }
  }
  return { path: FALLBACK_SHELL, args: [] }
}

/**
 * 补齐终端子进程必需的环境变量。
 *
 * 玲珑容器里的 GUI 启动不携带 `$SHELL`，`~/.bashrc` 里的 dircolors 等工具依赖它
 * 判断输出格式，缺失会打出 `dircolors: no SHELL environment variable`——bash 会从
 * 账户记录补一个同名变量但并不导出，子进程读不到。空值等价于缺失，必须先剔除，
 * 否则「补一个」的判据看不到它。
 *
 * `TERM` 反过来**总是**被覆盖成本窗口的终端类型：这个窗口的渲染端就是 xterm.js，
 * 它按 xterm-256color 的协议收发，而继承来的值描述的是「谁启动了应用」——从脚本或
 * 服务里启动时可能是 `dumb`，那样 vim、less、tmux 会直接拒绝启动。
 *
 * @param base - 继承的环境（登录 shell 读取结果）。
 * @param shell - {@link resolveTerminalShell} 选定的 shell，用作 SHELL 兜底。
 * @returns 新对象，调用方的环境不被改动。
 */
export function resolveTerminalEnvironment(base: NodeJS.ProcessEnv, shell: string): NodeJS.ProcessEnv {
  const environment: NodeJS.ProcessEnv = {}
  for (const [name, value] of Object.entries(base)) {
    if (value === undefined) continue
    if (name === 'SHELL' && value === '') continue
    if (name === 'TERM') continue
    environment[name] = value
  }
  if (environment.SHELL === undefined) environment.SHELL = shell
  environment.TERM = FALLBACK_TERM
  return environment
}

/** 创建会话所需的全部输入；每次 {@link DesktopTerminalSession.start} 只消费一次。 */
export interface DesktopTerminalSessionOptions {
  /** 拉起 PTY 的函数。 */
  readonly spawn: DesktopTerminalSpawn
  /** 选定的 shell 与参数。 */
  readonly shell: { readonly path: string; readonly args: readonly string[] }
  /** 起始工作目录。 */
  readonly cwd: string
  /** 子进程环境。 */
  readonly environment: NodeJS.ProcessEnv
  /** 初始列数。 */
  readonly cols: number
  /** 初始行数。 */
  readonly rows: number
  /** @param data - 原始输出，可能含 ANSI 转义序列。 */
  readonly onOutput: (data: string) => void
  /** @param exitCode - shell 的退出码。 */
  readonly onExit: (exitCode: number) => void
}

/**
 * 一个 PTY 会话的生命周期。
 *
 * 会话只被 {@link start} 拉起一次；重建走新的实例，这样「已退出」状态不会被
 * 半重置的旧实例掩盖。{@link dispose} 之后不再转发任何输出或退出事件。
 */
export class DesktopTerminalSession {
  private pty: IPty | undefined
  private killTimer: ReturnType<typeof setTimeout> | undefined
  private disposed = false

  /** @param options - 拉起参数与事件回调。 */
  constructor(private readonly options: DesktopTerminalSessionOptions) {}

  /**
   * 拉起 PTY。
   *
   * @throws 原生模块不可用或 fork 失败时抛出，由调用方转成失败视图。
   */
  start(): void {
    const { spawn, shell, cwd, environment, cols, rows, onOutput, onExit } = this.options
    const pty = spawn(shell.path, [...shell.args], {
      name: FALLBACK_TERM, cols, rows, cwd, env: environment,
    })
    this.pty = pty
    pty.onData((data) => { if (!this.disposed) onOutput(data) })
    pty.onExit(({ exitCode }) => {
      this.clearKillTimer()
      if (!this.disposed) onExit(exitCode)
    })
  }

  /**
   * 把输入写给 PTY。
   * @param data - 原始字节串；写入失败（进程刚好退出）时静默丢弃。
   */
  write(data: string): void {
    if (this.disposed) return
    this.pty?.write(data)
  }

  /**
   * 同步窗口行列数。
   * @param cols - 列数。@param rows - 行数。
   */
  resize(cols: number, rows: number): void {
    if (this.disposed || cols <= 0 || rows <= 0) return
    this.pty?.resize(cols, rows)
  }

  /**
   * 结束会话：先 SIGHUP，宽限期后 SIGKILL 兜底。
   *
   * 幂等：重复调用只保留第一次安排的终止流程。这里不等待退出——窗口关闭与
   * 应用退出都不该被一个前台作业拖住，进程本身由宽限期后的 SIGKILL 收尾。
   */
  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    const pty = this.pty
    this.pty = undefined
    if (pty === undefined) return
    // 交互式 shell 忽略 SIGTERM；SIGHUP 是终端关闭的语义信号，且经 kill(2) 直达。
    try { pty.kill('SIGHUP') } catch (_alreadyExited) { /* 进程已退出，无需再杀 */ }
    this.killTimer = setTimeout(() => {
      this.killTimer = undefined
      try { pty.kill('SIGKILL') } catch (_alreadyExited) { /* 宽限期内自行退出 */ }
    }, TERMINATION_GRACE_MS)
    // 应用退出不该为一个终端的宽限期续命。
    this.killTimer.unref()
  }

  private clearKillTimer(): void {
    if (this.killTimer === undefined) return
    clearTimeout(this.killTimer)
    this.killTimer = undefined
  }
}

/**
 * 账户记录里的登录 shell。
 * @returns shell 路径，读不到时为 undefined。
 */
export function accountLoginShell(): string | undefined {
  try {
    // `userInfo()` reports `null` where the account record carries no shell at all, which
    // the POSIX typings do not express.
    return userInfo().shell ?? undefined
  } catch (_noAccountRecord) {
    // 容器里 uid 可能没有账户记录；兜底由调用方给。
    return undefined
  }
}

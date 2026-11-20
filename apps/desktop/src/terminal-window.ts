/**
 * 托盘「终端」窗口：一个壳拥有的容器级交互式 shell。
 *
 * 为什么是独立原生窗口而不是主窗口里的弹框：Linux 上关窗只是隐藏主窗口，挂在
 * 主窗口 DOM 上的弹框此时不可见，而托盘恰恰是窗口隐藏时唯一能点到的入口——
 * 「关于」窗口也因同一原因独立成窗。
 *
 * 为什么另起一份 PTY 而不是复用右侧栏终端：侧栏终端属于 Session，走 Host RPC，
 * Host 没起来就没有终端；而托盘的入口必须在 harness 起不来时仍能打开一个 shell，
 * 那正是用户想进去看它的时刻。
 *
 * 全部文案由这里下发的视图给出，页面不自行拼任何一句。
 *
 * @module @deepseek-ai/dsh-desktop/terminal-window
 */

import { BrowserWindow, clipboard, ipcMain, type IpcMainInvokeEvent } from 'electron'
import { accountLoginShell, DesktopTerminalSession, loadNodePty, resolveTerminalEnvironment, resolveTerminalShell,
  type DesktopTerminalSpawn } from './terminal-session.ts'
import { TERMINAL_IPC, type DesktopTerminalView } from './terminal-contract.ts'
import { hideMenuBar } from './menu-bar.ts'
import type { DesktopLocale } from './locale.ts'

/** 壳拥有的终端文档；`main.ts` 的 `dsh-app://shell` 路由负责送出它。 */
const page = 'dsh-app://shell/terminal.html'

/**
 * 初始几何。
 *
 * 终端窗口不记忆上次尺寸：它不是第二个工作区，而是一个临时逃生口。宽高按
 * 80 × 24 字符网格加内部留白定，够看常用命令又不遮住整个桌面；用户可自行缩放。
 */
export const TERMINAL_WINDOW_SIZE = { width: 900, height: 560 } as const

/** 页面在 fit 出真实行列之前用的保守初值。 */
const FALLBACK_COLS = 80
const FALLBACK_ROWS = 24

/** 一次写入的长度上限：超过即视为异常调用，而不是把内存交给页面。 */
const MAX_INPUT_BYTES = 1_000_000

/** 窗口需要的主进程输入。 */
export interface DesktopTerminalWindowOptions {
  /** 只暴露终端通道的隔离桥。 */
  readonly preload: string
  /** 语言读取器；窗口打开期间语言变化要重绘。 */
  readonly locale: () => DesktopLocale
  /** 终端子进程的起始工作目录。 */
  readonly cwd: () => string
  /** 拉起 PTY 的函数；测试注入桩，生产用 node-pty。 */
  readonly spawn?: DesktopTerminalSpawn
}

/** 拥有唯一一个终端窗口与它的 PTY 会话，并在窗口存在期间持有对应 IPC 通道。 */
export class DesktopTerminalWindow {
  private window: BrowserWindow | undefined
  private session: DesktopTerminalSession | undefined
  private phase: DesktopTerminalView['phase'] = 'running'
  private exitCode: number | undefined
  private disposed = false

  /** @param options - 隔离桥、语言、工作目录与 PTY 工厂。 */
  constructor(private readonly options: DesktopTerminalWindowOptions) {}

  /**
   * 打开终端窗口；已经打开时只聚焦并重绘。
   *
   * 托盘项会被重复点击，因此这里必须幂等，且不能叠出第二个窗口。
   */
  show(): void {
    if (this.disposed) return
    const existing = this.window
    if (existing !== undefined && !existing.isDestroyed()) {
      if (existing.isMinimized()) existing.restore()
      existing.show()
      existing.focus()
      this.publish()
      return
    }
    // 不设 parent：主窗口可能已隐藏，隐藏窗口的子窗口不可靠；提升主窗口又会把用户
    // 从手上的事情拽走。这个窗口只从托盘进来，自己就是顶层窗口。
    const window = new BrowserWindow({
      ...TERMINAL_WINDOW_SIZE,
      useContentSize: true,
      minWidth: 480,
      minHeight: 260,
      show: false,
      title: this.options.locale().messages.terminalWindow,
      // 终端始终自带深色画布（与右侧栏终端一致的约定），窗口底色对齐画布，
      // 避免首帧白闪与缩放时露出的白边。
      backgroundColor: '#1a1a1a',
      webPreferences: {
        preload: this.options.preload,
        nodeIntegration: false,
        contextIsolation: true,
        sandbox: true,
        webSecurity: true,
        devTools: true,
      },
    })
    this.window = window
    // 终端窗口没有任何原生菜单项是自己需要的；只把菜单栏设为自动隐藏会让 Alt
    // 键（以及带 Alt 的截图快捷键）把它重新拉出来，因此直接移除整条菜单栏。
    hideMenuBar(window)
    this.installHandlers(window)
    window.once('ready-to-show', () => { window.show() })
    window.on('closed', () => {
      this.endSession()
      if (this.window === window) this.window = undefined
      this.removeHandlers()
    })
    window.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))
    window.webContents.on('will-navigate', (event, url) => { if (url !== page) event.preventDefault() })
    void window.loadURL(page)
  }

  /** 跟随壳语言变化重绘已打开的窗口。 */
  relabel(): void {
    const window = this.window
    if (window === undefined || window.isDestroyed()) return
    window.setTitle(this.options.locale().messages.terminalWindow)
    this.publish()
  }

  /** 销毁窗口并结束会话；应用退出时调用，保证 shell 不残留。 */
  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    const window = this.window
    this.window = undefined
    this.removeHandlers()
    this.endSession()
    if (window !== undefined && !window.isDestroyed()) window.destroy()
  }

  /** @returns 当前视图；文字全部取自壳语言字典。 */
  private view(): DesktopTerminalView {
    const { messages, id } = this.options.locale()
    const status = this.phase === 'running' ? undefined
      : this.phase === 'exited' ? messages.terminalExited.replaceAll('{code}', String(this.exitCode ?? 0))
        : messages.terminalFailed
    return {
      locale: id,
      title: messages.terminalWindow,
      screenLabel: messages.terminalScreenLabel,
      phase: this.phase,
      menuCopy: messages.copy,
      menuPaste: messages.paste,
      ...status === undefined ? {} : { status },
    }
  }

  /** 把当前视图推给已载入的文档；首次载入自己会读一次。 */
  private publish(): void {
    const window = this.window
    if (window === undefined || window.isDestroyed() || window.webContents.isDestroyed()) return
    window.webContents.send(TERMINAL_IPC.state, this.view())
  }

  /** 结束会话并保留「已退出」这一事实，避免关窗与重建之间状态漂移。 */
  private endSession(): void {
    this.session?.dispose()
    this.session = undefined
  }

  /** 窗口存在期间注册本模块的通道。 */
  private installHandlers(window: BrowserWindow): void {
    this.removeHandlers()
    ipcMain.handle(TERMINAL_IPC.status, (event) => { this.owned(event, window); return this.view() })
    ipcMain.handle(TERMINAL_IPC.start, (event, cols: unknown, rows: unknown) => {
      this.owned(event, window)
      this.start(sessionSize(cols, FALLBACK_COLS), sessionSize(rows, FALLBACK_ROWS))
      return this.view()
    })
    ipcMain.handle(TERMINAL_IPC.write, (event, data: unknown) => {
      this.owned(event, window)
      if (typeof data !== 'string' || data.length > MAX_INPUT_BYTES) throw new Error('desktop terminal: invalid input')
      this.session?.write(data)
    })
    ipcMain.handle(TERMINAL_IPC.resize, (event, cols: unknown, rows: unknown) => {
      this.owned(event, window)
      this.session?.resize(sessionSize(cols, FALLBACK_COLS), sessionSize(rows, FALLBACK_ROWS))
    })
    // 剪贴板属于主进程：页面的自绘菜单只决定何时复制/粘贴，不直接碰系统剪贴板。
    // 这样终端窗口不必依赖 document 焦点，也不会在容器化的发行版里因权限被拒。
    ipcMain.handle(TERMINAL_IPC.clipboardRead, async (event) => {
      this.owned(event, window)
      return await clipboard.readText()
    })
    ipcMain.handle(TERMINAL_IPC.clipboardWrite, async (event, text: unknown) => {
      this.owned(event, window)
      if (typeof text !== 'string' || text.length > MAX_INPUT_BYTES) throw new Error('desktop terminal: invalid clipboard text')
      await clipboard.writeText(text)
    })
  }

  /** 撤销本模块注册的全部通道，无论窗口是否还在。 */
  private removeHandlers(): void {
    for (const channel of [TERMINAL_IPC.status, TERMINAL_IPC.start, TERMINAL_IPC.write, TERMINAL_IPC.resize,
      TERMINAL_IPC.clipboardRead, TERMINAL_IPC.clipboardWrite]) {
      ipcMain.removeHandler(channel)
    }
  }

  /**
   * 拉起会话，或复用仍在跑的那一个。
   *
   * 复用是刻意的：页面重载（Ctrl+R）与窗口重新显示都不该无声地杀掉用户正在跑的
   * 命令；只有会话自己退出、或窗口/应用关闭才结束它。复用时的行列以本次请求为准。
   *
   * @param cols - 页面实测列数。@param rows - 页面实测行数。
   */
  private start(cols: number, rows: number): void {
    const existing = this.session
    if (this.phase === 'running' && existing !== undefined) {
      existing.resize(cols, rows)
      return
    }
    this.endSession()
    this.exitCode = undefined
    this.phase = 'running'
    const session = this.createSession(cols, rows)
    try {
      session.start()
    } catch (error) {
      // 原生模块缺失或 fork 失败（例如容器里没有可用 shell）都是「这个入口暂时起
      // 不来」，不是应用该崩。原文进日志，界面只显示一句本地化的失败。
      this.phase = 'failed'
      console.error('desktop terminal: session failed to start', error)
      this.publish()
      return
    }
    this.session = session
    this.publish()
  }

  /**
   * 组装一个会话实例。
   * @param cols - 初始列数。@param rows - 初始行数。
   * @returns 尚未拉起的会话。
   */
  private createSession(cols: number, rows: number): DesktopTerminalSession {
    const shell = resolveTerminalShell(process.env, accountLoginShell())
    const spawn = this.options.spawn ?? ((file, args, options) => loadNodePty().spawn(file, args, options))
    return new DesktopTerminalSession({
      spawn,
      shell,
      cwd: this.options.cwd(),
      cols,
      rows,
      environment: resolveTerminalEnvironment(process.env, shell.path),
      onOutput: (data) => {
        const window = this.window
        if (window === undefined || window.isDestroyed() || window.webContents.isDestroyed()) return
        window.webContents.send(TERMINAL_IPC.output, data)
      },
      onExit: (exitCode) => {
        this.phase = 'exited'
        this.exitCode = exitCode
        this.publish()
      },
    })
  }

  /**
   * 拒绝任何不是本窗口文档发来的调用。
   * @param event - 待校验的 IPC 调用。
   * @param window - 拥有本模块通道的窗口。
   */
  private owned(event: IpcMainInvokeEvent, window: BrowserWindow): void {
    if (this.disposed || this.window !== window || window.isDestroyed()
      || event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame
      || event.senderFrame.url !== page) {
      throw new Error('desktop terminal: rejected unowned document')
    }
  }
}

/**
 * 把页面给的行列数收敛成正整数。
 * @param value - 页面传入的原始值。@param fallback - 非法时使用的初值。
 * @returns 1 到 1000 之间的整数。
 */
function sessionSize(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isInteger(value) && value > 0 && value <= 1000 ? value : fallback
}

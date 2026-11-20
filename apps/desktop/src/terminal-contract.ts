/**
 * 托盘「终端」窗口与它那份隔离 preload 之间的线协议。
 *
 * 本模块刻意不 import 任何东西：沙箱 preload 的 `require` 只能解析 Electron 与固定
 * 白名单里的内建模块，一旦这里引用了带 Node 依赖的模块，桥就整个加载不起来。
 * 通道名、视图字段与页面可用的操作因此集中在这里，两端各引一份而不各自命名。
 *
 * @module @deepseek-ai/dsh-desktop/terminal-contract
 */

/** 只有终端文档可用：调用方向 main 请求，事件方向 main 推送。 */
export const TERMINAL_IPC = {
  status: 'dsh-terminal:status',
  start: 'dsh-terminal:start',
  write: 'dsh-terminal:write',
  resize: 'dsh-terminal:resize',
  clipboardRead: 'dsh-terminal:clipboard-read',
  clipboardWrite: 'dsh-terminal:clipboard-write',
  state: 'dsh-terminal:state',
  output: 'dsh-terminal:output',
} as const

/** 会话在窗口里的三态：运行中、已退出、起不来。 */
export type DesktopTerminalPhase = 'running' | 'exited' | 'failed'

/**
 * 主进程下发的终端视图。
 *
 * 页面不自行拼任何一句文案：所有文字都由壳语言字典给出，因此窗口不会与托盘菜单
 * 说两种语言，也不会出现字典里没有的词。
 */
export interface DesktopTerminalView {
  /** 文档语言，页面用它设置自己的 `lang`。 */
  readonly locale: string
  /** 窗口标题。 */
  readonly title: string
  /** 终端屏幕的无障碍名称。 */
  readonly screenLabel: string
  /** 当前会话状态。 */
  readonly phase: DesktopTerminalPhase
  /** 退出或失败时显示的状态行；运行中省略。 */
  readonly status?: string
  /** 右键菜单「复制」的文案。 */
  readonly menuCopy: string
  /** 右键菜单「粘贴」的文案。 */
  readonly menuPaste: string
}

/** 终端文档可以请求的全部操作。 */
export interface DesktopTerminalApi {
  /** @returns 当前视图；主进程同时开始拉起会话或复用已有会话。 */
  status(): Promise<DesktopTerminalView>
  /**
   * 报告页面实测的行列数，首次调用同时拉起会话。
   * @param cols - 列数。@param rows - 行数。
   * @returns 拉起或复用之后的视图。
   */
  start(cols: number, rows: number): Promise<DesktopTerminalView>
  /**
   * 把按键数据原样写给 PTY。
   * @param data - xterm 编码后的输入，含控制字符与转义序列。
   */
  write(data: string): Promise<void>
  /**
   * 同步行列数（SIGWINCH），全屏程序靠它重绘。
   * @param cols - 列数。@param rows - 行数。
   */
  resize(cols: number, rows: number): Promise<void>
  /** 读取系统剪贴板文本；剪贴板里没有文本时为空串。 */
  readClipboard(): Promise<string>
  /**
   * 用文本替换系统剪贴板内容。
   * @param text - 要写入的文本。
   */
  writeClipboard(text: string): Promise<void>
  /** @param listener - 视图变化订阅。@returns 取消订阅。 */
  subscribe(listener: (view: DesktopTerminalView) => void): () => void
  /** @param listener - PTY 输出订阅。@returns 取消订阅。 */
  output(listener: (data: string) => void): () => void
}

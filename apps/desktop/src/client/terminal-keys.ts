/**
 * 托盘终端窗口的按键归属：哪些组合属于系统剪贴板，而不是 PTY。
 *
 * 为什么需要这层判断：xterm 按终端惯例把 Ctrl+V 编成控制字符 0x16（readline 的
 * quoted-insert），Ctrl+Shift+V 与 Shift+Insert 同样被编成转义序列。桌面壳里这些
 * 组合必须落到系统粘贴上，否则用户只能一个字一个字敲进终端。
 *
 * 判断为真时页面让 xterm 放行该按键（`attachCustomKeyEventHandler` 返回 false），
 * 由 Chromium 执行默认粘贴、Blink 派发 paste 事件，xterm 自己的粘贴处理再按
 * bracketed-paste 模式把文本写进 PTY。三组组合都依赖这一条链路，因此放行时不能
 * 调用 `preventDefault`。
 *
 * 除这三组之外的按键——包括 Esc 与 Ctrl+C——仍然全部属于 PTY：Ctrl+C 是 SIGINT，
 * 不是复制，所以不在这里放行。
 *
 * @module @deepseek-ai/dsh-desktop/terminal-keys
 */

/** 参与判断的最小键盘事件字段；取自 `KeyboardEvent`，便于直接单测。 */
export interface TerminalShortcutEvent {
  readonly type: string
  readonly key: string
  readonly ctrlKey: boolean
  readonly shiftKey: boolean
  readonly altKey: boolean
  readonly metaKey: boolean
}

/**
 * 该按键是否应放行给系统的默认粘贴。
 *
 * 只认 keydown：keyup 与 keypress 不需要放行，放行反而可能让 xterm 漏掉自己该处理的阶段。
 * @param event - 来自 xterm 自定义按键处理器的键盘事件。
 * @returns 是 `Ctrl+V`、`Ctrl+Shift+V`、`Shift+Insert` 之一时为 true。
 */
export function isClipboardPasteShortcut(event: TerminalShortcutEvent): boolean {
  if (event.type !== 'keydown' || event.altKey || event.metaKey) return false
  if (event.key === 'Insert') return event.shiftKey && !event.ctrlKey
  return event.ctrlKey && event.key.toLowerCase() === 'v'
}

/**
 * 终端页面里属于系统剪贴板的按键组合。
 *
 * 浏览器里 Ctrl+V、Ctrl+Shift+V、Shift+Insert 由浏览器 UI 处理，按键事件根本不会
 * 到达页面，所以这套判断在 Web 端只是空转。但 DSH 桌面壳的主窗口是 Electron：
 * 这些组合会进入渲染进程，而 xterm 会把它们编成控制字符 0x16 或转义序列发给
 * shell，于是终端里按 Ctrl+V 只会得到“没有反应”。
 *
 * 判断为真时组件让 xterm 放行该按键（`attachCustomKeyEventHandler` 返回 false），
 * 由 Chromium 执行默认粘贴、Blink 派发 paste 事件，xterm 的粘贴处理再按
 * bracketed-paste 模式把文本写进 PTY。放行时不能 `preventDefault`，否则默认粘贴
 * 也会被拦掉。
 *
 * 除这三组之外的按键仍全部属于 PTY：Ctrl+C 是 SIGINT，不是复制，因此不在这里。
 *
 * @module @deepseek-ai/dsh-client-ui-sidebar-terminal/paste-keys
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

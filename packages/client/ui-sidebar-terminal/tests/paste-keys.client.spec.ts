/** Web 终端粘贴组合的判断：哪些按键应放行给系统剪贴板，哪些仍属于 PTY。 */
import { expect, it } from 'vitest'
import { isClipboardPasteShortcut, type TerminalShortcutEvent } from '../src/client/paste-keys.ts'

/** 构造一个只改所需字段的键盘事件。 */
function key(over: Partial<TerminalShortcutEvent>): TerminalShortcutEvent {
  return {
    type: 'keydown', key: 'v', ctrlKey: false, shiftKey: false, altKey: false, metaKey: false, ...over,
  }
}

it('reserves the three paste gestures for the system clipboard', () => {
  expect(isClipboardPasteShortcut(key({ ctrlKey: true }))).toBe(true)
  expect(isClipboardPasteShortcut(key({ ctrlKey: true, shiftKey: true, key: 'V' }))).toBe(true)
  expect(isClipboardPasteShortcut(key({ shiftKey: true, key: 'Insert' }))).toBe(true)
})

it('leaves every other key to the PTY', () => {
  expect(isClipboardPasteShortcut(key({ ctrlKey: true, key: 'c' }))).toBe(false)
  expect(isClipboardPasteShortcut(key({ ctrlKey: true, shiftKey: true, key: 'C' }))).toBe(false)
  expect(isClipboardPasteShortcut(key({ ctrlKey: true, key: 'Insert' }))).toBe(false)
  expect(isClipboardPasteShortcut(key({}))).toBe(false)
  expect(isClipboardPasteShortcut(key({ ctrlKey: true, altKey: true }))).toBe(false)
  expect(isClipboardPasteShortcut(key({ ctrlKey: true, metaKey: true }))).toBe(false)
  expect(isClipboardPasteShortcut(key({ shiftKey: true, altKey: true, key: 'Insert' }))).toBe(false)
})

it('only acts on keydown so xterm keeps its other key phases', () => {
  expect(isClipboardPasteShortcut(key({ type: 'keyup', ctrlKey: true }))).toBe(false)
  expect(isClipboardPasteShortcut(key({ type: 'keypress', ctrlKey: true }))).toBe(false)
  expect(isClipboardPasteShortcut(key({ type: 'keyup', shiftKey: true, key: 'Insert' }))).toBe(false)
})

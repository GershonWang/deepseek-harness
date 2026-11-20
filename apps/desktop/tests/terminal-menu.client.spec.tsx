// @vitest-environment jsdom
/** 终端右键菜单：定位夹取、启用状态、键盘导航与关闭路径。 */
import { afterEach, expect, it, vi } from 'vitest'
import { MENU_MARGIN, TerminalContextMenu, menuPosition, type TerminalMenuAction } from '../src/client/terminal-menu.ts'

afterEach(() => { document.body.replaceChildren() })

/** 两项标准菜单：复制与带快捷键提示的粘贴；每项都能单独覆盖。 */
function actions(overrides: Partial<Record<'copy' | 'paste', Partial<TerminalMenuAction>>> = {}): {
  readonly items: TerminalMenuAction[]
  readonly run: { readonly copy: ReturnType<typeof vi.fn>; readonly paste: ReturnType<typeof vi.fn> }
} {
  const run = { copy: vi.fn(), paste: vi.fn() }
  return {
    run,
    items: [
      { id: 'copy', label: '复制', disabled: false, run: run.copy, ...overrides.copy },
      { id: 'paste', label: '粘贴', hint: 'Ctrl+V', disabled: false, run: run.paste, ...overrides.paste },
    ],
  }
}

/** 挂载菜单并返回它的行按钮，顺序与菜单项一致。 */
function rows(): HTMLButtonElement[] {
  return [...document.querySelectorAll<HTMLButtonElement>('.terminal-menu-item')]
}

it('keeps the menu inside the window and leaves a margin at the edges', () => {
  const size = { width: 140, height: 80 }
  const viewport = { width: 800, height: 600 }
  expect(menuPosition({ x: 200, y: 120 }, size, viewport)).toEqual({ left: 200, top: 120 })
  expect(menuPosition({ x: 790, y: 590 }, size, viewport)).toEqual({
    left: viewport.width - size.width - MENU_MARGIN,
    top: viewport.height - size.height - MENU_MARGIN,
  })
  // 窗口比菜单还小时贴住留白，不产生负坐标。
  expect(menuPosition({ x: 5, y: 5 }, { width: 900, height: 700 }, viewport)).toEqual({
    left: MENU_MARGIN,
    top: MENU_MARGIN,
  })
})

it('renders the shell labels, the shortcut hint and disabled rows', () => {
  const restore = vi.fn()
  const menu = new TerminalContextMenu(document.body, restore)
  menu.openAt({ x: 20, y: 20 }, actions({ copy: { disabled: true } }).items)
  expect(rows().map(row => row.textContent)).toEqual(['复制', '粘贴Ctrl+V'])
  expect(rows()[0]!.disabled).toBe(true)
  expect(rows()[1]!.disabled).toBe(false)
  // 只把焦点交给第一个可用项，禁用的复制不接收键盘。
  expect(document.activeElement).toBe(rows()[1])
})

it('runs an enabled action and returns focus to the terminal', () => {
  const restore = vi.fn()
  const menu = new TerminalContextMenu(document.body, restore)
  const { items, run } = actions()
  menu.openAt({ x: 20, y: 20 }, items)
  rows()[1]!.click()
  expect(run.paste).toHaveBeenCalledOnce()
  expect(run.copy).not.toHaveBeenCalled()
  expect(menu.isOpen).toBe(false)
  expect(restore).toHaveBeenCalledWith(true)
})

it('ignores clicks on a disabled row', () => {
  const restore = vi.fn()
  const menu = new TerminalContextMenu(document.body, restore)
  const { items, run } = actions({ copy: { disabled: true } })
  menu.openAt({ x: 20, y: 20 }, items)
  rows()[0]!.click()
  expect(run.copy).not.toHaveBeenCalled()
  expect(menu.isOpen).toBe(true)
})

it('closes on Escape and gives the focus back to the terminal', () => {
  const restore = vi.fn()
  const menu = new TerminalContextMenu(document.body, restore)
  menu.openAt({ x: 20, y: 20 }, actions().items)
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  expect(menu.isOpen).toBe(false)
  expect(restore).toHaveBeenCalledWith(true)
})

it('closes on an outside press without stealing focus from the click target', () => {
  const restore = vi.fn()
  const menu = new TerminalContextMenu(document.body, restore)
  menu.openAt({ x: 20, y: 20 }, actions().items)
  document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
  expect(menu.isOpen).toBe(false)
  expect(restore).toHaveBeenCalledWith(false)
})

it('closes when the window loses focus or resizes', () => {
  const restore = vi.fn()
  const menu = new TerminalContextMenu(document.body, restore)
  menu.openAt({ x: 20, y: 20 }, actions().items)
  window.dispatchEvent(new Event('blur'))
  expect(menu.isOpen).toBe(false)
  expect(restore).toHaveBeenCalledWith(false)

  menu.openAt({ x: 20, y: 20 }, actions().items)
  window.dispatchEvent(new Event('resize'))
  expect(menu.isOpen).toBe(false)
})

it('walks the enabled rows with the arrow keys and runs the focused one', () => {
  const restore = vi.fn()
  const menu = new TerminalContextMenu(document.body, restore)
  const { items, run } = actions()
  menu.openAt({ x: 20, y: 20 }, items)
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  expect(document.activeElement).toBe(rows()[1])
  // 向下越过末项回到第一项。
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  expect(document.activeElement).toBe(rows()[0])
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
  expect(run.copy).toHaveBeenCalledOnce()
  expect(menu.isOpen).toBe(false)
})

it('removes its element and listeners when disposed', () => {
  const restore = vi.fn()
  const menu = new TerminalContextMenu(document.body, restore)
  menu.openAt({ x: 20, y: 20 }, actions().items)
  menu.dispose()
  expect(menu.isOpen).toBe(false)
  expect(document.querySelector('.terminal-menu')).toBeNull()
  // 关闭之后再次按 Esc 不应再调用恢复焦点回调。
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  expect(restore).toHaveBeenCalledOnce()
})

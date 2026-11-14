import type { MenuItemConstructorOptions } from 'electron'
import { afterEach, expect, it, vi } from 'vitest'
import { resolveDesktopLocale } from '../src/locale.ts'
import { DesktopTray } from '../src/tray.ts'

const native = await vi.hoisted(async () => {
  const { EventEmitter } = await import('node:events')
  const trays: FakeTray[] = []
  class FakeTray extends EventEmitter {
    readonly setToolTip = vi.fn()
    readonly setContextMenu = vi.fn()
    readonly destroy = vi.fn()
    constructor(readonly image: unknown) { super(); trays.push(this) }
  }
  const menus: MenuItemConstructorOptions[][] = []
  return {
    trays, FakeTray, menus,
    createFromPath: vi.fn((path: string) => ({ path })),
    buildFromTemplate: vi.fn((template: MenuItemConstructorOptions[]) => { menus.push(template); return { template } }),
  }
})
vi.mock('electron', () => ({
  Tray: native.FakeTray,
  nativeImage: { createFromPath: native.createFromPath },
  Menu: { buildFromTemplate: native.buildFromTemplate },
}))

afterEach(() => { native.trays.length = 0; native.menus.length = 0; vi.clearAllMocks() })

function setup(locale = 'en', about?: () => void, terminal?: () => void, checkUpdates?: () => void) {
  let current = resolveDesktopLocale(locale)
  const open = vi.fn()
  const quit = vi.fn()
  const tray = new DesktopTray({ iconPath: 'C:/app/resources/tray.ico', locale: () => current, open, quit,
    ...about === undefined ? {} : { about },
    ...terminal === undefined ? {} : { terminal },
    ...checkUpdates === undefined ? {} : { checkUpdates } })
  return { tray, open, quit, native: native.trays[0]!, setLocale: (next: string) => { current = resolveDesktopLocale(next) } }
}

function labels(menu: MenuItemConstructorOptions[]): (string | undefined)[] {
  return menu.map(item => item.type === 'separator' ? 'separator' : item.label)
}

it('shows the application icon with its name as the tooltip and an Open / Quit menu', () => {
  const f = setup()
  expect(native.createFromPath).toHaveBeenCalledWith('C:/app/resources/tray.ico')
  expect(f.native.image).toEqual({ path: 'C:/app/resources/tray.ico' })
  expect(f.native.setToolTip).toHaveBeenCalledWith('DeepSeek Harness')
  expect(labels(native.menus[0]!)).toEqual(['Open DeepSeek Harness', 'separator', 'Quit DeepSeek Harness'])
  expect(f.native.setContextMenu).toHaveBeenCalledWith({ template: native.menus[0] })
})

it('opens the window on a single click and routes menu entries to the open and quit actions', () => {
  const f = setup()
  f.native.emit('click')
  expect(f.open).toHaveBeenCalledOnce()
  const menu = native.menus[0]!
  ;(menu[0] as { click: () => void }).click()
  ;(menu[2] as { click: () => void }).click()
  expect(f.open).toHaveBeenCalledTimes(2)
  expect(f.quit).toHaveBeenCalledOnce()
})

it('offers About only where the caller supplies it, between Open and Quit', () => {
  const about = vi.fn()
  const f = setup('zh', about)
  // The tray entry says only what it opens: its neighbours are one-word actions.
  expect(labels(native.menus[0]!)).toEqual(['打开 DeepSeek Harness', '关于', 'separator', '退出 DeepSeek Harness'])
  ;(native.menus[0]![1] as { click: () => void }).click()
  expect(about).toHaveBeenCalledOnce()
  expect(f.open).not.toHaveBeenCalled()
  // A platform keeping its own About entry leaves the same Open / Quit pair as before.
  setup()
  expect(labels(native.menus[1]!)).toEqual(['Open DeepSeek Harness', 'separator', 'Quit DeepSeek Harness'])
})

it('places the terminal between the toolchain market and About, where the caller supplies it', () => {
  const about = vi.fn()
  const terminal = vi.fn()
  setup('zh', about, terminal)
  // A shell window, not a command on the current workspace: it sits with the other
  // shell-owned windows instead of next to Open/Quit.
  expect(labels(native.menus[0]!)).toEqual(['打开 DeepSeek Harness', '终端', '关于', 'separator', '退出 DeepSeek Harness'])
  ;(native.menus[0]![1] as { click: () => void }).click()
  expect(terminal).toHaveBeenCalledOnce()
  expect(about).not.toHaveBeenCalled()
  // Windows keeps its shorter menu because the window is Linux-only.
  setup()
  expect(labels(native.menus[1]!)).toEqual(['Open DeepSeek Harness', 'separator', 'Quit DeepSeek Harness'])
})

it('offers Check for Updates where the caller supplies it, after the window entries', () => {
  const checkUpdates = vi.fn()
  const about = vi.fn()
  setup('zh', about, undefined, checkUpdates)
  // A command on the running application, not another shell window: it sits after the
  // window entries and before the separator that introduces Quit.
  expect(labels(native.menus[0]!)).toEqual(['打开 DeepSeek Harness', '关于', '检查更新…', 'separator', '退出 DeepSeek Harness'])
  ;(native.menus[0]![2] as { click: () => void }).click()
  expect(checkUpdates).toHaveBeenCalledOnce()
  expect(about).not.toHaveBeenCalled()
  // Windows keeps the caption Application popup, so its tray omits the command.
  setup()
  expect(labels(native.menus[1]!)).toEqual(['Open DeepSeek Harness', 'separator', 'Quit DeepSeek Harness'])
})

it('relabels the menu in the current locale and ignores relabel after disposal', () => {
  const f = setup()
  f.setLocale('zh')
  f.tray.relabel()
  expect(labels(native.menus[1]!)).toEqual(['打开 DeepSeek Harness', 'separator', '退出 DeepSeek Harness'])
  f.tray.dispose()
  f.tray.dispose()
  expect(f.native.destroy).toHaveBeenCalledOnce()
  f.tray.relabel()
  expect(native.menus).toHaveLength(2)
})

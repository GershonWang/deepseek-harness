import { afterEach, expect, it, vi } from 'vitest'
import { ABOUT_IPC } from '../src/about-contract.ts'
import { ABOUT_IDENTITY, ABOUT_REPOSITORIES, composeAboutView } from '../src/about-info.ts'
import { ABOUT_WINDOW_SIZE, DesktopAboutWindow } from '../src/about-window.ts'
import { resolveDesktopLocale } from '../src/locale.ts'

const PAGE = 'dsh-app://shell/about.html'

const fixture = await vi.hoisted(async () => {
  const { EventEmitter } = await import('node:events')
  const handlers = new Map<string, (...args: unknown[]) => unknown>()
  const windows: FakeWindow[] = []
  const clipboard = { text: '', writeText: vi.fn<(value: string) => Promise<void>>(), readText: vi.fn<() => Promise<string>>() }
  const openExternal = vi.fn<(url: string) => Promise<void>>()
  class FakeWindow extends EventEmitter {
    destroyed = false
    readonly webContents = Object.assign(new EventEmitter(), {
      send: vi.fn(),
      setWindowOpenHandler: vi.fn(),
      isDestroyed: () => this.destroyed,
      mainFrame: { url: PAGE },
    })
    readonly loadURL = vi.fn(async () => {})
    readonly restored = vi.fn()
    readonly show = vi.fn()
    readonly focus = vi.fn()
    readonly setTitle = vi.fn()
    readonly setMenuBarVisibility = vi.fn<(visible: boolean) => void>()
    constructor(readonly options: Record<string, unknown>) { super(); windows.push(this) }
    isDestroyed() { return this.destroyed }
    isMinimized() { return false }
    restore() { this.restored() }
    destroy() { this.destroyed = true; this.emit('closed') }
  }
  return { handlers, windows, clipboard, openExternal, FakeWindow }
})

vi.mock('electron', () => ({
  BrowserWindow: fixture.FakeWindow,
  clipboard: {
    writeText: async (value: string) => { fixture.clipboard.text = value; await fixture.clipboard.writeText(value) },
    readText: async () => { await fixture.clipboard.readText(); return fixture.clipboard.text },
  },
  shell: { openExternal: fixture.openExternal },
  ipcMain: {
    handle: (name: string, fn: (...args: unknown[]) => unknown) => { fixture.handlers.set(name, fn) },
    removeHandler: (name: string) => { fixture.handlers.delete(name) },
  },
}))

let windows: DesktopAboutWindow[] = []
afterEach(() => {
  for (const window of windows) window.dispose()
  windows = []
  fixture.windows.length = 0
  fixture.handlers.clear()
  fixture.clipboard.text = ''
  vi.clearAllMocks()
})

function setup(options: { locale?: string; version?: string; harnessVersion?: string } = {}) {
  const window = new DesktopAboutWindow({
    preload: 'preload-about.cjs',
    locale: () => resolveDesktopLocale(options.locale ?? 'en'),
    version: () => options.version ?? '1.0.0',
    harnessVersion: () => options.harnessVersion ?? '0.2.2',
  })
  windows.push(window)
  return { window }
}

function invoke(channel: string, ...args: unknown[]) {
  const view = fixture.windows.at(-1)!
  const handler = fixture.handlers.get(channel)
  if (handler === undefined) throw new Error(`no handler for ${channel}`)
  return handler({ sender: view.webContents, senderFrame: view.webContents.mainFrame }, ...args)
}

it('opens an independent window with the composed facts', () => {
  const f = setup({ locale: 'zh-CN', version: '0.2.2-rc.2', harnessVersion: '0.2.2' })
  f.window.show()
  const view = fixture.windows[0]!
  expect(view.options).toMatchObject({
    width: ABOUT_WINDOW_SIZE.width,
    height: ABOUT_WINDOW_SIZE.height,
    resizable: false,
    title: '关于 DeepSeek Harness',
  })
  // The main window may be hidden when the tray is the only way back, so this
  // window is owned by the application rather than parented to it.
  expect(view.options.parent).toBeUndefined()
  expect(view.options.webPreferences).toMatchObject({ preload: 'preload-about.cjs', sandbox: true, contextIsolation: true })
  // 菜单栏是移除而不是"自动隐藏"：后者按 Alt 会把它重新拉回来，而系统截图快捷键带 Alt。
  expect(view.options.autoHideMenuBar).toBeUndefined()
  expect(view.setMenuBarVisibility.mock.calls).toEqual([[false]])
  expect(view.loadURL).toHaveBeenCalledWith(PAGE)

  const status = invoke(ABOUT_IPC.status) as ReturnType<typeof composeAboutView>
  expect(status).toMatchObject({ locale: 'zh-CN', product: 'DeepSeek Harness', version: '0.2.2-rc.2',
    packageId: 'com.deepseek.dsh-desktop', form: '当前形态：Electron 版' })
  expect(status.fields).toEqual([
    { label: 'DSH 版本', value: '0.2.2' },
    { label: 'DSH 仓库', value: ABOUT_IDENTITY.upstreamRepo, url: ABOUT_IDENTITY.upstreamRepo },
    { label: '玲珑封装作者', value: 'Jokul' },
    { label: '玲珑封装仓库', value: ABOUT_IDENTITY.forkRepo, url: ABOUT_IDENTITY.forkRepo },
  ])
})

it('refocuses the window it already owns instead of stacking a second one', () => {
  const f = setup()
  f.window.show()
  f.window.show()
  expect(fixture.windows).toHaveLength(1)
  expect(fixture.windows[0]!.focus).toHaveBeenCalledOnce()
  expect(fixture.windows[0]!.webContents.send).toHaveBeenCalledWith(ABOUT_IPC.changed, expect.objectContaining({
    locale: 'en', packageId: 'com.deepseek.dsh-desktop',
  }))
})

it('copies the package ID through main and confirms the write', async () => {
  const f = setup()
  f.window.show()
  expect(await invoke(ABOUT_IPC.copyPackageId)).toBe(true)
  expect(fixture.clipboard.writeText).toHaveBeenCalledWith('com.deepseek.dsh-desktop')
})

it('opens only a repository it displayed', async () => {
  const f = setup()
  f.window.show()
  await invoke(ABOUT_IPC.open, ABOUT_REPOSITORIES[0])
  expect(fixture.openExternal).toHaveBeenCalledWith(ABOUT_IDENTITY.upstreamRepo)
  await expect(invoke(ABOUT_IPC.open, 'https://example.com')).rejects.toThrow(/unoffered/)
  await expect(invoke(ABOUT_IPC.open, 42)).rejects.toThrow(/invalid/)
  expect(fixture.openExternal).toHaveBeenCalledOnce()
})

it('rejects any document but its own and withdraws every channel on disposal', async () => {
  const f = setup()
  f.window.show()
  const handler = fixture.handlers.get(ABOUT_IPC.status)!
  expect(() => handler({ sender: {}, senderFrame: { url: PAGE } })).toThrow(/unowned/)
  expect(() => handler({ sender: fixture.windows[0]!.webContents, senderFrame: { url: 'dsh-app://shell/update-dialog.html' } }))
    .toThrow(/unowned/)
  f.window.dispose()
  expect(fixture.handlers.size).toBe(0)
  expect(fixture.windows[0]!.destroyed).toBe(true)
})

it('reinstalls its channels when the window is closed and opened again', () => {
  const f = setup()
  f.window.show()
  const first = fixture.windows[0]!
  first.destroy()
  expect(fixture.handlers.size).toBe(0)
  f.window.show()
  expect(fixture.windows).toHaveLength(2)
  expect(fixture.handlers.has(ABOUT_IPC.status)).toBe(true)
  expect(invoke(ABOUT_IPC.status)).toMatchObject({ packageId: 'com.deepseek.dsh-desktop' })
})

it('follows a shell language change while the window is open', () => {
  const f = setup({ locale: 'en' })
  f.window.show()
  const window = fixture.windows[0]!
  window.webContents.send.mockClear()
  vi.spyOn(window, 'setTitle')
  const relabel = setup({ locale: 'zh-CN' })
  relabel.window.show()
  fixture.windows.at(-1)!.webContents.send.mockClear()
  relabel.window.relabel()
  expect(fixture.windows.at(-1)!.setTitle).toHaveBeenCalledWith('关于 DeepSeek Harness')
  expect(fixture.windows.at(-1)!.webContents.send).toHaveBeenCalledWith(ABOUT_IPC.changed, expect.objectContaining({
    title: '关于 DeepSeek Harness',
  }))
  expect(f.window).toBeDefined()
})

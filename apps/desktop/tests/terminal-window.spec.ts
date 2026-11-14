import type { IPty, IPtyForkOptions } from 'node-pty'
import { afterEach, expect, it, vi } from 'vitest'
import { resolveDesktopLocale } from '../src/locale.ts'
import { TERMINAL_IPC, type DesktopTerminalView } from '../src/terminal-contract.ts'
import type { DesktopTerminalSpawn } from '../src/terminal-session.ts'
import { DesktopTerminalWindow, TERMINAL_WINDOW_SIZE } from '../src/terminal-window.ts'

const PAGE = 'dsh-app://shell/terminal.html'

const fixture = await vi.hoisted(async () => {
  const { EventEmitter } = await import('node:events')
  const handlers = new Map<string, (...args: unknown[]) => unknown>()
  const windows: FakeWindow[] = []
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
    constructor(readonly options: Record<string, unknown>) { super(); windows.push(this) }
    isDestroyed() { return this.destroyed }
    isMinimized() { return false }
    restore() { this.restored() }
    destroy() { this.destroyed = true; this.emit('closed') }
  }
  return { handlers, windows, FakeWindow }
})

vi.mock('electron', () => ({
  BrowserWindow: fixture.FakeWindow,
  ipcMain: {
    handle: (name: string, fn: (...args: unknown[]) => unknown) => { fixture.handlers.set(name, fn) },
    removeHandler: (name: string) => { fixture.handlers.delete(name) },
  },
}))

/** One fake PTY: records what it was asked to do and lets a test drive output and exit. */
class FakePty {
  readonly pid = 4242
  readonly written: string[] = []
  readonly resized: Array<{ cols: number; rows: number }> = []
  readonly kills: string[] = []
  private data: ((data: string) => void) | undefined
  private exit: ((event: { exitCode: number; signal?: number }) => void) | undefined
  constructor(readonly file: string, readonly args: string[], readonly options: IPtyForkOptions) {}

  onData(listener: (data: string) => void) { this.data = listener; return { dispose: () => { this.data = undefined } } }
  onExit(listener: (event: { exitCode: number; signal?: number }) => void) {
    this.exit = listener
    return { dispose: () => { this.exit = undefined } }
  }
  write(data: string) { this.written.push(data) }
  resize(cols: number, rows: number) { this.resized.push({ cols, rows }) }
  kill(signal?: string) { this.kills.push(signal ?? 'SIGTERM') }
  pause() {}
  resume() {}

  emit(data: string): void { this.data?.(data) }
  quits(exitCode: number): void { this.exit?.({ exitCode }) }
}

const ptys: FakePty[] = []
const spawn: DesktopTerminalSpawn = (file, args, options) => {
  const pty = new FakePty(file, args, options)
  ptys.push(pty)
  return pty as unknown as IPty
}

const windows: DesktopTerminalWindow[] = []
afterEach(() => {
  for (const window of windows.splice(0)) window.dispose()
  fixture.windows.length = 0
  fixture.handlers.clear()
  ptys.length = 0
  vi.clearAllMocks()
})

function setup(locale = 'en') {
  const window = new DesktopTerminalWindow({
    preload: 'preload-terminal.cjs', locale: () => resolveDesktopLocale(locale), cwd: () => '/home/user', spawn,
  })
  windows.push(window)
  return { window, setLocale: (next: string) => { locale = next } }
}

function invoke(channel: string, ...args: unknown[]) {
  const view = fixture.windows.at(-1)!
  const handler = fixture.handlers.get(channel)
  if (handler === undefined) throw new Error(`no handler for ${channel}`)
  return handler({ sender: view.webContents, senderFrame: view.webContents.mainFrame }, ...args)
}

/** The view main last pushed into the document. */
function pushed(): DesktopTerminalView {
  const call = fixture.windows.at(-1)!.webContents.send.mock.calls.at(-1)
  if (call === undefined) throw new Error('nothing was published')
  expect(call[0]).toBe(TERMINAL_IPC.state)
  return call[1] as DesktopTerminalView
}

it('opens an independent window owned by the application rather than the main window', () => {
  const f = setup('zh-CN')
  f.window.show()
  const view = fixture.windows[0]!
  expect(view.options).toMatchObject({
    width: TERMINAL_WINDOW_SIZE.width,
    height: TERMINAL_WINDOW_SIZE.height,
    title: '终端',
  })
  // The main window may be hidden when the tray is the only way in, so this window is
  // owned by the application instead of being parented to a hidden one.
  expect(view.options.parent).toBeUndefined()
  expect(view.options.webPreferences).toMatchObject({ preload: 'preload-terminal.cjs', sandbox: true, contextIsolation: true })
  expect(view.loadURL).toHaveBeenCalledWith(PAGE)
})

it('starts one session on the shell it resolved and forwards input and size', () => {
  const f = setup()
  f.window.show()
  expect(invoke(TERMINAL_IPC.start, 120, 40)).toMatchObject({ phase: 'running', locale: 'en' })
  const pty = ptys[0]!
  // The shell is the account's login shell or a POSIX fallback, never a hard-coded one.
  expect(pty.file).toMatch(/^\//u)
  expect(pty.options).toMatchObject({ cols: 120, rows: 40, cwd: '/home/user' })
  expect(pty.options.env?.SHELL).toBe(pty.file)
  expect(pty.options.env?.TERM).toBe('xterm-256color')
  invoke(TERMINAL_IPC.write, 'ls -la\r')
  expect(pty.written).toEqual(['ls -la\r'])
  invoke(TERMINAL_IPC.resize, 100, 30)
  expect(pty.resized).toEqual([{ cols: 100, rows: 30 }])
})

it('reuses the running session so a reload cannot kill the user’s command', () => {
  const f = setup()
  f.window.show()
  invoke(TERMINAL_IPC.start, 80, 24)
  invoke(TERMINAL_IPC.start, 90, 30)
  expect(ptys).toHaveLength(1)
  // The second report only reconciles the size with the new layout.
  expect(ptys[0]!.resized).toEqual([{ cols: 90, rows: 30 }])
})

it('reports the exit code and lets the page start a replacement session', () => {
  const f = setup('zh-CN')
  f.window.show()
  invoke(TERMINAL_IPC.start, 80, 24)
  ptys[0]!.quits(3)
  expect(pushed()).toMatchObject({ phase: 'exited', status: 'Shell 已退出，退出码 3。' })
  invoke(TERMINAL_IPC.start, 80, 24)
  expect(ptys).toHaveLength(2)
  // A fresh session carries no status line: the exit text described the previous shell.
  expect(pushed()).toMatchObject({ phase: 'running' })
  expect(pushed().status).toBeUndefined()
})

it('turns a failing spawn into a localized failure instead of an exception', () => {
  const failing = new DesktopTerminalWindow({
    preload: 'preload-terminal.cjs', locale: () => resolveDesktopLocale('zh-CN'), cwd: () => '/home/user',
    spawn: () => { throw new Error('node-pty unavailable') },
  })
  windows.push(failing)
  vi.spyOn(console, 'error').mockImplementation(() => {})
  failing.show()
  expect(invoke(TERMINAL_IPC.start, 80, 24)).toMatchObject({ phase: 'failed', status: '终端启动失败。' })
  expect(ptys).toHaveLength(0)
})

it('pushes output only while the window can receive it', () => {
  const f = setup()
  f.window.show()
  invoke(TERMINAL_IPC.start, 80, 24)
  ptys[0]!.emit('total 8\r\n')
  expect(fixture.windows[0]!.webContents.send).toHaveBeenCalledWith(TERMINAL_IPC.output, 'total 8\r\n')
  fixture.windows[0]!.webContents.send.mockClear()
  fixture.windows[0]!.destroy()
  ptys[0]!.emit('late output')
  expect(fixture.windows[0]!.webContents.send).not.toHaveBeenCalled()
})

it('refocuses the window it owns and relabels it without stacking another', () => {
  const f = setup()
  f.window.show()
  f.window.show()
  expect(fixture.windows).toHaveLength(1)
  expect(fixture.windows[0]!.focus).toHaveBeenCalledOnce()
  f.setLocale('zh-CN')
  f.window.relabel()
  expect(fixture.windows[0]!.setTitle).toHaveBeenCalledWith('终端')
  expect(pushed()).toMatchObject({ title: '终端', screenLabel: '终端屏幕' })
})

it('rejects any document but its own and withdraws every channel on disposal', () => {
  const f = setup()
  f.window.show()
  const handler = fixture.handlers.get(TERMINAL_IPC.status)!
  expect(() => handler({ sender: {}, senderFrame: { url: PAGE } })).toThrow(/unowned/u)
  expect(() => handler({ sender: fixture.windows[0]!.webContents, senderFrame: { url: 'dsh-app://shell/about.html' } }))
    .toThrow(/unowned/u)
  f.window.dispose()
  expect(fixture.handlers.size).toBe(0)
  expect(fixture.windows[0]!.destroyed).toBe(true)
})

it('rejects input that is not a bounded string', () => {
  const f = setup()
  f.window.show()
  invoke(TERMINAL_IPC.start, 80, 24)
  expect(() => invoke(TERMINAL_IPC.write, 42)).toThrow(/invalid input/u)
  expect(() => invoke(TERMINAL_IPC.write, 'x'.repeat(1_000_001))).toThrow(/invalid input/u)
  expect(ptys[0]!.written).toEqual([])
})

it('ends the shell when the window closes, with SIGHUP before the kill fallback', () => {
  const f = setup()
  f.window.show()
  invoke(TERMINAL_IPC.start, 80, 24)
  fixture.windows[0]!.destroy()
  expect(ptys[0]!.kills).toEqual(['SIGHUP'])
  // Closing the window is not a reason to wait on the grace period in the test.
  f.window.dispose()
})

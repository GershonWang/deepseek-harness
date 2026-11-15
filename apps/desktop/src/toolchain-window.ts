/**
 * The shell-owned toolchain window: browse, install, switch, and remove tools.
 *
 * A separate window rather than an overlay because closing the main window hides it; a
 * card living in that window's DOM would be invisible, and raising the main window would
 * move the user out of whatever they were doing. It opens from the tray, so it stays
 * reachable while the workspace is hidden.
 *
 * Only one install runs at a time. Installing two archives concurrently would interleave
 * their progress reports on one cancel control and compete for the same cache directory,
 * so a second request is refused rather than queued.
 *
 * @module @deepseek-ai/dsh-desktop/toolchain-window
 */

import { BrowserWindow, ipcMain, type IpcMainInvokeEvent } from 'electron'
import { TOOLCHAIN_IPC, type ToolchainProgressView, type ToolchainView } from './toolchain-contract.ts'
import { hideMenuBar } from './menu-bar.ts'
import type { DesktopLocale } from './locale.ts'

/** Shell-owned document this window draws; `main.ts` serves it from the renderer directory. */
const page = 'dsh-app://shell/toolchain.html'

/**
 * Window geometry.
 *
 * Sized for four cards per row and enough rows to see a category at a glance: the
 * catalog holds about fifty cards, and a window showing a dozen makes scrolling the
 * dominant interaction. The minimum is much smaller so the window still works on a
 * laptop screen.
 */
export const TOOLCHAIN_WINDOW_SIZE = { width: 1180, height: 820 } as const

/** Inputs the window needs from the running application. */
export interface DesktopToolchainWindowOptions {
  /** Isolated bridge exposing only the toolchain channels. */
  readonly preload: string
  /** Locale reader, so an open window follows a language change. */
  readonly locale: () => DesktopLocale
  /** Compose the current view. */
  readonly view: () => Promise<ToolchainView>
  /** Install one version; resolves when the install settles. */
  readonly install: (id: string, version: string, report: (progress: ToolchainProgressView) => void, signal: AbortSignal) => Promise<void>
  /** Remove one installed version. */
  readonly uninstall: (id: string, version: string) => Promise<void>
  /** Activate one installed version. */
  readonly activate: (id: string, version: string) => Promise<void>
  /** Stand a tool down so the bundled copy takes over again. */
  readonly deactivate: (id: string) => Promise<void>
  /** Refetch the index. */
  readonly refresh: () => Promise<void>
}

/** Owns the single toolchain window and the IPC access it grants. */
export class DesktopToolchainWindow {
  private window: BrowserWindow | undefined
  private disposed = false
  /** Abort handle for the running install, absent when idle. */
  private running: AbortController | undefined

  /** @param options - locale, view, and action readers. */
  constructor(private readonly options: DesktopToolchainWindowOptions) {}

  /**
   * Show the window, or focus the one already open.
   *
   * A second click must not stack a second window: the tray entry stays where it was.
   */
  show(): void {
    if (this.disposed) return
    const existing = this.window
    if (existing !== undefined && !existing.isDestroyed()) {
      if (existing.isMinimized()) existing.restore()
      existing.show()
      existing.focus()
      void this.publish()
      return
    }
    const window = new BrowserWindow({
      ...TOOLCHAIN_WINDOW_SIZE,
      useContentSize: true,
      minWidth: 680,
      minHeight: 480,
      show: false,
      title: this.options.locale().messages.toolchainMenu,
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
    // The market draws its own controls, so a native Application/Edit row would only add
    // English chrome to a localized document — and it would reappear on every Alt press,
    // which is exactly what a system screenshot shortcut sends. `hideMenuBar` removes the
    // row outright instead of letting Alt restore it.
    hideMenuBar(window)
    this.installHandlers(window)
    window.once('ready-to-show', () => { window.show() })
    window.on('closed', () => {
      // A window closed mid-install leaves nothing to report to, so the install stops.
      this.running?.abort()
      this.running = undefined
      if (this.window === window) this.window = undefined
      this.removeHandlers()
    })
    window.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))
    window.webContents.on('will-navigate', (event, url) => { if (url !== page) event.preventDefault() })
    void window.loadURL(page)
  }

  /** Follow a shell language change while the window is open. */
  relabel(): void {
    const window = this.window
    if (window === undefined || window.isDestroyed()) return
    window.setTitle(this.options.locale().messages.toolchainMenu)
    void this.publish()
  }

  /** Destroy the window and withdraw its IPC access. */
  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    this.running?.abort()
    this.running = undefined
    const window = this.window
    this.window = undefined
    this.removeHandlers()
    if (window !== undefined && !window.isDestroyed()) window.destroy()
  }

  /** Push the current view into the open document. */
  private async publish(): Promise<void> {
    const window = this.window
    if (window === undefined || window.webContents.isDestroyed()) return
    const view = await this.options.view().catch(() => undefined)
    if (view === undefined || this.window !== window || window.webContents.isDestroyed()) return
    window.webContents.send(TOOLCHAIN_IPC.changed, view)
  }

  /** @param view - progress report to deliver. */
  private report(view: ToolchainProgressView): void {
    const window = this.window
    if (window === undefined || window.webContents.isDestroyed()) return
    window.webContents.send(TOOLCHAIN_IPC.progress, view)
  }

  /** Register this window's channels for as long as it exists. */
  private installHandlers(window: BrowserWindow): void {
    this.removeHandlers()
    ipcMain.handle(TOOLCHAIN_IPC.status, async (event) => {
      this.owned(event, window)
      return this.options.view()
    })
    ipcMain.handle(TOOLCHAIN_IPC.install, async (event, id: unknown, version: unknown) => {
      this.owned(event, window)
      if (typeof id !== 'string' || typeof version !== 'string') throw new Error('desktop toolchain: invalid install request')
      await this.install(id, version)
    })
    ipcMain.handle(TOOLCHAIN_IPC.cancel, (event) => {
      this.owned(event, window)
      this.running?.abort()
    })
    ipcMain.handle(TOOLCHAIN_IPC.uninstall, async (event, id: unknown, version: unknown) => {
      this.owned(event, window)
      if (typeof id !== 'string' || typeof version !== 'string') throw new Error('desktop toolchain: invalid uninstall request')
      // Removing a version under a running install would race the same directory.
      if (this.running !== undefined) throw new Error('desktop toolchain: an install is running')
      await this.options.uninstall(id, version)
      await this.publish()
    })
    ipcMain.handle(TOOLCHAIN_IPC.activate, async (event, id: unknown, version: unknown) => {
      this.owned(event, window)
      if (typeof id !== 'string' || typeof version !== 'string') throw new Error('desktop toolchain: invalid activate request')
      if (this.running !== undefined) throw new Error('desktop toolchain: an install is running')
      await this.options.activate(id, version)
      await this.publish()
    })
    ipcMain.handle(TOOLCHAIN_IPC.deactivate, async (event, id: unknown) => {
      this.owned(event, window)
      if (typeof id !== 'string') throw new Error('desktop toolchain: invalid deactivate request')
      // Removing the active link under a running install would race the same directory.
      if (this.running !== undefined) throw new Error('desktop toolchain: an install is running')
      await this.options.deactivate(id)
      await this.publish()
    })
    ipcMain.handle(TOOLCHAIN_IPC.refresh, async (event) => {
      this.owned(event, window)
      await this.options.refresh()
      await this.publish()
    })
  }

  /**
   * Run one install and keep the window in step with it.
   * @param id - tool id.
   * @param version - version to install.
   * @throws when another install is already running.
   */
  private async install(id: string, version: string): Promise<void> {
    if (this.running !== undefined) throw new Error('desktop toolchain: an install is running')
    const controller = new AbortController()
    this.running = controller
    try {
      await this.options.install(id, version, (progress) => { this.report(progress) }, controller.signal)
    } finally {
      if (this.running === controller) this.running = undefined
      await this.publish()
    }
  }

  /** Withdraw every channel this module owns, whether or not a window is still open. */
  private removeHandlers(): void {
    for (const channel of Object.values(TOOLCHAIN_IPC)) {
      // `changed` and `progress` are push-only; removing an unregistered handler is a no-op.
      ipcMain.removeHandler(channel)
    }
  }

  /**
   * Reject any sender that is not this window's shell document.
   * @param event - IPC invocation to check.
   * @param window - window that owns this module's channels.
   */
  private owned(event: IpcMainInvokeEvent, window: BrowserWindow): void {
    if (this.disposed || this.window !== window || window.isDestroyed()
      || event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame
      || event.senderFrame.url !== page) {
      throw new Error('desktop toolchain: rejected unowned document')
    }
  }
}

/**
 * The shell-owned About window: identity, versions, and where this build came from.
 *
 * It is a separate native window rather than an overlay on the main window
 * because closing the main window hides it: a modal living in that window's DOM
 * would be invisible, and raising the main window to show an About card would
 * move the user out of whatever they were doing. The window is opened from the
 * tray only, so it stays reachable while the workspace is hidden.
 *
 * Every string and every allowed destination is decided here; the document
 * renders what it receives and hands repository URLs back for validation.
 *
 * @module @deepseek-ai/dsh-desktop/about-window
 */

import { BrowserWindow, clipboard, ipcMain, shell, type IpcMainInvokeEvent } from 'electron'
import { ABOUT_IPC, type DesktopAboutView } from './about-contract.ts'
import { ABOUT_REPOSITORIES, composeAboutView } from './about-info.ts'
import { hideMenuBar } from './menu-bar.ts'
import type { DesktopLocale } from './locale.ts'

/** Shell-owned document this window draws; `main.ts` serves it from the renderer directory. */
const page = 'dsh-app://shell/about.html'

/**
 * Fixed geometry: the content is a fixed set of rows, so the window neither grows
 * nor shrinks. The width fits one row of the widest shipped label (Portuguese,
 * 146 px) and a 339 px monospace repository URL, plus a 40 px margin each side.
 */
export const ABOUT_WINDOW_SIZE = { width: 580, height: 460 } as const

/** Inputs the window needs from the running application. */
export interface DesktopAboutWindowOptions {
  /** Isolated bridge exposing only the About channels. */
  readonly preload: string
  /** Locale reader, so a relabel while the window is open follows the shell language. */
  readonly locale: () => DesktopLocale
  /** Desktop client version for this build. */
  readonly version: () => string
  /** Distribution package version this build carries, when the packaging step wrote one. */
  readonly packageVersion?: () => string | undefined
  /** DSH runtime version bundled with this build. */
  readonly harnessVersion: () => string
}

/** Owns the single About window and the IPC access it grants. */
export class DesktopAboutWindow {
  private window: BrowserWindow | undefined
  private disposed = false

  /** @param options - Locale and version readers. */
  constructor(private readonly options: DesktopAboutWindowOptions) {}

  /**
   * Show the About window, or focus the one already open.
   *
   * A second click must not stack a second window: the tray entry stays where it
   * was, so a user can click it again while the window is already up.
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
    // No parent window: the main window may be hidden, and a child of a hidden
    // window is not reliably shown, while raising it would move the user out of
    // whatever they were doing. This window is reachable from the tray on its own.
    const window = new BrowserWindow({
      ...ABOUT_WINDOW_SIZE,
      useContentSize: true,
      resizable: false,
      maximizable: false,
      fullscreenable: false,
      show: false,
      title: this.options.locale().messages.aboutMenu,
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
    // The document composes every string it shows; a native Application/Edit row would add
    // English chrome, and leaving the bar merely auto-hidden lets Alt bring it back.
    hideMenuBar(window)
    this.installHandlers(window)
    window.once('ready-to-show', () => { window.show() })
    window.on('closed', () => {
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
    window.setTitle(this.options.locale().messages.aboutMenu)
    this.publish()
  }

  /** Destroy the window and withdraw its IPC access. */
  dispose(): void {
    if (this.disposed) return
    this.disposed = true
    const window = this.window
    this.window = undefined
    this.removeHandlers()
    if (window !== undefined && !window.isDestroyed()) window.destroy()
  }

  /** @returns the view for the current locale and versions, or undefined once disposed. */
  private view(): DesktopAboutView | undefined {
    if (this.disposed) return undefined
    const packageVersion = this.options.packageVersion?.()
    return composeAboutView({
      locale: this.options.locale(),
      version: this.options.version(),
      harnessVersion: this.options.harnessVersion(),
      ...(packageVersion === undefined ? {} : { packageVersion }),
    })
  }

  /**
   * Push the current view into an already-loaded document; a first load reads it itself.
   *
   * The channel is the same one the document subscribes to on startup, so a
   * relabelled window and a freshly opened one take the same path.
   */
  private publish(): void {
    const window = this.window
    const view = this.view()
    if (window === undefined || view === undefined || window.webContents.isDestroyed()) return
    window.webContents.send(ABOUT_IPC.changed, view)
  }

  /** Register this window's channels for as long as it exists. */
  private installHandlers(window: BrowserWindow): void {
    this.removeHandlers()
    ipcMain.handle(ABOUT_IPC.status, (event) => { this.owned(event, window); return this.view() })
    ipcMain.handle(ABOUT_IPC.copyPackageId, async (event) => {
      this.owned(event, window)
      return this.copyPackageId()
    })
    ipcMain.handle(ABOUT_IPC.open, async (event, value: unknown) => {
      this.owned(event, window)
      if (typeof value !== 'string') throw new Error('desktop about: invalid repository URL')
      // The document submits a destination it displayed; anything else is not from this view.
      if (!ABOUT_REPOSITORIES.includes(value)) throw new Error('desktop about: unoffered repository URL')
      await shell.openExternal(value)
    })
  }

  /** Withdraw every channel this module owns, whether or not a window is still open. */
  private removeHandlers(): void {
    for (const channel of [ABOUT_IPC.status, ABOUT_IPC.copyPackageId, ABOUT_IPC.open]) ipcMain.removeHandler(channel)
  }

  /**
   * Copy the package ID and confirm the clipboard kept it.
   * @returns whether the clipboard now holds the package ID.
   */
  private async copyPackageId(): Promise<boolean> {
    const view = this.view()
    if (view === undefined) return false
    await clipboard.writeText(view.packageId)
    // Electron's clipboard is asynchronous here, so confirm the write instead of assuming it.
    return await clipboard.readText() === view.packageId
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
      throw new Error('desktop about: rejected unowned document')
    }
  }
}

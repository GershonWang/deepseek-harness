/**
 * Desktop tray: the always-present way back to a hidden window and the explicit quit entry.
 *
 * Closing the main window hides it on every platform, so a hidden run needs a
 * visible way back and a way to quit. Windows and Linux use different image
 * formats — the ICO carries per-scale bitmaps, while a Linux panel loads a single
 * PNG — but the behavior is the same.
 */

import { Menu, nativeImage, Tray } from 'electron'
import type { DesktopLocale } from './locale.ts'

/** Main-process actions the tray triggers; both run the same paths as the window and application menu. */
export interface DesktopTrayOptions {
  /** Tray image: a multi-size ICO on Windows, a PNG on Linux. */
  readonly iconPath: string
  readonly locale: () => DesktopLocale
  /** Show and focus the primary window. */
  readonly open: () => void
  /** Request quit through the same confirmation as every other quit entry. */
  readonly quit: () => void
  /**
   * Open the About window.
   *
   * Omitted where the platform already offers its own About entry: macOS uses
   * the system About panel and Windows its application menu command, so a second
   * tray entry there would present different facts under the same name.
   */
  readonly about?: () => void
}

/** Tray icon present for the whole run, not only while the window is hidden. */
export class DesktopTray {
  private tray: Tray | undefined

  /** @param options - Icon path, locale reader, and the open, about, and quit actions. */
  constructor(private readonly options: DesktopTrayOptions) {
    const tray = new Tray(nativeImage.createFromPath(options.iconPath))
    this.tray = tray
    tray.on('click', () => { options.open() })
    this.relabel()
  }

  /** Rebuild the tooltip and context menu in the current locale. */
  relabel(): void {
    const tray = this.tray
    if (tray === undefined) return
    const { messages } = this.options.locale()
    const about = this.options.about
    tray.setToolTip(messages.aboutProduct)
    tray.setContextMenu(Menu.buildFromTemplate([
      { label: messages.openApplication, click: () => { this.options.open() } },
      ...about === undefined ? [] : [{ label: messages.aboutTray, click: () => { about() } }],
      { type: 'separator' },
      { label: messages.quitApplication, click: () => { this.options.quit() } },
    ]))
  }

  /** Remove the icon; called once the quit is confirmed so no dead icon outlives the process. */
  dispose(): void {
    const tray = this.tray
    this.tray = undefined
    tray?.destroy()
  }
}

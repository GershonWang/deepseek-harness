/** Menu-bar visibility for product windows. */

import type { BrowserWindow } from 'electron'

/**
 * Remove the native menu bar from a product window while keeping its accelerators.
 *
 * Linux and Windows otherwise draw the literal Application and Edit rows inside the
 * window, whose labels come from Electron's built-in roles rather than the shell locale;
 * the product already publishes its own entries, so those rows duplicate them in a
 * language the rest of the interface does not use. macOS draws the menu in the system bar
 * instead of the window and is left alone.
 *
 * The application menu stays registered, so F12 and the other accelerators it declares
 * keep working, and the hidden state survives later `show()` and `setApplicationMenu()`
 * calls. `autoHideMenuBar` stays off, which is what keeps the single Alt key from
 * restoring the bar.
 * @param window - product window whose menu bar should not appear.
 */
export function hideMenuBar(window: Pick<BrowserWindow, 'setMenuBarVisibility'>): void {
  if (process.platform === 'darwin') return
  window.setMenuBarVisibility(false)
}

/**
 * Whether this build carries an update feed, decided once and shared by every
 * update entry point.
 *
 * electron-updater reads `app-update.yml` beside the application resources and
 * nothing else; a package that ships no such file cannot check, download, or
 * install a release. The Linux bundle is exactly that case: distribution moves
 * outside the application, so electron-builder writes no feed and no
 * `app-update.yml` reaches the installed tree. Detecting the file rather than
 * the platform keeps this answer correct if a Linux feed is ever added, and keeps
 * macOS and Windows, which always ship one, unchanged.
 *
 * @module @deepseek-ai/dsh-desktop/update-availability
 */

import { existsSync } from 'node:fs'
import { join } from 'node:path'
import { app } from 'electron'

/** Feed descriptor electron-updater reads from the application resources. */
export const UPDATE_FEED_FILENAME = 'app-update.yml'

/**
 * Report whether ordinary and mandatory updates can be served by this build.
 *
 * @param isPackaged - Whether the running application is an installed build.
 * @param resourcesPath - Directory holding the packaged application resources.
 * @returns True only when an unpackaged run cannot be offered an update and a
 * packaged one actually carries the feed descriptor.
 */
export function hasPackagedUpdateSource(
  isPackaged: boolean = app.isPackaged,
  resourcesPath: string = process.resourcesPath,
): boolean {
  // An unpackaged run must never contact a feed: its files are not the installed
  // application the download would replace.
  if (!isPackaged) return false
  try {
    return existsSync(join(resourcesPath, UPDATE_FEED_FILENAME))
  } catch {
    // An unreadable resources directory is equivalent to a missing feed; updates
    // stay unavailable instead of failing every entry point that asks.
    return false
  }
}

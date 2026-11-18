/**
 * Resolve the Landlock launcher to the path a child process can actually spawn.
 *
 * Electron rewrites archive *reads* onto the sibling `app.asar.unpacked`
 * directory, not spawn targets: `child_process` hands the path to the kernel,
 * which finds a directory entry inside the archive file instead of an
 * executable (ENOTDIR). A packaged application therefore has to spawn the
 * unpacked sibling explicitly, even though `require.resolve` reports the
 * archive path and reading through it works.
 * @module @deepseek-ai/dsh-sandbox-local/launcher-path
 */

import { existsSync } from 'node:fs'

/**
 * The launcher path to probe and exec.
 *
 * The unpacked sibling is preferred only when it exists, so a package that
 * predates the desktop packaging rule that unpacks this binary keeps the
 * resolved path and fails the same way it does today instead of acquiring a
 * new, differently-located failure. Installs outside an archive — the source
 * launch and every non-Electron profile — have no `.asar` segment, so the
 * resolved path is returned unchanged.
 * @param resolved - the launcher path resolved from the installed platform package.
 * @returns the path to spawn, inside `app.asar.unpacked` when a packaged application provides it there.
 */
export function spawnableLauncherPath(resolved: string): string {
  const unpacked = resolved.replace(/\.asar(?=[\\/])/u, '.asar.unpacked')
  return unpacked !== resolved && existsSync(unpacked) ? unpacked : resolved
}

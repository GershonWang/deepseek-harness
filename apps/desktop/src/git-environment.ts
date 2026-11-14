/**
 * Git environment for the Linglong-packaged Desktop shell.
 *
 * Git's compile-time exec path is `/usr/lib/git-core`, which the Linglong
 * container does not have: the packaged `git` shipped by `buildext.apt.depends`
 * keeps all 170 helpers (including `git-remote-https`) under `${PREFIX}/lib/git-core`
 * instead. Without redirecting `GIT_EXEC_PATH`, local operations work while every
 * network operation fails with `git: 'remote-https' is not a git command`, so
 * `clone`, `fetch`, `pull`, and `push` over HTTPS all break — the exact commands a
 * coding session needs.
 *
 * The same applies to the init templates: a packaged git looks for them under
 * `/usr/share/git-core/templates`. Their absence only drops the sample hooks from a
 * fresh repository, so it is redirected when present and ignored otherwise.
 *
 * @module @deepseek-ai/dsh-desktop/git-environment
 */

import { access, stat } from 'node:fs/promises'
import { delimiter, dirname, join } from 'node:path'

/** Environment variables this module sets. */
export const GIT_EXEC_PATH_ENV = 'GIT_EXEC_PATH'
/** Init templates directory. */
export const GIT_TEMPLATE_DIR_ENV = 'GIT_TEMPLATE_DIR'

/** Executable directory of a packaged git, relative to the application root. */
const EXEC_DIR = join('lib', 'git-core')

/** Init templates of a packaged git, relative to the application root. */
const TEMPLATE_DIR = join('share', 'git-core', 'templates')

/** Whether a path is an existing directory. */
async function isDirectory(path: string): Promise<boolean> {
  try {
    return (await stat(path)).isDirectory()
  } catch {
    return false
  }
}

/**
 * Whether the packaged git carries the helper directory.
 *
 * The check is the helper itself rather than the directory: a directory without
 * `git-remote-https` would redirect git to a tree that cannot reach a remote either.
 * @param appRoot - installation root containing `lib/` and `share/`.
 * @returns whether `${appRoot}/lib/git-core/git-remote-https` exists and is executable.
 */
export async function hasPackagedGitHelpers(appRoot: string): Promise<boolean> {
  const helper = join(appRoot, EXEC_DIR, 'git-remote-https')
  try {
    await access(helper)
    return true
  } catch {
    return false
  }
}

/**
 * Resolve the git environment entries for a packaged installation.
 *
 * Precedence matches the rest of the shell environment: a value the surrounding
 * environment already sets is a deliberate choice and is not overwritten. Setting
 * `GIT_EXEC_PATH` by hand is also how a user points git at a different installation,
 * and that must keep working.
 * @param appRoot - installation root containing `lib/` and `share/`.
 * @param environment - environment to read for existing values.
 * @returns the entries to add, or an empty list when nothing is packaged or already set.
 */
export async function resolveGitEnvironment(
  appRoot: string,
  environment: NodeJS.ProcessEnv = process.env,
): Promise<Record<string, string>> {
  const resolved: Record<string, string> = {}
  if (environment[GIT_EXEC_PATH_ENV] === undefined && (await hasPackagedGitHelpers(appRoot))) {
    resolved[GIT_EXEC_PATH_ENV] = join(appRoot, EXEC_DIR)
  }
  if (environment[GIT_TEMPLATE_DIR_ENV] === undefined) {
    const templates = join(appRoot, TEMPLATE_DIR)
    if (await isDirectory(templates)) resolved[GIT_TEMPLATE_DIR_ENV] = templates
  }
  return resolved
}

/**
 * Apply the git environment to an environment mapping.
 *
 * The Host child and everything it spawns inherit this mapping, so the values must
 * be set on it rather than on `process.env` alone: the login shell read replaces the
 * whole environment, and a value written only to `process.env` would be lost.
 * @param environment - mapping to update in place.
 * @param entries - entries from {@link resolveGitEnvironment}.
 */
export function applyGitEnvironment(environment: NodeJS.ProcessEnv, entries: Readonly<Record<string, string>>): void {
  for (const [name, value] of Object.entries(entries)) environment[name] = value
}

/**
 * Installation root of a Linglong-packaged Desktop application.
 *
 * The Linglong layout puts the Electron tree under `${PREFIX}/electron`, so the
 * executable sits one level below the prefix that holds the merged `lib/`, `bin/`,
 * and `share/` trees. `resourcesPath` cannot serve here: it points inside the
 * Electron tree, while the git directories are merged beside it.
 * @param execPath - Electron's `process.execPath`.
 * @returns the `${PREFIX}` directory holding the merged dependency trees.
 */
export function packagedAppRoot(execPath: string): string {
  return dirname(dirname(execPath))
}

/**
 * Compose a PATH value with an additional leading directory.
 * @param path - current PATH value.
 * @param directory - directory to place first.
 * @returns the PATH value with the directory first.
 */
export function withLeadingPath(path: string | undefined, directory: string): string {
  const existing = path ?? ''
  return existing === '' ? directory : `${directory}${delimiter}${existing}`
}

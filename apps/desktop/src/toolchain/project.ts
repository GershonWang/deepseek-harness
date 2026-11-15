/**
 * Toolchain activation: which version is live, and what the shared entry points expose.
 *
 * A tool's active version is a symlink under `current/`. The `bin/` and `lib/`
 * directories beside it are rebuilt from those symlinks rather than accumulated, so a
 * reinstall, an upgrade, or a moved home directory repairs itself: the harness puts
 * `<tools>/bin` on PATH and `<tools>/lib` on the library path, and both must describe
 * what is installed right now.
 *
 * Everything under `bin/` becomes a command on PATH, so every name written there is
 * checked against escaping the tools root: a `bin_names` entry of `../../.bashrc` would
 * otherwise delete a file outside the directory before linking into it.
 *
 * @module @deepseek-ai/dsh-desktop/toolchain/project
 */

import { chmod, lstat, mkdir, readdir, readlink, rename, rm, stat, symlink } from 'node:fs/promises'
import { isAbsolute, join, relative } from 'node:path'
import { activeVersion, isValidToolId, listVersions, toolsLayout, type Tool, type ToolVersion, type ToolsLayout } from './catalog.ts'
import { compareVersions } from './version.ts'

/** Result of a repair pass over the shared entry points. */
export interface ReconcileResult {
  /** Command names now linked into `bin/`. */
  readonly commands: readonly string[]
  /** Problems found and skipped; a non-empty list means the tree was partly untrusted. */
  readonly problems: readonly string[]
}

/**
 * Whether a relative path stays inside its base.
 * @param path - path to test.
 * @returns whether it is a local relative path with no upward escape.
 */
function isLocalRelative(path: string): boolean {
  if (path === '' || isAbsolute(path)) return false
  const normalized = relative('.', path)
  return normalized !== '..' && !normalized.startsWith('../')
}

/**
 * Whether `candidate` is `base` itself or lies beneath it.
 *
 * Distinct from {@link isLocalRelative} because the container directory of a
 * single-file distribution *is* the install root: `relative(root, root)` is the empty
 * string, which is a local path but not one {@link isLocalRelative} accepts.
 * @param base - containing directory.
 * @param candidate - directory to test.
 * @returns whether the candidate stays within the base.
 */
function isWithin(base: string, candidate: string): boolean {
  const path = relative(base, candidate)
  return path === '' || (path !== '..' && !path.startsWith('../') && !isAbsolute(path))
}

/**
 * Whether a name can become a command in `bin/`.
 * @param name - candidate command name.
 * @returns whether the name is a plain local name.
 */
function isSafeCommandName(name: string): boolean {
  return isLocalRelative(name) && !name.includes('/') && !name.includes('\\') && name !== '.' && name !== '..'
}

/**
 * Switch a tool to an installed version and rebuild the entry points.
 *
 * The link is written to a temporary name and renamed into place: a symlink cannot be
 * updated atomically, and removing it first leaves a window where the tool is
 * uninstalled as far as PATH is concerned.
 * @param layout - tools layout.
 * @param id - tool id.
 * @param version - installed version to activate.
 * @throws when the id is unusable or the version is not installed.
 */
export async function setActiveVersion(
  layout: ToolsLayout,
  id: string,
  version: string,
  tools: readonly Tool[] = [],
): Promise<void> {
  if (!isValidToolId(id)) throw new Error(`toolchain: refusing unusable tool id ${JSON.stringify(id)}`)
  try {
    if (!(await stat(layout.versionDir(id, version))).isDirectory()) throw new Error('not a directory')
  } catch {
    throw new Error(`toolchain: ${id} ${version} is not installed`)
  }
  const link = layout.currentLink(id)
  await mkdir(join(layout.root, 'current'), { recursive: true, mode: 0o755 })
  const staging = `${link}.tmp`
  await rm(staging, { force: true })
  await symlink(layout.versionDir(id, version), staging)
  await rename(staging, link)
  await reconcileBinLinks(layout, tools)
}

/**
 * Choose the version that takes over when the active one is removed.
 *
 * The index's recommended version wins when it is still installed: a user who installed
 * the recommendation expects to return to it. Otherwise the highest version remains.
 * Taking the alphabetical last, as an earlier implementation did, selected `8u504` over
 * `21.0.12.1` and silently swapped PATH's `java` for an older build.
 * @param catalogTool - the tool from the index, when it is still listed.
 * @param remaining - installed versions left after the removal.
 * @returns the version to activate, or undefined when none remain.
 */
export function fallbackVersion(catalogTool: Tool | undefined, remaining: readonly string[]): string | undefined {
  if (remaining.length === 0) return undefined
  const recommended = catalogTool?.versions[0]?.version
  if (recommended !== undefined && remaining.includes(recommended)) return recommended
  let best = remaining[0]
  for (const version of remaining) {
    if (best !== undefined && compareVersions(version, best) > 0) best = version
  }
  return best
}

/**
 * Stand a tool down so whatever the container provides takes over again.
 *
 * Removing the `current` link is what makes this work: the tools `bin/` entry is rebuilt
 * from those links, so with the link gone the command resolves to the copy the image
 * ships. The installed directories are kept — this is "stop using the market copy", not
 * "uninstall it", and discarding a 300 MB toolchain because the user wanted to compare
 * against the bundled build would be a destructive answer to a reversible wish.
 * @param layout - tools layout.
 * @param id - tool id.
 * @param tools - the catalog, so the rebuilt entry points keep their declarations.
 * @throws when the id is unusable.
 */
export async function deactivateVersion(
  layout: ToolsLayout,
  id: string,
  tools: readonly Tool[] = [],
): Promise<void> {
  if (!isValidToolId(id)) throw new Error(`toolchain: refusing unusable tool id ${JSON.stringify(id)}`)
  // Idempotent: a tool that is already inactive has nothing to remove, and the entry
  // points are still rebuilt so a stale link left by an interrupted run gets cleaned.
  await rm(layout.currentLink(id), { force: true })
  await reconcileBinLinks(layout, tools)
}

/**
 * Uninstall one version, promoting a successor when the removed one was active.
 * @param layout - tools layout.
 * @param id - tool id.
 * @param version - version to remove.
 * @param catalogTool - the tool from the index, used to pick a successor.
 * @throws when the id is unusable or the version is not installed.
 */
export async function uninstallVersion(
  layout: ToolsLayout,
  id: string,
  version: string,
  catalogTool: Tool | undefined,
  tools: readonly Tool[] = [],
): Promise<void> {
  if (!isValidToolId(id)) throw new Error(`toolchain: refusing unusable tool id ${JSON.stringify(id)}`)
  const target = layout.versionDir(id, version)
  try {
    if (!(await stat(target)).isDirectory()) throw new Error('not a directory')
  } catch {
    throw new Error(`toolchain: ${id} ${version} is not installed`)
  }
  const active = await activeVersion(layout, id)
  // Drop the link before the directory: a link pointing at a removed tree is a broken
  // tool on PATH for as long as the next activation takes.
  if (active === version) await rm(layout.currentLink(id), { force: true })
  await rm(target, { recursive: true, force: true })
  if (active === version) {
    const remaining = await listVersions(layout, id)
    const next = fallbackVersion(catalogTool, remaining)
    if (next !== undefined) await setActiveVersion(layout, id, next, tools)
  }
  await reconcileBinLinks(layout, tools)
}

/**
 * Candidate directories that may hold a tool's executables, in preference order.
 *
 * A version that declares `bin_dirs` uses exactly those (Rust splits `cargo/bin` from
 * `rustc/bin`). Otherwise `bin/` is the convention, and a single-file distribution puts
 * its executable at the root.
 * @param root - installed version directory.
 * @param version - the version entry from the index, when known.
 * @returns existing directories to scan.
 */
async function executableDirectories(root: string, version: ToolVersion | undefined): Promise<string[]> {
  const declared = version?.bin_dirs ?? []
  if (declared.length > 0) {
    const usable = declared.filter(isLocalRelative)
    return usable.map(rel => join(root, rel))
  }
  const bin = join(root, 'bin')
  try {
    if ((await stat(bin)).isDirectory()) return [bin]
  } catch {
    // No bin/: fall through to the single-file layout.
  }
  return [root]
}

/**
 * Link every executable in one directory into `bin/`.
 *
 * `names` maps an archive file name to the command name to expose; an empty target hides
 * that file, which is how distribution install scripts stay out of PATH.
 * @param source - directory to scan.
 * @param linkDir - shared `bin/` directory.
 * @param seen - command names written so far, mutated.
 * @param names - command name overrides from the index.
 * @param problems - collection for skipped entries, mutated.
 */
async function linkExecutables(
  source: string,
  linkDir: string,
  seen: Set<string>,
  names: Readonly<Record<string, string>> | undefined,
  problems: string[],
): Promise<void> {
  let entries: string[]
  try { entries = await readdir(source) } catch { return }
  for (const file of entries) {
    const path = join(source, file)
    let mode: number
    try {
      const info = await stat(path)
      if (info.isDirectory()) continue
      mode = info.mode
    } catch { continue }
    if ((mode & 0o111) === 0) continue
    const override = names?.[file]
    if (override === '') continue
    const name = override ?? file
    if (!isSafeCommandName(name)) {
      // Skipped, not fatal: the rest of the directory is still trustworthy.
      problems.push(`toolchain: command name ${JSON.stringify(name)} escapes the bin directory`)
      continue
    }
    const link = join(linkDir, name)
    await rm(link, { force: true })
    try {
      await symlink(path, link)
      seen.add(name)
    } catch (error) {
      problems.push(`toolchain: could not link ${name}: ${error instanceof Error ? error.message : String(error)}`)
    }
  }
}

/**
 * Remove symlinks in a directory that this pass did not create.
 *
 * Every unreferenced link goes, whether or not its target still exists: a link whose
 * tool was uninstalled is broken on PATH, and one whose tool is still installed but no
 * longer exposes that command would otherwise keep a stale command reachable. A real
 * file is never removed — only links this module owns.
 * @param dir - directory to clean.
 * @param seen - names to keep.
 */
async function cleanStaleLinks(dir: string, seen: ReadonlySet<string>): Promise<void> {
  let entries: string[]
  try { entries = await readdir(dir) } catch { return }
  for (const name of entries) {
    if (seen.has(name)) continue
    const path = join(dir, name)
    try {
      if ((await lstat(path)).isSymbolicLink()) await rm(path, { force: true })
    } catch {
      // A link that vanished mid-scan needs no cleanup.
    }
  }
}

/**
 * Rebuild `bin/` and `lib/` from the currently active versions.
 *
 * Called after every activation change and once at startup, so a reinstall, an upgrade,
 * or a home directory that moved all repair themselves. A tool whose `current` link
 * points outside the tools root is skipped with a problem reported rather than linked:
 * this directory is on PATH, so an escaping target would put arbitrary files on it.
 * @param layout - tools layout.
 * @param tools - the catalog, for each version's declared directories and names.
 * @returns the commands linked and the problems skipped.
 */
export async function reconcileBinLinks(
  layout: ToolsLayout,
  tools: readonly Tool[] = [],
): Promise<ReconcileResult> {
  const problems: string[] = []
  const binDir = join(layout.root, 'bin')
  const libDir = join(layout.root, 'lib')
  await mkdir(binDir, { recursive: true, mode: 0o755 })
  await mkdir(libDir, { recursive: true, mode: 0o755 })
  // A missing `current/` means nothing is installed — but `bin/` may still hold links
  // from an earlier state, so the pass continues and cleans them instead of returning.
  let current: string[] = []
  try {
    current = await readdir(join(layout.root, 'current'))
  } catch {
    current = []
  }
  const seenCommands = new Set<string>()
  const seenLibs = new Set<string>()
  const byId = new Map(tools.map(tool => [tool.id, tool]))
  for (const id of current) {
    const root = await resolveActiveRoot(layout, id)
    if (root === undefined) continue
    if (!isWithin(layout.root, root)) {
      problems.push(`toolchain: current/${id} points outside the tools root`)
      continue
    }
    const tool = byId.get(id)
    const version = tool?.versions.find(entry => entry.version === idVersion(root, id))
    for (const directory of await executableDirectories(root, version)) {
      if (!isWithin(root, directory)) {
        problems.push(`toolchain: ${id} declares a bin directory outside its root`)
        continue
      }
      await linkExecutables(directory, binDir, seenCommands, version?.bin_names, problems)
    }
    await bindLibraryDirectory(root, id, libDir, version, seenLibs, problems)
  }
  await cleanStaleLinks(binDir, seenCommands)
  await cleanStaleLinks(libDir, seenLibs)
  return { commands: [...seenCommands].sort(), problems }
}

/**
 * Resolve the real directory behind a tool's `current` link.
 * @param layout - tools layout.
 * @param id - tool id.
 * @returns absolute target path, or undefined when the link is missing or unreadable.
 */
async function resolveActiveRoot(layout: ToolsLayout, id: string): Promise<string | undefined> {
  try {
    const target = await readlink(layout.currentLink(id))
    return isAbsolute(target) ? target : join(layout.root, 'current', target)
  } catch {
    return undefined
  }
}

/**
 * Read the version label out of an installed directory name.
 * @param root - install directory path.
 * @param id - tool id.
 * @returns the version portion, or empty when the name does not carry the id.
 */
function idVersion(root: string, id: string): string {
  const base = root.slice(root.lastIndexOf('/') + 1)
  const prefix = `${id}-`
  return base.startsWith(prefix) ? base.slice(prefix.length) : ''
}

/**
 * Point `<tools>/lib/<id>` at the active version's library directory.
 *
 * A declared `lib_rel` wins; without one the conventional `lib64`/`lib` are probed.
 * A declaration that escapes the tool root is a malformed index: it reports a problem
 * instead of falling back, because rebinding silently would hide the broken entry.
 * @param root - installed version directory.
 * @param id - tool id.
 * @param libDir - shared `lib/` directory.
 * @param version - the version entry from the index, when known.
 * @param seen - library bindings written so far, mutated.
 * @param problems - collection for skipped entries, mutated.
 */
async function bindLibraryDirectory(
  root: string,
  id: string,
  libDir: string,
  version: ToolVersion | undefined,
  seen: Set<string>,
  problems: string[],
): Promise<void> {
  const declared = version?.lib_rel
  if (declared !== undefined && declared !== '' && !isLocalRelative(declared)) {
    problems.push(`toolchain: ${id} declares a library directory outside its root`)
    return
  }
  const candidates = declared === undefined || declared === ''
    ? ['lib64', 'lib']
    : [declared, 'lib64', 'lib']
  for (const candidate of candidates) {
    const path = join(root, candidate)
    try {
      if (!(await stat(path)).isDirectory()) continue
    } catch {
      continue
    }
    const link = join(libDir, id)
    await rm(link, { force: true })
    await symlink(path, link)
    seen.add(id)
    return
  }
}

/**
 * Make the shared entry point directories usable and report the current state.
 * @param root - tools directory.
 * @returns the layout and the reconcile result.
 */
export async function prepareToolsRoot(
  root: string,
  tools: readonly Tool[] = [],
): Promise<{ layout: ToolsLayout; result: ReconcileResult }> {
  const layout = toolsLayout(root)
  await mkdir(layout.root, { recursive: true, mode: 0o755 })
  await chmod(layout.root, 0o755).catch(() => undefined)
  return { layout, result: await reconcileBinLinks(layout, tools) }
}

/**
 * Place the tools `bin/` first on a PATH value.
 *
 * First rather than last: the point of installing a tool is that the command resolves to
 * it, and a copy already on PATH (a distribution package, a leftover shim) would
 * otherwise win. The call is idempotent so a repeated prepare does not stack entries.
 * @param path - current PATH value.
 * @param binDir - tools entry point directory.
 * @returns the PATH value with that directory first.
 */
export function withToolsPath(path: string | undefined, binDir: string): string {
  const existing = path === undefined || path === '' ? '' : path
  const parts = existing === '' ? [] : existing.split(':').filter(part => part !== '' && part !== binDir)
  return [binDir, ...parts].join(':')
}

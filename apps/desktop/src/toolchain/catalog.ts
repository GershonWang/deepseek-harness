/**
 * Toolchain catalog: the shipped tool definitions, their versions, and the on-disk
 * installation layout.
 *
 * The index is data, not code: the bundled copy is the offline fallback and a remote
 * copy replaces it at runtime, so a new tool or category does not wait for a release.
 * Category labels travel with the index for the same reason.
 *
 * Installation layout under the tools directory:
 * ```
 * <dir>/<id>-<version>/     one installed version
 * <dir>/current/<id>        symlink to the active version
 * <dir>/cache/              downloaded archives
 * ```
 *
 * @module @deepseek-ai/dsh-desktop/toolchain/catalog
 */

import { readdir, readFile, readlink, stat } from 'node:fs/promises'
import { join } from 'node:path'
import { compareVersions, featureLine, sortVersionsDesc } from './version.ts'

/** One installable version of a tool. */
export interface ToolVersion {
  /** Version label exactly as published. */
  readonly version: string
  /**
   * Feature line this version belongs to, as declared by the index.
   *
   * Declared rather than derived because no automatic split is universal; see
   * {@link featureLine}.
   */
  readonly major?: string
  /** Archive URL. */
  readonly url: string
  /** Expected SHA-256 of the archive, lowercase hex. */
  readonly sha256: string
  /** Declared archive size in bytes, when the index carries one. */
  readonly size?: number
  /** Directory holding executables, relative to the extracted root. */
  readonly bin_rel?: string
  /** Directory holding shared libraries, relative to the extracted root. */
  readonly lib_rel?: string
  /** Several executable directories, used when a distribution splits them. */
  readonly bin_dirs?: readonly string[]
  /**
   * Command names that override the archive's own file names.
   *
   * A key is the file name inside the archive and the value is the command name to
   * expose. An empty value hides that file: distributions ship install and helper
   * scripts with the executable bit, and the default layout probe would otherwise
   * link them into `bin/` as stray commands.
   */
  readonly bin_names?: Readonly<Record<string, string>>
}

/** One installable tool. */
export interface Tool {
  /** Stable id, also the install directory and symlink name. */
  readonly id: string
  /** Display name. */
  readonly name: string
  /** Category id; the label comes from the index. */
  readonly category: string
  /** Short description. */
  readonly description: string
  /** Command names this tool provides, used for conflict reporting. */
  readonly provides: readonly string[]
  /** Tool ids this one depends on, one level deep. */
  readonly dependencies: readonly string[]
  /** Available versions; the first is the recommended one. */
  readonly versions: readonly ToolVersion[]
}

/** The parsed index document. */
export interface ToolIndex {
  /** Index schema version. */
  readonly version: number
  /** When the index was produced. */
  readonly updated_at?: string
  /** Tools in display order. */
  readonly tools: readonly Tool[]
  /** Category id to localized label; absent for older indexes. */
  readonly category_labels?: Readonly<Record<string, string>>
}

/** Installed versions grouped under one feature line, for the card's version control. */
export interface VersionGroup {
  /** Feature line id. */
  readonly major: string
  /** Newest version available on this line. */
  latest: string
  /** Small versions offered on this line, newest first. */
  versions: readonly string[]
  /** Installed versions on this line, newest first. */
  installed: readonly string[]
  /** Active version when it belongs to this line, otherwise empty. */
  active: string
}

/**
 * Validate and normalize a parsed index document.
 *
 * The index is external data, so this is a wire boundary: a malformed document must
 * fail here rather than surface as an unreadable card or a path built from a hostile
 * id. {@link isValidToolId} guards ids because they become directory names.
 * @param value - parsed JSON value.
 * @returns the index.
 * @throws when the document is not a well-formed tool index.
 */
export function parseToolIndex(value: unknown): ToolIndex {
  if (!record(value)) throw new Error('toolchain: index must be a JSON object')
  const rawTools = value.tools
  if (!Array.isArray(rawTools)) throw new Error('toolchain: index.tools must be a list')
  const tools = rawTools.map((entry, position) => parseTool(entry, position))
  const seen = new Set<string>()
  for (const tool of tools) {
    if (seen.has(tool.id)) throw new Error(`toolchain: duplicate tool id ${JSON.stringify(tool.id)}`)
    seen.add(tool.id)
  }
  const labels = value.category_labels
  return {
    version: typeof value.version === 'number' ? value.version : 1,
    ...(typeof value.updated_at === 'string' ? { updated_at: value.updated_at } : {}),
    tools,
    ...(record(labels) ? { category_labels: labels as Record<string, string> } : {}),
  }
}

/**
 * Parse one tool entry.
 * @param value - candidate tool object.
 * @param position - its index in the list, for diagnostics.
 * @returns the validated tool.
 * @throws when a required field is missing or a version entry is malformed.
 */
function parseTool(value: unknown, position: number): Tool {
  if (!record(value)) throw new Error(`toolchain: tools[${String(position)}] must be an object`)
  const id = value.id
  if (typeof id !== 'string' || !isValidToolId(id)) {
    throw new Error(`toolchain: tools[${String(position)}].id is not a usable directory name`)
  }
  const rawVersions = value.versions
  if (!Array.isArray(rawVersions) || rawVersions.length === 0) {
    throw new Error(`toolchain: tool ${JSON.stringify(id)} declares no versions`)
  }
  return {
    id,
    name: typeof value.name === 'string' && value.name !== '' ? value.name : id,
    category: typeof value.category === 'string' ? value.category : 'other',
    description: typeof value.description === 'string' ? value.description : '',
    provides: stringList(value.provides),
    dependencies: stringList(value.dependencies),
    versions: rawVersions.map((entry, index) => parseVersion(entry, id, index)),
  }
}

/**
 * Parse one version entry.
 * @param value - candidate version object.
 * @param id - owning tool id, for diagnostics.
 * @param position - its index in the version list.
 * @returns the validated version.
 * @throws when the version, URL, or hash is missing.
 */
function parseVersion(value: unknown, id: string, position: number): ToolVersion {
  if (!record(value)) throw new Error(`toolchain: ${id}.versions[${String(position)}] must be an object`)
  const version = value.version
  const url = value.url
  const sha256 = value.sha256
  if (typeof version !== 'string' || version === '') throw new Error(`toolchain: ${id} has a version without a label`)
  if (typeof url !== 'string' || url === '') throw new Error(`toolchain: ${id} ${version} has no url`)
  if (typeof sha256 !== 'string' || sha256 === '') throw new Error(`toolchain: ${id} ${version} has no sha256`)
  return {
    version,
    url,
    sha256: sha256.toLowerCase(),
    ...(typeof value.major === 'string' && value.major !== '' ? { major: value.major } : {}),
    ...(typeof value.size === 'number' && Number.isSafeInteger(value.size) && value.size > 0 ? { size: value.size } : {}),
    ...(typeof value.bin_rel === 'string' ? { bin_rel: value.bin_rel } : {}),
    ...(typeof value.lib_rel === 'string' ? { lib_rel: value.lib_rel } : {}),
    ...(Array.isArray(value.bin_dirs) ? { bin_dirs: stringList(value.bin_dirs) } : {}),
    ...(record(value.bin_names) ? { bin_names: value.bin_names as Record<string, string> } : {}),
  }
}

/**
 * Whether an id can safely become a directory name.
 *
 * Ids arrive from the remote index and from a project file, then reach
 * `remove`/`rmdir` through the version directory path. An id containing a path
 * separator or `..` would delete files outside the tools directory. Callers already
 * compare ids against the catalog, but the deletion path must not depend on every call
 * site remembering that.
 * @param id - candidate id.
 * @returns whether the id is a local, non-empty name.
 */
export function isValidToolId(id: string): boolean {
  if (id === '' || id === '.' || id === '..') return false
  if (id.includes('/') || id.includes('\\')) return false
  return true
}

/** @param value - value to test. @returns whether it is a plain object. */
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/** @param value - value to read. @returns its string members, ignoring anything else. */
function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
}

/** Resolved directory layout of one tools root. */
export interface ToolsLayout {
  /** Root holding installed versions. */
  readonly root: string
  /** Directory of one installed version. */
  readonly versionDir: (id: string, version: string) => string
  /** Symlink naming the active version of one tool. */
  readonly currentLink: (id: string) => string
  /** Download cache. */
  readonly cache: string
}

/**
 * Describe the on-disk layout under a tools root.
 * @param root - tools directory, for example `~/.dsh-tools`.
 * @returns the layout helpers.
 */
export function toolsLayout(root: string): ToolsLayout {
  return {
    root,
    versionDir: (id, version) => join(root, `${id}-${version}`),
    currentLink: id => join(root, 'current', id),
    cache: join(root, 'cache'),
  }
}

/**
 * List the tools that currently have an active version.
 * @param layout - tools layout.
 * @returns ids in order; empty when nothing is installed.
 */
export async function listInstalled(layout: ToolsLayout): Promise<string[]> {
  try {
    const entries = await readdir(join(layout.root, 'current'))
    return entries.sort()
  } catch {
    return []
  }
}

/**
 * List every installed version of one tool.
 * @param layout - tools layout.
 * @param id - tool id.
 * @returns version labels in ascending order.
 */
export async function listVersions(layout: ToolsLayout, id: string): Promise<string[]> {
  const prefix = `${id}-`
  let entries: string[]
  try {
    entries = await readdir(layout.root)
  } catch {
    return []
  }
  return entries
    .filter(name => name.length > prefix.length && name.startsWith(prefix))
    .map(name => name.slice(prefix.length))
    .sort()
}

/**
 * Read the active version of one tool.
 * @param layout - tools layout.
 * @param id - tool id.
 * @returns the active version label, or empty when the tool is not active.
 */
export async function activeVersion(layout: ToolsLayout, id: string): Promise<string> {
  let target: string
  try {
    target = await readlink(layout.currentLink(id))
  } catch {
    return ''
  }
  const base = target.slice(target.lastIndexOf('/') + 1)
  const prefix = `${id}-`
  return base.length > prefix.length && base.startsWith(prefix) ? base.slice(prefix.length) : ''
}

/**
 * Whether one version directory exists.
 * @param layout - tools layout.
 * @param id - tool id.
 * @param version - version label.
 * @returns whether the version is installed.
 */
export async function isInstalled(layout: ToolsLayout, id: string, version: string): Promise<boolean> {
  try {
    return (await stat(layout.versionDir(id, version))).isDirectory()
  } catch {
    return false
  }
}

/**
 * Group a tool's versions into the feature lines a card offers.
 *
 * Installed versions the index no longer lists keep a line of their own: dropping them
 * would leave the user unable to see, switch back to, or uninstall a copy that still
 * occupies disk, which is harder to explain than an extra group.
 * @param tool - the tool.
 * @param installed - installed versions of this tool.
 * @param active - currently active version, empty when none.
 * @returns groups ordered newest line first.
 */
export function buildVersionGroups(tool: Tool, installed: readonly string[], active: string): VersionGroup[] {
  const byLine = new Map<string, { major: string; latest: string; installed: string[] }>()
  const order: string[] = []
  const ensure = (major: string): { major: string; latest: string; installed: string[] } => {
    const existing = byLine.get(major)
    if (existing !== undefined) return existing
    const created = { major, latest: '', installed: [] as string[] }
    byLine.set(major, created)
    order.push(major)
    return created
  }
  // Record the line each listed version belongs to, so an installed version is grouped
  // by the index's own declaration instead of the fallback rule: `0.26.1` falls back to
  // "0" while the index declares "0.26", and splitting one line under two rules would
  // invent a version menu for a single-line tool.
  const declared = new Map<string, string>()
  for (const version of tool.versions) {
    const group = ensure(featureLine(version.major, version.version))
    declared.set(version.version, group.major)
    if (group.latest === '' || compareVersions(version.version, group.latest) > 0) group.latest = version.version
  }
  for (const version of installed) {
    const major = declared.get(version) ?? featureLine(undefined, version)
    const group = ensure(major)
    group.installed.push(version)
    // An orphan version is the only visible member of its line.
    if (group.latest === '') group.latest = version
  }
  const groups: VersionGroup[] = []
  for (const major of order) {
    const group = byLine.get(major)
    if (group === undefined) continue
    if (group.installed.length > 1) sortVersionsDesc(group.installed)
    groups.push({
      major: group.major,
      latest: group.latest,
      versions: mergeVersions(group.latest, group.installed),
      installed: group.installed,
      active: group.installed.includes(active) ? active : '',
    })
  }
  // Newest line first, so the menu's "earlier is newer" matches the patch ordering.
  groups.sort((left, right) => compareVersions(right.latest, left.latest))
  return groups
}

/**
 * Merge one line's newest version with its installed versions, newest first, deduplicated.
 *
 * Older patches on the same line are not listed: they differ only by patch level and
 * would bury the version the user actually chooses between.
 * @param latest - newest version on the line.
 * @param installed - installed versions on the line.
 * @returns the small versions to offer.
 */
function mergeVersions(latest: string, installed: readonly string[]): string[] {
  const out = latest === '' ? [] : [latest]
  for (const version of installed) {
    if (version !== latest && !out.includes(version)) out.push(version)
  }
  sortVersionsDesc(out)
  return out
}

/**
 * Read and parse one index file.
 * @param path - absolute path of the index document.
 * @returns the parsed index.
 * @throws when the file cannot be read or does not parse.
 */
export async function readToolIndex(path: string): Promise<ToolIndex> {
  return parseToolIndex(JSON.parse(await readFile(path, 'utf8')))
}

/**
 * Format a byte count for a card's size line.
 * @param bytes - size in bytes.
 * @returns a short human-readable size, or empty when the size is unknown.
 */
export function formatBytes(bytes: number | undefined): string {
  if (bytes === undefined || bytes <= 0) return ''
  const units = ['B', 'KB', 'MB', 'GB'] as const
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit += 1 }
  const rounded = value >= 10 || unit === 0 ? Math.round(value) : Math.round(value * 10) / 10
  return `${String(rounded)} ${units[unit]}`
}

/**
 * One-time import of a legacy Web-client profile into the Electron Desktop profile.
 *
 * The Web client runs the `web` profile; the Electron shell runs the reserved
 * `desktop` profile. Settings live in a profile's `cordis.patch.yml` and installed
 * plugins live in its `package.json` plus `node_modules`, so switching clients
 * changes which directory the harness reads: none of that configuration follows
 * the user across. Session history, workspaces, attachments, skills, and
 * credentials stay under the shared Harness home and are unaffected.
 *
 * This module owns the whole decision and execution path and never touches
 * Electron APIs, so its behavior is unit-testable. Three rules govern it:
 * - The legacy profile is read-only. Nothing here writes to, moves, or deletes it.
 * - Every write is preceded by a full backup of the destination.
 * - Any failure restores the destination, leaving the Desktop profile as it was.
 *
 * @module @deepseek-ai/dsh-desktop/profile-migration
 */

import { cp, mkdir, readdir, readFile, rm, stat, writeFile } from 'node:fs/promises'
import { basename, dirname, join } from 'node:path'
import { PROFILE_TEMPLATES } from '@deepseek-ai/dsh-official-bundles'
import { loadOverlayPatches, readProfileManifest } from '@deepseek-ai/dsh-app-boot'
import { PROFILE_PATCH_FILENAME } from '@deepseek-ai/dsh-app-boot'

/** Marker file recording a completed import, so the prompt does not repeat. */
export const MIGRATION_MARKER = '.dsh-migrated-from-web'

/** Profile name the Web client runs. */
const LEGACY_PROFILE_NAME = 'web'

/** Profile name the Electron shell reserves. */
const DESKTOP_PROFILE_NAME = 'desktop'

/** Profile manifest filename. */
const MANIFEST_FILENAME = 'package.json'

/** User-granted plugin version exemptions, carried over so they need not be re-granted. */
const COMPATIBILITY_FILENAME = 'compatibility.json'

/** One patch entry identified by its loader id. */
export interface PatchEntry {
  /** Loader entry id this patch targets or inserts. */
  readonly id: string
  /** Serialized form for display and equality comparison. */
  readonly serialized: string
}

/** How one loader id differs between the two profiles. */
export type PatchConflict =
  /** Present in only one profile; copied without asking. */
  | { readonly kind: 'single'; readonly id: string; readonly source: PatchEntry }
  /** Present in both with identical meaning; one copy is kept silently. */
  | { readonly kind: 'identical'; readonly id: string }
  /** Present in both with different meaning; the user chooses. */
  | { readonly kind: 'differing'; readonly id: string; readonly legacy: PatchEntry; readonly desktop: PatchEntry }

/** An installed plugin offered for import. */
export interface PluginCandidate {
  /** Package name. */
  readonly name: string
  /** Version range recorded in the manifest. */
  readonly spec: string
  /** Whether the legacy profile lists it as an enabled bundle. */
  readonly enabled: boolean
}

/** What the legacy profile offers to import. */
export interface MigrationSource {
  /** Absolute legacy profile directory. */
  readonly dir: string
  /** Patch entries keyed by loader id. */
  readonly patches: readonly PatchEntry[]
  /** Enabled bundle list, in order. */
  readonly bundles: readonly string[]
  /** Installed dependencies. */
  readonly dependencies: Readonly<Record<string, string>>
  /** Plugins the user may choose, one per dependency that is also a bundle. */
  readonly plugins: readonly PluginCandidate[]
  /** Whether the profile carries version exemptions worth copying. */
  readonly hasCompatibility: boolean
}

/** Why a migration cannot start. */
export interface MigrationBlocker {
  /** Stable discriminator for localized presentation. */
  readonly reason: 'no-legacy' | 'already-migrated'
  /** Legacy profile directory inspected, when one exists. */
  readonly dir?: string
}

/** The user's choices, resolved from the dialog. */
export interface MigrationSelection {
  /** Import the application settings (patch entries). */
  readonly settings: boolean
  /** Plugin names to import; ignored when `settings` is false. */
  readonly plugins: readonly string[]
  /** Winning patch entry per differing id: the legacy copy or the desktop copy. */
  readonly conflicts: Readonly<Record<string, 'legacy' | 'desktop'>>
}

/** Result of a completed import. */
export interface MigrationOutcome {
  /** Entries imported from the legacy profile. */
  readonly importedPatches: number
  /** Plugins imported. */
  readonly importedPlugins: number
  /** Backup directory of the destination, or undefined when nothing existed to back up. */
  readonly backup: string | undefined
}

/**
 * Resolve the two profiles this module operates on.
 * @param profilesRoot - directory holding every profile.
 * @returns the legacy and desktop profile directories.
 */
export function profileLocations(profilesRoot: string): { legacy: string; desktop: string } {
  return {
    legacy: join(profilesRoot, LEGACY_PROFILE_NAME),
    desktop: join(profilesRoot, DESKTOP_PROFILE_NAME),
  }
}

/** Whether a directory exists. @param path - path to test. @returns whether it exists. */
async function exists(path: string): Promise<boolean> {
  try {
    await stat(path)
    return true
  } catch {
    return false
  }
}

/**
 * Read one profile's patch entries, keyed by loader id.
 *
 * Entries without an id (a bare `insert` list) cannot be addressed by id, so they
 * are keyed by a positional marker that keeps them distinct and in order.
 * @param binName - diagnostic prefix for malformed patches.
 * @param dir - profile directory.
 * @returns patch entries in file order.
 */
async function readPatchEntries(binName: string, dir: string): Promise<PatchEntry[]> {
  const path = join(dir, PROFILE_PATCH_FILENAME)
  if (!(await exists(path))) return []
  const patches = loadOverlayPatches(binName, path)
  return patches.map((patch, index) => {
    const id = typeof patch.id === 'string' && patch.id !== '' ? patch.id : `#insert-${index}`
    return { id, serialized: JSON.stringify(patch) }
  })
}

/**
 * Read a profile's patch entries for conflict presentation.
 *
 * The prompt must compare against what the destination actually holds; without
 * this the conflict list stays empty and every conflicting entry falls back to a
 * silent default. Exported so the caller can build the view from real content
 * rather than re-implementing patch parsing.
 * @param binName - diagnostic prefix for malformed patches.
 * @param dir - profile directory to read.
 * @returns patch entries in file order, or an empty list when the profile has no patch.
 */
export async function readProfilePatchEntries(binName: string, dir: string): Promise<PatchEntry[]> {
  return readPatchEntries(binName, dir)
}

/**
 * Classify how each loader id differs between the legacy and desktop profiles.
 *
 * An id present on both sides with identical serialized meaning is reported as
 * `identical` rather than `differing`, so formatting-only or duplicate copies do
 * not make the user adjudicate a conflict that does not exist.
 * @param legacy - legacy patch entries.
 * @param desktop - desktop patch entries.
 * @returns one classification per distinct id, legacy order first.
 */
export function classifyPatches(legacy: readonly PatchEntry[], desktop: readonly PatchEntry[]): PatchConflict[] {
  const desktopById = new Map(desktop.map(entry => [entry.id, entry]))
  const seen = new Set<string>()
  const conflicts: PatchConflict[] = []
  for (const entry of legacy) {
    if (seen.has(entry.id)) continue
    seen.add(entry.id)
    const counterpart = desktopById.get(entry.id)
    if (counterpart === undefined) {
      conflicts.push({ kind: 'single', id: entry.id, source: entry })
      continue
    }
    conflicts.push(counterpart.serialized === entry.serialized
      ? { kind: 'identical', id: entry.id }
      : { kind: 'differing', id: entry.id, legacy: entry, desktop: counterpart })
  }
  for (const entry of desktop) {
    if (seen.has(entry.id)) continue
    seen.add(entry.id)
  }
  return conflicts
}

/**
 * Inspect the legacy profile and report what could be imported.
 *
 * @param binName - diagnostic prefix for malformed manifests.
 * @param legacyDir - the legacy profile directory.
 * @returns the importable content, or undefined when the directory holds no profile.
 */
export async function readMigrationSource(binName: string, legacyDir: string): Promise<MigrationSource | undefined> {
  if (!(await exists(join(legacyDir, MANIFEST_FILENAME)))) return undefined
  const manifest = readProfileManifest(binName, legacyDir)
  const bundles = manifest.dsh?.profile?.bundles ?? []
  const dependencies = manifest.dependencies ?? {}
  const plugins = Object.entries(dependencies).map(([name, spec]) => ({
    name,
    spec,
    enabled: (bundles as readonly string[]).includes(name),
  }))
  return {
    dir: legacyDir,
    patches: await readPatchEntries(binName, legacyDir),
    bundles: [...bundles],
    dependencies: { ...dependencies },
    plugins,
    hasCompatibility: await exists(join(legacyDir, COMPATIBILITY_FILENAME)),
  }
}

/**
 * Bundles the Electron shell writes into a fresh Desktop profile.
 *
 * The Desktop profile is application-owned and therefore has no entry in
 * `PROFILE_TEMPLATES`; `project-manager.ts` initializes it from the shipped `web`
 * template. This mirrors that template instead of inventing a second list.
 * @returns the bundle names a freshly initialized Desktop profile carries.
 */
function desktopTemplateBundles(): readonly string[] {
  return PROFILE_TEMPLATES[LEGACY_PROFILE_NAME]?.bundles ?? []
}

/**
 * Whether the desktop profile is still exactly what `initProfile` wrote.
 *
 * The manifest must declare no dependencies and the template's bundle list, the
 * patch must parse to an empty list, and no plugins may be installed. Any other
 * state means the user has configured the Desktop profile, and an import must
 * merge into it rather than replace it.
 * @param binName - diagnostic prefix for malformed manifests.
 * @param desktopDir - the desktop profile directory.
 * @returns whether the profile is untouched.
 */
export async function isPristineProfile(binName: string, desktopDir: string): Promise<boolean> {
  if (!(await exists(join(desktopDir, MANIFEST_FILENAME)))) return true
  const manifest = readProfileManifest(binName, desktopDir)
  if (Object.keys(manifest.dependencies ?? {}).length > 0) return false
  const expected = desktopTemplateBundles()
  const bundles = manifest.dsh?.profile?.bundles ?? []
  if (bundles.length !== expected.length || bundles.some((name, index) => name !== expected[index])) return false
  if ((await readPatchEntries(binName, desktopDir)).length > 0) return false
  const modules = join(desktopDir, 'node_modules')
  if (!(await exists(modules))) return true
  const installed = await readdir(modules).catch(() => [])
  return installed.filter(name => !name.startsWith('.')).length === 0
}

/**
 * Decide whether this launch should offer an import.
 *
 * @param binName - diagnostic prefix for malformed manifests.
 * @param profilesRoot - directory holding every profile.
 * @returns a blocker when no import applies, otherwise the source to offer.
 */
export async function detectMigration(
  binName: string,
  profilesRoot: string,
): Promise<{ blocker: MigrationBlocker } | { source: MigrationSource; pristine: boolean }> {
  const { legacy, desktop } = profileLocations(profilesRoot)
  const source = await readMigrationSource(binName, legacy)
  if (source === undefined) return { blocker: { reason: 'no-legacy' } }
  if (await exists(join(desktop, MIGRATION_MARKER))) {
    return { blocker: { reason: 'already-migrated', dir: legacy } }
  }
  return { source, pristine: await isPristineProfile(binName, desktop) }
}

/**
 * Compose the patch list to write into the desktop profile.
 *
 * The result preserves the desktop profile's own entries and applies the user's
 * choices: single-source entries are appended, identical entries keep the desktop
 * copy, and differing entries take whichever side the user selected.
 *
 * A differing entry with no explicit choice takes the legacy copy. The user asked
 * to import, so the source value is what carries their intent; defaulting to the
 * destination silently discarded edited configuration when the conflict list failed
 * to reach the user.
 * @param legacy - legacy patch entries.
 * @param desktop - desktop patch entries.
 * @param selection - the user's choices.
 * @returns serialized patch objects in write order.
 */
export function composeMergedPatches(
  legacy: readonly PatchEntry[],
  desktop: readonly PatchEntry[],
  selection: MigrationSelection,
): unknown[] {
  const desktopById = new Map(desktop.map(entry => [entry.id, entry]))
  const legacyById = new Map(legacy.map(entry => [entry.id, entry]))
  const conflicts = classifyPatches(legacy, desktop)
  const ordered: unknown[] = []
  const emitted = new Set<string>()
  for (const conflict of conflicts) {
    if (emitted.has(conflict.id)) continue
    emitted.add(conflict.id)
    if (conflict.kind === 'identical') {
      const kept = selection.settings ? legacyById.get(conflict.id) : desktopById.get(conflict.id)
      if (kept !== undefined) ordered.push(JSON.parse(kept.serialized))
      continue
    }
    if (conflict.kind === 'single') {
      if (!selection.settings) continue
      ordered.push(JSON.parse(conflict.source.serialized))
      continue
    }
    const winner = selection.conflicts[conflict.id] ?? 'legacy'
    ordered.push(JSON.parse((winner === 'legacy' ? conflict.legacy : conflict.desktop).serialized))
  }
  for (const entry of desktop) {
    if (emitted.has(entry.id)) continue
    emitted.add(entry.id)
    ordered.push(JSON.parse(entry.serialized))
  }
  return ordered
}

/**
 * Merge the selected plugins into the desktop manifest's bundle list and dependencies.
 * @param desktopManifest - current desktop manifest.
 * @param source - legacy source.
 * @param selection - the user's choices.
 * @returns the next manifest, or the current one when nothing changes.
 */
export function mergeManifest(
  desktopManifest: Record<string, unknown>,
  source: MigrationSource,
  selection: MigrationSelection,
): Record<string, unknown> {
  const chosen = new Set(selection.plugins)
  const dependencies: Record<string, string> = { ...(desktopManifest.dependencies as Record<string, string> | undefined) }
  for (const plugin of source.plugins) {
    if (chosen.has(plugin.name)) dependencies[plugin.name] = plugin.spec
  }
  const dsh = (desktopManifest.dsh ?? {}) as Record<string, unknown>
  const profile = (dsh.profile ?? {}) as Record<string, unknown>
  const bundles = [...((profile.bundles ?? []) as string[])]
  for (const plugin of source.plugins) {
    if (!chosen.has(plugin.name) || !plugin.enabled) continue
    if (!bundles.includes(plugin.name)) bundles.push(plugin.name)
  }
  return { ...desktopManifest, dependencies, dsh: { ...dsh, profile: { ...profile, bundles } } }
}

/**
 * Copy one directory tree, resolving nothing and preserving links as recorded.
 * @param from - source directory.
 * @param to - destination directory.
 */
async function copyTree(from: string, to: string): Promise<void> {
  await cp(from, to, { recursive: true, force: true })
}

/**
 * 将用户选择的旧版 profile 内容导入 Desktop profile。
 *
 * 目标清单不存在时，以随包 Web 模板的 bundles 为基础，再合并用户选择的插件；已有清单保持原有内容。目标存在时，写入前会完整备份；导入失败时先尝试恢复本次写入的路径。旧版 profile 始终只读。
 * @param options - 工具名称、profile 路径、旧版内容与用户选择。
 * @returns 本次导入结果及目标备份路径。
 * @throws 任一步骤失败时，先尝试恢复目标，再重新抛出原始错误。
 */
export async function applyMigration(options: {
  readonly binName: string
  readonly desktopDir: string
  readonly source: MigrationSource
  readonly selection: MigrationSelection
}): Promise<MigrationOutcome> {
  const { binName, desktopDir, source, selection } = options
  const stamp = new Date().toISOString().replaceAll(/[:.]/gu, '-')
  const backup = await exists(desktopDir) ? `${desktopDir}.bak-${stamp}` : undefined
  let importedPatches = 0
  let importedPlugins = 0
  /** Profile-relative paths this import creates or replaces, in write order. */
  const written: string[] = []
  try {
    if (backup !== undefined) await cp(desktopDir, backup, { recursive: true, force: true })
    await mkdir(desktopDir, { recursive: true, mode: 0o700 })

    const legacyPatches = source.patches
    const desktopPatches = await readPatchEntries(binName, desktopDir)
    const merged = composeMergedPatches(legacyPatches, desktopPatches, selection)
    const patchFile = join(desktopDir, PROFILE_PATCH_FILENAME)
    const yaml = (await import('js-yaml')).default.dump(merged, { lineWidth: -1, noRefs: true })
    await writeFile(patchFile, yaml, { mode: 0o600 })
    importedPatches = merged.length
    written.push(PROFILE_PATCH_FILENAME)

    const manifestFile = join(desktopDir, MANIFEST_FILENAME)
    const manifest = await exists(manifestFile)
      ? JSON.parse(await readFile(manifestFile, 'utf8')) as Record<string, unknown>
      : {
        name: `dsh-profile-${DESKTOP_PROFILE_NAME}`,
        private: true,
        dependencies: {},
        dsh: { profile: { bundles: [...desktopTemplateBundles()] } },
      }
    const next = mergeManifest(manifest, source, selection)
    await writeFile(manifestFile, `${JSON.stringify(next, undefined, 2)}\n`, { mode: 0o600 })
    written.push(MANIFEST_FILENAME)

    for (const plugin of source.plugins) {
      if (!selection.plugins.includes(plugin.name)) continue
      const from = join(source.dir, 'node_modules', plugin.name)
      if (!(await exists(from))) continue
      const relative = join('node_modules', plugin.name)
      await copyTree(from, join(desktopDir, relative))
      written.push(relative)
      importedPlugins += 1
    }

    if (source.hasCompatibility) {
      const compatibility = join(source.dir, COMPATIBILITY_FILENAME)
      await cp(compatibility, join(desktopDir, COMPATIBILITY_FILENAME), { force: true })
      written.push(COMPATIBILITY_FILENAME)
    }

    await writeFile(join(desktopDir, MIGRATION_MARKER),
      `${JSON.stringify({ from: basename(source.dir), at: new Date().toISOString(), importedPatches, importedPlugins }, undefined, 2)}\n`,
      { mode: 0o600 })
    return { importedPatches, importedPlugins, backup }
  } catch (error) {
    await restore(desktopDir, backup, written)
    throw error
  }
}

/**
 * Undo a failed import by restoring only the paths it wrote.
 *
 * The destination directory itself is never removed: it holds the profile lock the
 * caller still holds, plus files this import never touched (an installed plugin
 * tree, caches, the user's own additions). Replacing the whole directory also
 * discarded the completed-migration marker, which made every later launch offer the
 * same import again. A path the backup does not carry is removed instead, because it
 * did not exist before the import.
 * @param desktopDir - destination directory.
 * @param backup - backup of the directory, or undefined when there was nothing to back up.
 * @param written - profile-relative paths this import created or replaced.
 */
async function restore(desktopDir: string, backup: string | undefined, written: readonly string[]): Promise<void> {
  for (const relative of written) {
    const target = join(desktopDir, relative)
    try {
      const from = backup === undefined ? undefined : join(backup, relative)
      if (from !== undefined && (await exists(from))) {
        await rm(target, { recursive: true, force: true })
        await mkdir(dirname(target), { recursive: true })
        await cp(from, target, { recursive: true, force: true })
      } else {
        await rm(target, { recursive: true, force: true })
      }
    } catch {
      // A path that cannot be restored leaves the backup in place for manual recovery.
    }
  }
}

/**
 * Remove a completed migration's marker so the prompt can appear again.
 * @param profilesRoot - directory holding every profile.
 */
export async function clearMigrationMarker(profilesRoot: string): Promise<void> {
  const { desktop } = profileLocations(profilesRoot)
  await rm(join(desktop, MIGRATION_MARKER), { force: true })
}

/**
 * Directory a marker or backup sits beside, exposed for callers that report paths.
 * @param profileDir - a profile directory.
 * @returns its parent directory.
 */
export function profilesRootOf(profileDir: string): string {
  return dirname(profileDir)
}

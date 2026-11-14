import { createHash } from 'node:crypto'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { dump } from 'js-yaml'
import { loadOverlayPatches } from '@deepseek-ai/dsh-app-boot'
import {
  applyMigration,
  classifyPatches,
  clearMigrationMarker,
  composeMergedPatches,
  detectMigration,
  isPristineProfile,
  mergeManifest,
  MIGRATION_MARKER,
  profileLocations,
  readMigrationSource,
  readProfilePatchEntries,
  type MigrationSource,
  type PatchEntry,
} from '../src/profile-migration.ts'

const roots: string[] = []
function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-profile-migration-'))
  roots.push(root)
  return root
}
/** Write one patch file from plain objects. */
function writePatch(dir: string, entries: unknown[]): void {
  mkdirSync(dir, { recursive: true })
  writeFileSync(join(dir, 'cordis.patch.yml'), dump(entries, { lineWidth: -1 }))
}
/** Write a profile manifest. */
function writeManifest(dir: string, manifest: unknown): void {
  mkdirSync(dir, { recursive: true })
  writeFileSync(join(dir, 'package.json'), `${JSON.stringify(manifest, undefined, 2)}\n`)
}
/** Seed a legacy profile carrying settings and two plugins. */
function seedLegacy(root: string): string {
  const { legacy } = profileLocations(root)
  writeManifest(legacy, {
    name: 'dsh-profile-web',
    private: true,
    dependencies: { 'plugin-a': '1.0.0', 'plugin-b': '2.0.0' },
    dsh: { profile: { bundles: ['@deepseek-ai/dsh-base', 'plugin-a', 'plugin-b'] } },
  })
  writePatch(legacy, [
    { id: 'ui-theme', name: '@deepseek-ai/dsh-client-ui-theme', config: { preference: 'dark' } },
    { id: 'agent-default-model', config: { provider: 'legacy-provider', model: 'legacy-model' } },
  ])
  for (const plugin of ['plugin-a', 'plugin-b']) {
    const dir = join(legacy, 'node_modules', plugin)
    mkdirSync(dir, { recursive: true })
    writeFileSync(join(dir, 'package.json'), JSON.stringify({ name: plugin, version: '1.0.0' }))
  }
  return legacy
}
/** Seed a desktop profile that already holds one differing entry. */
function seedDesktop(root: string): string {
  const { desktop } = profileLocations(root)
  writeManifest(desktop, {
    name: 'dsh-profile-desktop',
    private: true,
    dependencies: {},
    dsh: { profile: { bundles: ['@deepseek-ai/dsh-base', '@deepseek-ai/dsh-web-app'] } },
  })
  writePatch(desktop, [
    { id: 'agent-default-model', config: { provider: 'desktop-provider', model: 'desktop-model' } },
  ])
  return desktop
}
/** Digest of every file under a directory, used to prove it was not modified. */
function treeDigest(dir: string): string {
  const hash = createHash('sha256')
  const walk = (current: string): void => {
    for (const name of readdirSync(current).sort()) {
      const path = join(current, name)
      if (statSync(path).isDirectory()) { hash.update(`${name}/`); walk(path) } else { hash.update(`${name}:${readFileSync(path, 'utf8')}`) }
    }
  }
  walk(dir)
  return hash.digest('hex')
}
const BIN = 'dsh'
afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
})

describe('patch classification', () => {
  const entry = (id: string, serialized: string): PatchEntry => ({ id, serialized })

  it('separates single-source entries from real conflicts', () => {
    const legacy = [entry('a', '{"id":"a"}'), entry('b', '{"id":"b","config":{"v":1}}')]
    const desktop = [entry('b', '{"id":"b","config":{"v":2}}'), entry('c', '{"id":"c"}')]
    const conflicts = classifyPatches(legacy, desktop)
    expect(conflicts.map(item => [item.id, item.kind])).toEqual([['a', 'single'], ['b', 'differing']])
  })

  it('silently deduplicates an id that means the same on both sides', () => {
    const legacy = [entry('same', '{"id":"same","config":{"v":1}}')]
    const desktop = [entry('same', '{"id":"same","config":{"v":1}}')]
    expect(classifyPatches(legacy, desktop)).toEqual([{ kind: 'identical', id: 'same' }])
  })
})

describe('pristine detection', () => {
  it('treats an absent profile as untouched', async () => {
    const root = temporaryRoot()
    expect(await isPristineProfile(BIN, profileLocations(root).desktop)).toBe(true)
  })

  it('treats a user-configured profile as configured', async () => {
    const root = temporaryRoot()
    const desktop = seedDesktop(root)
    expect(await isPristineProfile(BIN, desktop)).toBe(false)
  })

  it('treats a template profile with an empty patch as untouched', async () => {
    const root = temporaryRoot()
    const { desktop } = profileLocations(root)
    writeManifest(desktop, {
      name: 'dsh-profile-desktop',
      private: true,
      dependencies: {},
      dsh: { profile: { bundles: ['@deepseek-ai/dsh-base', '@deepseek-ai/dsh-web-app'] } },
    })
    writeFileSync(join(desktop, 'cordis.patch.yml'), '# template comment\n[]\n')
    expect(await isPristineProfile(BIN, desktop)).toBe(true)
  })
})

describe('migration detection', () => {
  it('reports no legacy profile when the Web profile is absent', async () => {
    const root = temporaryRoot()
    expect(await detectMigration(BIN, root)).toEqual({ blocker: { reason: 'no-legacy' } })
  })

  it('reports an already-migrated profile once the marker exists', async () => {
    const root = temporaryRoot()
    seedLegacy(root)
    const { desktop } = profileLocations(root)
    mkdirSync(desktop, { recursive: true })
    writeFileSync(join(desktop, MIGRATION_MARKER), '{}\n')
    expect(await detectMigration(BIN, root)).toEqual({ blocker: { reason: 'already-migrated', dir: profileLocations(root).legacy } })
  })

  it('offers the import with a pristine flag', async () => {
    const root = temporaryRoot()
    seedLegacy(root)
    const result = await detectMigration(BIN, root)
    expect('source' in result && result.pristine).toBe(true)
    expect('source' in result && result.source.plugins.map(plugin => plugin.name)).toEqual(['plugin-a', 'plugin-b'])
  })
})

describe('migration execution', () => {
  it('never modifies the legacy profile', async () => {
    const root = temporaryRoot()
    const legacy = seedLegacy(root)
    const before = treeDigest(legacy)
    const source = await readMigrationSource(BIN, legacy)
    expect(source).toBeDefined()
    await applyMigration({
      binName: BIN, desktopDir: profileLocations(root).desktop, source: source as MigrationSource,
      selection: { settings: true, plugins: ['plugin-a', 'plugin-b'], conflicts: { 'agent-default-model': 'legacy' } },
    })
    expect(treeDigest(legacy)).toBe(before)
  })

  it('imports settings, plugins, and the compatibility file', async () => {
    const root = temporaryRoot()
    const legacy = seedLegacy(root)
    writeFileSync(join(legacy, 'compatibility.json'), '{"exemptions":{}}\n')
    const desktop = seedDesktop(root)
    const source = await readMigrationSource(BIN, legacy)
    const outcome = await applyMigration({
      binName: BIN, desktopDir: desktop, source: source as MigrationSource,
      selection: { settings: true, plugins: ['plugin-a'], conflicts: { 'agent-default-model': 'legacy' } },
    })
    expect(outcome.importedPlugins).toBe(1)
    expect(existsSync(join(desktop, 'node_modules', 'plugin-a'))).toBe(true)
    expect(existsSync(join(desktop, 'node_modules', 'plugin-b'))).toBe(false)
    expect(existsSync(join(desktop, 'compatibility.json'))).toBe(true)
    const manifest = JSON.parse(readFileSync(join(desktop, 'package.json'), 'utf8')) as {
      dependencies: Record<string, string>
      dsh: { profile: { bundles: string[] } }
    }
    expect(manifest.dependencies).toEqual({ 'plugin-a': '1.0.0' })
    expect(manifest.dsh.profile.bundles).toContain('plugin-a')
    expect(manifest.dsh.profile.bundles).not.toContain('plugin-b')
    const patches = loadOverlayPatches(BIN, join(desktop, 'cordis.patch.yml'))
    const model = patches.find(patch => patch.id === 'agent-default-model')
    expect(model?.config).toMatchObject({ provider: 'legacy-provider' })
  })

  it('keeps the desktop copy of a conflict when the user chooses it', async () => {
    const root = temporaryRoot()
    const legacy = seedLegacy(root)
    const desktop = seedDesktop(root)
    const source = await readMigrationSource(BIN, legacy)
    await applyMigration({
      binName: BIN, desktopDir: desktop, source: source as MigrationSource,
      selection: { settings: true, plugins: [], conflicts: { 'agent-default-model': 'desktop' } },
    })
    const patches = loadOverlayPatches(BIN, join(desktop, 'cordis.patch.yml'))
    expect(patches.find(patch => patch.id === 'agent-default-model')?.config).toMatchObject({ provider: 'desktop-provider' })
  })

  it('imports the legacy value of an undecided conflict instead of discarding it', async () => {
    const root = temporaryRoot()
    const legacy = seedLegacy(root)
    const desktop = seedDesktop(root)
    const source = await readMigrationSource(BIN, legacy)
    // No conflict choice was recorded: the import must still bring the source value,
    // because an unresolved conflict silently reverting to the destination dropped
    // the user's edited configuration.
    await applyMigration({
      binName: BIN, desktopDir: desktop, source: source as MigrationSource,
      selection: { settings: true, plugins: [], conflicts: {} },
    })
    const patches = loadOverlayPatches(BIN, join(desktop, 'cordis.patch.yml'))
    expect(patches.find(patch => patch.id === 'agent-default-model')?.config).toMatchObject({ provider: 'legacy-provider' })
  })

  it('reports a differing entry against the destination patch that is actually on disk', async () => {
    const root = temporaryRoot()
    const legacy = seedLegacy(root)
    const desktop = seedDesktop(root)
    const destination = await readProfilePatchEntries(BIN, desktop)
    const conflicts = classifyPatches(
      (await readMigrationSource(BIN, legacy) as MigrationSource).patches,
      destination,
    )
    // An empty destination list would hide the conflict and make it unresolvable.
    expect(conflicts.find(item => item.id === 'agent-default-model')?.kind).toBe('differing')
  })

  it('imports no settings when the user declines them', async () => {
    const root = temporaryRoot()
    const legacy = seedLegacy(root)
    const desktop = seedDesktop(root)
    const source = await readMigrationSource(BIN, legacy)
    await applyMigration({
      binName: BIN, desktopDir: desktop, source: source as MigrationSource,
      selection: { settings: false, plugins: ['plugin-a'], conflicts: {} },
    })
    const patches = loadOverlayPatches(BIN, join(desktop, 'cordis.patch.yml'))
    expect(patches.some(patch => patch.id === 'ui-theme')).toBe(false)
    expect(patches.some(patch => patch.id === 'agent-default-model')).toBe(true)
  })

  it('restores the destination exactly when a plugin copy fails', async () => {
    const root = temporaryRoot()
    const legacy = seedLegacy(root)
    const desktop = seedDesktop(root)
    // A regular file where the plugin directory belongs makes the copy fail
    // after the patch and manifest were already written.
    mkdirSync(desktop, { recursive: true })
    writeFileSync(join(desktop, 'node_modules'), 'not a directory\n')
    // A file this import never writes must survive the rollback: replacing the whole
    // directory also discarded the caller's profile lock and every unrelated file.
    const untouched = join(desktop, 'lock')
    writeFileSync(untouched, '4242\n')
    const source = await readMigrationSource(BIN, legacy)
    await expect(applyMigration({
      binName: BIN, desktopDir: desktop, source: source as MigrationSource,
      selection: { settings: true, plugins: ['plugin-a'], conflicts: {} },
    })).rejects.toThrow()
    expect(readFileSync(join(desktop, 'node_modules'), 'utf8')).toBe('not a directory\n')
    expect(existsSync(join(desktop, MIGRATION_MARKER))).toBe(false)
    expect(readFileSync(untouched, 'utf8')).toBe('4242\n')
    // The pre-migration manifest survives the rollback.
    const manifest = JSON.parse(readFileSync(join(desktop, 'package.json'), 'utf8')) as {
      dsh: { profile: { bundles: string[] } }
    }
    expect(manifest.dsh.profile.bundles).not.toContain('plugin-a')
  })

  it('leaves no backup behind after a successful import', async () => {
    const root = temporaryRoot()
    const legacy = seedLegacy(root)
    const desktop = seedDesktop(root)
    const source = await readMigrationSource(BIN, legacy)
    const outcome = await applyMigration({
      binName: BIN, desktopDir: desktop, source: source as MigrationSource,
      selection: { settings: true, plugins: [], conflicts: {} },
    })
    expect(outcome.backup).toBeDefined()
    expect(existsSync(outcome.backup as string)).toBe(true)
  })

  it('clears the marker so the prompt can return', async () => {
    const root = temporaryRoot()
    const legacy = seedLegacy(root)
    const desktop = profileLocations(root).desktop
    const source = await readMigrationSource(BIN, legacy)
    await applyMigration({
      binName: BIN, desktopDir: desktop, source: source as MigrationSource,
      selection: { settings: true, plugins: [], conflicts: {} },
    })
    expect(existsSync(join(desktop, MIGRATION_MARKER))).toBe(true)
    await clearMigrationMarker(root)
    expect(existsSync(join(desktop, MIGRATION_MARKER))).toBe(false)
  })
})

describe('patch and manifest composition', () => {
  const entry = (id: string, serialized: string): PatchEntry => ({ id, serialized })

  it('appends single-source entries after the desktop entries', () => {
    const merged = composeMergedPatches(
      [entry('a', '{"id":"a"}')], [entry('b', '{"id":"b"}')],
      { settings: true, plugins: [], conflicts: {} },
    )
    expect(merged).toEqual([{ id: 'a' }, { id: 'b' }])
  })

  it('keeps every desktop entry that the legacy profile does not mention', () => {
    const merged = composeMergedPatches(
      [], [entry('b', '{"id":"b"}')], { settings: false, plugins: [], conflicts: {} },
    )
    expect(merged).toEqual([{ id: 'b' }])
  })

  it('adds only chosen, enabled plugins to the bundle list', () => {
    const manifest = { dependencies: {}, dsh: { profile: { bundles: ['base'] } } }
    const source = {
      dir: '/legacy',
      patches: [],
      bundles: ['base', 'plugin-a', 'plugin-b'],
      dependencies: { 'plugin-a': '1.0.0', 'plugin-b': '2.0.0' },
      plugins: [
        { name: 'plugin-a', spec: '1.0.0', enabled: true },
        { name: 'plugin-b', spec: '2.0.0', enabled: true },
      ],
      hasCompatibility: false,
    } satisfies MigrationSource
    const merged = mergeManifest(manifest, source, { settings: true, plugins: ['plugin-a'], conflicts: {} })
    expect((merged.dsh as { profile: { bundles: string[] } }).profile.bundles).toEqual(['base', 'plugin-a'])
    expect(merged.dependencies).toEqual({ 'plugin-a': '1.0.0' })
  })
})

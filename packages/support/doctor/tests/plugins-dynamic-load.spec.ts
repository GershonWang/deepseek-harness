/**
 * Tests for the `plugin-dynamic-load` doctor check: one real probe boot of
 * the profile, binary bisection to the culprit bundle, and the interaction
 * case no single bundle reproduces.
 *
 * Every fixture home mirrors loader-probe.spec.ts: a web profile naming
 * `@deepseek-ai/dsh-sdk-minimal` as its base tree plus synthetic third-party
 * bundles under the profile's node_modules. Bundles resolve from the pnpm
 * virtual store that vitest exposes to spawned children through NODE_PATH,
 * exactly the environment the check's subprocess runs in.
 */

import { mkdtemp, rm, mkdir, writeFile } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { readProfileManifest, resolveProfileDir } from '@deepseek-ai/dsh-app-boot'
import { runRepair } from '../src/index.ts'
import { pluginDynamicLoadCheck, pluginChecks } from '../src/checks/plugins.ts'

/** A single probe boot takes seconds; bound each case like loader-probe.spec. */
const PROBE_BOUND = 120_000

/** A module only a third-party broken-plugin fixture imports. */
const MISSING_DEP = 'dep-that-does-not-exist-xyz-12345'

/** A function plugin that activates without side effects or services. */
const NOOP_PLUGIN = 'export default function () { void 0 }\n'

/**
 * Healthy third-party patch: one entry activating a bundled no-op plugin.
 * A no-op keeps multiple healthy bundles coexistable (a service-bearing
 * plugin like the timer registers a singleton and cannot appear twice).
 */
function healthyPatch(id: string): string {
  return [
    '- insert:',
    `    - id: ${id}`,
    '      name: ./noop.js',
  ].join('\n') + '\n'
}

/** Third-party patch inserting a plugin file that imports ${MISSING_DEP}. */
function brokenPatch(): string {
  return [
    '- insert:',
    '    - id: broken-plugin',
    '      name: ./broken-plugin.js',
  ].join('\n') + '\n'
}

/** Write a profile manifest under the home naming the given bundle layers. */
async function writeProfile(home: string, bundles: readonly string[]): Promise<void> {
  const dir = join(home, 'profiles', 'web')
  await mkdir(dir, { recursive: true })
  await writeFile(join(dir, 'package.json'), JSON.stringify({
    name: 'dsh-profile-web',
    private: true,
    dependencies: {},
    dsh: { profile: { bundles: [...bundles], patchReload: 'live' } },
  }, undefined, 2) + '\n')
}

/** Write a third-party bundle package under the profile's node_modules. */
async function writeBundle(
  home: string, bundleName: string, patch: string,
): Promise<void> {
  const dir = join(home, 'profiles', 'web', 'node_modules', bundleName)
  await mkdir(dir, { recursive: true })
  await writeFile(join(dir, 'package.json'), JSON.stringify({
    name: bundleName,
    version: '1.0.0',
    private: true,
    // 插件模块是 ESM（.js 含 import/export）：不声明 type:module 会被 Node 当
    // CJS require，含 import 语法时抛 ERR_REQUIRE_CYCLE_MODULE 而非真实的
    // 模块缺失错误（与真实 ESM 插件行为一致）。
    type: 'module',
    dsh: { bundle: { patch: './cordis.patch.yml' } },
  }, undefined, 2) + '\n')
  await writeFile(join(dir, 'cordis.patch.yml'), patch)
}

/** Add a plugin module file inside an existing third-party bundle. */
async function writeBundleFile(home: string, bundleName: string, fileName: string, content: string): Promise<void> {
  await writeFile(
    join(home, 'profiles', 'web', 'node_modules', bundleName, fileName),
    content,
  )
}

describe('plugin-dynamic-load', () => {
  let home: string

  beforeEach(() => {
    home = ''
  })

  afterEach(async () => {
    if (home !== '') await rm(home, { recursive: true, force: true })
  })

  it('is registered in pluginChecks', () => {
    expect(pluginChecks.some(c => c.id === 'plugin-dynamic-load')).toBe(true)
  })

  it('passes when no third-party bundles are installed', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-dyn-none-'))
    const result = await pluginDynamicLoadCheck.check(home)
    expect(result.ok).toBe(true)
    expect(result.message).toContain('无第三方插件')
    expect(result.fixable).toBe(false)
  })

  it('fails and names a bundle whose plugin imports a missing module', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-dyn-bad-'))
    await writeProfile(home, ['@deepseek-ai/dsh-sdk-minimal', 'third-party-bad'])
    await writeBundle(home, 'third-party-bad', brokenPatch())
    await writeBundleFile(home, 'third-party-bad', 'broken-plugin.js',
      `import { value } from '${MISSING_DEP}'\nexport default function () { void value }\n`)

    const result = await pluginDynamicLoadCheck.check(home)
    expect(result.ok).toBe(false)
    expect(result.message).toContain('third-party-bad')
    expect(result.fixable).toBe(true)
    expect(result.suggestedLevel).toBe(2)
    expect(result.detail).toContain(MISSING_DEP)
  }, PROBE_BOUND)

  it('bisects to the bad bundle among healthy ones', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-dyn-bisect-'))
    await writeProfile(home, ['@deepseek-ai/dsh-sdk-minimal', 'third-party-healthy', 'third-party-bad'])
    await writeBundle(home, 'third-party-healthy', healthyPatch('healthy-carrier'))
    await writeBundleFile(home, 'third-party-healthy', 'noop.js', NOOP_PLUGIN)
    await writeBundle(home, 'third-party-bad', brokenPatch())
    await writeBundleFile(home, 'third-party-bad', 'broken-plugin.js',
      `import { value } from '${MISSING_DEP}'\nexport default function () { void value }\n`)

    const result = await pluginDynamicLoadCheck.check(home)
    expect(result.ok).toBe(false)
    expect(result.message).toContain('third-party-bad')
    expect(result.fixable).toBe(true)
  }, PROBE_BOUND)

  it('passes when every third-party bundle loads', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-dyn-ok-'))
    await writeProfile(home, ['@deepseek-ai/dsh-sdk-minimal', 'third-party-ok-a', 'third-party-ok-b'])
    await writeBundle(home, 'third-party-ok-a', healthyPatch('ok-a-noop'))
    await writeBundleFile(home, 'third-party-ok-a', 'noop.js', NOOP_PLUGIN)
    await writeBundle(home, 'third-party-ok-b', healthyPatch('ok-b-noop'))
    await writeBundleFile(home, 'third-party-ok-b', 'noop.js', NOOP_PLUGIN)

    const result = await pluginDynamicLoadCheck.check(home)
    expect(result.ok).toBe(true)
    expect(result.message).toContain('所有 2 个第三方插件加载正常')
  }, PROBE_BOUND)

  it('reports an unlocatable failure when only the pair of bundles breaks', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-dyn-interact-'))
    // trip-bundle evaluates first and sets the flag; partner-bundle throws
    // because of it. Either alone loads fine, so no single bundle is the
    // culprit and the bisection must stay inconclusive.
    await writeProfile(home, ['@deepseek-ai/dsh-sdk-minimal', 'trip-bundle', 'partner-bundle'])
    await writeBundle(home, 'trip-bundle', [
      '- insert:',
      '    - id: trip-plugin',
      '      name: ./trip.js',
    ].join('\n') + '\n')
    await writeBundleFile(home, 'trip-bundle', 'trip.js',
      'globalThis.__tripLoaded = true\nexport default function () { void 0 }\n')
    await writeBundle(home, 'partner-bundle', [
      '- insert:',
      '    - id: partner-plugin',
      '      name: ./partner.js',
    ].join('\n') + '\n')
    await writeBundleFile(home, 'partner-bundle', 'partner.js',
      "if (globalThis.__tripLoaded === true) { throw new Error('incompatible pair detected') }\nexport default function () { void 0 }\n")

    const result = await pluginDynamicLoadCheck.check(home)
    expect(result.ok).toBe(false)
    expect(result.message).toContain('未能定位')
    expect(result.fixable).toBe(false)
    expect(result.detail).toContain('incompatible pair detected')
  }, PROBE_BOUND)

  it('treats an unloadable profile as a pass with a notice', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-dyn-noprofile-'))
    // A manifest whose patchReload is neither "live" nor "startup" makes
    // loadProfile throw before any bundle can be listed.
    const dir = join(home, 'profiles', 'web')
    await mkdir(dir, { recursive: true })
    await writeFile(join(dir, 'package.json'), JSON.stringify({
      name: 'dsh-profile-web',
      private: true,
      dependencies: {},
      dsh: { profile: { bundles: [], patchReload: 'invalid' } },
    }, undefined, 2) + '\n')

    const result = await pluginDynamicLoadCheck.check(home)
    expect(result.ok).toBe(true)
    expect(result.message).toContain('profile not loadable')
  })

  describe('repair', () => {
    it('removes the culprit bundle from the profile and the check passes afterwards', async () => {
      home = await mkdtemp(join(tmpdir(), 'dsh-dyn-repair-'))
      await writeProfile(home, ['@deepseek-ai/dsh-sdk-minimal', 'third-party-bad'])
      await writeBundle(home, 'third-party-bad', brokenPatch())
      await writeBundleFile(home, 'third-party-bad', 'broken-plugin.js',
        `import { value } from '${MISSING_DEP}'\nexport default function () { void value }\n`)

      const repair = await runRepair(2, home)

      expect(repair.applied.map(a => a.checkId)).toContain('plugin-dynamic-load')
      // Back up the original manifest into the repair run's backup directory.
      const backupDir = repair.backups[0]!
      expect(existsSync(join(backupDir, 'web-profile.package.json'))).toBe(true)
      // The culprit layer is gone from the profile's bundle list.
      const manifest = readProfileManifest('doctor-repair-test', resolveProfileDir('web', home))
      expect(manifest.dsh?.profile?.bundles).toEqual(['@deepseek-ai/dsh-sdk-minimal'])

      const after = await pluginDynamicLoadCheck.check(home)
      expect(after.ok).toBe(true)
    }, PROBE_BOUND)

    it('reports nothing to repair when every third-party bundle loads', async () => {
      home = await mkdtemp(join(tmpdir(), 'dsh-dyn-fix-ok-'))
      await writeProfile(home, ['@deepseek-ai/dsh-sdk-minimal', 'third-party-ok'])
      await writeBundle(home, 'third-party-ok', healthyPatch('fix-ok-noop'))
      await writeBundleFile(home, 'third-party-ok', 'noop.js', NOOP_PLUGIN)

      const backupDir = join(home, 'backups', 'doctor-test')
      await mkdir(backupDir, { recursive: true })
      const result = await pluginDynamicLoadCheck.fix!(home, backupDir)
      expect(result.ok).toBe(true)
      expect(result.message).toContain('无需修复')
      // No manifest was touched.
      const manifest = readProfileManifest('doctor-repair-test', resolveProfileDir('web', home))
      expect(manifest.dsh?.profile?.bundles).toContain('third-party-ok')
    }, PROBE_BOUND)

    it('reports it cannot fix when no single bundle reproduces the failure', async () => {
      home = await mkdtemp(join(tmpdir(), 'dsh-dyn-fix-interact-'))
      // The same interacting pair as the unlocatable check case: either bundle
      // alone loads fine, so the repair cannot locate anything to disable.
      await writeProfile(home, ['@deepseek-ai/dsh-sdk-minimal', 'trip-bundle', 'partner-bundle'])
      await writeBundle(home, 'trip-bundle', [
        '- insert:',
        '    - id: trip-plugin',
        '      name: ./trip.js',
      ].join('\n') + '\n')
      await writeBundleFile(home, 'trip-bundle', 'trip.js',
        'globalThis.__tripLoaded = true\nexport default function () { void 0 }\n')
      await writeBundle(home, 'partner-bundle', [
        '- insert:',
        '    - id: partner-plugin',
        '      name: ./partner.js',
      ].join('\n') + '\n')
      await writeBundleFile(home, 'partner-bundle', 'partner.js',
        "if (globalThis.__tripLoaded === true) { throw new Error('incompatible pair detected') }\nexport default function () { void 0 }\n")

      const backupDir = join(home, 'backups', 'doctor-test')
      await mkdir(backupDir, { recursive: true })
      const result = await pluginDynamicLoadCheck.fix!(home, backupDir)
      expect(result.ok).toBe(false)
      expect(result.message).toContain('未能定位')
    }, PROBE_BOUND)

    it('removes every independently broken bundle, not just the first', async () => {
      // 两个第三方 bundle 各自独立损坏（移除其一后另一个冒头）：修复应
      // 循环定位并全部移除，直到全量探测通过 —— 对应真实环境的多个坏插件。
      home = await mkdtemp(join(tmpdir(), 'dsh-dyn-repair-multi-'))
      await writeProfile(home,
        ['@deepseek-ai/dsh-sdk-minimal', 'third-party-bad-a', 'third-party-bad-b'])
      for (const name of ['third-party-bad-a', 'third-party-bad-b']) {
        await writeBundle(home, name, brokenPatch())
        await writeBundleFile(home, name, 'broken-plugin.js',
          `import { value } from '${MISSING_DEP}'\nexport default function () { void value }\n`)
      }

      const repair = await runRepair(2, home)

      expect(repair.applied.map(a => a.checkId)).toContain('plugin-dynamic-load')
      // 两个坏 bundle 都从 profile bundle 列表消失。
      const manifest = readProfileManifest('doctor-repair-test', resolveProfileDir('web', home))
      expect(manifest.dsh?.profile?.bundles).toEqual(['@deepseek-ai/dsh-sdk-minimal'])
      // 修复后检查通过（只剩官方树可加载）。
      const after = await pluginDynamicLoadCheck.check(home)
      expect(after.ok).toBe(true)
    }, PROBE_BOUND)
  })
})

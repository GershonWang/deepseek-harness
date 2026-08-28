/**
 * Plugin-level diagnostic checks: profile bundle resolvability,
 * third-party inventory, patch composability, and one live-load probe.
 *
 * Most checks are static; `pluginDynamicLoadCheck` additionally spawns the
 * loader-probe subprocess for a bounded real boot, catching plugin modules
 * whose imports no longer resolve after an upgrade without starting the full
 * supervisor path.
 *
 * @module @deepseek-ai/dsh-doctor/checks/plugins
 */

import { execFile } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  loadProfile,
  composeEntries,
  readProfileManifest,
  resolveProfileDir,
  writeProfileManifest,
} from '@deepseek-ai/dsh-app-boot'
import { writeFileAtomic } from '@deepseek-ai/dsh-atomic-write'
import { bisectBy } from '../bisect-by.js'
import type { DoctorCheck, CheckResult, FixResult } from '../types.js'

const require = createRequire(import.meta.url)

function isOfficialBundle(packageName: string): boolean {
  return packageName.startsWith('@deepseek-ai/')
}

function webAppAnchor(): string {
  return require.resolve('@deepseek-ai/dsh-web-app/package.json')
}

const pluginBundlesResolvable: DoctorCheck = {
  id: 'plugin-bundles-resolvable',
  name: 'Profile bundles resolvable',
  category: 'plugin',
  severity: 'fatal',
  check: async (dshHome: string): Promise<CheckResult> => {
    try {
      const profile = loadProfile('doctor', 'web', webAppAnchor(), dshHome)
      const official = profile.layers.filter(l => isOfficialBundle(l.packageName))
      const thirdParty = profile.layers.filter(l => !isOfficialBundle(l.packageName))
      const base = {
        ok: true,
        message: `${profile.layers.length} bundles resolved (${official.length} official, ${thirdParty.length} third-party)`,
        fixable: thirdParty.length > 0,
        suggestedLevel: 1 as const,
      }
      if (thirdParty.length > 0) {
        return { ...base, detail: `Third-party: ${thirdParty.map(t => t.packageName).join(', ')}` }
      }
      return base
    } catch (err) {
      const msg = (err as Error).message
      const thirdPartyMentioned = msg.includes('cannot resolve profile bundle')
      const base = {
        ok: false,
        message: `Cannot resolve profile bundles: ${msg}`,
        fixable: true,
        suggestedLevel: 1 as const,
      }
      if (thirdPartyMentioned) {
        return { ...base, detail: 'A third-party plugin may have broken dependencies after upgrade. Try plugin-safe mode.' }
      }
      return base
    }
  },
}

const pluginPatchComposable: DoctorCheck = {
  id: 'plugin-patch-composable',
  name: 'Profile patch layers compose cleanly',
  category: 'plugin',
  severity: 'error',
  check: async (dshHome: string): Promise<CheckResult> => {
    try {
      const profile = loadProfile('doctor', 'web', webAppAnchor(), dshHome)
      const allLayers = [
        ...profile.layers.map(l => l.patches),
        profile.patches,
      ]
      const warnings: string[] = []
      const entries = composeEntries(allLayers, (msg) => {
        warnings.push(msg)
      })
      if (warnings.length === 0) {
        return {
          ok: true,
          message: `${entries.length} entries composed with zero patch warnings`,
          fixable: false,
          suggestedLevel: 2,
        }
      }
      return {
        ok: false,
        message: `${warnings.length} patch warning(s): ${warnings.slice(0, 3).join('; ')}${warnings.length > 3 ? '...' : ''}`,
        detail: warnings.join('\n'),
        fixable: true,
        suggestedLevel: 2,
      }
    } catch (err) {
      return {
        ok: false,
        message: `Cannot compose patches: ${(err as Error).message}`,
        fixable: true,
        suggestedLevel: 2,
      }
    }
  },
}

const pluginThirdPartyList: DoctorCheck = {
  id: 'plugin-third-party-list',
  name: 'Third-party plugin bundles',
  category: 'plugin',
  severity: 'info',
  check: async (dshHome: string): Promise<CheckResult> => {
    try {
      const profile = loadProfile('doctor', 'web', webAppAnchor(), dshHome)
      const thirdParty = profile.layers.filter(l => !isOfficialBundle(l.packageName))
      if (thirdParty.length === 0) {
        return {
          ok: true,
          message: 'No third-party bundles installed',
          fixable: false,
          suggestedLevel: 1,
        }
      }
      return {
        ok: true,
        message: `${thirdParty.length} third-party bundle(s): ${thirdParty.map(t => t.packageName).join(', ')}`,
        detail: 'If harness fails to start after upgrade, try plugin-safe mode to skip third-party bundles.',
        fixable: thirdParty.length > 0,
        suggestedLevel: 1,
      }
    } catch {
      return {
        ok: true,
        message: 'Cannot list third-party bundles (profile not loadable)',
        fixable: false,
        suggestedLevel: 1,
      }
    }
  },
}

// Verify each user-patch target id exists in the composed entry list.
const pluginPatchTargets: DoctorCheck = {
  id: 'plugin-patch-targets',
  name: 'User patch targets exist',
  category: 'plugin',
  severity: 'warning',
  check: async (dshHome: string): Promise<CheckResult> => {
    try {
      const profile = loadProfile('doctor', 'web', webAppAnchor(), dshHome)
      if (profile.patches.length === 0) {
        return { ok: true, message: 'No user patches', fixable: false, suggestedLevel: 2 }
      }

      // Compose WITHOUT user patches first to get the baseline entry ids.
      const baselineEntries = composeEntries(profile.layers.map(l => l.patches))
      const baselineIds = new Set(baselineEntries.map(e => e.id).filter(Boolean))

      // Now find user patch targets that don't exist in baseline.
      const missingIds: string[] = []
      for (const patch of profile.patches) {
        if (patch.id && !baselineIds.has(patch.id as string)) {
          missingIds.push(String(patch.id))
        }
      }

      if (missingIds.length === 0) {
        return {
          ok: true,
          message: `All ${profile.patches.length} user patch targets exist`,
          fixable: false,
          suggestedLevel: 2,
        }
      }

      return {
        ok: false,
        message: `${missingIds.length} user patch(es) target unknown entries: ${missingIds.slice(0, 5).join(', ')}${missingIds.length > 5 ? '...' : ''}`,
        detail: 'These patches may reference entries that were renamed or removed in the latest harness version. The patches will be silently skipped at startup.',
        fixable: true,
        suggestedLevel: 2,
      }
    } catch {
      return {
        ok: true,
        message: 'Cannot verify patch targets (profile not loadable)',
        fixable: false,
        suggestedLevel: 2,
      }
    }
  },
}

/** Profile the dynamic-load check probes; must match the static checks. */
const DYNAMIC_PROFILE = 'web'
/** Bound a single probe run before the probe itself reports a timeout. */
const DYNAMIC_PROBE_TIMEOUT_MS = 60_000
/** Upper bound for captured probe output (a failing load stack can be long). */
const DYNAMIC_PROBE_MAX_BUFFER = 10 * 1024 * 1024
/** The loader-probe subprocess, located next to this module. */
const loaderProbePath = fileURLToPath(new URL('../loader-probe.ts', import.meta.url))

interface LoaderProbeOutcome {
  /** Exit code of the probe; -1 when the spawn itself failed. */
  code: number
  /** Captured stdout and stderr, trimmed and joined. */
  output: string
}

/**
 * Run the loader-probe subprocess against `dshHome`, loading every bundle
 * layer when `include` is empty or only the named third-party subset.
 * @param dshHome - harness home, passed as `--dsh-home`.
 * @param include - third-party bundle names to load (`--include` per name).
 * @returns the probe exit code and its captured output.
 */
function runLoaderProbe(dshHome: string, include: readonly string[]): Promise<LoaderProbeOutcome> {
  const env: NodeJS.ProcessEnv = { ...process.env }
  // The explicit `--dsh-home` argument owns the home; a stray ambient
  // DSH_HOME would otherwise redirect the probe to another installation.
  delete env.DSH_HOME
  return new Promise((resolve) => {
    execFile(
      process.execPath,
      [
        '--import', 'tsx/esm',
        loaderProbePath,
        '--dsh-home', dshHome,
        '--profile', DYNAMIC_PROFILE,
        '--timeout', String(DYNAMIC_PROBE_TIMEOUT_MS),
        ...include.flatMap(name => ['--include', name]),
      ],
      { env, maxBuffer: DYNAMIC_PROBE_MAX_BUFFER, encoding: 'utf8' },
      (error, stdout, stderr) => {
        const output = [stdout, stderr].filter(Boolean).join('\n').trim()
        if (error === null) {
          resolve({ code: 0, output })
        } else {
          // A non-zero exit rejects with the numeric exit code; the -1 arm
          // only fires when the probe binary itself cannot spawn.
          /* v8 ignore next 3 -- no test can break process.execPath; the -1 arm keeps the outcome typed. */
          resolve({
            code: typeof error.code === 'number' ? error.code : -1,
            output: output === '' ? error.message : output,
          })
        }
      },
    )
  })
}

/** One third-party inspection pass shared by the check and its repair. */
interface LocateCulpritResult {
  /** Whether the profile manifest loaded enough to enumerate bundles. */
  loadable: boolean
  /** Third-party bundle names currently layered in the profile. */
  thirdParty: string[]
  /**
   * Captured full-tree probe output, or the profile-read error when
   * `loadable` is false.
   */
  output: string
  /** Whether the full tree (every bundle layer) booted cleanly. */
  fullOk: boolean
  /** The bundle that alone breaks the load; null when none is found. */
  culprit: string | null
}

/**
 * Enumerate the profile's third-party bundles, boot them all through the
 * loader probe once, and bisect to the single bundle that breaks the load.
 * Shared by the check (which reports the culprit) and its repair (which
 * re-locates the culprit at fix time instead of trusting check state).
 * @param dshHome - harness home passed to loadProfile and the probe.
 * @returns the load outcome and the located culprit, if any.
 */
async function locateCulprit(dshHome: string): Promise<LocateCulpritResult> {
  let thirdParty: string[]
  try {
    const profile = loadProfile('doctor', 'web', webAppAnchor(), dshHome)
    thirdParty = profile.layers
      .filter(layer => !isOfficialBundle(layer.packageName))
      .map(layer => layer.packageName)
  } catch (err) {
    return { loadable: false, thirdParty: [], output: (err as Error).message, fullOk: false, culprit: null }
  }
  if (thirdParty.length === 0) {
    return { loadable: true, thirdParty, output: '', fullOk: true, culprit: null }
  }

  const full = await runLoaderProbe(dshHome, [])
  if (full.code === 0) {
    return { loadable: true, thirdParty, output: full.output, fullOk: true, culprit: null }
  }

  // The full tree failed to load; isolate the offending bundle. A subset
  // "is bad" when loading only it still fails — with the full set failing
  // and the empty set passing, bisectBy's contract holds.
  const culprit = await bisectBy(thirdParty, async (subset) => {
    const result = await runLoaderProbe(dshHome, subset)
    return result.code !== 0
  })
  return { loadable: true, thirdParty, output: full.output, fullOk: false, culprit }
}

/**
 * Load every third-party bundle once; on failure, binary-search which bundle
 * breaks the load and report it. Failures only a real boot exposes (plugin
 * modules importing dependencies the installation no longer provides) land
 * here, so the report can name the culprit instead of the whole tree. Its
 * repair removes the culprit bundle from the profile's bundle list after
 * backing up the manifest, then re-boots to prove the tree loads.
 */
export const pluginDynamicLoadCheck: DoctorCheck = {
  id: 'plugin-dynamic-load',
  name: '插件运行时兼容性',
  category: 'plugin',
  severity: 'fatal',
  check: async (dshHome: string): Promise<CheckResult> => {
    const located = await locateCulprit(dshHome)
    if (!located.loadable) {
      return {
        ok: true,
        message: `Cannot list third-party bundles (profile not loadable): ${located.output}`,
        fixable: false,
        suggestedLevel: 2,
      }
    }
    if (located.thirdParty.length === 0) {
      return { ok: true, message: '无第三方插件', fixable: false, suggestedLevel: 2 }
    }
    if (located.fullOk) {
      return {
        ok: true,
        message: `所有 ${located.thirdParty.length} 个第三方插件加载正常`,
        fixable: false,
        suggestedLevel: 2,
      }
    }
    if (located.culprit !== null) {
      return {
        ok: false,
        message: `插件 ${located.culprit} 导致启动失败（缺少运行依赖或损坏）`,
        detail: located.output,
        fixable: true,
        suggestedLevel: 2,
      }
    }
    return {
      ok: false,
      message: '第三方插件导致启动失败，未能定位',
      detail: located.output,
      fixable: false,
      suggestedLevel: 2,
    }
  },
  fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
    const located = await locateCulprit(dshHome)
    if (!located.loadable) {
      return { ok: false, message: '无法读取 profile，无法自动修复' }
    }
    if (located.thirdParty.length === 0 || located.fullOk) {
      return { ok: true, message: '插件加载已正常，无需修复' }
    }
    if (located.culprit === null) {
      return { ok: false, message: '未能定位问题插件，无法自动修复' }
    }
    const culprit = located.culprit

    // Third-party bundles are profile layers listed in `dsh.profile.bundles`,
    // so disabling one means removing it from the manifest's bundle list —
    // the same exclusion DSH_SAFE_MODE=plugins applies to every non-official
    // bundle. Back up the raw manifest first and restore it on verify failure.
    const profileDir = resolveProfileDir('web', dshHome)
    const manifest = readProfileManifest('doctor', profileDir)
    const bundles = manifest.dsh?.profile?.bundles ?? []
    if (!bundles.includes(culprit)) {
      return { ok: true, message: '插件加载已正常，无需修复' }
    }

    const manifestPath = join(profileDir, 'package.json')
    const original = readFileSync(manifestPath, 'utf8')
    const backupPath = join(backupDir, 'web-profile.package.json')
    await writeFileAtomic(backupPath, original, { mode: 0o600, dirMode: 0o700 })

    writeProfileManifest(profileDir, {
      ...manifest,
      dsh: {
        ...manifest.dsh,
        profile: {
          ...manifest.dsh?.profile,
          bundles: bundles.filter(bundle => bundle !== culprit),
        },
      },
    })

    const verify = await runLoaderProbe(dshHome, [])
    if (verify.code !== 0) {
      await writeFileAtomic(manifestPath, original, { mode: 0o600 })
      return {
        ok: false,
        message: `移除 ${culprit} 后加载仍然失败，已还原 profile manifest`,
        backupPath,
      }
    }
    return {
      ok: true,
      message: `已从 profile bundles 移除导致加载失败的插件 ${culprit}（原 manifest 已备份）`,
      backupPath,
    }
  },
}

export const pluginChecks: DoctorCheck[] = [
  pluginBundlesResolvable,
  pluginPatchComposable,
  pluginPatchTargets,
  pluginThirdPartyList,
  pluginDynamicLoadCheck,
]

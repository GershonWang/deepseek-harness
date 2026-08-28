/**
 * Plugin-level diagnostic checks: profile bundle resolvability,
 * third-party inventory, and patch composability.
 *
 * Full activation dry-run is intentionally out of scope for the doctor:
 * the real harness startup already exercises that path via the supervisor.
 * The doctor focuses on static checks that catch the most common
 * upgrade-breakage scenarios without spinning up the full plugin tree.
 *
 * @module @deepseek-ai/dsh-doctor/checks/plugins
 */

import { createRequire } from 'node:module'
import {
  loadProfile,
  composeEntries,
} from '@deepseek-ai/dsh-app-boot'
import type { DoctorCheck, CheckResult } from '../types.js'

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

export const pluginChecks: DoctorCheck[] = [
  pluginBundlesResolvable,
  pluginPatchComposable,
  pluginPatchTargets,
  pluginThirdPartyList,
]

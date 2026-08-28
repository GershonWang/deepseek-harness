/**
 * Binary search helper for identifying which third-party bundle is causing
 * a profile to fail on load.
 *
 * The algorithm:
 *  1. Collect all non-official bundles in the profile.
 *  2. Repeatedly split the suspect set in half and try loading with the first
 *     half disabled. If the load succeeds, the culprit is in the disabled half;
 *     if it fails, it's in the still-enabled half.
 *  3. Stop when one bundle remains or the set is empty.
 *
 * This is a diagnostic tool, not a fix — it tells the user WHICH plugin is
 * broken so they can remove/update/report it.
 *
 * @module @deepseek-ai/dsh-doctor/bisect
 */

import { writeFileSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { loadProfile, PROFILE_PATCH_FILENAME } from '@deepseek-ai/dsh-app-boot'
import type { Profile } from '@deepseek-ai/dsh-app-boot'
import { createRequire } from 'node:module'
import { bisectBy } from './bisect-by.js'

const OFFICIAL_PREFIX = '@deepseek-ai/'

function resolveInstallAnchor(): string {
  try {
    return createRequire(import.meta.url).resolve('@deepseek-ai/dsh-web-app/package.json')
  } catch {
    return ''
  }
}

/**
 * Result of a binary search for a problematic third-party bundle.
 */
export interface BisectResult {
  /** Whether a single culprit was identified. */
  found: boolean
  /** The identified problematic bundle, or empty string if none found. */
  culprit: string
  /** Number of load attempts made. */
  attempts: number
  /** All bundles that were tested and ruled out. */
  ruledOut: string[]
  /** If the search couldn't complete (e.g. all bundles pass), this explains why. */
  reason?: string
}

interface BisectOptions {
  /** The profile name to test. Defaults to 'web'. */
  profile?: string
  /** DSH home directory. Resolved from env if not provided. */
  dshHome?: string
  /** Optional install anchor for bundle resolution. */
  installAnchor?: string
}

/**
 * Run binary search to find which third-party bundle causes a profile load failure.
 *
 * Only useful when the profile fails to load with all third-party bundles
 * enabled. If the profile loads fine, returns `{ found: false, reason: "..." }`.
 *
 * @param options - configuration for the bisect run.
 * @returns the bisect result identifying the culprit (if found).
 */
export async function bisectThirdPartyBundles(options: BisectOptions = {}): Promise<BisectResult> {
  const profileName = options.profile ?? 'web'
  const installAnchor = options.installAnchor ?? resolveInstallAnchor()

  if (!installAnchor) {
    return { found: false, culprit: '', attempts: 0, ruledOut: [], reason: 'Cannot resolve install anchor' }
  }

  // Step 1: Load the full profile to get third-party bundle list.
  let fullProfile: Profile
  try {
    fullProfile = loadProfile('bisect', profileName, installAnchor, '')
  } catch (err) {
    return {
      found: false,
      culprit: '',
      attempts: 0,
      ruledOut: [],
      reason: `Cannot load profile even without modifications: ${(err as Error).message}`,
    }
  }

  const thirdParty = fullProfile.layers
    .filter(l => !l.packageName.startsWith(OFFICIAL_PREFIX))
    .map(l => l.packageName)

  if (thirdParty.length === 0) {
    return {
      found: false,
      culprit: '',
      attempts: 0,
      ruledOut: [],
      reason: 'No third-party bundles found',
    }
  }

  const tmpPatchDir = join(options.dshHome ?? '/tmp/dsh-doctor-bisect', 'profiles', profileName)
  mkdirSync(tmpPatchDir, { recursive: true })
  const tmpPatchPath = join(tmpPatchDir, PROFILE_PATCH_FILENAME)

  let attempts = 0

  /**
   * Disable the given set of bundles by writing a patch file and loading.
   * Returns true when the load succeeds (no error thrown).
   */
  function tryLoadWithDisabled(disabledBundles: string[]): boolean {
    attempts += 1

    const patchLines: string[] = []
    for (const bundleName of disabledBundles) {
      const layer = fullProfile.layers.find(l => l.packageName === bundleName)
      if (!layer) continue
      for (const patch of layer.patches) {
        const id = patch.id
        if (!id) continue
        patchLines.push(`- id: ${String(id)}`)
        patchLines.push('  disabled: true')
      }
    }
    writeFileSync(tmpPatchPath, patchLines.join('\n') + '\n')

    try {
      loadProfile('bisect', profileName, installAnchor, '', {
        extraPatchFiles: [tmpPatchPath],
      })
      return true
    } catch {
      return false
    }
  }

  // Baseline: does it load with ALL third-party disabled?
  const allDisabledOk = tryLoadWithDisabled(thirdParty)
  if (!allDisabledOk) {
    return {
      found: false,
      culprit: '',
      attempts,
      ruledOut: [],
      reason: 'Profile still fails with all third-party bundles disabled — the issue is not in third-party plugins',
    }
  }

  // Baseline: does it fail with all enabled?
  const allEnabledFails = !tryLoadWithDisabled([])
  if (!allEnabledFails) {
    return {
      found: false,
      culprit: '',
      attempts,
      ruledOut: thirdParty,
      reason: 'Profile loads fine with all third-party bundles — no culprit found at load time',
    }
  }

  // Use the generic bisectBy framework.
  // The predicate answers: "does the profile still fail when only the given
  // subset of third-party bundles is enabled?" — i.e. the bad behavior
  // (load failure) is still present.
  const culprit = await bisectBy(thirdParty, async (subset) => {
    const toDisable = thirdParty.filter(b => !subset.includes(b))
    return !tryLoadWithDisabled(toDisable)
  })

  if (!culprit) {
    return {
      found: false,
      culprit: '',
      attempts,
      ruledOut: [],
      reason: 'Binary search inconclusive — issue may involve multiple bundles interacting',
    }
  }

  return {
    found: true,
    culprit,
    attempts,
    ruledOut: thirdParty.filter(b => b !== culprit),
  }
}

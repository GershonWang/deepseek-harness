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
    .filter((l) => !l.packageName.startsWith(OFFICIAL_PREFIX))
    .map((l) => l.packageName)

  if (thirdParty.length === 0) {
    return {
      found: false,
      culprit: '',
      attempts: 0,
      ruledOut: [],
      reason: 'No third-party bundles found',
    }
  }

  const suspects = [...thirdParty]
  const ruledOut: string[] = []
  let attempts = 0

  const tmpPatchDir = join(options.dshHome ?? '/tmp/dsh-doctor-bisect', 'profiles', profileName)
  mkdirSync(tmpPatchDir, { recursive: true })
  const tmpPatchPath = join(tmpPatchDir, PROFILE_PATCH_FILENAME)

  function tryLoadWithDisabled(disabledBundles: string[]): boolean {
    attempts += 1

    const patchLines: string[] = []
    for (const bundleName of disabledBundles) {
      const layer = fullProfile.layers.find((l) => l.packageName === bundleName)
      if (!layer) continue
      for (const patch of layer.patches) {
        const id = patch.id
        if (!id) continue
        patchLines.push(`- id: ${String(id)}`)
        patchLines.push(`  disabled: true`)
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

  // Binary search: narrow down the suspects
  let low = 0
  let high = suspects.length - 1

  while (low < high) {
    const mid = Math.floor((low + high) / 2)
    const toDisable = suspects.slice(low, mid + 1)
    const alreadyRuled = suspects.slice(0, low)
    const allDisabled = [...alreadyRuled, ...toDisable]

    const loadsOk = tryLoadWithDisabled(allDisabled)

    if (loadsOk) {
      // Disabling the first half fixed it → culprit is in the first half
      high = mid
      // The second half is now ruled out
      for (let i = mid + 1; i <= high; i++) {
        const name = suspects[i]
        if (name && !ruledOut.includes(name)) ruledOut.push(name)
      }
    } else {
      // Still fails → culprit is in the second half
      low = mid + 1
      // The first half is ruled out
      for (let i = low - 1; i >= 0; i--) {
        const name = suspects[i]
        if (!name || ruledOut.includes(name)) break
        ruledOut.push(name)
      }
    }
  }

  // Verify the final suspect
  const finalCulprit = suspects[low] ?? ''
  if (!finalCulprit) {
    return {
      found: false,
      culprit: '',
      attempts,
      ruledOut,
      reason: 'No suspect remaining after binary search',
    }
  }
  const verifyDisable = thirdParty.filter((b) => b !== finalCulprit)
  const verifyOk = tryLoadWithDisabled(verifyDisable)

  if (verifyOk) {
    return {
      found: true,
      culprit: finalCulprit,
      attempts,
      ruledOut: ruledOut.filter((b) => b !== finalCulprit),
    }
  }

  return {
    found: false,
    culprit: '',
    attempts,
    ruledOut,
    reason: 'Binary search inconclusive — issue may involve multiple bundles interacting',
  }
}

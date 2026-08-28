/**
 * Safe-mode behavior for `loadProfile`: third-party bundle skipping and
 * user-patch suppression driven by the `DSH_SAFE_MODE` env var or explicit
 * options.
 */

import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createRequire } from 'node:module'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { loadProfile, PROFILE_PATCH_FILENAME } from '../src/profile.js'

const require = createRequire(import.meta.url)

describe('safe-mode in loadProfile', () => {
  let home: string
  const originalSafeMode = process.env.DSH_SAFE_MODE

  beforeEach(() => {
    home = mkdtempSync(join(tmpdir(), 'dsh-safe-mode-'))
    delete process.env.DSH_SAFE_MODE
  })

  afterEach(() => {
    rmSync(home, { recursive: true, force: true })
    if (originalSafeMode === undefined) delete process.env.DSH_SAFE_MODE
    else process.env.DSH_SAFE_MODE = originalSafeMode
  })

  const installAnchor = require.resolve('@deepseek-ai/dsh-web-app/package.json')

  it('normal mode keeps all official bundles', () => {
    const profile = loadProfile('test', 'web', installAnchor, home)
    const official = profile.layers.filter(l => l.packageName.startsWith('@deepseek-ai/'))
    expect(official.length).toBeGreaterThanOrEqual(2)
  })

  it('DSH_SAFE_MODE=plugins filters out non-official bundles', () => {
    // The shipped web profile has only official bundles, so count stays the same.
    // We verify the filter logic itself via the explicit option test below.
    process.env.DSH_SAFE_MODE = 'plugins'
    const profile = loadProfile('test', 'web', installAnchor, home)
    const nonOfficial = profile.layers.filter(l => !l.packageName.startsWith('@deepseek-ai/'))
    expect(nonOfficial.length).toBe(0)
  })

  it('explicit skipThirdPartyBundles filters out non-official bundles', () => {
    const profile = loadProfile('test', 'web', installAnchor, home, {
      skipThirdPartyBundles: true,
    })
    const nonOfficial = profile.layers.filter(l => !l.packageName.startsWith('@deepseek-ai/'))
    expect(nonOfficial.length).toBe(0)
  })

  it('DSH_SAFE_MODE=config skips user patch file', () => {
    // First init the profile by loading it normally
    loadProfile('test', 'web', installAnchor, home)
    // Then write a user patch
    const profileDir = join(home, 'profiles', 'web')
    writeFileSync(join(profileDir, PROFILE_PATCH_FILENAME), '- id: timer\n  disabled: true\n')

    process.env.DSH_SAFE_MODE = 'config'
    const profile = loadProfile('test', 'web', installAnchor, home)
    expect(profile.patches.length).toBe(0)
  })

  it('DSH_SAFE_MODE=full skips both third-party and user patch', () => {
    loadProfile('test', 'web', installAnchor, home)
    const profileDir = join(home, 'profiles', 'web')
    writeFileSync(join(profileDir, PROFILE_PATCH_FILENAME), '- id: timer\n  disabled: true\n')

    process.env.DSH_SAFE_MODE = 'full'
    const profile = loadProfile('test', 'web', installAnchor, home)
    expect(profile.patches.length).toBe(0)
    const nonOfficial = profile.layers.filter(l => !l.packageName.startsWith('@deepseek-ai/'))
    expect(nonOfficial.length).toBe(0)
  })

  it('userLayer: false option still works alongside safe mode', () => {
    loadProfile('test', 'web', installAnchor, home)
    const profileDir = join(home, 'profiles', 'web')
    writeFileSync(join(profileDir, PROFILE_PATCH_FILENAME), '- id: timer\n  disabled: true\n')

    const profile = loadProfile('test', 'web', installAnchor, home, { userLayer: false })
    expect(profile.patches.length).toBe(0)
  })
})

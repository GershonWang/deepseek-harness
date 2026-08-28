/**
 * Configuration-level diagnostic checks.
 * @module @deepseek-ai/dsh-doctor/checks/config
 */

import { readFileSync, existsSync, renameSync } from 'node:fs'
import { join } from 'node:path'
import * as yaml from 'js-yaml'
import { writeFileAtomic } from '@deepseek-ai/dsh-atomic-write'
import { loadOptionalPatches } from '@deepseek-ai/dsh-app-boot'
import type { DoctorCheck, CheckResult, FixResult } from '../types.js'

const cfgSettingsYaml: DoctorCheck = {
  id: 'cfg-settings-yaml',
  name: 'settings.yaml syntax and structure',
  category: 'config',
  severity: 'fatal',
  check: async (dshHome: string): Promise<CheckResult> => {
    const settingsPath = join(dshHome, 'settings.yaml')
    if (!existsSync(settingsPath)) {
      return { ok: true, message: 'No settings.yaml (will use defaults on startup)', fixable: false, suggestedLevel: 2 }
    }
    try {
      const content = readFileSync(settingsPath, 'utf8')
      const parsed = yaml.load(content)
      if (parsed !== null && typeof parsed !== 'object' && !Array.isArray(parsed)) {
        return {
          ok: false,
          message: `settings.yaml top-level value is not an object (got ${typeof parsed})`,
          fixable: true,
          suggestedLevel: 2,
        }
      }
      return { ok: true, message: 'settings.yaml is valid YAML', fixable: false, suggestedLevel: 2 }
    } catch (err) {
      return {
        ok: false,
        message: `settings.yaml is not valid YAML: ${(err as Error).message}`,
        fixable: true,
        suggestedLevel: 2,
      }
    }
  },
  fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
    const settingsPath = join(dshHome, 'settings.yaml')
    const backupPath = join(backupDir, 'settings.yaml')
    const content = readFileSync(settingsPath, 'utf8')
    await writeFileAtomic(backupPath, content, { mode: 0o600, dirMode: 0o700 })
    // Move to .doctor-bak so the system starts with default settings
    const bakPath = join(dshHome, 'settings.yaml.doctor-bak')
    renameSync(settingsPath, bakPath)
    return {
      ok: true,
      message: 'settings.yaml moved to .doctor-bak (will use defaults on next start)',
      backupPath,
    }
  },
}

const cfgUserPatch: DoctorCheck = {
  id: 'cfg-user-patch',
  name: 'User cordis.patch.yml syntax and structure',
  category: 'config',
  severity: 'fatal',
  check: async (dshHome: string): Promise<CheckResult> => {
    const profileDir = join(dshHome, 'profiles', 'web')
    const patchPath = join(profileDir, PROFILE_PATCH_FILENAME)
    if (!existsSync(patchPath)) {
      return { ok: true, message: 'No user patch file for web profile', fixable: false, suggestedLevel: 2 }
    }
    try {
      loadOptionalPatches('doctor', patchPath)
      return { ok: true, message: 'cordis.patch.yml is valid', fixable: false, suggestedLevel: 2 }
    } catch (err) {
      return {
        ok: false,
        message: `cordis.patch.yml is invalid: ${(err as Error).message}`,
        fixable: true,
        suggestedLevel: 2,
      }
    }
  },
  fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
    const patchPath = join(dshHome, 'profiles', 'web', PROFILE_PATCH_FILENAME)
    const backupPath = join(backupDir, PROFILE_PATCH_FILENAME)
    const content = readFileSync(patchPath, 'utf8')
    await writeFileAtomic(backupPath, content, { mode: 0o600, dirMode: 0o700 })
    // Rename to .disabled
    const disabledPath = join(dshHome, 'profiles', 'web', `${PROFILE_PATCH_FILENAME}.disabled`)
    renameSync(patchPath, disabledPath)
    return {
      ok: true,
      message: 'cordis.patch.yml disabled (renamed to .disabled)',
      backupPath,
    }
  },
}

// Mirrors app-boot/profile.ts — keep in sync.
const PROFILE_PATCH_FILENAME = 'cordis.patch.yml'

export const configChecks: DoctorCheck[] = [cfgSettingsYaml, cfgUserPatch]

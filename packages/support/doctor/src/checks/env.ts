/**
 * Environment-level diagnostic checks.
 * @module @deepseek-ai/dsh-doctor/checks/env
 */

import { statfs } from 'node:fs/promises'
import { readFileSync, existsSync } from 'node:fs'
import { join } from 'node:path'
import { parseEnv } from 'node:util'
import { writeFileAtomic } from '@deepseek-ai/dsh-atomic-write'
import { BOOTSTRAP_NAMES, BOOTSTRAP_PREFIXES, isBootstrapOnly } from '@deepseek-ai/dsh-app-boot'
import type { DoctorCheck, CheckResult, FixResult } from '../types.js'

const MIN_DISK_SPACE_BYTES = 50 * 1024 * 1024 // 50 MB

const envNodeVersion: DoctorCheck = {
  id: 'env-node-version',
  name: 'Node.js version',
  category: 'env',
  severity: 'fatal',
  check: async (): Promise<CheckResult> => {
    const major = Number(process.versions.node.split('.')[0])
    if (major >= 24) {
      return { ok: true, message: `Node.js ${process.versions.node}`, fixable: false, suggestedLevel: 1 }
    }
    return {
      ok: false,
      message: `Node.js version ${process.versions.node} is too old; DSH requires Node >= 24`,
      fixable: false,
      suggestedLevel: 1,
    }
  },
}

const envDiskSpace: DoctorCheck = {
  id: 'env-disk-space',
  name: 'Disk space (DSH home)',
  category: 'env',
  severity: 'error',
  check: async (dshHome: string): Promise<CheckResult> => {
    try {
      const stats = await statfs(dshHome)
      const available = Number(BigInt(stats.bavail) * BigInt(stats.bsize))
      if (available >= MIN_DISK_SPACE_BYTES) {
        return {
          ok: true,
          message: `${(available / 1024 / 1024).toFixed(1)} MB available`,
          fixable: false,
          suggestedLevel: 1,
        }
      }
      return {
        ok: false,
        message: `Only ${(available / 1024 / 1024).toFixed(1)} MB available; need at least 50 MB`,
        fixable: false,
        suggestedLevel: 1,
      }
    } catch (err) {
      return { ok: false, message: `Cannot stat ${dshHome}: ${String(err)}`, fixable: false, suggestedLevel: 1 }
    }
  },
}

const envBootstrapEnv: DoctorCheck = {
  id: 'env-bootstrap-env',
  name: '.env bootstrap-only variables',
  category: 'env',
  severity: 'fatal',
  check: async (dshHome: string): Promise<CheckResult> => {
    const envPath = join(dshHome, '.env')
    if (!existsSync(envPath)) {
      return { ok: true, message: 'No .env file in DSH home', fixable: false, suggestedLevel: 1 }
    }
    try {
      const content = readFileSync(envPath, 'utf8')
      const values = parseEnv(content) as Record<string, string>
      const bad: string[] = []
      for (const name of Object.keys(values)) {
        if (isBootstrapOnly(name)) bad.push(name)
      }
      if (bad.length === 0) {
        return { ok: true, message: '.env is clean', fixable: false, suggestedLevel: 1 }
      }
      return {
        ok: false,
        message: `.env contains bootstrap-only variables that will prevent startup: ${bad.join(', ')}`,
        detail: `These variables can only come from the inherited process environment; remove them from ~/.dsh/.env`,
        fixable: true,
        suggestedLevel: 1,
      }
    } catch (err) {
      return {
        ok: false,
        message: `Failed to read .env: ${String(err)}`,
        fixable: false,
        suggestedLevel: 1,
      }
    }
  },
  fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
    const envPath = join(dshHome, '.env')
    const backupPath = join(backupDir, '.env')
    const content = readFileSync(envPath, 'utf8')
    await writeFileAtomic(backupPath, content, { mode: 0o600, dirMode: 0o700 })
    // Comment out bootstrap-only lines
    const lines = content.split('\n')
    const cleaned = lines.map((line) => {
      const match = line.match(/^\s*([A-Z0-9_]+)\s*=/)
      if (match && isBootstrapOnly(match[1]!)) {
        return `# [doctor disabled - bootstrap-only] ${line}`
      }
      return line
    }).join('\n')
    await writeFileAtomic(envPath, cleaned, { mode: 0o600 })
    return { ok: true, message: 'Commented out bootstrap-only variables in .env', backupPath }
  },
}

export const envChecks: DoctorCheck[] = [envNodeVersion, envDiskSpace, envBootstrapEnv]

// Re-export for consumers that also want the raw utilities.
export { BOOTSTRAP_NAMES, BOOTSTRAP_PREFIXES, isBootstrapOnly }

/**
 * Diagnostic and repair framework for DeepSeek Harness installations.
 *
 * Register checks with {@link registerCheck}, then run {@link runDiagnosis}
 * to produce a report or {@link runRepair} to apply automated fixes.
 *
 * @module @deepseek-ai/dsh-doctor
 */

import { mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { resolveDshHome } from '@deepseek-ai/dsh-home-paths'
import { envChecks } from './checks/env.js'
import { configChecks } from './checks/config.js'
import { pluginChecks } from './checks/plugins.js'
import { dataChecks } from './checks/data.js'
import type {
  DoctorCheck,
  DoctorReport,
  DoctorReportCheckEntry,
  DoctorReportSummary,
  RepairLevel,
  RepairReport,
} from './types.ts'

export type {
  CheckCategory,
  CheckResult,
  DoctorCheck,
  DoctorReport,
  DoctorReportCheckEntry,
  DoctorReportSummary,
  FixResult,
  RepairAppliedEntry,
  RepairLevel,
  RepairReport,
  RepairSkippedEntry,
  Severity,
} from './types.ts'

const checks: DoctorCheck[] = []

// Auto-register built-in checks.
for (const check of envChecks) checks.push(check)
for (const check of configChecks) checks.push(check)
for (const check of pluginChecks) checks.push(check)
for (const check of dataChecks) checks.push(check)

/**
 * Register a diagnostic check.
 *
 * Checks are run in registration order by {@link runDiagnosis}.
 * @param check - the check definition to register.
 */
export function registerCheck(check: DoctorCheck): void {
  checks.push(check)
}

/**
 * Clear all registered checks.
 *
 * Test-only helper: resets the registry between test cases so suites
 * do not leak registrations into each other.
 */
export function _resetRegistry(): void {
  checks.length = 0
}

/**
 * Run all registered diagnostic checks and produce a report.
 *
 * Checks are executed concurrently. The report preserves registration order.
 * @param dshHome - optional explicit harness home path; resolves the default when omitted.
 * @returns the full diagnosis report.
 */
export async function runDiagnosis(dshHome?: string): Promise<DoctorReport> {
  const home = resolveDshHome(dshHome)
  const results = await Promise.all(checks.map(async (check): Promise<DoctorReportCheckEntry> => {
    const result = await check.check(home)
    return {
      id: check.id,
      name: check.name,
      category: check.category,
      severity: check.severity,
      result,
    }
  }))

  const summary = summarize(results)

  return {
    dshHome: home,
    generatedAt: new Date().toISOString(),
    checks: results,
    summary,
  }
}

function summarize(entries: DoctorReportCheckEntry[]): DoctorReportSummary {
  let ok = 0
  let failed = 0
  let fatal = 0
  let fixable = 0

  for (const entry of entries) {
    if (entry.result.ok) {
      ok += 1
    } else {
      failed += 1
      if (entry.severity === 'fatal') fatal += 1
      if (entry.result.fixable) fixable += 1
    }
  }

  return { total: entries.length, ok, failed, fatal, fixable }
}

/**
 * Run diagnosis, then apply repairs for fixable failures whose
 * `suggestedLevel` is at or below the requested `level`.
 *
 * A single backup directory is created for the entire repair run and
 * passed to each check's `fix` implementation so it can preserve
 * pre-repair state.
 *
 * Repairs run sequentially in registration order.
 * @param level - maximum repair effort level to authorize.
 * @param dshHome - optional explicit harness home path; resolves the default when omitted.
 * @returns the repair report listing applied and skipped repairs.
 */
export async function runRepair(level: RepairLevel, dshHome?: string): Promise<RepairReport> {
  const home = resolveDshHome(dshHome)
  const diagnosis = await runDiagnosis(home)

  const backupDir = join(home, 'backups', `doctor-${Date.now()}`)
  await mkdir(backupDir, { recursive: true })

  const applied: RepairReport['applied'] = []
  const skipped: RepairReport['skipped'] = []
  const backups: string[] = [backupDir]

  for (const entry of diagnosis.checks) {
    if (entry.result.ok) continue
    if (!entry.result.fixable) {
      skipped.push({ checkId: entry.id, reason: 'no fix available' })
      continue
    }
    if (entry.result.suggestedLevel > level) {
      skipped.push({
        checkId: entry.id,
        reason: `suggestedLevel ${entry.result.suggestedLevel} exceeds requested level ${level}`,
      })
      continue
    }

    const check = checks.find(c => c.id === entry.id)
    if (!check?.fix) {
      skipped.push({ checkId: entry.id, reason: 'fix implementation missing' })
      continue
    }

    const fixResult = await check.fix(home, backupDir)
    if (fixResult.ok) {
      applied.push({ checkId: entry.id, message: fixResult.message })
    } else {
      skipped.push({ checkId: entry.id, reason: fixResult.message })
    }
  }

  return { level, backups, applied, skipped }
}

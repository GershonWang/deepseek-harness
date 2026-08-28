/**
 * `dsh doctor` command handler: run diagnostics, optionally run repair,
 * and print results as human-readable text or JSON.
 * @module @deepseek-ai/dsh/doctor
 */

import type { DoctorReport, RepairReport } from '@deepseek-ai/dsh-doctor'
import { runDiagnosis, runRepair } from '@deepseek-ai/dsh-doctor'

interface DoctorOptions {
  repair?: number
  json: boolean
}

const severityRank: Record<string, number> = {
  fatal: 3,
  error: 2,
  warning: 1,
  info: 0,
}

function severityIcon(severity: string, ok: boolean): string {
  if (ok) return '✓'
  switch (severity) {
    case 'fatal': return '✗'
    case 'error': return '✗'
    case 'warning': return '⚠'
    default: return 'ℹ'
  }
}

function formatHuman(report: DoctorReport): string {
  const lines: string[] = []
  lines.push(`DSH Home: ${report.dshHome}`)
  lines.push(`Generated: ${report.generatedAt}`)
  lines.push('')

  const sorted = [...report.checks].sort((a, b) => {
    if (a.result.ok !== b.result.ok) return a.result.ok ? 1 : -1
    return (severityRank[b.severity] ?? 0) - (severityRank[a.severity] ?? 0)
  })

  const failed = sorted.filter(c => !c.result.ok)
  if (failed.length === 0) {
    lines.push('All checks passed ✓')
  } else {
    lines.push(`${report.summary.failed} issue(s) found (${report.summary.fixable} fixable):`)
    lines.push('')
    for (const c of failed) {
      const icon = severityIcon(c.severity, false)
      lines.push(`  ${icon} [${c.severity.toUpperCase()}] ${c.name}`)
      lines.push(`      ${c.result.message}`)
      if (c.result.detail) {
        lines.push(`      ${c.result.detail}`)
      }
      if (c.result.fixable) {
        lines.push(`      → Auto-fixable (level ${c.result.suggestedLevel})`)
      }
      lines.push('')
    }
  }

  const passed = sorted.filter(c => c.result.ok)
  if (passed.length > 0) {
    lines.push(`${report.summary.ok} check(s) passed:`)
    for (const c of passed) {
      lines.push(`  ✓ [${c.severity}] ${c.name}: ${c.result.message}`)
    }
  }

  if (report.summary.fixable > 0) {
    lines.push('')
    lines.push('Run: dsh doctor --repair [level]  (level 1 = mild, 2 = moderate, 3 = destructive)')
  }

  return lines.join('\n')
}

function formatRepairHuman(report: RepairReport): string {
  const lines: string[] = []
  lines.push(`Repair level ${report.level} complete.`)
  lines.push(`  Applied: ${report.applied.length}`)
  lines.push(`  Skipped: ${report.skipped.length}`)
  if (report.backups.length > 0) {
    lines.push(`  Backups: ${report.backups.join(', ')}`)
  }
  lines.push('')

  if (report.applied.length > 0) {
    lines.push('Applied repairs:')
    for (const a of report.applied) {
      lines.push(`  ✓ ${a.checkId}: ${a.message}`)
    }
    lines.push('')
  }

  if (report.skipped.length > 0) {
    lines.push('Skipped:')
    for (const s of report.skipped) {
      lines.push(`  - ${s.checkId}: ${s.reason}`)
    }
  }

  return lines.join('\n')
}

/**
 * Run the doctor command and return the process exit code.
 * @param options - doctor invocation options.
 * @returns process exit code (0 = all ok / repair succeeded, 1 = issues found).
 */
export async function runDoctor(options: DoctorOptions): Promise<number> {
  if (options.repair !== undefined) {
    const report = await runRepair(options.repair as 1 | 2 | 3)
    if (options.json) {
      console.log(JSON.stringify(report, null, 2))
    } else {
      console.log(formatRepairHuman(report))
    }
    return report.applied.length > 0 ? 0 : 1
  }

  const report = await runDiagnosis()
  if (options.json) {
    console.log(JSON.stringify(report, null, 2))
  } else {
    console.log(formatHuman(report))
  }
  return report.summary.fatal > 0 ? 1 : 0
}

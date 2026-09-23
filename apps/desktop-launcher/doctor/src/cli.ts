#!/usr/bin/env node
/**
 * doctor 的命令行入口。
 *
 * 为什么在这里而不是 apps/cli：doctor 是 launcher 私有的诊断框架，唯一消费者是
 * Go 壳的启动前预检与 doctor 面板。入口留在上游的 apps/cli 里会让该目录长期承载
 * fork 的接线，每次合并上游都在同一批文件上冲突。
 *
 * 支持的参数与 Go 壳实际使用的三种形态一一对应（见 internal/preflight）：
 *   --json --quick            快速预检，跳过需要真实 boot 的加载探测
 *   --json                    全量诊断
 *   --json --repair <1|2|3>   分级修复
 *
 * 退出码：诊断时 fatal 检查数大于 0 为 1，否则 0；修复时仍有跳过项为 1，否则 0。
 * 非零退出码表示"发现问题"，不是命令执行失败——Go 壳依赖这个区分。
 *
 * @module @dsh-desktop/doctor/cli
 */

import { parseArgs } from 'node:util'
import { runDiagnosis, runRepair } from './index.js'
import type { DoctorReport, RepairReport } from './types.js'

/** 命令行选项。 */
interface DoctorOptions {
  /** 修复等级；缺省表示只诊断不修复。 */
  repair?: number
  /** 输出 JSON 而非人类可读文本。 */
  json: boolean
  /** 跳过加载探测检查；透传给 runDiagnosis。 */
  quick?: boolean
}

/** 失败检查的排序权重，越大越靠前。 */
const severityRank: Record<string, number> = {
  fatal: 3,
  error: 2,
  warning: 1,
  info: 0,
}

/**
 * 取一条检查的状态图标。
 * @param severity - 检查声明的严重级。
 * @param ok - 是否通过。
 * @returns 单个图标字符。
 */
function severityIcon(severity: string, ok: boolean): string {
  if (ok) return '✓'
  switch (severity) {
    case 'fatal': return '✗'
    case 'error': return '✗'
    case 'warning': return '⚠'
    default: return 'ℹ'
  }
}

/**
 * 把诊断报告渲染成人类可读文本。失败项按严重级降序排在前面，通过项收在末尾——
 * 排查时先看失败项，通过项只是完整性证据。
 * @param report - 诊断报告。
 * @returns 多行文本。
 */
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
      lines.push(`  ${severityIcon(c.severity, false)} [${c.severity.toUpperCase()}] ${c.name}`)
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
    lines.push('Repair from the launcher, or run this entry with: --repair <level>  (level 1 = mild, 2 = moderate, 3 = destructive)')
  }

  return lines.join('\n')
}

/**
 * 把修复报告渲染成人类可读文本。
 * @param report - 修复报告。
 * @returns 多行文本。
 */
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
 * 执行一次 doctor 调用并把结果写到 stdout。
 * @param options - 命令行选项。
 * @returns 进程退出码。
 */
export async function runDoctor(options: DoctorOptions): Promise<number> {
  if (options.repair !== undefined) {
    const report = await runRepair(options.repair as 1 | 2 | 3)
    console.log(options.json ? JSON.stringify(report, null, 2) : formatRepairHuman(report))
    // 没有跳过项说明没有任何失败项需要处理（全部通过或修复已应用）——
    // "无可修"不是错误；有跳过项才表示仍有未完成的问题。
    return report.skipped.length === 0 ? 0 : 1
  }

  const report = await runDiagnosis(undefined, options.quick === true ? { quick: true } : {})
  console.log(options.json ? JSON.stringify(report, null, 2) : formatHuman(report))
  return report.summary.fatal > 0 ? 1 : 0
}

/**
 * 解析命令行并执行。参数错误时打印用法并以 2 退出——2 与"发现问题"的 1 区分开，
 * 便于 Go 壳把调用错误和诊断结论分开处理。
 * @param argv - 去掉 node 与脚本路径后的参数列表。
 * @returns 进程退出码。
 */
export async function main(argv: readonly string[]): Promise<number> {
  let parsed
  try {
    parsed = parseArgs({
      args: [...argv],
      options: {
        json: { type: 'boolean', default: false },
        quick: { type: 'boolean', default: false },
        repair: { type: 'string' },
      },
      strict: true,
    })
  } catch (error) {
    console.error(`doctor: ${String(error)}`)
    console.error('usage: doctor [--json] [--quick] [--repair <1|2|3>]')
    return 2
  }

  const rawRepair = parsed.values.repair
  let repair: number | undefined
  if (rawRepair !== undefined) {
    if (!/^[123]$/u.test(rawRepair)) {
      console.error(`doctor: --repair must be 1, 2 or 3, got ${JSON.stringify(rawRepair)}`)
      return 2
    }
    repair = Number(rawRepair)
  }

  return runDoctor({
    json: parsed.values.json,
    quick: parsed.values.quick,
    ...repair === undefined ? {} : { repair },
  })
}

process.exitCode = await main(process.argv.slice(2))

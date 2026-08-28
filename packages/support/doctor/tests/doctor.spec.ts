import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  _resetRegistry,
  registerCheck,
  runDiagnosis,
  runRepair,
} from '../src/index.ts'
import type { DoctorCheck } from '../src/types.ts'

let tempHome: string

beforeEach(async () => {
  _resetRegistry()
  tempHome = await mkdtemp(join(tmpdir(), 'dsh-doctor-test-'))
})

afterEach(async () => {
  await rm(tempHome, { recursive: true, force: true })
})

function makeCheck(overrides: Partial<DoctorCheck> & { id: string }): DoctorCheck {
  const base: DoctorCheck = {
    id: overrides.id,
    name: overrides.name ?? overrides.id,
    category: overrides.category ?? 'config',
    severity: overrides.severity ?? 'error',
    check: overrides.check ?? (async () => ({ ok: true, message: 'ok', fixable: false, suggestedLevel: 1 })),
  }
  if (overrides.fix !== undefined) {
    base.fix = overrides.fix
  }
  return base
}

describe('runDiagnosis with empty registry', () => {
  it('returns an all-ok report with zero checks', async () => {
    const report = await runDiagnosis(tempHome)

    expect(report.dshHome).toBe(tempHome)
    expect(report.checks).toEqual([])
    expect(report.summary).toEqual({
      total: 0,
      ok: 0,
      failed: 0,
      fatal: 0,
      fixable: 0,
    })
    expect(new Date(report.generatedAt).getTime()).toBeLessThanOrEqual(Date.now())
  })
})

describe('runDiagnosis with failing checks', () => {
  it('reflects a failed check in the report and summary', async () => {
    registerCheck(makeCheck({
      id: 'env-ok',
      category: 'env',
      severity: 'info',
      check: async () => ({ ok: true, message: 'environment looks good', fixable: false, suggestedLevel: 1 }),
    }))

    registerCheck(makeCheck({
      id: 'config-bad',
      category: 'config',
      severity: 'warning',
      check: async () => ({
        ok: false,
        message: 'config file is malformed',
        detail: 'parse error at line 42',
        fixable: true,
        suggestedLevel: 1,
      }),
    }))

    registerCheck(makeCheck({
      id: 'data-fatal',
      category: 'data',
      severity: 'fatal',
      check: async () => ({
        ok: false,
        message: 'database is corrupted',
        fixable: true,
        suggestedLevel: 3,
      }),
    }))

    const report = await runDiagnosis(tempHome)

    expect(report.checks).toHaveLength(3)
    expect(report.checks[0]!.id).toBe('env-ok')
    expect(report.checks[0]!.result.ok).toBe(true)
    expect(report.checks[1]!.id).toBe('config-bad')
    expect(report.checks[1]!.result.ok).toBe(false)
    expect(report.checks[1]!.result.detail).toBe('parse error at line 42')
    expect(report.checks[2]!.id).toBe('data-fatal')
    expect(report.checks[2]!.severity).toBe('fatal')

    expect(report.summary).toEqual({
      total: 3,
      ok: 1,
      failed: 2,
      fatal: 1,
      fixable: 2,
    })
  })
})

describe('runRepair', () => {
  it('applies level-1 fixes for fixable checks at suggestedLevel <= 1', async () => {
    let fixCalled = false
    registerCheck(makeCheck({
      id: 'fixable-l1',
      category: 'config',
      severity: 'warning',
      check: async () => ({
        ok: false,
        message: 'something is off',
        fixable: true,
        suggestedLevel: 1,
      }),
      fix: async (_dshHome, _backupDir) => {
        fixCalled = true
        return { ok: true, message: 'restored default config' }
      },
    }))

    const report = await runRepair(1, tempHome)

    expect(report.level).toBe(1)
    expect(report.applied).toEqual([
      { checkId: 'fixable-l1', message: 'restored default config' },
    ])
    expect(report.skipped).toEqual([])
    expect(report.backups).toHaveLength(1)
    expect(fixCalled).toBe(true)
  })

  it('skips repairs whose suggestedLevel exceeds the requested level', async () => {
    let fixCalled = false
    registerCheck(makeCheck({
      id: 'safe-check',
      category: 'env',
      severity: 'info',
      check: async () => ({ ok: true, message: 'fine', fixable: false, suggestedLevel: 1 }),
    }))

    registerCheck(makeCheck({
      id: 'level-2-fix',
      category: 'config',
      severity: 'error',
      check: async () => ({
        ok: false,
        message: 'needs a moderate fix',
        fixable: true,
        suggestedLevel: 2,
      }),
      fix: async () => {
        fixCalled = true
        return { ok: true, message: 'fixed' }
      },
    }))

    registerCheck(makeCheck({
      id: 'level-3-fix',
      category: 'data',
      severity: 'fatal',
      check: async () => ({
        ok: false,
        message: 'needs a destructive fix',
        fixable: true,
        suggestedLevel: 3,
      }),
    }))

    registerCheck(makeCheck({
      id: 'unfixable',
      category: 'plugin',
      severity: 'warning',
      check: async () => ({
        ok: false,
        message: 'cannot be auto-fixed',
        fixable: false,
        suggestedLevel: 1,
      }),
    }))

    const report = await runRepair(1, tempHome)

    expect(report.level).toBe(1)
    expect(report.applied).toEqual([])
    expect(fixCalled).toBe(false)

    const skippedIds = report.skipped.map(s => s.checkId).sort()
    expect(skippedIds).toEqual(['level-2-fix', 'level-3-fix', 'unfixable'].sort())

    const level2Skip = report.skipped.find(s => s.checkId === 'level-2-fix')
    expect(level2Skip?.reason).toContain('exceeds requested level')

    const unfixableSkip = report.skipped.find(s => s.checkId === 'unfixable')
    expect(unfixableSkip?.reason).toBe('no fix available')
  })
})

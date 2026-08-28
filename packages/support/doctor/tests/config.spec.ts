import { mkdir, mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { _resetRegistry, runDiagnosis, runRepair, registerCheck } from '../src/index.ts'
import { configChecks } from '../src/checks/config.ts'

let tempHome: string

beforeEach(async () => {
  _resetRegistry()
  tempHome = await mkdtemp(join(tmpdir(), 'dsh-doctor-cfg-'))
  for (const c of configChecks) registerCheck(c)
})

afterEach(async () => {
  await rm(tempHome, { recursive: true, force: true })
})

describe('cfg-settings-yaml', () => {
  it('passes when settings.yaml is missing', async () => {
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'cfg-settings-yaml')
    expect(c?.result.ok).toBe(true)
    expect(c?.result.message).toContain('No settings.yaml')
  })

  it('passes when settings.yaml is valid YAML object', async () => {
    await writeFile(join(tempHome, 'settings.yaml'), 'llm-deepseek:\n  model: deepseek-v4\n')
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'cfg-settings-yaml')
    expect(c?.result.ok).toBe(true)
  })

  it('fails on invalid YAML', async () => {
    await writeFile(join(tempHome, 'settings.yaml'), 'invalid: yaml: [unclosed')
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'cfg-settings-yaml')
    expect(c?.result.ok).toBe(false)
    expect(c?.result.fixable).toBe(true)
    expect(c?.result.suggestedLevel).toBe(2)
  })

  it('fails when top-level is not an object', async () => {
    await writeFile(join(tempHome, 'settings.yaml'), 'just a string\n')
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'cfg-settings-yaml')
    expect(c?.result.ok).toBe(false)
    expect(c?.result.message).toContain('not an object')
  })

  it('level-2 repair backs up and renames settings.yaml', async () => {
    await writeFile(join(tempHome, 'settings.yaml'), 'bad: yaml: [')
    const repair = await runRepair(2, tempHome)
    expect(repair.applied.some(a => a.checkId === 'cfg-settings-yaml')).toBe(true)

    const { existsSync } = await import('node:fs')
    expect(existsSync(join(tempHome, 'settings.yaml'))).toBe(false)
    expect(existsSync(join(tempHome, 'settings.yaml.doctor-bak'))).toBe(true)
  })

  it('level-1 repair does NOT touch settings.yaml (needs level 2)', async () => {
    await writeFile(join(tempHome, 'settings.yaml'), 'bad: yaml: [')
    const repair = await runRepair(1, tempHome)
    const skipped = repair.skipped.find(s => s.checkId === 'cfg-settings-yaml')
    expect(skipped).toBeDefined()

    const { existsSync } = await import('node:fs')
    expect(existsSync(join(tempHome, 'settings.yaml'))).toBe(true)
  })
})

describe('cfg-user-patch', () => {
  beforeEach(async () => {
    await mkdir(join(tempHome, 'profiles', 'web'), { recursive: true })
  })

  it('passes when no user patch exists', async () => {
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'cfg-user-patch')
    expect(c?.result.ok).toBe(true)
    expect(c?.result.message).toContain('No user patch')
  })

  it('passes when patch is valid YAML array', async () => {
    await writeFile(
      join(tempHome, 'profiles', 'web', 'cordis.patch.yml'),
      '- id: test\n  config:\n    key: value\n',
    )
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'cfg-user-patch')
    expect(c?.result.ok).toBe(true)
  })

  it('fails on invalid patch YAML', async () => {
    await writeFile(
      join(tempHome, 'profiles', 'web', 'cordis.patch.yml'),
      'not: an: array: because: nested: too: deep\n',
    )
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'cfg-user-patch')
    expect(c?.result.ok).toBe(false)
    expect(c?.result.fixable).toBe(true)
    expect(c?.result.suggestedLevel).toBe(2)
  })

  it('level-2 repair disables the patch file', async () => {
    await writeFile(
      join(tempHome, 'profiles', 'web', 'cordis.patch.yml'),
      'bad: yaml: [',
    )
    const repair = await runRepair(2, tempHome)
    expect(repair.applied.some(a => a.checkId === 'cfg-user-patch')).toBe(true)

    const { existsSync } = await import('node:fs')
    expect(existsSync(join(tempHome, 'profiles', 'web', 'cordis.patch.yml'))).toBe(false)
    expect(existsSync(join(tempHome, 'profiles', 'web', 'cordis.patch.yml.disabled'))).toBe(true)
  })
})

import { mkdtemp, writeFile, rm, mkdir } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { _resetRegistry, runDiagnosis, registerCheck } from '../src/index.ts'
import { pluginChecks } from '../src/checks/plugins.ts'

let tempHome: string

beforeEach(async () => {
  _resetRegistry()
  tempHome = await mkdtemp(join(tmpdir(), 'dsh-doctor-plugin-'))
  for (const c of pluginChecks) registerCheck(c)
})

afterEach(async () => {
  await rm(tempHome, { recursive: true, force: true })
})

describe('plugin-bundles-resolvable', () => {
  it('resolves official bundles for a fresh profile', async () => {
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'plugin-bundles-resolvable')
    expect(c?.result.ok).toBe(true)
    expect(c?.result.message).toContain('bundles resolved')
    expect(c?.result.message).toContain('official')
  })
})

describe('plugin-patch-composable', () => {
  it('composes cleanly with only official bundles', async () => {
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'plugin-patch-composable')
    expect(c?.result.ok).toBe(true)
    expect(c?.result.message).toContain('entries composed')
  })
})

describe('plugin-patch-targets', () => {
  it('passes with no user patches', async () => {
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'plugin-patch-targets')
    expect(c?.result.ok).toBe(true)
    expect(c?.result.message).toContain('No user patches')
  })

  it('catches user patches targeting removed entries', async () => {
    await mkdir(join(tempHome, 'profiles', 'web'), { recursive: true })
    await writeFile(
      join(tempHome, 'profiles', 'web', 'cordis.patch.yml'),
      '- id: some-removed-plugin\n  disabled: true\n',
    )
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'plugin-patch-targets')
    expect(c?.result.ok).toBe(false)
    expect(c?.result.message).toContain('some-removed-plugin')
  })
})

describe('plugin-third-party-list', () => {
  it('reports no third-party bundles for a fresh profile', async () => {
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'plugin-third-party-list')
    expect(c?.result.ok).toBe(true)
    expect(c?.result.message).toContain('No third-party')
  })
})

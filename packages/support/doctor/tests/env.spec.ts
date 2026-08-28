import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { _resetRegistry, runDiagnosis, runRepair, registerCheck } from '../src/index.ts'
import { envChecks } from '../src/checks/env.ts'

let tempHome: string

beforeEach(async () => {
  _resetRegistry()
  tempHome = await mkdtemp(join(tmpdir(), 'dsh-doctor-env-'))
  for (const c of envChecks) registerCheck(c)
})

afterEach(async () => {
  await rm(tempHome, { recursive: true, force: true })
})

describe('env-node-version', () => {
  it('reports current node version', async () => {
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'env-node-version')
    expect(c).toBeDefined()
    expect(c?.result.message).toContain('Node.js')
    // Current Node should always pass (we're running under >= 24 in dev)
    expect(c?.result.ok).toBe(true)
  })
})

describe('env-disk-space', () => {
  it('checks disk space of the dsh home', async () => {
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'env-disk-space')
    expect(c).toBeDefined()
    expect(c?.result.message).toContain('available')
    // Dev machines should have > 50MB
    expect(c?.result.ok).toBe(true)
  })
})

describe('env-bootstrap-env', () => {
  it('passes when no .env exists', async () => {
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'env-bootstrap-env')
    expect(c?.result.ok).toBe(true)
    expect(c?.result.message).toContain('No .env')
  })

  it('passes when .env has no bootstrap variables', async () => {
    await writeFile(join(tempHome, '.env'), 'OPENAI_API_KEY=test\nMY_CUSTOM_VAR=123\n')
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'env-bootstrap-env')
    expect(c?.result.ok).toBe(true)
  })

  it('detects bootstrap-only variables in .env', async () => {
    await writeFile(join(tempHome, '.env'), 'DEEPSEEK_API_KEY=test\nPATH=/bad/bin\n')
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'env-bootstrap-env')
    expect(c?.result.ok).toBe(false)
    expect(c?.result.message).toContain('PATH')
    expect(c?.result.fixable).toBe(true)
    expect(c?.result.suggestedLevel).toBe(1)
  })

  it('detects DSH_ prefixed variables in .env', async () => {
    await writeFile(join(tempHome, '.env'), 'DSH_HOME=/custom\n')
    const report = await runDiagnosis(tempHome)
    const c = report.checks.find(x => x.id === 'env-bootstrap-env')
    expect(c?.result.ok).toBe(false)
    expect(c?.result.message).toContain('DSH_HOME')
  })

  it('repair level 1 comments out bootstrap variables', async () => {
    await writeFile(join(tempHome, '.env'), 'DEEPSEEK_API_KEY=test\nPATH=/usr/bin\nFOO=bar\n')
    const repair = await runRepair(1, tempHome)
    expect(repair.applied.some(a => a.checkId === 'env-bootstrap-env')).toBe(true)

    const { readFileSync } = await import('node:fs')
    const content = readFileSync(join(tempHome, '.env'), 'utf8')
    expect(content).toContain('# [doctor disabled')
    expect(content).toContain('PATH=/usr/bin')
    // Non-bootstrap lines are untouched
    expect(content).toMatch(/DEEPSEEK_API_KEY=test/)
    expect(content).toMatch(/FOO=bar/)
  })
})

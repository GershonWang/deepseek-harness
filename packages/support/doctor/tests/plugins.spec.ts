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

  it('repair disables only the broken patch row and keeps the file', async () => {
    await mkdir(join(tempHome, 'profiles', 'web'), { recursive: true })
    // 坏条目：target 在基准合成中不存在。
    await writeFile(
      join(tempHome, 'profiles', 'web', 'cordis.patch.yml'),
      '- id: some-removed-plugin\n  config: { foo: bar }\n',
    )
    const pre = await runDiagnosis(tempHome)
    const preC = pre.checks.find(x => x.id === 'plugin-patch-composable')
    expect(preC?.result.ok).toBe(false)
    expect(preC?.result.fixable).toBe(true)

    // 单独调 composable 的 fix（不走 runRepair 全流程，避免 plugin-patch-targets
    // 的"删除孤儿补丁"修复在同一轮把坏条目删掉、混淆验证目标）。
    const { pluginChecks: checks } = await import('../src/checks/plugins.ts')
    const composable = checks.find(c => c.id === 'plugin-patch-composable')!
    const backupDir = join(tempHome, 'backups', 'doctor-test')
    await mkdir(backupDir, { recursive: true })
    const fixResult = await composable.fix!(tempHome, backupDir)
    expect(fixResult.ok).toBe(true)
    expect(fixResult.message).toContain('some-removed-plugin')

    // 原文件仍在（不再改名为 .disabled），坏条目被移除。
    const { existsSync, readFileSync } = await import('node:fs')
    const patchPath = join(tempHome, 'profiles', 'web', 'cordis.patch.yml')
    expect(existsSync(patchPath)).toBe(true)
    expect(existsSync(patchPath + '.disabled')).toBe(false)
    const content = readFileSync(patchPath, 'utf8')
    expect(content).not.toContain('some-removed-plugin')

    // 修复后复检：该检查应通过（坏条目已禁用）。
    const post = await runDiagnosis(tempHome)
    const postC = post.checks.find(x => x.id === 'plugin-patch-composable')
    expect(postC?.result.ok).toBe(true)
  })

  it('repair with no patch file reports nothing to fix instead of crashing', async () => {
    // 补丁文件不存在（未创建或已被禁用过）：修复应返回"无需修复"而非 ENOENT 崩溃。
    const backupDir = join(tempHome, 'backups', 'doctor-test')
    await mkdir(backupDir, { recursive: true })
    const { pluginChecks: checks } = await import('../src/checks/plugins.ts')
    const composable = checks.find(c => c.id === 'plugin-patch-composable')
    const result = await composable!.fix!(tempHome, backupDir)
    expect(result.ok).toBe(true)
    expect(result.message).toContain('无需修复')
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

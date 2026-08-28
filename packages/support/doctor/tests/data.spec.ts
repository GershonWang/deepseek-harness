/**
 * Tests for data-layer diagnostic checks: session log integrity,
 * attachment storage sanity, and the corrupt-session archive fix.
 */

import { describe, beforeEach, afterEach, it, expect } from 'vitest'
import { mkdtempSync, rmSync, writeFileSync, mkdirSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { runDiagnosis, runRepair, _resetRegistry } from '../src/index.js'

describe('data checks', () => {
  let home: string

  beforeEach(async () => {
    home = mkdtempSync(join(tmpdir(), 'dsh-doctor-data-'))
    _resetRegistry()
    // Re-register to pick up new registry state
    const { envChecks } = await import('../src/checks/env.js')
    const { configChecks } = await import('../src/checks/config.js')
    const { pluginChecks } = await import('../src/checks/plugins.js')
    const { dataChecks } = await import('../src/checks/data.js')
    const { registerCheck } = await import('../src/index.js')
    for (const c of envChecks) registerCheck(c)
    for (const c of configChecks) registerCheck(c)
    for (const c of pluginChecks) registerCheck(c)
    for (const c of dataChecks) registerCheck(c)
  })

  afterEach(() => {
    rmSync(home, { recursive: true, force: true })
  })

  function makeSession(projectDir: string, sessionId: string, content: Buffer | string): string {
    const dir = join(home, 'sessions', projectDir, sessionId)
    mkdirSync(dir, { recursive: true })
    const file = join(dir, 'session.jsonl')
    writeFileSync(file, content)
    return file
  }

  function makeZstdSession(projectDir: string, sessionId: string, buf: Buffer): string {
    const dir = join(home, 'sessions', projectDir, sessionId)
    mkdirSync(dir, { recursive: true })
    const file = join(dir, 'session.jsonl.zstd')
    writeFileSync(file, buf)
    return file
  }

  it('passes when no sessions directory exists', async () => {
    const report = await runDiagnosis(home)
    const integrity = report.checks.find(c => c.id === 'data-sessions-integrity')
    expect(integrity).toBeDefined()
    expect(integrity!.result.ok).toBe(true)
  })

  it('passes for a valid plain JSONL session', async () => {
    makeSession('projects--test--', 'sess-abc', JSON.stringify({
      type: 'session', version: 0, id: 'abc', createdAt: Date.now(), delegationDepth: 0,
    }) + '\n')

    const report = await runDiagnosis(home)
    const integrity = report.checks.find(c => c.id === 'data-sessions-integrity')!
    expect(integrity.result.ok).toBe(true)
  })

  it('detects corrupt plain JSONL session (invalid first line)', async () => {
    makeSession('projects--test--', 'sess-bad', 'this is not json {{{\n')

    const report = await runDiagnosis(home)
    const integrity = report.checks.find(c => c.id === 'data-sessions-integrity')!
    expect(integrity.result.ok).toBe(false)
    expect(integrity.result.message).toContain('1 session log(s) corrupted')
    expect(integrity.result.fixable).toBe(true)
    expect(integrity.result.suggestedLevel).toBe(2)
  })

  it('detects empty session file', async () => {
    makeSession('projects--test--', 'sess-empty', '')

    const report = await runDiagnosis(home)
    const integrity = report.checks.find(c => c.id === 'data-sessions-integrity')!
    expect(integrity.result.ok).toBe(false)
    expect(integrity.result.detail).toContain('empty file')
  })

  it('detects bad zstd magic', async () => {
    makeZstdSession('projects--test--', 'sess-bad-zstd', Buffer.from([0x00, 0x00, 0x00, 0x00, 0x01]))

    const report = await runDiagnosis(home)
    const integrity = report.checks.find(c => c.id === 'data-sessions-integrity')!
    expect(integrity.result.ok).toBe(false)
    expect(integrity.result.detail).toContain('invalid zstd magic')
  })

  it('passes for valid zstd magic (4 bytes)', async () => {
    // Just the magic number — content is not decoded; we only check magic for corruption.
    const magic = Buffer.alloc(4)
    magic.writeUInt32LE(0xFD2FB528, 0)
    makeZstdSession('projects--test--', 'sess-valid-zstd', magic)

    const report = await runDiagnosis(home)
    const integrity = report.checks.find(c => c.id === 'data-sessions-integrity')!
    expect(integrity.result.ok).toBe(true)
  })

  it('fix archives corrupt sessions to sessions/.corrupt/', async () => {
    makeSession('projects--test--', 'sess-good', JSON.stringify({
      type: 'session', version: 0, id: 'good', createdAt: Date.now(), delegationDepth: 0,
    }) + '\n')
    makeSession('projects--test--', 'sess-bad', 'corrupt\n')

    const before = await runDiagnosis(home)
    const beforeIntegrity = before.checks.find(c => c.id === 'data-sessions-integrity')!
    expect(beforeIntegrity.result.ok).toBe(false)

    const repair = await runRepair(2, home)
    expect(repair.applied.some(a => a.checkId === 'data-sessions-integrity')).toBe(true)
    expect(repair.backups.length).toBeGreaterThan(0)

    // Good session should still be there
    expect(existsSync(join(home, 'sessions', 'projects--test--', 'sess-good', 'session.jsonl'))).toBe(true)
    // Bad session should be moved to .corrupt/
    expect(existsSync(join(home, 'sessions', 'projects--test--', 'sess-bad', 'session.jsonl'))).toBe(false)
    expect(existsSync(join(home, 'sessions', '.corrupt', 'projects--test--', 'sess-bad', 'session.jsonl'))).toBe(true)

    // Re-diagnosis should pass
    const after = await runDiagnosis(home)
    const afterIntegrity = after.checks.find(c => c.id === 'data-sessions-integrity')!
    expect(afterIntegrity.result.ok).toBe(true)
  })

  it('attachment check passes without attachment dir', async () => {
    const report = await runDiagnosis(home)
    const attach = report.checks.find(c => c.id === 'data-attachments')!
    expect(attach.result.ok).toBe(true)
  })

  it('attachment check counts items', async () => {
    mkdirSync(join(home, 'attachments'), { recursive: true })
    writeFileSync(join(home, 'attachments', 'img1.png'), 'fake')
    writeFileSync(join(home, 'attachments', 'img2.png'), 'fake')

    const report = await runDiagnosis(home)
    const attach = report.checks.find(c => c.id === 'data-attachments')!
    expect(attach.result.ok).toBe(true)
    expect(attach.result.message).toContain('2 item(s)')
  })
})

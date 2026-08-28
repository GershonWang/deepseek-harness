/**
 * Data-level diagnostic checks: session log integrity, corrupt session
 * archival, and attachment storage sanity.
 * @module @deepseek-ai/dsh-doctor/checks/data
 */

import { readdirSync, readFileSync, existsSync, renameSync, mkdirSync } from 'node:fs'
import { join, relative } from 'node:path'
import { writeFileAtomic } from '@deepseek-ai/dsh-atomic-write'
import type { DoctorCheck, CheckResult, FixResult } from '../types.js'

const SESSIONS_DIR = 'sessions'
const CORRUPT_DIR = '.corrupt'

interface SessionCorruptionInfo {
  relativePath: string
  error: string
}

function* walkSessionFiles(root: string, dir: string, depth: number = 0): Generator<string> {
  if (depth > 4) return // project-dir/session-dir/session.jsonl.zstd = depth 3
  if (!existsSync(dir)) return
  const entries = readdirSync(dir, { withFileTypes: true })
  for (const entry of entries) {
    const full = join(dir, entry.name)
    if (entry.name.startsWith('.')) continue // skip .corrupt and other dotfiles
    if (entry.isDirectory()) {
      yield* walkSessionFiles(root, full, depth + 1)
    } else if (entry.name === 'session.jsonl.zstd' || entry.name === 'session.jsonl') {
      yield full
    }
  }
}

function checkSessionFile(filePath: string): string | null {
  try {
    const buf = readFileSync(filePath)
    if (buf.length === 0) return 'empty file'

    if (filePath.endsWith('.zstd')) {
      // Zstandard: first 4 bytes should be magic number 0xFD2FB528 (little-endian: 28 B5 2F FD)
      if (buf.length < 4) return 'truncated zstd header'
      const magic = buf.readUInt32LE(0)
      if (magic !== 0xFD2FB528) return `invalid zstd magic (0x${magic.toString(16)})`
      // We can't easily fully decode zstd without a dependency, but magic check
      // catches the most common corruption (zeroed file, wrong file type).
      return null
    }

    // Plain JSONL: try parsing the first line (header)
    const firstLine = buf.toString('utf8', 0, Math.min(buf.length, 2048)).split('\n')[0]
    if (!firstLine) return 'empty first line'
    try {
      const obj = JSON.parse(firstLine)
      if (obj.type !== 'session') return `first line is not a session header (type: ${JSON.stringify(obj.type)})`
      if (typeof obj.id !== 'string') return 'session header missing id'
      return null
    } catch {
      return 'first line is not valid JSON'
    }
  } catch (err) {
    return `read error: ${(err as Error).message}`
  }
}

function findCorruptSessions(dshHome: string): SessionCorruptionInfo[] {
  const root = join(dshHome, SESSIONS_DIR)
  const corrupt: SessionCorruptionInfo[] = []
  if (!existsSync(root)) return corrupt
  for (const filePath of walkSessionFiles(dshHome, root)) {
    const error = checkSessionFile(filePath)
    if (error !== null) {
      corrupt.push({
        relativePath: relative(root, filePath),
        error,
      })
    }
  }
  return corrupt
}

const dataSessionsIntegrity: DoctorCheck = {
  id: 'data-sessions-integrity',
  name: 'Session log integrity',
  category: 'data',
  severity: 'error',
  check: async (dshHome: string): Promise<CheckResult> => {
    const root = join(dshHome, SESSIONS_DIR)
    if (!existsSync(root)) {
      return {
        ok: true,
        message: 'No session directory yet',
        fixable: false,
        suggestedLevel: 2,
      }
    }
    const corrupt = findCorruptSessions(dshHome)
    if (corrupt.length === 0) {
      return {
        ok: true,
        message: 'All session logs readable',
        fixable: false,
        suggestedLevel: 2,
      }
    }
    return {
      ok: false,
      message: `${corrupt.length} session log(s) corrupted or unreadable`,
      detail: corrupt.map(c => `${c.relativePath}: ${c.error}`).join('\n'),
      fixable: true,
      suggestedLevel: 2,
    }
  },
  fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
    const root = join(dshHome, SESSIONS_DIR)
    const corruptDir = join(root, CORRUPT_DIR)
    const corrupt = findCorruptSessions(dshHome)
    if (corrupt.length === 0) {
      return { ok: true, message: 'No corrupt sessions to archive' }
    }

    // Back up the whole corrupt dir state first
    mkdirSync(corruptDir, { recursive: true })
    await writeFileAtomic(
      join(backupDir, 'corrupt-sessions.json'),
      JSON.stringify(corrupt, null, 2),
      { mode: 0o600, dirMode: 0o700 },
    )

    let moved = 0
    for (const c of corrupt) {
      const src = join(root, c.relativePath)
      if (!existsSync(src)) continue
      // Build target: same relative path under sessions/.corrupt/
      const target = join(corruptDir, c.relativePath)
      const targetDir = join(target, '..')
      mkdirSync(targetDir, { recursive: true })
      try {
        renameSync(src, target)
        // Also try to archive the parent session dir if it's now empty-ish
        const sessionDir = join(src, '..')
        const siblings = readdirSync(sessionDir).filter(n => !n.startsWith('.'))
        if (siblings.length === 0) {
          renameSync(sessionDir, join(corruptDir, relative(root, sessionDir)))
        }
        moved += 1
      } catch {
        // skip individual failures
      }
    }

    return {
      ok: true,
      message: `Archived ${moved} corrupt session(s) to sessions/${CORRUPT_DIR}/`,
      backupPath: join(backupDir, 'corrupt-sessions.json'),
    }
  },
}

const dataAttachments: DoctorCheck = {
  id: 'data-attachments',
  name: 'Attachment storage directory',
  category: 'data',
  severity: 'info',
  check: async (dshHome: string): Promise<CheckResult> => {
    const attachDir = join(dshHome, 'attachments')
    if (!existsSync(attachDir)) {
      return {
        ok: true,
        message: 'No attachment directory yet',
        fixable: false,
        suggestedLevel: 1,
      }
    }
    try {
      const entries = readdirSync(attachDir)
      const count = entries.filter(n => !n.startsWith('.')).length
      return {
        ok: true,
        message: `Attachment storage has ${count} item(s)`,
        fixable: false,
        suggestedLevel: 1,
      }
    } catch (err) {
      return {
        ok: false,
        message: `Cannot read attachment directory: ${(err as Error).message}`,
        fixable: false,
        suggestedLevel: 1,
      }
    }
  },
}

export const dataChecks: DoctorCheck[] = [dataSessionsIntegrity, dataAttachments]

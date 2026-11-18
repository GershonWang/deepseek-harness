/**
 * `spawnableLauncherPath` tests. The rewrite exists for one real failure: a
 * packaged application resolves the Landlock launcher inside `app.asar`, where
 * spawning it fails with ENOTDIR even though electron-builder wrote the same
 * file to the sibling `app.asar.unpacked` directory.
 */

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { spawnableLauncherPath } from '../src/launcher-path.ts'

/** Temp dirs created by this file, removed after each test. */
const tempDirs: string[] = []
afterEach(() => {
  for (const dir of tempDirs.splice(0)) rmSync(dir, { recursive: true, force: true })
})

/** A fresh temp dir that stands in for an Electron `resources` directory. */
function resourcesDir(): string {
  const dir = mkdtempSync(join(tmpdir(), 'dsh-launcher-path-'))
  tempDirs.push(dir)
  return dir
}

describe('spawnable Launcher path', () => {
  it('keeps an install outside an archive unchanged', () => {
    const resolved = '/opt/dsh/node_modules/@deepseek-ai/node-addon-system-linux-x64/bin/landlock-run'
    expect(spawnableLauncherPath(resolved)).toBe(resolved)
  })

  it('spawns the unpacked sibling an archive-based application provides', () => {
    const resources = resourcesDir()
    const relative = join('dsh', 'node_modules', '@deepseek-ai', 'node-addon-system-linux-x64', 'bin', 'landlock-run')
    const unpacked = join(resources, 'app.asar.unpacked', relative)
    mkdirSync(join(unpacked, '..'), { recursive: true })
    writeFileSync(unpacked, 'fixture', { mode: 0o755 })
    expect(spawnableLauncherPath(join(resources, 'app.asar', relative))).toBe(unpacked)
  })

  it('keeps the archive path when no unpacked sibling exists', () => {
    // A package that predates the unpack rule must keep today's failure instead of gaining a new one.
    const resources = resourcesDir()
    const resolved = join(resources, 'app.asar', 'dsh', 'node_modules', 'pkg', 'bin', 'landlock-run')
    expect(spawnableLauncherPath(resolved)).toBe(resolved)
  })
})

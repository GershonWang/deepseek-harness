import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { execFileSync } from 'node:child_process'
import { afterEach, describe, expect, it } from 'vitest'
import { archiveFormat, extractArchive } from '../src/toolchain/archive.ts'

const roots: string[] = []
function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-archive-'))
  roots.push(root)
  return root
}
afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
})

describe('archive format inference', () => {
  it('reads the suffix of a plain URL', () => {
    expect(archiveFormat('https://e.test/go1.27.1.linux-amd64.tar.gz')).toBe('tar.gz')
    expect(archiveFormat('https://e.test/tool.tar.xz')).toBe('tar.xz')
    expect(archiveFormat('https://e.test/tool.zip')).toBe('zip')
  })

  it('ignores a query string on a signed CDN URL', () => {
    // Signed URLs append parameters after the file name, which must not hide the suffix.
    expect(archiveFormat('https://e.test/tool.tar.gz?X-Amz-Signature=abc')).toBe('tar.gz')
    expect(archiveFormat('https://e.test/tool.zip#frag')).toBe('zip')
  })

  it('falls back to tar.gz for an unknown suffix', () => {
    expect(archiveFormat('https://e.test/download')).toBe('tar.gz')
  })
})

describe('extraction', () => {
  it('extracts a tar.gz archive and keeps the executable bit', async () => {
    const root = temporaryRoot()
    const source = join(root, 'src')
    mkdirSync(join(source, 'bin'), { recursive: true })
    writeFileSync(join(source, 'bin', 'tool'), '#!/bin/sh\necho hi\n', { mode: 0o755 })
    const archive = join(root, 'a.tar.gz')
    execFileSync('tar', ['-czf', archive, '-C', source, '.'])
    const destination = join(root, 'out')
    await extractArchive(archive, 'tar.gz', destination)
    const { statSync } = await import('node:fs')
    // The archive supplies the mode; extraction must not flatten it.
    expect(statSync(join(destination, 'bin', 'tool')).mode & 0o111).not.toBe(0)
  })

  it('extracts a zip archive', async () => {
    const root = temporaryRoot()
    const source = join(root, 'src')
    mkdirSync(source, { recursive: true })
    writeFileSync(join(source, 'file.txt'), 'content\n')
    const archive = join(root, 'a.zip')
    execFileSync('zip', ['-q', '-r', archive, '.'], { cwd: source })
    const destination = join(root, 'out')
    await extractArchive(archive, 'zip', destination)
    const { readFileSync } = await import('node:fs')
    expect(readFileSync(join(destination, 'file.txt'), 'utf8')).toBe('content\n')
  })

  it('reports a malformed archive with the tool message', async () => {
    const root = temporaryRoot()
    const archive = join(root, 'broken.tar.gz')
    writeFileSync(archive, 'not an archive')
    // Rethrowing the tool's own text beats inventing a classification.
    await expect(extractArchive(archive, 'tar.gz', join(root, 'out'))).rejects.toThrow(/extraction failed/u)
  })
})

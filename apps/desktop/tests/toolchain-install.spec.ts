import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { createServer, type Server } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { toolsLayout, type ToolVersion } from '../src/toolchain/catalog.ts'
import { installVersion, readInstallMetadata, type InstallProgress } from '../src/toolchain/install.ts'

const roots: string[] = []
const servers: Server[] = []
function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-install-'))
  roots.push(root)
  return root
}
afterEach(async () => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
  await Promise.all(servers.splice(0).map(server => new Promise<void>((resolve) => { server.close(() => { resolve() }) })))
})

/**
 * Build a tar.gz whose single top directory wraps a bin/ tree.
 * @returns the archive bytes and the hash of them.
 */
function buildArchive(): { bytes: Buffer; sha256: string } {
  const work = temporaryRoot()
  mkdirSync(join(work, 'tree', 'bin'), { recursive: true })
  writeFileSync(join(work, 'tree', 'bin', 'demo'), '#!/bin/sh\necho demo\n', { mode: 0o755 })
  const archive = join(work, 'a.tar.gz')
  execFileSync('tar', ['-czf', archive, '-C', work, 'tree'])
  const bytes = readFileSync(archive)
  return { bytes, sha256: createHash('sha256').update(bytes).digest('hex') }
}

/**
 * Serve one archive, honouring a Range header so resumption can be exercised.
 * @param bytes - archive bytes.
 * @param options.ignoreRange - pretend the server cannot resume.
 * @returns the URL and the number of requests seen.
 */
async function serveArchive(bytes: Buffer, options: { ignoreRange?: boolean } = {}): Promise<{ url: string; requests: () => number }> {
  let count = 0
  const server = createServer((request, response) => {
    count += 1
    const range = request.headers.range
    if (range !== undefined && options.ignoreRange !== true) {
      const start = Number.parseInt(range.replace(/bytes=(\d+)-/u, '$1'), 10)
      response.writeHead(206, {
        'content-type': 'application/octet-stream',
        'content-range': `bytes ${String(start)}-${String(bytes.length - 1)}/${String(bytes.length)}`,
        'content-length': String(bytes.length - start),
      })
      response.end(bytes.subarray(start))
      return
    }
    response.writeHead(200, { 'content-type': 'application/octet-stream', 'content-length': String(bytes.length) })
    response.end(bytes)
  })
  servers.push(server)
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', () => { resolve() })
  })
  const address = server.address()
  if (address === null || typeof address === 'string') throw new Error('test server did not bind')
  return { url: `http://127.0.0.1:${String(address.port)}/demo.tar.gz`, requests: () => count }
}

/** @param url - archive URL. @param sha256 - expected hash. @returns a version entry. */
function versionEntry(url: string, sha256: string): ToolVersion {
  return { version: '1.0.0', url, sha256, bin_rel: 'bin' }
}

describe('install', () => {
  it('downloads, verifies, extracts, and commits one version', async () => {
    const { bytes, sha256 } = buildArchive()
    const { url } = await serveArchive(bytes)
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    const phases: string[] = []
    const target = await installVersion(layout, 'demo', versionEntry(url, sha256), {
      onProgress: (progress) => { if (!phases.includes(progress.phase)) phases.push(progress.phase) },
    })
    expect(phases).toEqual(['downloading', 'verifying', 'extracting', 'done'])
    // A single wrapping directory is hoisted, so bin/ sits at the install root.
    expect(readFileSync(join(target, 'bin', 'demo'), 'utf8')).toContain('demo')
    expect(await readInstallMetadata(target)).toMatchObject({ id: 'demo', version: '1.0.0' })
  })

  it('reuses the cached archive on a second install', async () => {
    const { bytes, sha256 } = buildArchive()
    const { url, requests } = await serveArchive(bytes)
    const layout = toolsLayout(temporaryRoot())
    await installVersion(layout, 'demo', versionEntry(url, sha256))
    await installVersion(layout, 'demo', { ...versionEntry(url, sha256), version: '1.0.1' })
    // The hash names the cache entry, so the second install downloads nothing.
    expect(requests()).toBe(1)
  })

  it('rejects an archive whose hash does not match and leaves no partial install', async () => {
    const { bytes } = buildArchive()
    const { url } = await serveArchive(bytes)
    const layout = toolsLayout(temporaryRoot())
    await expect(installVersion(layout, 'demo', versionEntry(url, 'b'.repeat(64)))).rejects.toThrow(/sha256 mismatch/u)
    const { existsSync } = await import('node:fs')
    expect(existsSync(layout.versionDir('demo', '1.0.0'))).toBe(false)
  })

  it('resumes from an existing partial instead of downloading from zero', async () => {
    const { bytes, sha256 } = buildArchive()
    const { url, requests } = await serveArchive(bytes)
    const layout = toolsLayout(temporaryRoot())
    mkdirSync(layout.cache, { recursive: true })
    // Seed a partial holding the first half, named exactly as the installer names it.
    const { createHash } = await import('node:crypto')
    const partName = `${createHash('sha256').update(url).digest('hex')}.part`
    writeFileSync(join(layout.cache, partName), bytes.subarray(0, Math.floor(bytes.length / 2)))
    await installVersion(layout, 'demo', versionEntry(url, sha256))
    expect(requests()).toBe(1)
    expect(readFileSync(join(layout.versionDir('demo', '1.0.0'), 'bin', 'demo'), 'utf8')).toContain('demo')
  })

  it('restarts from zero when the server ignores the range request', async () => {
    const { bytes, sha256 } = buildArchive()
    const { url } = await serveArchive(bytes, { ignoreRange: true })
    const layout = toolsLayout(temporaryRoot())
    mkdirSync(layout.cache, { recursive: true })
    const { createHash } = await import('node:crypto')
    const partName = `${createHash('sha256').update(url).digest('hex')}.part`
    // Appending to a partial the server did not honor would corrupt the archive.
    writeFileSync(join(layout.cache, partName), bytes.subarray(0, Math.floor(bytes.length / 2)))
    await installVersion(layout, 'demo', versionEntry(url, sha256))
    expect(readFileSync(join(layout.versionDir('demo', '1.0.0'), 'bin', 'demo'), 'utf8')).toContain('demo')
  })

  it('stops when the caller cancels', async () => {
    const { bytes, sha256 } = buildArchive()
    const { url } = await serveArchive(bytes)
    const layout = toolsLayout(temporaryRoot())
    const controller = new AbortController()
    controller.abort()
    await expect(installVersion(layout, 'demo', versionEntry(url, sha256), {
      signal: controller.signal,
      onProgress: (progress: InstallProgress) => { if (progress.phase === 'downloading') controller.abort() },
    })).rejects.toThrow()
  })

  it('refuses a version with no pinned hash', async () => {
    const layout = toolsLayout(temporaryRoot())
    await expect(installVersion(layout, 'demo', { version: '1.0.0', url: 'https://e.test/a.tar.gz', sha256: '' }))
      .rejects.toThrow(/no sha256/u)
  })
})

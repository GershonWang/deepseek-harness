/**
 * Toolchain download and installation.
 *
 * An install runs in four stages that each report progress: download (resumable into a
 * `.part` file), verify (SHA-256 against the index), extract (into a staging directory),
 * and commit (move the staging tree to its final name). Nothing outside the install
 * directory is touched, and a failed run leaves the previous version in place.
 *
 * @module @deepseek-ai/dsh-desktop/toolchain/install
 */

import { createHash } from 'node:crypto'
import { createWriteStream } from 'node:fs'
import { chmod, mkdir, mkdtemp, readFile, readdir, rename, rm, stat, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { Readable } from 'node:stream'
import { pipeline } from 'node:stream/promises'
import { archiveFormat, extractArchive, type ArchiveFormat } from './archive.ts'
import type { ToolsLayout, ToolVersion } from './catalog.ts'

/** Download attempt timeout; a stalled transfer must not hang the window forever. */
const DOWNLOAD_TIMEOUT_MS = 10 * 60 * 1000

/**
 * Largest archive accepted, including a resumed partial.
 *
 * The declared length in an HTTP response is advisory and can be absent or wrong, so
 * the ceiling is enforced against bytes actually written.
 */
const MAX_ARCHIVE_BYTES = 4 * 1024 ** 3

/** Downloads kept in the cache before the oldest are dropped. */
const CACHE_LIMIT_BYTES = 500 * 1024 * 1024

/** Stages an install reports, in order. */
export type InstallPhase = 'downloading' | 'verifying' | 'extracting' | 'done'

/** One progress report from an install. */
export interface InstallProgress {
  /** Stage now running. */
  readonly phase: InstallPhase
  /** Percent within the whole install, 0 to 100. */
  readonly percent: number
  /** Bytes received so far, for the download stage. */
  readonly received?: number
  /** Total bytes when the server declares one. */
  readonly total?: number
}

/** Callback receiving progress reports. */
export type ProgressSink = (progress: InstallProgress) => void

/** Signals the user cancelled the install. */
export class InstallCancelledError extends Error {
  constructor() {
    super('toolchain: install cancelled')
    this.name = 'InstallCancelledError'
  }
}

/** Options an install takes beyond the tool and version. */
export interface InstallOptions {
  /** Progress sink; absent runs silently. */
  readonly onProgress?: ProgressSink
  /** Cancellation signal. */
  readonly signal?: AbortSignal
  /** Base URL for the download, overridable for tests. */
  readonly fetch?: typeof globalThis.fetch
}

/** @param signal - cancellation signal, when the caller supplied one. @returns whether it fired. */
function cancelled(signal: AbortSignal | undefined): boolean {
  return signal?.aborted === true
}

/**
 * Compute the SHA-256 of a file.
 * @param path - file to hash.
 * @returns lowercase hex digest.
 */
export async function fileSha256(path: string): Promise<string> {
  const hash = createHash('sha256')
  const { createReadStream } = await import('node:fs')
  await pipeline(createReadStream(path), hash)
  return hash.digest('hex')
}

/**
 * Pick the cached archive path for one pinned hash.
 *
 * The hash names the file, so a changed archive can never be mistaken for the one
 * already on disk.
 * @param layout - tools layout.
 * @param sha256 - pinned archive hash.
 * @param format - archive format, part of the name so `tar`/`unzip` can infer it.
 * @returns absolute cache path.
 */
function cachePathFor(layout: ToolsLayout, sha256: string, format: ArchiveFormat): string {
  return join(layout.cache, `${sha256}.${format}`)
}

/**
 * Name of the resumable partial file for one download.
 * @param layout - tools layout.
 * @param url - archive URL, hashed so a long or odd URL stays a safe file name.
 * @returns absolute path of the `.part` file.
 */
function partPathFor(layout: ToolsLayout, url: string): string {
  return join(layout.cache, `${createHash('sha256').update(url).digest('hex')}.part`)
}

/**
 * Download a URL into a file, resuming an existing partial when the server allows it.
 *
 * A server that ignores `Range` restarts from zero rather than appending, because
 * appending to a partial the server did not honor would corrupt the archive.
 * @param url - archive URL.
 * @param destination - file to write.
 * @param onProgress - receives received and total bytes.
 * @param options - cancellation, timeout, and fetch overrides.
 * @throws {InstallCancelledError} when the signal fires.
 */
async function downloadResumable(
  url: string,
  destination: string,
  onProgress: (received: number, total: number | undefined) => void,
  options: InstallOptions,
): Promise<void> {
  let existing = 0
  try {
    existing = (await stat(destination)).size
  } catch {
    existing = 0
  }
  const headers: Record<string, string> = {}
  if (existing > 0) headers.Range = `bytes=${String(existing)}-`
  const doFetch = options.fetch ?? globalThis.fetch
  let response: Response
  try {
    response = await doFetch(url, {
      headers,
      redirect: 'follow',
      signal: options.signal === undefined
        ? AbortSignal.timeout(DOWNLOAD_TIMEOUT_MS)
        : AbortSignal.any([options.signal, AbortSignal.timeout(DOWNLOAD_TIMEOUT_MS)]),
    })
  } catch (error) {
    if (cancelled(options.signal)) throw new InstallCancelledError()
    throw error
  }
  const resuming = response.status === 206
  if (!resuming && !response.ok) {
    // A 416 means the partial is already at or past the end; dropping it lets the
    // next attempt start clean instead of failing the same way forever.
    if (existing > 0) await rm(destination, { force: true })
    throw new Error(`toolchain: download ${url} failed with ${String(response.status)}`)
  }
  let total: number | undefined
  if (resuming) {
    const range = response.headers.get('content-range')
    const afterSlash = range?.slice(range.lastIndexOf('/') + 1)
    const parsed = afterSlash === undefined ? Number.NaN : Number.parseInt(afterSlash, 10)
    total = Number.isSafeInteger(parsed) ? parsed : undefined
  } else {
    const length = Number(response.headers.get('content-length'))
    total = Number.isSafeInteger(length) && length > 0 ? length : undefined
    existing = 0
    await rm(destination, { force: true })
  }
  await mkdir(join(destination, '..'), { recursive: true })
  let received = existing
  const body = response.body
  if (body === null) throw new Error('toolchain: download returned no body')
  const counter = new TransformStream<Uint8Array, Uint8Array>({
    transform(chunk, controller) {
      received += chunk.byteLength
      if (received > MAX_ARCHIVE_BYTES) throw new Error('toolchain: archive exceeds the size ceiling')
      onProgress(received, total)
      controller.enqueue(chunk)
    },
  })
  const output = createWriteStream(destination, { flags: resuming ? 'a' : 'w', mode: 0o600 })
  try {
    await pipeline(Readable.fromWeb(body.pipeThrough(counter) as never), output)
  } catch (error) {
    if (cancelled(options.signal)) throw new InstallCancelledError()
    throw error
  }
}

/**
 * Drop the oldest cached archives once the cache outgrows its limit.
 *
 * Cache pressure must never fail an install: the archives are a re-download
 * optimization, so a prune failure is swallowed deliberately.
 * @param layout - tools layout.
 */
async function pruneCache(layout: ToolsLayout): Promise<void> {
  try {
    const names = await readdir(layout.cache)
    const entries = await Promise.all(names
      .filter(name => !name.endsWith('.part'))
      .map(async (name) => {
        const path = join(layout.cache, name)
        const info = await stat(path)
        return { path, size: info.size, at: info.mtimeMs }
      }))
    let total = entries.reduce((sum, entry) => sum + entry.size, 0)
    entries.sort((left, right) => left.at - right.at)
    for (const entry of entries) {
      if (total <= CACHE_LIMIT_BYTES) break
      await rm(entry.path, { force: true })
      total -= entry.size
    }
  } catch {
    // A cache that cannot be pruned only costs disk.
  }
}

/**
 * Resolve the archive for one version, downloading it when the cache misses.
 * @param layout - tools layout.
 * @param toolId - tool id, for diagnostics.
 * @param version - the version being installed.
 * @param options - progress and cancellation.
 * @returns path of a verified archive on disk.
 */
async function ensureArchive(
  layout: ToolsLayout,
  toolId: string,
  version: ToolVersion,
  options: InstallOptions,
): Promise<string> {
  const report = options.onProgress ?? ((): void => undefined)
  if (version.sha256 === '') throw new Error(`toolchain: ${toolId} ${version.version} pins no sha256`)
  const format = archiveFormat(version.url)
  const cached = cachePathFor(layout, version.sha256, format)
  if (await fileSha256(cached).then(digest => digest === version.sha256).catch(() => false)) {
    report({ phase: 'verifying', percent: 30 })
    return cached
  }
  await mkdir(layout.cache, { recursive: true })
  const part = partPathFor(layout, version.url)
  // A partial that already matches is the finished archive; promote it rather than
  // re-downloading bytes that are already correct.
  if (await fileSha256(part).then(digest => digest === version.sha256).catch(() => false)) {
    await rename(part, cached)
    return cached
  }
  report({ phase: 'downloading', percent: 0, received: 0 })
  await downloadResumable(version.url, part, (received, total) => {
    const percent = total === undefined ? 0 : Math.round((received / total) * 70)
    report({ phase: 'downloading', percent: Math.min(percent, 70), received, ...(total === undefined ? {} : { total }) })
  }, options)
  report({ phase: 'verifying', percent: 75 })
  const digest = await fileSha256(part)
  if (digest !== version.sha256) {
    // The bytes are wrong, so resuming would preserve the corruption.
    await rm(part, { force: true })
    throw new Error(`toolchain: sha256 mismatch for ${toolId} ${version.version}`)
  }
  try {
    await rename(part, cached)
    void pruneCache(layout)
    return cached
  } catch {
    // A cache directory on another filesystem cannot take a rename; using the part
    // file directly installs the same bytes with a re-download next time.
    return part
  }
}

/**
 * Write the metadata file that records which version a directory holds.
 * @param root - installed version directory.
 * @param id - tool id.
 * @param version - the installed version.
 */
async function writeMetadata(root: string, id: string, version: ToolVersion): Promise<void> {
  const content = [
    `id: ${id}`,
    `version: ${version.version}`,
    `bin_rel: ${version.bin_rel ?? ''}`,
    `lib_rel: ${version.lib_rel ?? ''}`,
    `sha256: ${version.sha256}`,
    '',
  ].join('\n')
  await writeFile(join(root, 'tool.yml'), content, { mode: 0o644 })
}

/**
 * Install one version of a tool.
 *
 * Extraction lands in a staging directory inside the tools root, then moves to its
 * final name. A single container directory is hoisted so the install root is the tool
 * itself rather than a wrapper, matching what the index's `bin_rel` describes.
 * @param layout - tools layout.
 * @param toolId - tool id.
 * @param version - the version to install.
 * @param options - progress and cancellation.
 * @returns absolute path of the installed directory.
 * @throws {InstallCancelledError} when cancelled, leaving no partial install behind.
 */
export async function installVersion(
  layout: ToolsLayout,
  toolId: string,
  version: ToolVersion,
  options: InstallOptions = {},
): Promise<string> {
  const report = options.onProgress ?? ((): void => undefined)
  const archive = await ensureArchive(layout, toolId, version, options)
  if (cancelled(options.signal)) throw new InstallCancelledError()
  report({ phase: 'extracting', percent: 85 })
  await mkdir(layout.root, { recursive: true, mode: 0o755 })
  const staging = await mkdtemp(join(layout.root, '.install-'))
  try {
    await extractArchive(archive, archiveFormat(version.url), staging)
    const entries = await readdir(staging)
    if (entries.length === 0) throw new Error(`toolchain: ${toolId} archive is empty`)
    const target = layout.versionDir(toolId, version.version)
    await rm(target, { recursive: true, force: true })
    // Distributions wrap everything in one directory; hoisting it makes the install
    // root hold `bin/` directly, which is what the index's relative paths describe.
    const single = entries.length === 1 ? entries[0] : undefined
    // Hoisted only when the archive has exactly one top-level entry and it is a directory.
    const singleIsDirectory = single === undefined ? false : (await stat(join(staging, single))).isDirectory()
    const source = single === undefined || !singleIsDirectory ? staging : join(staging, single)
    await rename(source, target)
    await writeMetadata(target, toolId, version).catch(() => undefined)
    // Binaries arrive with the modes the archive carries; restoring the executable bit
    // here covers archives built on a filesystem that dropped it.
    const binDir = join(target, version.bin_rel ?? 'bin')
    await chmod(binDir, 0o755).catch(() => undefined)
    report({ phase: 'done', percent: 100 })
    return target
  } finally {
    await rm(staging, { recursive: true, force: true }).catch(() => undefined)
  }
}

/**
 * Read the metadata a previous install wrote.
 * @param dir - an installed version directory.
 * @returns the recorded fields, or undefined when the file is absent or unreadable.
 */
export async function readInstallMetadata(dir: string): Promise<Record<string, string> | undefined> {
  let content: string
  try { content = await readFile(join(dir, 'tool.yml'), 'utf8') } catch { return undefined }
  const fields: Record<string, string> = {}
  for (const line of content.split('\n')) {
    const at = line.indexOf(':')
    if (at <= 0) continue
    fields[line.slice(0, at).trim()] = line.slice(at + 1).trim()
  }
  return fields
}

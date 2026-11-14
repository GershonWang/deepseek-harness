/**
 * Remote tool index: fetch, cache, and fallback.
 *
 * The index is data, so a new tool or category does not wait for a release. It is
 * fetched from a URL pinned to a commit rather than a branch: the index carries each
 * archive's download URL and SHA-256, and a hash only proves the archive matches the
 * index — not that the index itself is trustworthy. Pinning moves the trust anchor from
 * "whoever controls the branch" to "this binary". The cost is that shipping an index
 * update means publishing a client too.
 *
 * A failed fetch is never fatal: an expired cache still works, and with no cache the
 * bundled index remains in effect. The caller learns which source won so the window can
 * say how fresh the list is.
 *
 * @module @deepseek-ai/dsh-desktop/toolchain/remote
 */

import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { parseToolIndex, type ToolIndex } from './catalog.ts'

/** How long a fetched index stays authoritative. */
export const INDEX_CACHE_TTL_MS = 24 * 60 * 60 * 1000

/**
 * Default index URL, read from a branch dedicated to publishing it.
 *
 * The index supplies both each archive's URL and its SHA-256, and a hash proves only that
 * an archive matches the index — not that the index is trustworthy. A branch is therefore
 * a weaker anchor than a commit: whoever can push it can replace index and hashes
 * together. It is used deliberately: a commit hash went stale the moment the index path
 * moved, and a dead URL means the tool list can never update. The exposure is contained
 * by dedicating a branch to this one file, so a change to it is a lone commit rather than
 * one hidden among feature work, and by restricting who may push there
 * (docs/toolchain-index-branch.md).
 *
 * `DSH_TOOLCHAIN_INDEX_URL` can still override it for a mirror; the environment and the
 * binary share one trust domain, so that adds no surface.
 */
const DEFAULT_INDEX_URL = 'https://raw.githubusercontent.com/GershonWang/deepseek-harness/'
  + 'toolchain-index/apps/linyaps/desktop-launcher/internal/toolchain/tools/index.json'

/** Largest index document accepted; the shipped one is about 30 KB. */
const MAX_INDEX_BYTES = 4 * 1024 * 1024

/** Fetch timeout for one index request. */
const INDEX_TIMEOUT_MS = 30 * 1000

/** Which source supplied the tools in effect. */
export type IndexSource = 'remote' | 'cache' | 'builtin'

/** Outcome of loading the index. */
export interface IndexLoad {
  /** The index in effect. */
  readonly index: ToolIndex
  /** Where it came from. */
  readonly source: IndexSource
  /** Fetch failure, present for diagnosis when a fallback was used. */
  readonly error?: Error
}

/** Inputs the loader needs. */
export interface IndexLoadOptions {
  /** Directory holding the index cache. */
  readonly cacheDir: string
  /** The index compiled into the application, used when nothing else is available. */
  readonly builtin: ToolIndex
  /** URL override; defaults to the pinned constant. */
  readonly url?: string
  /** Fetch override, for tests. */
  readonly fetch?: typeof globalThis.fetch
}

/**
 * Resolve the index URL, honouring the mirror override.
 * @returns the URL to fetch.
 */
export function indexUrl(): string {
  const override = process.env.DSH_TOOLCHAIN_INDEX_URL
  return override !== undefined && override !== '' ? override : DEFAULT_INDEX_URL
}

/** @param cacheDir - index cache directory. @returns path of the cached document. */
function cachePath(cacheDir: string): string {
  return join(cacheDir, 'index.json')
}

/** @param cacheDir - index cache directory. @returns path of the fetch timestamp. */
function metaPath(cacheDir: string): string {
  return join(cacheDir, 'index.meta')
}

/**
 * Whether the cached index is still fresh.
 *
 * A missing or unreadable timestamp counts as stale: freshness is recorded separately so
 * that a cache written before timestamps existed cannot be treated as current.
 * @param cacheDir - index cache directory.
 * @param now - current epoch milliseconds, injectable for tests.
 * @returns whether the cache may be used without a fetch.
 */
export async function isCacheFresh(cacheDir: string, now: number = Date.now()): Promise<boolean> {
  try {
    const written = Number.parseInt(await readFile(metaPath(cacheDir), 'utf8'), 10)
    if (!Number.isSafeInteger(written)) return false
    return now - written < INDEX_CACHE_TTL_MS
  } catch {
    return false
  }
}

/**
 * Read the cached index.
 * @param cacheDir - index cache directory.
 * @returns the cached index, or undefined when absent or unparsable.
 */
async function readCachedIndex(cacheDir: string): Promise<ToolIndex | undefined> {
  try {
    return parseToolIndex(JSON.parse(await readFile(cachePath(cacheDir), 'utf8')))
  } catch {
    return undefined
  }
}

/**
 * Write the index and its fetch time.
 *
 * A cache write failure must not block the fetch from taking effect, so the caller
 * treats this as best-effort.
 * @param cacheDir - index cache directory.
 * @param body - raw index document.
 * @param now - current epoch milliseconds.
 */
async function storeIndex(cacheDir: string, body: string, now: number): Promise<void> {
  await mkdir(cacheDir, { recursive: true, mode: 0o755 })
  await writeFile(cachePath(cacheDir), body, { mode: 0o644 })
  await writeFile(metaPath(cacheDir), String(now), { mode: 0o644 })
}

/**
 * Fetch and validate the index document.
 * @param url - index URL.
 * @param fetchImpl - fetch implementation.
 * @returns the parsed index and the raw body.
 * @throws when the request fails, the status is not 200, or the document is too large or invalid.
 */
async function fetchIndex(url: string, fetchImpl: typeof globalThis.fetch): Promise<{ index: ToolIndex; body: string }> {
  const response = await fetchImpl(url, { redirect: 'follow', signal: AbortSignal.timeout(INDEX_TIMEOUT_MS) })
  if (!response.ok) throw new Error(`toolchain: index ${url} returned ${String(response.status)}`)
  const body = await response.text()
  if (body.length > MAX_INDEX_BYTES) throw new Error('toolchain: index document exceeds the size ceiling')
  return { index: parseToolIndex(JSON.parse(body)), body }
}

/**
 * Load the index using the cache-then-remote-then-fallback order.
 *
 * 1. A fresh cache is used without a request, so the common start costs nothing.
 * 2. Otherwise fetch; success stores the cache and takes effect.
 * 3. A failed fetch falls back to the cache even when expired — an outdated tool list
 *    is better than none.
 * 4. With no cache at all, the bundled index stays in effect.
 * @param options - cache directory, bundled index, and overrides.
 * @returns the index in effect, its source, and any fetch error.
 */
export async function loadIndex(options: IndexLoadOptions): Promise<IndexLoad> {
  const now = Date.now()
  if (await isCacheFresh(options.cacheDir, now)) {
    const cached = await readCachedIndex(options.cacheDir)
    if (cached !== undefined) return { index: cached, source: 'cache' }
  }
  return refreshIndex(options, now)
}

/**
 * Fetch the index, ignoring cache freshness.
 *
 * Backs both the periodic refresh and the window's explicit refresh control.
 * @param options - cache directory, bundled index, and overrides.
 * @param now - current epoch milliseconds.
 * @returns the index in effect, its source, and any fetch error.
 */
export async function refreshIndex(options: IndexLoadOptions, now: number = Date.now()): Promise<IndexLoad> {
  const fetchImpl = options.fetch ?? globalThis.fetch
  try {
    const { index, body } = await fetchIndex(options.url ?? indexUrl(), fetchImpl)
    // A cache that cannot be written only costs a refetch next start.
    await storeIndex(options.cacheDir, body, now).catch(() => undefined)
    return { index, source: 'remote' }
  } catch (error) {
    const failure = error instanceof Error ? error : new Error(String(error))
    const cached = await readCachedIndex(options.cacheDir)
    if (cached !== undefined) return { index: cached, source: 'cache', error: failure }
    return { index: options.builtin, source: 'builtin', error: failure }
  }
}

/**
 * Read the bundled index that ships beside the application.
 *
 * Absent or unreadable is not an error the window should show: a first run offline still
 * needs a catalog, and a missing resource is a packaging defect that a later fetch
 * repairs. The caller receives undefined and decides what to render.
 * @param resourcesPath - Electron's `process.resourcesPath`.
 * @returns the bundled index, or undefined when the resource is missing or invalid.
 */
export async function readBundledIndex(resourcesPath: string): Promise<ToolIndex | undefined> {
  try {
    return parseToolIndex(JSON.parse(await readFile(join(resourcesPath, 'toolchain', 'index.json'), 'utf8')))
  } catch {
    return undefined
  }
}

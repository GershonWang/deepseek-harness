import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { parseToolIndex, type ToolIndex } from '../src/toolchain/catalog.ts'
import {
  INDEX_CACHE_TTL_MS,
  isCacheFresh,
  loadIndex,
  readBundledIndex,
  refreshIndex,
} from '../src/toolchain/remote.ts'

const roots: string[] = []
function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-remote-'))
  roots.push(root)
  return root
}
afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
})

/** Build a minimal valid index carrying one tool with a distinguishing name. */
function indexDocument(toolId: string): string {
  return JSON.stringify({
    version: 1,
    tools: [{
      id: toolId, name: toolId, category: 'modern-cli', description: '', provides: [], dependencies: [],
      versions: [{ version: '1.0.0', url: `https://e.test/${toolId}.tar.gz`, sha256: 'a'.repeat(64) }],
    }],
    category_labels: { 'modern-cli': '现代 CLI' },
  })
}
/** The bundled fallback index. */
const builtin: ToolIndex = parseToolIndex(JSON.parse(indexDocument('builtin-tool')) as unknown)

/** A fetch that always answers with the given body. */
function okFetch(body: string): typeof globalThis.fetch {
  return async () => new Response(body, { status: 200 })
}
/** A fetch that always fails. */
const offlineFetch: typeof globalThis.fetch = async () => { throw new Error('offline') }

describe('cache freshness', () => {
  it('treats a missing timestamp as stale', async () => {
    // Freshness is recorded separately, so a cache written before timestamps existed
    // must not be trusted as current.
    expect(await isCacheFresh(temporaryRoot())).toBe(false)
  })

  it('accepts a cache written inside the window and rejects an expired one', async () => {
    const dir = temporaryRoot()
    const now = Date.now()
    writeFileSync(join(dir, 'index.meta'), String(now))
    expect(await isCacheFresh(dir, now)).toBe(true)
    expect(await isCacheFresh(dir, now + INDEX_CACHE_TTL_MS + 1)).toBe(false)
  })
})

describe('index loading', () => {
  it('falls back to the bundled index when offline with no cache', async () => {
    const load = await loadIndex({ cacheDir: temporaryRoot(), builtin, fetch: offlineFetch })
    expect(load.source).toBe('builtin')
    expect(load.index.tools[0]?.id).toBe('builtin-tool')
    // The failure is reported for diagnosis, not treated as fatal.
    expect(load.error).toBeDefined()
  })

  it('uses a fresh cache without a request', async () => {
    const cacheDir = temporaryRoot()
    writeFileSync(join(cacheDir, 'index.json'), indexDocument('cached-tool'))
    writeFileSync(join(cacheDir, 'index.meta'), String(Date.now()))
    let called = false
    const load = await loadIndex({
      cacheDir, builtin,
      fetch: async () => { called = true; return new Response('', { status: 200 }) },
    })
    expect(load.source).toBe('cache')
    expect(load.index.tools[0]?.id).toBe('cached-tool')
    expect(called).toBe(false)
  })

  it('stores a fetched index and reports it as remote', async () => {
    const cacheDir = temporaryRoot()
    const load = await loadIndex({ cacheDir, builtin, fetch: okFetch(indexDocument('remote-tool')) })
    expect(load.source).toBe('remote')
    expect(load.index.tools[0]?.id).toBe('remote-tool')
    // The store makes the next start free.
    expect(await isCacheFresh(cacheDir)).toBe(true)
  })

  it('falls back to an expired cache rather than the bundled copy', async () => {
    const cacheDir = temporaryRoot()
    writeFileSync(join(cacheDir, 'index.json'), indexDocument('cached-tool'))
    writeFileSync(join(cacheDir, 'index.meta'), String(Date.now() - INDEX_CACHE_TTL_MS * 2))
    // An outdated tool list is still better than none.
    const load = await loadIndex({ cacheDir, builtin, fetch: offlineFetch })
    expect(load.source).toBe('cache')
    expect(load.index.tools[0]?.id).toBe('cached-tool')
  })

  it('falls back through the same order on an explicit refresh', async () => {
    const cacheDir = temporaryRoot()
    writeFileSync(join(cacheDir, 'index.json'), indexDocument('cached-tool'))
    writeFileSync(join(cacheDir, 'index.meta'), String(Date.now()))
    // A refresh ignores freshness, so an offline refresh still answers from the cache.
    const load = await refreshIndex({ cacheDir, builtin, fetch: offlineFetch })
    expect(load.source).toBe('cache')
  })

  it('rejects a non-200 response and falls back', async () => {
    const load = await loadIndex({
      cacheDir: temporaryRoot(), builtin,
      fetch: async () => new Response('', { status: 503 }),
    })
    expect(load.source).toBe('builtin')
    expect(load.error?.message).toMatch(/503/u)
  })

  it('rejects an unparsable document and falls back', async () => {
    const load = await loadIndex({ cacheDir: temporaryRoot(), builtin, fetch: okFetch('not json') })
    expect(load.source).toBe('builtin')
  })
})

describe('bundled index', () => {
  it('reads the resource beside the application', async () => {
    const resources = temporaryRoot()
    mkdirSync(join(resources, 'toolchain'), { recursive: true })
    writeFileSync(join(resources, 'toolchain', 'index.json'), indexDocument('bundled-tool'))
    const index = await readBundledIndex(resources)
    expect(index?.tools[0]?.id).toBe('bundled-tool')
  })

  it('reports nothing when the resource is missing', async () => {
    // A missing resource is a packaging defect a later fetch repairs, not a crash.
    expect(await readBundledIndex(temporaryRoot())).toBeUndefined()
  })
})

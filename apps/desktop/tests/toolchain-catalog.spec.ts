import { mkdirSync, mkdtempSync, rmSync, symlinkSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import {
  activeVersion,
  buildVersionGroups,
  formatBytes,
  isInstalled,
  isValidToolId,
  listInstalled,
  listVersions,
  parseToolIndex,
  toolsLayout,
  type Tool,
} from '../src/toolchain/catalog.ts'

const roots: string[] = []
function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-toolchain-'))
  roots.push(root)
  return root
}
/** Build one minimal tool. */
function tool(id: string, versions: Array<{ version: string; major?: string }>): Tool {
  return {
    id, name: id, category: 'language-sdk', description: '', provides: [], dependencies: [],
    versions: versions.map(entry => ({
      version: entry.version,
      url: `https://example.test/${id}-${entry.version}.tar.gz`,
      sha256: 'a'.repeat(64),
      ...(entry.major === undefined ? {} : { major: entry.major }),
    })),
  }
}
afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
})

describe('index parsing', () => {
  it('accepts a well-formed index', () => {
    const index = parseToolIndex({
      version: 1,
      tools: [{ id: 'go', name: 'Go', category: 'language-sdk', description: 'x', provides: ['go'], dependencies: [], versions: [{ version: '1.27.1', url: 'https://e.test/a', sha256: 'AA' }] }],
      category_labels: { 'language-sdk': '语言 SDK' },
    })
    expect(index.tools[0]?.id).toBe('go')
    // The hash is compared case-insensitively later, so it is normalized here.
    expect(index.tools[0]?.versions[0]?.sha256).toBe('aa')
  })

  it('rejects an id that could escape the tools directory', () => {
    // Ids become directory names and later reach remove(); a separator must fail here.
    for (const id of ['../etc', 'a/b', 'a\\b', '', '.', '..']) {
      expect(() => parseToolIndex({
        tools: [{ id, versions: [{ version: '1', url: 'https://e.test/a', sha256: 'aa' }] }],
      })).toThrow()
    }
  })

  it('rejects duplicate ids', () => {
    const entry = { id: 'go', versions: [{ version: '1', url: 'https://e.test/a', sha256: 'aa' }] }
    expect(() => parseToolIndex({ tools: [entry, entry] })).toThrow(/duplicate/u)
  })

  it('rejects a tool without versions and a version without a hash', () => {
    expect(() => parseToolIndex({ tools: [{ id: 'go', versions: [] }] })).toThrow()
    expect(() => parseToolIndex({ tools: [{ id: 'go', versions: [{ version: '1', url: 'https://e.test/a' }] }] })).toThrow()
  })

  it('treats ids that are ordinary names as usable', () => {
    for (const id of ['go', 'node', 'jdk-21', 'a.b']) expect(isValidToolId(id)).toBe(true)
  })
})

describe('version groups', () => {
  it('keeps one group per declared feature line', () => {
    const groups = buildVersionGroups(
      tool('python', [{ version: '3.14.7', major: '3.14' }, { version: '3.13.15', major: '3.13' }]),
      [], '',
    )
    expect(groups.map(group => group.major)).toEqual(['3.14', '3.13'])
  })

  it('splits a tool whose index declares separate lines', () => {
    const groups = buildVersionGroups(
      tool('jdk', [
        { version: '25.0.4.1', major: '25' }, { version: '8u504', major: '8' },
      ]), [], '',
    )
    expect(groups.map(group => group.latest)).toEqual(['25.0.4.1', '8u504'])
  })

  it('keeps an installed version the index no longer lists', () => {
    // Dropping it would leave a copy the user cannot see, switch back to, or uninstall.
    const groups = buildVersionGroups(tool('go', [{ version: '1.27.1' }]), ['1.24.0'], '')
    expect(groups.some(group => group.installed.includes('1.24.0'))).toBe(true)
  })

  it('marks the active version on the line that holds it', () => {
    const groups = buildVersionGroups(
      tool('jdk', [{ version: '21.0.12.1', major: '21' }, { version: '17.0.20.1', major: '17' }]),
      ['17.0.20.1'], '17.0.20.1',
    )
    expect(groups.find(group => group.major === '17')?.active).toBe('17.0.20.1')
    expect(groups.find(group => group.major === '21')?.active).toBe('')
  })
})

describe('installed layout', () => {
  it('lists installed versions by prefix and reads the active one from the link', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    for (const version of ['1.26.0', '1.27.1']) {
      mkdirSync(layout.versionDir('go', version), { recursive: true })
    }
    mkdirSync(join(root, 'current'), { recursive: true })
    symlinkSync(layout.versionDir('go', '1.27.1'), layout.currentLink('go'))
    expect(await listVersions(layout, 'go')).toEqual(['1.26.0', '1.27.1'])
    expect(await activeVersion(layout, 'go')).toBe('1.27.1')
    expect(await listInstalled(layout)).toEqual(['go'])
    expect(await isInstalled(layout, 'go', '1.26.0')).toBe(true)
    expect(await isInstalled(layout, 'go', '9.9.9')).toBe(false)
  })

  it('reports nothing for an empty tools root', async () => {
    const layout = toolsLayout(temporaryRoot())
    expect(await listInstalled(layout)).toEqual([])
    expect(await listVersions(layout, 'go')).toEqual([])
    expect(await activeVersion(layout, 'go')).toBe('')
  })
})

describe('size formatting', () => {
  it('renders a card size', () => {
    expect(formatBytes(70_553_950)).toBe('67 MB')
    expect(formatBytes(1024)).toBe('1 KB')
    expect(formatBytes(undefined)).toBe('')
    expect(formatBytes(0)).toBe('')
  })
})

import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { activeVersion, listVersions, toolsLayout, type Tool } from '../src/toolchain/catalog.ts'
import {
  deactivateVersion,
  fallbackVersion,
  prepareToolsRoot,
  reconcileBinLinks,
  setActiveVersion,
  uninstallVersion,
  withToolsPath,
} from '../src/toolchain/project.ts'

const roots: string[] = []
function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-project-'))
  roots.push(root)
  return root
}
afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
})

/** Build one catalog tool. */
function tool(
  id: string,
  versions: Array<{ version: string; bin_dirs?: string[]; bin_names?: Record<string, string>; lib_rel?: string }>,
): Tool {
  return {
    id, name: id, category: 'modern-cli', description: '', provides: [], dependencies: [],
    versions: versions.map(entry => ({
      version: entry.version, url: `https://e.test/${id}.tar.gz`, sha256: 'a'.repeat(64),
      ...(entry.bin_dirs === undefined ? {} : { bin_dirs: entry.bin_dirs }),
      ...(entry.bin_names === undefined ? {} : { bin_names: entry.bin_names }),
      ...(entry.lib_rel === undefined ? {} : { lib_rel: entry.lib_rel }),
    })),
  }
}
/** Point a tool's current link at one installed version, without rebuilding bin/. */
function linkCurrent(root: string, id: string, version: string): void {
  const layout = toolsLayout(root)
  mkdirSync(join(root, 'current'), { recursive: true })
  rmSync(layout.currentLink(id), { force: true })
  symlinkSync(layout.versionDir(id, version), layout.currentLink(id))
}
/** Create one installed version with the given executable files. */
/**
 * Create one installed version's files without touching the active link.
 *
 * Activation is a separate step in every case here, so a helper that also linked the
 * version would decide the active one by accident.
 */
function seedVersion(root: string, id: string, version: string, files: Record<string, number>): void {
  const dir = toolsLayout(root).versionDir(id, version)
  mkdirSync(dir, { recursive: true })
  for (const [name, mode] of Object.entries(files)) {
    const path = join(dir, name)
    mkdirSync(join(path, '..'), { recursive: true })
    writeFileSync(path, '#!/bin/sh\n', { mode })
  }
}

describe('activation', () => {
  it('activates a version and links its executables into the shared bin', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'demo', '1.0.0', { 'bin/demo': 0o755, 'bin/tool': 0o755 })
    await setActiveVersion(layout, 'demo', '1.0.0', [tool('demo', [{ version: '1.0.0' }])])
    expect(await activeVersion(layout, 'demo')).toBe('1.0.0')
    expect(readdirSync(join(root, 'bin')).sort()).toEqual(['demo', 'tool'])
    // The link must resolve, not merely exist.
    expect(statSync(join(root, 'bin', 'demo')).mode & 0o111).not.toBe(0)
  })

  it('links executables from a single-file distribution rooted at the install directory', async () => {
    // `relative(root, root)` is empty, which is a local path; treating it as an escape
    // would silently drop every distribution that ships no bin/ directory.
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'demo', '1.0.0', { demo: 0o755 })
    await setActiveVersion(layout, 'demo', '1.0.0', [tool('demo', [{ version: '1.0.0' }])])
    expect(readdirSync(join(root, 'bin'))).toEqual(['demo'])
  })

  it('honours declared bin_dirs instead of probing bin/', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'rust', '1.98.1', { 'cargo/bin/cargo': 0o755, 'rustc/bin/rustc': 0o755 })
    await setActiveVersion(layout, 'rust', '1.98.1', [tool('rust', [{ version: '1.98.1', bin_dirs: ['cargo/bin', 'rustc/bin'] }])])
    expect(readdirSync(join(root, 'bin')).sort()).toEqual(['cargo', 'rustc'])
  })

  it('renames and hides commands as the index declares', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'yq', '1.0.0', { 'bin/yq_linux_amd64': 0o755, 'bin/install.sh': 0o755 })
    await setActiveVersion(layout, 'yq', '1.0.0', [
      tool('yq', [{ version: '1.0.0', bin_names: { yq_linux_amd64: 'yq', 'install.sh': '' } }]),
    ])
    // An empty target keeps distribution install scripts off PATH.
    expect(readdirSync(join(root, 'bin'))).toEqual(['yq'])
  })

  it('skips a command name that would escape the bin directory', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'bad', '1.0.0', { 'bin/evil': 0o755 })
    linkCurrent(root, 'bad', '1.0.0')
    const result = await reconcileBinLinks(layout, [tool('bad', [{ version: '1.0.0', bin_names: { evil: '../../.bashrc' } }])])
    expect(result.commands).toEqual([])
    expect(result.problems.join(' ')).toMatch(/escapes/u)
  })

  it('refuses to activate a version that is not installed', async () => {
    const layout = toolsLayout(temporaryRoot())
    await expect(setActiveVersion(layout, 'demo', '9.9.9')).rejects.toThrow(/not installed/u)
  })

  it('refuses a tool id that could escape the tools root', async () => {
    const layout = toolsLayout(temporaryRoot())
    await expect(setActiveVersion(layout, '../evil', '1.0.0')).rejects.toThrow(/unusable/u)
  })
})

describe('deactivation', () => {
  it('stands a tool down and keeps every downloaded version on disk', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'python', '3.14.7', { 'bin/python3': 0o755 })
    const catalog = [tool('python', [{ version: '3.14.7' }])]
    await setActiveVersion(layout, 'python', '3.14.7', catalog)
    expect(await activeVersion(layout, 'python')).toBe('3.14.7')

    await deactivateVersion(layout, 'python', catalog)

    // 链接没了，命令回到随包那份；但版本目录必须在——这是"切回随包"而不是卸载。
    expect(await activeVersion(layout, 'python')).toBe('')
    expect(await listVersions(layout, 'python')).toEqual(['3.14.7'])
    // 共享 bin 里的链接也要撤掉，否则 PATH 上仍指向市场版本。
    expect(readdirSync(join(root, 'bin'))).not.toContain('python3')
  })

  it('can be activated again afterwards', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'python', '3.14.7', { 'bin/python3': 0o755 })
    const catalog = [tool('python', [{ version: '3.14.7' }])]
    await setActiveVersion(layout, 'python', '3.14.7', catalog)
    await deactivateVersion(layout, 'python', catalog)
    await setActiveVersion(layout, 'python', '3.14.7', catalog)
    expect(await activeVersion(layout, 'python')).toBe('3.14.7')
    expect(readdirSync(join(root, 'bin'))).toContain('python3')
  })

  it('is idempotent for a tool that is already inactive', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    await prepareToolsRoot(root)
    // 重复点击不该报错，也不该顺手删掉别的东西。
    await deactivateVersion(layout, 'python', [])
    await deactivateVersion(layout, 'python', [])
    expect(await activeVersion(layout, 'python')).toBe('')
  })

  it('refuses an id that is not a usable directory name', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    // id 会进 rm 路径，越界名必须挡在入口。
    await expect(deactivateVersion(layout, '../../etc', [])).rejects.toThrow('refusing unusable tool id')
  })
})

describe('uninstall', () => {
  it('promotes the remaining version and refreshes the links', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'demo', '1.0.0', { 'bin/demo': 0o755 })
    seedVersion(root, 'demo', '2.0.0', { 'bin/demo': 0o755 })
    await setActiveVersion(layout, 'demo', '2.0.0', [tool('demo', [{ version: '1.0.0' }])])
    await uninstallVersion(layout, 'demo', '2.0.0', tool('demo', [{ version: '1.0.0' }]))
    // The recommendation wins when it is still installed.
    expect(await activeVersion(layout, 'demo')).toBe('1.0.0')
    expect(await listVersions(layout, 'demo')).toEqual(['1.0.0'])
  })

  it('drops the link when the last version goes away', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'demo', '1.0.0', { 'bin/demo': 0o755 })
    await setActiveVersion(layout, 'demo', '1.0.0')
    await uninstallVersion(layout, 'demo', '1.0.0', undefined)
    expect(await activeVersion(layout, 'demo')).toBe('')
    expect(readdirSync(join(root, 'bin'))).toEqual([])
  })
})

describe('PATH entry', () => {
  it('puts the tools directory first so an installed command wins', () => {
    // A distribution copy already on PATH would otherwise shadow the installed one.
    expect(withToolsPath('/usr/bin:/bin', '/tools/bin')).toBe('/tools/bin:/usr/bin:/bin')
  })

  it('is idempotent so a repeated prepare does not stack entries', () => {
    const once = withToolsPath('/usr/bin', '/tools/bin')
    expect(withToolsPath(once, '/tools/bin')).toBe(once)
  })

  it('handles an absent PATH', () => {
    expect(withToolsPath(undefined, '/tools/bin')).toBe('/tools/bin')
    expect(withToolsPath('', '/tools/bin')).toBe('/tools/bin')
  })
})

describe('fallback selection', () => {
  it('prefers the recommended version when it remains', () => {
    expect(fallbackVersion(tool('demo', [{ version: '2.0.0' }]), ['1.0.0', '2.0.0'])).toBe('2.0.0')
  })

  it('picks the numerically highest label, not the alphabetically last', () => {
    // Alphabetical order would choose `8u504` and silently downgrade PATH's java.
    expect(fallbackVersion(undefined, ['8u504', '21.0.12.1'])).toBe('21.0.12.1')
  })

  it('reports nothing when no version remains', () => {
    expect(fallbackVersion(tool('demo', [{ version: '1.0.0' }]), [])).toBeUndefined()
  })
})

describe('repair', () => {
  it('rebuilds the entry points from the current links', async () => {
    const root = temporaryRoot()
    seedVersion(root, 'demo', '1.0.0', { 'bin/demo': 0o755 })
    linkCurrent(root, 'demo', '1.0.0')
    const { result } = await prepareToolsRoot(root, [tool('demo', [{ version: '1.0.0' }])])
    expect(result.commands).toEqual(['demo'])
    expect(result.problems).toEqual([])
  })

  it('removes a stale link whose tool is gone', async () => {
    const root = temporaryRoot()
    mkdirSync(join(root, 'bin'), { recursive: true })
    symlinkSync('/nonexistent/gone', join(root, 'bin', 'gone'))
    const result = await reconcileBinLinks(toolsLayout(root))
    expect(result.commands).toEqual([])
    expect(readdirSync(join(root, 'bin'))).toEqual([])
  })

  it('skips a current link that points outside the tools root', async () => {
    const root = temporaryRoot()
    const outside = temporaryRoot()
    mkdirSync(join(root, 'current'), { recursive: true })
    // bin/ is on PATH, so an escaping target would put arbitrary files on it.
    symlinkSync(outside, join(root, 'current', 'evil'))
    const result = await reconcileBinLinks(toolsLayout(root))
    expect(result.problems.join(' ')).toMatch(/outside the tools root/u)
  })

  it('binds a library directory for the active version', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'demo', '1.0.0', { 'bin/demo': 0o755 })
    mkdirSync(join(layout.versionDir('demo', '1.0.0'), 'lib'), { recursive: true })
    linkCurrent(root, 'demo', '1.0.0')
    const result = await reconcileBinLinks(layout, [tool('demo', [{ version: '1.0.0' }])])
    expect(result.problems).toEqual([])
    expect(readdirSync(join(root, 'lib'))).toEqual(['demo'])
  })

  it('reports a declared library directory that escapes the tool root', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'demo', '1.0.0', { 'bin/demo': 0o755 })
    linkCurrent(root, 'demo', '1.0.0')
    const result = await reconcileBinLinks(layout, [tool('demo', [{ version: '1.0.0', lib_rel: '../../etc' }])])
    expect(result.problems.join(' ')).toMatch(/library directory outside/u)
  })

  it('keeps non-executable files out of the bin directory', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    seedVersion(root, 'demo', '1.0.0', { 'bin/demo': 0o755, 'bin/README.md': 0o644 })
    linkCurrent(root, 'demo', '1.0.0')
    const result = await reconcileBinLinks(layout, [tool('demo', [{ version: '1.0.0' }])])
    expect(result.commands).toEqual(['demo'])
    expect(readFileSync(join(root, 'bin', 'demo'), 'utf8')).toContain('sh')
  })
})

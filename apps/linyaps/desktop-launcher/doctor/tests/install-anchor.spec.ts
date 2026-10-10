/**
 * 安装锚点解析的测试。
 *
 * 覆盖三条解析路径（环境变量、位置推导、回退 web-app）以及位置推导的边界：
 * 候选清单不是安装根、候选清单不可读、上溯深度上限。
 */

import { createRequire } from 'node:module'
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { INSTALL_ANCHOR_ENV, findInstallAnchorAbove, resolveInstallAnchor } from '../src/install-anchor.ts'

/** 安装根的包名；与 `apps/cli` 的 INSTALL_ANCHOR 同源。 */
const INSTALLATION_NAME = '@deepseek-ai/dsh'

/** 在目录下写一份清单。 */
async function writeManifest(dir: string, name: string): Promise<string> {
  await mkdir(dir, { recursive: true })
  const path = join(dir, 'package.json')
  await writeFile(path, JSON.stringify({ name, version: '0.0.0' }, undefined, 2) + '\n')
  return path
}

describe('findInstallAnchorAbove', () => {
  let root: string

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), 'dsh-anchor-'))
  })

  afterEach(async () => {
    await rm(root, { recursive: true, force: true })
  })

  it('finds the installation manifest at the packaged depth', async () => {
    const start = join(root, 'harness', 'doctor', 'lib', 'types')
    await mkdir(start, { recursive: true })
    const manifest = await writeManifest(join(root, 'harness'), INSTALLATION_NAME)
    expect(findInstallAnchorAbove(start)).toBe(manifest)
  })

  it('skips candidates that are other packages and keeps walking', async () => {
    const start = join(root, 'harness', 'doctor', 'lib', 'types')
    await mkdir(start, { recursive: true })
    await writeManifest(join(root, 'harness', 'doctor'), '@dsh-desktop/doctor')
    const manifest = await writeManifest(join(root, 'harness'), INSTALLATION_NAME)
    expect(findInstallAnchorAbove(start)).toBe(manifest)
  })

  it('skips an unreadable candidate and keeps walking', async () => {
    const start = join(root, 'harness', 'doctor', 'lib', 'types')
    await mkdir(start, { recursive: true })
    await mkdir(join(root, 'harness', 'doctor'), { recursive: true })
    // 半写入的清单不是安装根，也不该让整次解析失败。
    await writeFile(join(root, 'harness', 'doctor', 'package.json'), '{ "name": "')
    const manifest = await writeManifest(join(root, 'harness'), INSTALLATION_NAME)
    expect(findInstallAnchorAbove(start)).toBe(manifest)
  })

  it('stops above the search depth', async () => {
    const start = join(root, 'a', 'b', 'c', 'd')
    await mkdir(start, { recursive: true })
    await writeManifest(root, INSTALLATION_NAME)
    expect(findInstallAnchorAbove(start)).toBeUndefined()
  })

  it('returns undefined without an installation manifest above', async () => {
    const start = join(root, 'x', 'y')
    await mkdir(start, { recursive: true })
    expect(findInstallAnchorAbove(start)).toBeUndefined()
  })
})

describe('resolveInstallAnchor', () => {
  let root: string

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), 'dsh-anchor-env-'))
    vi.stubEnv(INSTALL_ANCHOR_ENV, '')
  })

  afterEach(async () => {
    vi.unstubAllEnvs()
    await rm(root, { recursive: true, force: true })
  })

  it('honours an explicit installation anchor', async () => {
    const manifest = await writeManifest(join(root, 'harness'), INSTALLATION_NAME)
    vi.stubEnv(INSTALL_ANCHOR_ENV, manifest)
    expect(resolveInstallAnchor()).toBe(manifest)
  })

  it('throws when the declared anchor is unreadable', () => {
    vi.stubEnv(INSTALL_ANCHOR_ENV, join(root, 'harness', 'package.json'))
    expect(() => resolveInstallAnchor()).toThrow(/安装锚点不可读/u)
  })

  it('throws when the declared anchor is not the installation root', async () => {
    const manifest = await writeManifest(join(root, 'harness'), '@deepseek-ai/dsh-web-app')
    vi.stubEnv(INSTALL_ANCHOR_ENV, manifest)
    expect(() => resolveInstallAnchor()).toThrow(/不是安装根/u)
  })

  it('falls back to the web-app anchor without an override', () => {
    // 开发态仓库里没有安装根（doctor 的 node_modules 只链了 workspace 包），
    // 回退到迁移前的 web-app 锚点。
    const expected = createRequire(import.meta.url).resolve('@deepseek-ai/dsh-web-app/package.json')
    expect(resolveInstallAnchor()).toBe(expected)
    expect(dirname(expected)).toContain('web-app')
  })
})

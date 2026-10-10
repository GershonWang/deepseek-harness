/**
 * bundle 作用域判定的测试。
 *
 * 用一棵临时安装树做夹具（安装根清单 + `node_modules/<包>/package.json`），
 * 不依赖仓库布局或 NODE_PATH：判定只看"能否从安装锚点解析到"。
 */

import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { isOptInBundle, optInBundles } from '../src/bundle-scope.ts'

/** 安装自带的 optional bundle，由 profile 逐个选择启用。 */
const OPTIONAL_BUNDLE = '@deepseek-ai/dsh-experimental-auto-review'

describe('bundle-scope', () => {
  let root: string
  let installAnchor: string

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), 'dsh-scope-'))
    const installationDir = join(root, 'harness')
    await mkdir(join(installationDir, 'node_modules', '@deepseek-ai', 'dsh-base'), { recursive: true })
    await writeFile(
      join(installationDir, 'node_modules', '@deepseek-ai', 'dsh-base', 'package.json'),
      JSON.stringify({ name: '@deepseek-ai/dsh-base', version: '0.0.0' }) + '\n',
    )
    installAnchor = join(installationDir, 'package.json')
    await writeFile(installAnchor, JSON.stringify({ name: '@deepseek-ai/dsh', version: '0.0.0' }) + '\n')
  })

  afterEach(async () => {
    await rm(root, { recursive: true, force: true })
  })

  it('treats a package the installation supplies as shipped', () => {
    expect(isOptInBundle('@deepseek-ai/dsh-base', installAnchor)).toBe(false)
  })

  it('treats an optional bundle the installation also ships as opt-in', () => {
    // 安装自带不等于必装：optional bundle 由 profile 选择，因此可以单独停用。
    expect(isOptInBundle(OPTIONAL_BUNDLE, installAnchor)).toBe(true)
  })

  it('treats a package only the profile has as opt-in', () => {
    expect(isOptInBundle('dsh-cost-meter', installAnchor)).toBe(true)
  })

  it('keeps the layer order of the opt-in subset', () => {
    const layers = [
      { packageName: '@deepseek-ai/dsh-base' },
      { packageName: 'dsh-cost-meter' },
      { packageName: OPTIONAL_BUNDLE },
      { packageName: 'dshmarket' },
    ]
    expect(optInBundles(installAnchor, layers).map(layer => layer.packageName))
      .toEqual(['dsh-cost-meter', OPTIONAL_BUNDLE, 'dshmarket'])
  })

  it('returns an empty subset when every layer is shipped', () => {
    expect(optInBundles(installAnchor, [{ packageName: '@deepseek-ai/dsh-base' }])).toEqual([])
  })
})

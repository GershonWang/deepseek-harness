#!/usr/bin/env node
/**
 * 为 apps/desktop-launcher/doctor 建立依赖解析链接。
 *
 * 为什么需要：doctor 迁出 packages/ 后不再是 pnpm workspace 成员
 * （pnpm-workspace.yaml 的 `apps/*` 只匹配深度 1），pnpm install 不会为它建
 * node_modules；而仓库根 node_modules 也没有它的 workspace 依赖，Node 从
 * doctor/lib/types/ 向上查找必然失败。
 *
 * 为什么按 package.json 的 name 匹配而不是按目录名推导：workspace 的目录名与
 * 包名并非总是一致——`@deepseek-ai/cordis-plugin-include` 的目录是
 * `vendor/include`，`@deepseek-ai/dsh-web-app` 的目录是 `packages/bundle/web-app`。
 * 按目录名拼路径会漏掉它们，且漏掉时只表现为运行期 ERR_MODULE_NOT_FOUND。
 *
 * 依赖清单一律取自 doctor/package.json 的 dependencies，不在本脚本里另维护一份
 * 包列表——那份列表会比 package.json 先过期。
 *
 * 幂等：已存在且指向正确目标的链接跳过，指向错误的先删后建。
 */
import {
  existsSync, lstatSync, mkdirSync, readFileSync, readdirSync, readlinkSync, rmSync, symlinkSync,
} from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const toolsDir = dirname(fileURLToPath(import.meta.url))
const launcherDir = resolve(toolsDir, '..')
const repoRoot = resolve(launcherDir, '..', '..')
const doctorDir = join(launcherDir, 'doctor')

/**
 * 列出目录下的一级子目录名；目录不存在时返回空数组。
 * @param dir - 目标目录。
 * @returns 子目录名数组。
 */
function listDirs(dir) {
  if (!existsSync(dir)) return []
  return readdirSync(dir, { withFileTypes: true })
    .filter(entry => entry.isDirectory())
    .map(entry => entry.name)
}

/**
 * 建立「包名 → 源码目录」索引。仓库的 workspace 包分布在三处：
 * `packages/<group>/<pkg>`、`vendor/<group>/<pkg>`、`vendor/<pkg>`（vendor 的
 * 顶层包没有分组层）。三处都扫，以 package.json 的 name 为准。
 * @returns 包名到目录绝对路径的映射。
 */
function buildWorkspaceIndex() {
  const index = new Map()
  const candidates = []
  for (const group of listDirs(join(repoRoot, 'packages'))) {
    for (const pkg of listDirs(join(repoRoot, 'packages', group))) {
      candidates.push(join(repoRoot, 'packages', group, pkg))
    }
  }
  for (const first of listDirs(join(repoRoot, 'vendor'))) {
    candidates.push(join(repoRoot, 'vendor', first))
    for (const second of listDirs(join(repoRoot, 'vendor', first))) {
      candidates.push(join(repoRoot, 'vendor', first, second))
    }
  }
  for (const dir of candidates) {
    const manifestPath = join(dir, 'package.json')
    if (!existsSync(manifestPath)) continue
    try {
      const name = JSON.parse(readFileSync(manifestPath, 'utf8')).name
      if (typeof name === 'string' && !index.has(name)) index.set(name, dir)
    } catch {
      // 非法 package.json 由仓库自己的门禁负责报错，这里跳过即可。
    }
  }
  return index
}

const index = buildWorkspaceIndex()
const manifest = JSON.parse(readFileSync(join(doctorDir, 'package.json'), 'utf8'))
let linked = 0
let reused = 0

for (const name of Object.keys(manifest.dependencies ?? {})) {
  // workspace 包走索引；其余（js-yaml）从仓库根 node_modules 取，那里是 pnpm 装的实体。
  const target = index.get(name) ?? join(repoRoot, 'node_modules', name)
  if (!existsSync(target)) {
    console.error(`doctor-link-deps: 依赖目标不存在 ${name} → ${target}；先跑 pnpm install`)
    process.exit(1)
  }
  const linkPath = join(doctorDir, 'node_modules', name)
  mkdirSync(dirname(linkPath), { recursive: true })
  if (existsSync(linkPath)) {
    const current = lstatSync(linkPath)
    const pointsAtTarget = current.isSymbolicLink()
      && resolve(dirname(linkPath), readlinkSync(linkPath)) === resolve(target)
    if (pointsAtTarget) {
      reused += 1
      continue
    }
    rmSync(linkPath, { recursive: true, force: true })
  }
  symlinkSync(relative(dirname(linkPath), target), linkPath, 'dir')
  linked += 1
}

console.log(`doctor-link-deps: 新建 ${linked} 个链接，复用 ${reused} 个`)

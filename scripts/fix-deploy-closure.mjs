#!/usr/bin/env node
/**
 * 修复 pnpm deploy --legacy 产出的 dsh 闭包。
 *
 * 问题：pnpm deploy --legacy 有三个缺陷：
 * 1. peer deps 不自动安装（auto-install-peers=false）
 * 2. link: 覆盖的 workspace 包变成符号链接回源码
 * 3. legacy deploy 把部分直接依赖 hoist 到源码旁而非目标
 *
 * 用法：node scripts/fix-deploy-closure.mjs <harness-dir>
 *
 * 移植自 deepseek-harness-desktop/apps/desktop/scripts/prepare-runtime.mjs
 */
import { cpSync, existsSync, lstatSync, mkdirSync, readFileSync, readdirSync, realpathSync, rmSync, statSync } from 'node:fs'
import { dirname, join, sep } from 'node:path'

const harnessDir = process.argv[2]
if (!harnessDir) {
  console.error('用法: node fix-deploy-closure.mjs <harness-dir>')
  process.exit(1)
}

// apps/cli 的 node_modules 路径（deploy root 的 workspace node_modules）
const cliNodeModules = join(import.meta.dirname, '..', 'apps', 'cli', 'node_modules')

/**
 * 递归查找 node_modules/<scope>/<name> 目录。
 */
function findNested(root, scope, name) {
  const found = []
  const visit = (dir, depth) => {
    if (depth > 4) return
    const modules = join(dir, 'node_modules')
    if (!existsSync(modules)) return
    const scoped = join(modules, scope)
    if (existsSync(scoped)) {
      for (const entry of readdirSync(scoped)) {
        const candidate = join(scoped, entry)
        if (entry === name && statSync(candidate).isDirectory()) found.push(candidate)
      }
    }
    for (const entry of readdirSync(modules)) {
      if (entry === '.bin') continue
      const child = join(modules, entry)
      if (!statSync(child).isDirectory()) continue
      if (entry.startsWith('@')) {
        for (const sub of readdirSync(child)) {
          const pkg = join(child, sub)
          if (statSync(pkg).isDirectory()) visit(pkg, depth + 1)
        }
      } else {
        visit(child, depth + 1)
      }
    }
  }
  visit(root, 0)
  return found
}

/**
 * 查找目录下的第一个符号链接。
 */
function findSymlink(directory) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    const metadata = lstatSync(path)
    if (metadata.isSymbolicLink()) return path
    if (metadata.isDirectory()) {
      const nested = findSymlink(path)
      if (nested !== undefined) return nested
    }
  }
  return undefined
}

/**
 * 还原 legacy deploy 遗漏的直接依赖。
 */
function restoreLegacyHoists(dir) {
  const manifest = JSON.parse(readFileSync(join(dir, 'package.json'), 'utf8'))
  const restored = []
  for (const dependency of Object.keys(manifest.dependencies ?? {}).sort()) {
    const destination = join(dir, 'node_modules', dependency)
    if (existsSync(destination)) continue
    const source = join(cliNodeModules, dependency)
    if (!existsSync(source)) {
      throw new Error(`fix-deploy-closure: ${dependency} absent from both ${destination} and ${source}`)
    }
    mkdirSync(dirname(destination), { recursive: true })
    const nestedNodeModules = join(source, 'node_modules')
    cpSync(source, destination, {
      recursive: true,
      dereference: true,
      filter: (path) => path !== nestedNodeModules && !path.startsWith(nestedNodeModules + sep),
    })
    restored.push(dependency)
  }
  if (restored.length > 0) {
    console.log(`fix-deploy-closure: restored legacy hoists: ${restored.join(', ')}`)
  }
}

/**
 * 实体化符号链接，删除 .bin shims。
 */
function materializeStagedLinks(dir) {
  const nodeModules = join(dir, 'node_modules')
  for (;;) {
    const link = findSymlink(nodeModules)
    if (link === undefined) break
    const segments = link.slice(nodeModules.length + 1).split(sep)
    const binIndex = segments.lastIndexOf('.bin')
    if (binIndex >= 0) {
      rmSync(join(nodeModules, ...segments.slice(0, binIndex + 1)), { recursive: true, force: true })
      continue
    }
    const source = realpathSync(link)
    const nestedNodeModules = join(source, 'node_modules')
    rmSync(link, { recursive: true, force: true })
    cpSync(source, link, {
      recursive: true,
      dereference: true,
      filter: (path) => path !== nestedNodeModules && !path.startsWith(nestedNodeModules + sep),
    })
  }
}

/**
 * 裁剪异构 prebuild 和源码树。
 */
function pruneHarness(dir) {
  // node-pty 异构 prebuild
  const prebuilds = join(dir, 'node_modules', 'node-pty', 'prebuilds')
  if (existsSync(prebuilds)) {
    const keep = `${process.platform}-${process.arch}`
    for (const entry of readdirSync(prebuilds)) {
      if (entry !== keep) rmSync(join(prebuilds, entry), { recursive: true, force: true })
    }
  }
  // mistralai 源码树
  for (const mistralaiDir of findNested(dir, '@mistralai', 'mistralai')) {
    for (const sub of ['src', 'examples', 'tests', 'packages']) {
      rmSync(join(mistralaiDir, sub), { recursive: true, force: true })
    }
  }
}

// 执行修复
restoreLegacyHoists(harnessDir)
materializeStagedLinks(harnessDir)
pruneHarness(harnessDir)
console.log('fix-deploy-closure: done')

import { globSync } from 'node:fs'
import { defineConfig } from 'tsdown'
import { typertPlugin } from './packages/typert/generator/lib/types/tsdown-plugin.js'
import { WORKSPACE_TYPERT_FACES } from './scripts/typert-faces.ts'
import { workspacePackageGlobs } from './scripts/workspace-package-globs.ts'

function isBuildFaceClient(value: unknown): boolean {
  if (value === undefined || value === 'host') return false
  if (value === 'client') return true
  throw new Error(`tsdown: --env.DSH_BUILD_FACE must be host or client, received ${String(value)}`)
}

/**
 * The ordinary workspace build consumes JavaScript emitted by the Host
 * TypeScript project and runs Typert. The Client pass selects packages that
 * declare a browser bundle and lets their package-local configs emit both
 * their Node loader entry and browser artifact. `apps/desktop` bundles after
 * this pass (root package.json `build:lib:host`): its main bundle inlines
 * workspace devDependencies from their lib/ output, and tsdown builds
 * workspace members concurrently without ordering them.
 */
export default defineConfig(({ env }) => {
  const client = isBuildFaceClient(env?.DSH_BUILD_FACE)
  // workspace 成员由实际存在的 package.json 推导，因此上游重命名或删除包后残留的
  // 空壳目录（git 只删文件、不删目录）不会成为成员 —— 它没有入口产物，解析必然失败，
  // 且 tsdown 用 empathic 向上查找 package.json 会把错误伪装成根清单的
  // `Cannot find entry`。同理，显式 glob 也不会连带纳入 apps/web、benchmarks、
  // website、native 与 python（tsdown 的 auto/true 会）。Node 程序集单独追加。
  const packages = workspacePackageGlobs(globSync(['vendor/*/package.json', 'packages/*/*/package.json'], { cwd: import.meta.dirname }))
  return {
    workspace: [...packages, 'apps/cli', ...client ? [] : ['apps/desktop-host']],
    // Host face 用这条 glob 作为「没有自己 tsdown.config.ts 的包」的默认入口，打包到
    // 各包的 lib/ 生成 lib/index.js 等文件（Node.js exports 路径依赖它们）；Client
    // face 传空 entry，让这批包被 tsdown 的 workspace 过滤器跳过，改由包内构建产物。
    entry: client ? '' : ['lib/types/{index,startup}.js'],
    outDir: 'lib',
    format: ['esm'],
    platform: 'node',
    target: 'es2024',
    fixedExtension: false,
    dts: false,
    clean: false,
    plugins: client ? [] : [typertPlugin({ mode: 'workspace', faces: WORKSPACE_TYPERT_FACES })],
  }
})

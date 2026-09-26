import { existsSync, globSync } from 'node:fs'
import { join, sep } from 'node:path'
import { defineConfig } from 'tsdown'
import { typertPlugin } from './packages/typert/generator/lib/types/tsdown-plugin.js'

function isBuildFaceClient(value: unknown): boolean {
  if (value === undefined || value === 'host') return false
  if (value === 'client') return true
  throw new Error(`tsdown: --env.DSH_BUILD_FACE must be host or client, received ${String(value)}`)
}

/**
 * tsdown 的默认 workspace 排除项。显式提供 exclude 时 tsdown 会整体替换这些默认值，
 * 因此必须原样重述，否则 include 会递归匹配到 node_modules。
 */
const defaultWorkspaceExcludes = ['**/node_modules/**', '**/dist/**', '**/test?(s)/**', '**/t?(e)mp/**']

/**
 * include 通配命中、但自己不拥有 package.json 的目录，转换成 `相对路径/**` 排除项。
 *
 * tsdown 用 onlyDirectories 展开 include，于是上游重命名或删除包后残留的空壳目录
 * （git 只删文件、不删目录）也会成为 workspace 成员：它继承本配置的 entry 却没有入口
 * 产物，解析必然失败；而 tsdown 用 empathic 向上查找 package.json，空壳最终命中的是
 * 仓库根清单，报错因此伪装成 `[@deepseek-ai/dsh-root] Cannot find entry`，把排查引向
 * 根本不需要存在的根 lib/（上游 rolldown/tsdown#1067 与 #1069 各自覆盖诊断与修法，
 * 至今均未合并）。排除之后，workspace 成员必须先是一个包。
 * @param include - workspace 的 include 通配；排除项由它派生，避免目录范围写两份。
 * @returns 相对仓库根的排除项。
 */
function directoriesWithoutManifest(include: readonly string[]): string[] {
  return globSync(include, { withFileTypes: true })
    .filter(entry => entry.isDirectory() && !existsSync(join(entry.parentPath, entry.name, 'package.json')))
    .map(entry => `${join(entry.parentPath, entry.name).split(sep).join('/')}/**`)
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
  // 显式 include 只覆盖 vendor 下的 Cordis、TypeScript 包树与 Node 程序集；tsdown 的
  // auto/true 会连带纳入 apps/web、benchmarks、website、native 与 python。
  const include = client
    ? ['vendor/*', 'packages/*/*', 'apps/cli']
    : ['vendor/*', 'packages/*/*', 'apps/cli', 'apps/desktop-host']
  return {
    workspace: {
      include,
      exclude: [...defaultWorkspaceExcludes, ...directoriesWithoutManifest(include)],
    },
    // Host face 用这条 glob 作为「没有自己 tsdown.config.ts 的包」的默认入口，打包到
    // 各包的 lib/ 生成 lib/index.js 等文件（Node.js exports 路径依赖它们）；Client
    // face 传空 entry，让这批包被 tsdown 的 workspace 过滤器跳过，改由包内构建产物。
    entry: client ? '' : ['lib/types/{index,invariant,startup}.js'],
    outDir: 'lib',
    format: ['esm'],
    platform: 'node',
    target: 'es2024',
    fixedExtension: false,
    dts: false,
    clean: false,
    plugins: client ? [] : [typertPlugin({ mode: 'workspace', faces: ['host'] })],
  }
})

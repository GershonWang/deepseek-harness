/**
 * 把 pnpm deploy 生成的部署根清单从「源码形态」还原为「发布形态」。
 *
 * 为什么需要这一步：dsh 的按需官方 bundle 安装器靠两个特征判断当前安装是不是源码
 * checkout（packages/boot/plugin-manager/src/official-install-target.ts 的 `source`）：
 * 部署根名为 `@deepseek-ai/dsh`，且它的 dependencies 里存在 `workspace:` 协议值。
 * 一旦判为源码安装，安装器就要求安装目录是一份完整工作区（`<root>/apps/cli/package.json`
 * 与根清单名为 `@deepseek-ai/dsh-root`），否则抛出
 * 「requires its complete workspace checkout」；而 `pnpm deploy --legacy` 的产物恰好
 * 同时保留了这个名字与全部 `workspace:` 协议，于是玲珑包被误判成源码 checkout，
 * 「Codex / Claude Code 子智能体」这类按需 bundle 必然装不上。本步把协议值替换为
 * 部署目录内该包的实际版本，使安装器回到它的正常分支——按 `包名@版本` 从 registry 安装。
 *
 * 为什么这样做是安全的：发布产物本就不该带 `workspace:` 协议（上游
 * packages/util/plugin-market-index 对协议 range 直接判非法），官方 Electron 桌面端的
 * 产物也早已把依赖解析成具体版本（`workspace:` 计数为 0）。这里只改依赖的**值**、
 * 不改**键名**，而 plugin-manager 其它读取安装根清单的地方（已装 bundle 判定、bundle
 * 名单）全部基于键名；运行时解析走 node_modules 目录树，也不读依赖声明。
 *
 * 用法：node apps/linyaps/desktop-launcher/tools/normalize-deploy-manifest.mjs <harness-dir>
 */
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

/** 可能出现依赖协议的清单字段；部署产物的 dependencies 与 devDependencies 都带协议值。 */
const DEPENDENCY_FIELDS = ['dependencies', 'devDependencies', 'peerDependencies', 'optionalDependencies']

const harnessDir = process.argv[2]
if (!harnessDir) {
  console.error('用法: node apps/linyaps/desktop-launcher/tools/normalize-deploy-manifest.mjs <harness-dir>')
  process.exit(1)
}

const manifestPath = join(harnessDir, 'package.json')
if (!existsSync(manifestPath)) {
  console.error(`normalize-deploy-manifest: 部署根清单不存在: ${manifestPath}`)
  process.exit(1)
}
const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'))

/**
 * 读部署目录内某包的实际版本。
 * 版本只从 `node_modules/<name>/package.json` 取，不去猜「仓库当前版本」——部署产物才是
 * 运行时真正加载的那份，二者在分支切换或部分注入后可能不一致。
 * @param name - 依赖的包名，支持 scope。
 * @returns 具体版本号；包缺失或清单里没有可用版本时返回 undefined。
 */
function installedVersion(name) {
  const file = join(harnessDir, 'node_modules', name, 'package.json')
  if (!existsSync(file)) return undefined
  const version = JSON.parse(readFileSync(file, 'utf8')).version
  return typeof version === 'string' && version.length > 0 ? version : undefined
}

/** 协议值才算源码形态；具体版本、tag、file:、link: 一律不动。 */
function isWorkspaceRange(value) {
  return typeof value === 'string' && value.startsWith('workspace:')
}

/**
 * 递归收集清单里所有 `workspace:` 协议值及其位置。
 * 断言阶段用它扫描**全部**对象字段，而不只看上面列出的四个依赖字段：pnpm 或上游若把
 * 依赖挪到别的字段，这里会立刻以路径点名，而不是等到安装按需 bundle 时才发现。
 * @param node - 待扫描的清单片段。
 * @param path - 当前片段在清单里的路径，用于报错定位。
 * @returns 形如 `dependencies.@deepseek-ai/dsh-x=workspace:*` 的条目。
 */
function collectWorkspaceRanges(node, path = '') {
  if (Array.isArray(node)) return node.flatMap((item, index) => collectWorkspaceRanges(item, `${path}[${index}]`))
  if (node !== null && typeof node === 'object') {
    return Object.entries(node).flatMap(([key, value]) => collectWorkspaceRanges(value, path ? `${path}.${key}` : key))
  }
  return isWorkspaceRange(node) ? [`${path}=${node}`] : []
}

const resolved = []
for (const field of DEPENDENCY_FIELDS) {
  const entries = manifest[field]
  if (entries === null || typeof entries !== 'object') continue
  for (const [name, range] of Object.entries(entries)) {
    if (!isWorkspaceRange(range)) continue
    const version = installedVersion(name)
    if (version === undefined) {
      // 缺版本时不退让、不猜：这里静默跳过会让产物继续带协议值，安装按需 bundle 时才炸，
      // 而那时的现场只剩「requires its complete workspace checkout」，看不出是缺包。
      console.error(`normalize-deploy-manifest: ${field} 的 ${name} 声明为 ${range}，`
        + `但 ${join('node_modules', name)} 读不到版本；请先确认 deploy 闭包完整`)
      process.exit(1)
    }
    entries[name] = version
    resolved.push(`${name}@${version}`)
  }
}

if (resolved.length > 0) {
  writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`)
  console.log(`normalize-deploy-manifest: 已把 ${resolved.length} 项 workspace: 协议解析为具体版本`)
}

// 写回后重新扫描全清单，确保没有残留：残留意味着有字段不在上述四个依赖字段里，
// 安装器的源码判定仍会命中，问题只是被推迟到用户点「安装」的那一刻。
const leftovers = collectWorkspaceRanges(JSON.parse(readFileSync(manifestPath, 'utf8')))
if (leftovers.length > 0) {
  console.error('normalize-deploy-manifest: 清单仍残留 workspace: 协议，安装器会把本安装误判为源码 checkout：')
  for (const item of leftovers) console.error(`  - ${item}`)
  process.exit(1)
}

console.log('normalize-deploy-manifest: done')

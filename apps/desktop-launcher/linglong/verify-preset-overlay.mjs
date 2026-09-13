#!/usr/bin/env node
/**
 * 校验打包 overlay 与上游 standard 预设的一致性，并确认 persona 配置能被随包的
 * `@deepseek-ai/dsh-persona` schema 解析。
 *
 * 为什么需要它：`harness-overlay/agent-presets/standard/agent.cordis.yml` 是上游
 * `packages/preset/agent-presets/presets/standard/agent.cordis.yml` 的整文件副本，
 * 由 linglong.yaml 覆盖进包内 shipped root。副本一旦落后，打包会话就会静默地少
 * 挂插件——2026-09-06 上游把 persona 的 `text` 改成 `prefix`/`suffix` 后，副本
 * 同时丢掉 command-goal 与 present 两行也没人发现。另一条防线是 schema 断言：
 * 组合里的 persona 配置若与随包插件不兼容，cordis 的 resolveConfig 会在挂载时抛
 * ValidationError，而那是运行期、在用户机器上。
 *
 * 用法：
 *   node apps/desktop-launcher/linglong/verify-preset-overlay.mjs [源预设] [overlay]
 * 两个位置参数可省略，默认取仓库内的规范路径（自测脚本靠它指向临时副本）。
 */
import { existsSync, readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const ROOT = resolve(HERE, '..', '..', '..')
const require = createRequire(join(ROOT, 'package.json'))
const yaml = require('js-yaml')

const DEFAULT_SOURCE = join(ROOT, 'packages/preset/agent-presets/presets/standard/agent.cordis.yml')
const DEFAULT_OVERLAY = join(HERE, 'harness-overlay/agent-presets/standard/agent.cordis.yml')
const TOOLS_MANIFEST = join(HERE, 'tools.yaml')

/** 组合里出现 `!!js` 表达式，js-yaml 需要显式声明该标签才能解析整份文档。 */
const JS_EXPRESSION_TAG = new yaml.Type('tag:yaml.org,2002:js', { kind: 'scalar', resolve: () => true })
const COMPOSITION_SCHEMA = yaml.DEFAULT_SCHEMA.extend([JS_EXPRESSION_TAG])

/** 段落必须认领的容器事实，用于确认追加的那段没有被整体删掉或改写。 */
const REQUIRED_MARKER = 'Linglong sandbox container'

const failures = []
const fail = message => failures.push(message)

/** 读取并解析一份预设组合，解析失败按检查项失败处理而不是抛出。 */
function readComposition(file, label) {
  if (!existsSync(file)) {
    fail(`${label}不存在：${file}`)
    return undefined
  }
  try {
    const rows = yaml.load(readFileSync(file, 'utf8'), { schema: COMPOSITION_SCHEMA })
    if (!Array.isArray(rows)) {
      fail(`${label}顶层不是数组：${file}`)
      return undefined
    }
    return rows
  } catch (error) {
    fail(`${label}解析失败：${error.message.split('\n')[0]}`)
    return undefined
  }
}

/** 定位 persona 行；组合必须恰好有一行，否则后续断言没有意义。 */
function personaOf(rows, label) {
  const matches = rows.filter(row => row !== null && typeof row === 'object' && row.id === 'persona')
  if (matches.length !== 1) {
    fail(`${label}的 persona 行有 ${matches.length} 条，预期 1 条`)
    return undefined
  }
  return matches[0]
}

/**
 * 解析随包（或仓库内已构建）的 `@deepseek-ai/dsh-persona`，返回其 Config schema。
 * 打包闭包优先，因为它才是真正随用户分发的那份；缺失时回退到仓库构建产物，
 * 两者都没有则说明还没 prepare/build，属于调用方的错误。
 */
async function loadPersonaSchema() {
  const candidates = [
    join(ROOT, 'apps/desktop-launcher/linglong/stage/harness/node_modules/@deepseek-ai/dsh-persona/lib/index.js'),
    join(ROOT, 'packages/preset/persona/lib/index.js'),
  ]
  for (const candidate of candidates) {
    if (existsSync(candidate)) {
      const mod = await import(pathToFileURL(candidate).href)
      return { path: candidate, Config: mod.Config }
    }
  }
  fail('找不到 @deepseek-ai/dsh-persona：先运行 prepare-offline.sh 或 pnpm run build')
  return undefined
}

/** 从 persona 增量文本里取出被点名的随包工具与市场工具，用于和 tools.yaml 对账。 */
function namedTools(delta) {
  const shipped = /ships this toolchain:\s*([^.]+)\./.exec(delta)
  const market = /heavier tools\s*\(([^)]+)\)/.exec(delta)
  return {
    shipped: shipped ? shipped[1].split(',').map(name => name.trim()).filter(Boolean) : undefined,
    market: market ? market[1].split(',').map(name => name.trim()).filter(Boolean) : undefined,
  }
}

/** 逐项核对段落点名的工具确实在 tools.yaml 的对应清单里。 */
function checkNamedTools(delta) {
  let manifest
  try {
    manifest = yaml.load(readFileSync(TOOLS_MANIFEST, 'utf8'))
  } catch (error) {
    fail(`tools.yaml 解析失败：${error.message.split('\n')[0]}`)
    return
  }
  const declared = new Set(Object.keys(manifest?.tools ?? {}))
  const installable = new Set(Object.keys(manifest?.installable ?? {}))
  const { shipped, market } = namedTools(delta)
  if (shipped === undefined) fail('persona 段落里找不到「ships this toolchain: …」这句，无法与 tools.yaml 对账')
  else for (const name of shipped) if (!declared.has(name)) fail(`persona 段落点名了 ${name}，但 tools.yaml 的 tools: 没有它`)

  const expectedMarket = ['go', 'ripgrep']
  if (market === undefined) fail('persona 段落里找不到「heavier tools (…)」这句，无法与 tools.yaml 对账')
  else
    for (const name of [...new Set([...market, ...expectedMarket])])
      if (!installable.has(name)) fail(`persona 段落点名了 ${name}，但 tools.yaml 的 installable: 没有它`)
}

const sourcePath = process.argv[2] ?? DEFAULT_SOURCE
const overlayPath = process.argv[3] ?? DEFAULT_OVERLAY

const sourceRows = readComposition(sourcePath, '源预设')
const overlayRows = readComposition(overlayPath, 'overlay')
const sourcePersona = sourceRows && personaOf(sourceRows, '源预设')
const overlayPersona = overlayRows && personaOf(overlayRows, 'overlay')

// 1. roster：persona 之外必须与上游逐行一致（顺序敏感，插入/删除/改配置都会被抓到）
if (sourceRows && overlayRows && sourcePersona && overlayPersona) {
  const without = rows => rows.filter(row => row.id !== 'persona')
  const upstream = JSON.stringify(without(sourceRows))
  const local = JSON.stringify(without(overlayRows))
  if (upstream === local) {
    console.log(`OK   roster：persona 之外 ${without(sourceRows).length} 行与上游一致`)
  } else {
    const limit = Math.min(without(sourceRows).length, without(overlayRows).length)
    let at = -1
    for (let i = 0; i < limit; i++) {
      if (JSON.stringify(without(sourceRows)[i]) !== JSON.stringify(without(overlayRows)[i])) {
        at = i
        break
      }
    }
    const detail = at >= 0
      ? `第 ${at + 1} 行起不同：上游 id=${without(sourceRows)[at]?.id}，本地 id=${without(overlayRows)[at]?.id}`
      : `行数不同：上游 ${without(sourceRows).length} 行，本地 ${without(overlayRows).length} 行`
    fail(`roster 与上游漂移（${detail}）——按上游预设重新同步 overlay`)
  }
}

// 2. persona 配置：字段名、增量位置、以及那段容器说明是否还在
let delta
if (sourcePersona && overlayPersona) {
  const upstream = sourcePersona.config ?? {}
  const local = overlayPersona.config ?? {}
  if ('text' in local) fail('overlay 的 persona 用了 text 字段；dsh-persona 的 schema 是 prefix/suffix')
  if (typeof local.prefix !== 'string' || typeof local.suffix !== 'string') {
    fail('overlay 的 persona 必须同时给出 prefix 与 suffix 字符串')
  } else if (local.suffix !== upstream.suffix) {
    fail(`persona 的 suffix 与上游不一致：上游 ${JSON.stringify(upstream.suffix)}，本地 ${JSON.stringify(local.suffix)}`)
  } else if (!local.prefix.startsWith(upstream.prefix)) {
    fail('persona 的 prefix 没有以上游 prefix 开头——增量必须追加在末尾')
  } else {
    delta = local.prefix.slice(upstream.prefix.length)
    if (!delta.includes(REQUIRED_MARKER)) fail(`persona 增量里没有「${REQUIRED_MARKER}」——容器工具链说明丢失或被改写`)
    else if (delta.trim().length === 0) fail('persona 增量是空白——overlay 退化成上游原文')
    else {
      console.log(`OK   persona：prefix 增量 ${delta.length} 字符，含容器说明`)
      checkNamedTools(delta)
    }
  }
}

// 3. schema 断言：随包 dsh-persona 必须能解析 overlay 的 persona 配置
if (overlayPersona?.config !== undefined) {
  const persona = await loadPersonaSchema()
  if (persona) {
    try {
      persona.Config(overlayPersona.config)
      console.log(`OK   schema：随包 dsh-persona 接受该配置（${persona.path.slice(ROOT.length + 1)}）`)
    } catch (error) {
      fail(`随包 dsh-persona 拒绝 overlay 的 persona 配置：${error.message.split('\n')[0]}（挂载时会抛 ValidationError）`)
    }
  }
}

if (failures.length > 0) {
  console.error('verify-preset-overlay: 检查未通过')
  for (const message of failures) console.error(`  - ${message}`)
  process.exit(1)
}
console.log('verify-preset-overlay: 全部通过')

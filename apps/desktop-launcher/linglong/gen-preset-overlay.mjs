#!/usr/bin/env node
/**
 * 由上游 standard 预设派生玲珑打包用的覆盖补丁。
 *
 * 为什么需要它：打包会话必须在 persona 里带上容器工具链清单，而这份清单只能靠替换
 * standard 预设的 persona 配置生效。上游已把预设从「包内整份组合文件」改成 bundle 内
 * 的声明式 patch（`packages/bundle/web-app/presets/standard.patch.yml`，由一条 insert
 * 行注册 `preset-standard`），而 cordis 的补丁语义是整体替换目标属性
 * （`vendor/include/src/index.ts`），覆盖它就得重述整份 config。因此这里不再维护会落后
 * 的整文件副本——旧做法在上游 2026-09-06 把 persona 的 `text` 改成 `prefix`/`suffix` 时
 * 静默少挂了两行插件——改为每次构建从随包原文派生：roster 永远等于上游，唯一自研内容
 * 是 `harness-overlay/agent-presets/persona-container-toolchain.txt` 里那段说明。
 *
 * 两道断言随派生一起保留：段落点名的工具必须出现在 `tools.yaml` 的对应清单里；派生出的
 * persona 配置必须能被随包 `@deepseek-ai/dsh-persona` 的 schema 解析——schema 若到挂载
 * 时才失败，错误已经发生在用户机器上。
 *
 * 用法：
 *   node apps/desktop-launcher/linglong/gen-preset-overlay.mjs [源 patch] [输出 patch] [段落文件]
 * 三个位置参数可省略，默认取仓库内的规范路径（自测脚本靠它指向临时副本）。
 */
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const ROOT = resolve(HERE, '..', '..', '..')
const require = createRequire(join(ROOT, 'package.json'))
const yaml = require('js-yaml')

const DEFAULT_SOURCE = join(ROOT, 'packages/bundle/web-app/presets/standard.patch.yml')
const DEFAULT_OUTPUT = join(HERE, 'stage/preset-overlay/standard.patch.yml')
const DEFAULT_PARAGRAPH = join(HERE, 'harness-overlay/agent-presets/persona-container-toolchain.txt')
const TOOLS_MANIFEST = join(HERE, 'tools.yaml')
const PRESET_ID = 'preset-standard'

/** 段落必须认领的容器事实，用于确认追加的那段没有被整体删掉或改写。 */
const REQUIRED_MARKER = 'Linglong sandbox container'

/**
 * `!!js` 表达式（如 `disabled: !!js process.platform === 'win32'`）承载平台判断，
 * 按普通标量解析再 dump 会把它退化成常量 `true`，在 Linux 上错误地禁用 Windows 专用行。
 * 因此把标量包成带标记的对象，dump 时再原样写回标签。
 */
const JS_EXPRESSION_TAG = new yaml.Type('tag:yaml.org,2002:js', {
  kind: 'scalar',
  construct: data => ({ __js: data }),
  predicate: value => value !== null && typeof value === 'object' && typeof value.__js === 'string',
  represent: value => value.__js,
})
const SCHEMA = yaml.DEFAULT_SCHEMA.extend([JS_EXPRESSION_TAG])

const failures = []
const fail = message => failures.push(message)

/** 读取并解析一份 patch 文件，失败按检查项失败处理而不是抛出。 */
function readPatchList(file, label) {
  if (!existsSync(file)) {
    fail(`${label}不存在：${file}`)
    return undefined
  }
  try {
    const doc = yaml.load(readFileSync(file, 'utf8'), { schema: SCHEMA })
    if (!Array.isArray(doc)) {
      fail(`${label}顶层不是数组：${file}`)
      return undefined
    }
    return doc
  } catch (error) {
    fail(`${label}解析失败：${error.message.split('\n')[0]}`)
    return undefined
  }
}

/** 在 patch 列表里定位注册 preset-standard 的那条 insert；必须恰好一条。 */
function findPresetRow(doc, label) {
  const matches = []
  for (const patch of doc) {
    for (const entry of Array.isArray(patch?.insert) ? patch.insert : []) {
      if (entry?.id === PRESET_ID) matches.push(entry)
    }
  }
  if (matches.length !== 1) {
    fail(`${label}里 id=${PRESET_ID} 的 insert 行有 ${matches.length} 条，预期 1 条`)
    return undefined
  }
  return matches[0]
}

/** 定位预设的 persona 行；组合必须恰好有一行，否则后续断言没有意义。 */
function findPersona(entry, label) {
  const plugins = entry?.config?.plugins
  if (!Array.isArray(plugins)) {
    fail(`${label}的 config.plugins 不是数组`)
    return undefined
  }
  const matches = plugins.filter(plugin => plugin !== null && typeof plugin === 'object' && plugin.id === 'persona')
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
const outputPath = process.argv[3] ?? DEFAULT_OUTPUT
const paragraphPath = process.argv[4] ?? DEFAULT_PARAGRAPH

const source = readPatchList(sourcePath, '源 patch')
const preset = source && findPresetRow(source, '源 patch')
const persona = preset && findPersona(preset, `源 patch 的 ${PRESET_ID}`)

// 1. persona 配置：字段名必须是上游当前的 prefix/suffix，副本不存在走样空间
let delta
if (persona) {
  const config = persona.config ?? {}
  if ('text' in config) fail(`源 patch 的 persona 用了 text 字段；dsh-persona 的 schema 是 prefix/suffix`)
  if (typeof config.prefix !== 'string' || typeof config.suffix !== 'string') {
    fail(`源 patch 的 persona 必须同时给出 prefix 与 suffix 字符串`)
  } else {
    let paragraph
    try {
      paragraph = readFileSync(paragraphPath, 'utf8').trim()
    } catch {
      // 段落文件属于构建输入，缺失时没有可回退的来源，只能登记失败。
      fail(`段落文件不存在或不可读：${paragraphPath}`)
    }
    if (paragraph !== undefined) {
      if (!paragraph.includes(REQUIRED_MARKER)) {
        fail(`段落文件里没有「${REQUIRED_MARKER}」——容器工具链说明丢失或被改写`)
      } else if (config.prefix.includes(REQUIRED_MARKER)) {
        // 上游若自己带上容器说明，追加会变成重复段落；这是人工判断的岔口。
        fail(`源 patch 的 persona.prefix 已含「${REQUIRED_MARKER}」——上游可能已自带该段落，需人工确认`)
      } else {
        delta = `\n\n${paragraph}`
        config.prefix = `${config.prefix}${delta}`
      }
    }
  }
}

// 2. 往返无损：派生只允许改 persona.prefix，roster 与 !!js 表达式必须逐字保留。
//    js-yaml 的 dump 会丢注释，但语义（含自定义标签）不允许有差异。
if (persona?.config?.prefix !== undefined) {
  let replayed
  try {
    replayed = yaml.load(yaml.dump(source, { schema: SCHEMA, noRefs: true, lineWidth: -1 }), { schema: SCHEMA })
  } catch (error) {
    fail(`YAML 往返失败：${error.message.split('\n')[0]}`)
  }
  if (replayed !== undefined) {
    if (JSON.stringify(replayed) === JSON.stringify(source)) {
      const rowCount = (preset.config.plugins ?? []).filter(plugin => plugin?.id !== 'persona').length
      console.log(`OK   派生：persona 之外 ${rowCount} 行逐字保留上游内容（YAML 往返无差异）`)
    } else {
      fail('YAML 往返后内容发生变化——roster 或 !!js 表达式没能原样保留，不能写出该覆盖补丁')
    }
  }
}

// 3. 段落点名的工具与 tools.yaml 对账
if (delta !== undefined) {
  console.log(`OK   persona：prefix 增量 ${delta.length} 字符，含容器说明`)
  checkNamedTools(delta)
}

// 4. schema 断言：随包 dsh-persona 必须能解析派生出的 persona 配置
if (persona?.config !== undefined) {
  const personaSchema = await loadPersonaSchema()
  if (personaSchema) {
    try {
      personaSchema.Config(persona.config)
      console.log(`OK   schema：随包 dsh-persona 接受该配置（${relative(ROOT, personaSchema.path)}）`)
    } catch (error) {
      fail(`随包 dsh-persona 拒绝派生的 persona 配置：${error.message.split('\n')[0]}（挂载时会抛 ValidationError）`)
    }
  }
}

if (failures.length > 0) {
  console.error('gen-preset-overlay: 检查未通过')
  for (const message of failures) console.error(`  - ${message}`)
  process.exit(1)
}

mkdirSync(dirname(outputPath), { recursive: true })
writeFileSync(
  outputPath,
  `# 由 gen-preset-overlay.mjs 从 ${relative(ROOT, sourcePath)} 派生，勿手改。\n`
  + '# 唯一增量是 standard 预设 persona.prefix 末尾的容器工具链说明。\n'
  + yaml.dump(source, { schema: SCHEMA, noRefs: true, lineWidth: -1 }),
)
console.log(`gen-preset-overlay: 已写出 ${relative(ROOT, outputPath)}`)

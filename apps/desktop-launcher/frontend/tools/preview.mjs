/**
 * 桌面启动器前端的渲染与布局校验工具。
 *
 * 为什么需要它：`test-app.cjs` 用的 DOM 桩既不实现样式表也不实现布局，弹框高度、
 * 面板拉伸、主题配色这类问题它一个都看不见。本工具把 index.html 原样放进无头
 * Chromium 里渲染，用 DevTools 协议量真实几何，于是「切换连接模式时弹框高度是否
 * 变化」这种问题变成可断言的事实，而不是靠人盯截图。
 *
 * 不引入依赖：浏览器取本地 Playwright 缓存里的 Chromium（或 DSH_PREVIEW_BROWSER
 * / PATH 上的 chrome），协议走 Node 内置 WebSocket。前端本身是零构建的静态资源，
 * 这里也不引入构建链。
 *
 * 用法：
 *   node frontend/tools/preview.mjs render  [--out DIR] [--theme light|dark|both]
 *   node frontend/tools/preview.mjs measure [--out DIR] [--json]
 *   node frontend/tools/preview.mjs verify  [--out DIR]
 */
import { spawn } from 'node:child_process'
import { existsSync, readdirSync } from 'node:fs'
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { setTimeout as sleep } from 'node:timers/promises'

const HERE = dirname(fileURLToPath(import.meta.url))
const FRONTEND = resolve(HERE, '..')
const INDEX = join(FRONTEND, 'index.html')

/**
 * 一次性浏览器状态的根目录（每次运行在其下建独立子目录）。
 *
 * 必须落在 `frontend/` 之外：启动器用 `//go:embed all:frontend` 嵌入整个前端
 * 目录，而 go:embed 既不读 .gitignore，`all:` 前缀又连点号开头的目录一起嵌入，
 * 于是 Chromium 在 `Code Cache/pc/` 下写出的文件名（含 `;` 与反引号）会触发
 * Go 的嵌入文件名校验，让 `go build` 报 invalid name 并打断整个打包流程。
 *
 * 放在启动器 module 目录下：既在 embed 根之外，又仍在工作区内——受限文件沙箱
 * 只允许写工作区，Chromium 的 HOME/XDG 落到别处会卡在只读路径上。
 */
const SCRATCH_ROOT = resolve(FRONTEND, '..', '.preview-cache')

/** 令牌固定 43 个 base64url 字符（packages/client/connection 的 SECRET_BYTES=32）。 */
const TOKEN = 'hSSvsUVtS8wnTwXzRl0BXDfLdVtx5rPuyXH7ABs8Kc'
const EXTERNAL_ADDRESS = `http://192.168.1.20:3456/?token=${TOKEN}`

/** index.html 里初始即 hidden 的元素，applyState 每次都从这里重置。 */
const INITIALLY_HIDDEN = [
  'server-modal', 'server-address', 'server-copy',
  'safe-mode-row', 'safe-mode-active', 'fresh-home-active', 'external-panel',
]

/**
 * 状态夹具：每个条目按 app.js 的真实写法描述弹框那一刻的 DOM。
 * 新增状态只往这里加数据，不用碰渲染逻辑。
 */
const FIXTURES = {
  'container-running': {
    mode: 'container', panel: 'container',
    text: {
      'server-state': '运行中',
      'server-addr-origin': 'http://127.0.0.1:3456/',
      'server-addr-token': `?token=${TOKEN}`,
      'server-detail2': '586649',
    },
    className: { 'server-state': 'row-value state-value state-ok' },
    show: ['server-address', 'server-copy'],
    disabled: { 'server-start': true },
  },
  'container-starting': {
    mode: 'container', panel: 'container',
    text: { 'server-state': '启动中', 'server-detail1': 'harness 正在启动…' },
    className: { 'server-state': 'row-value state-value state-warn' },
    disabled: { 'server-start': true, 'server-stop': true },
  },
  'container-stopped': {
    mode: 'container', panel: 'container',
    text: { 'server-state': '已停止', 'server-detail1': 'exit status 1' },
    className: { 'server-state': 'row-value state-value state-muted' },
  },
  'container-failed-safemode': {
    mode: 'container', panel: 'container',
    text: {
      'server-state': '启动失败',
      'server-detail1': 'exit status 1',
      'server-detail2': '~/.cache/dsh-desktop/harness.log',
    },
    className: { 'server-state': 'row-value state-value state-danger' },
    show: ['safe-mode-row'],
  },
  'external-empty': {
    mode: 'external', panel: 'external',
    disabled: { 'ext-disconnect': true },
  },
  'external-connecting': {
    mode: 'external', panel: 'external',
    value: { 'ext-url': EXTERNAL_ADDRESS },
    text: { 'ext-state': '连接中…' },
    disabled: { 'ext-connect': true },
  },
  'external-connected': {
    mode: 'external', panel: 'external',
    value: { 'ext-url': EXTERNAL_ADDRESS },
    text: { 'ext-state': '已连接 192.168.1.20:3456' },
    disabled: { 'ext-connect': true },
  },
}

/**
 * 允许比其他状态高的夹具：失败态多出一组操作（安全模式入口），是刻意保留的增高。
 * 这些状态不参与「高度恒定」断言，但仍参与面板等高与输入框封顶断言。
 */
const TALLER_BY_DESIGN = new Set(['container-failed-safemode'])

/** verify 的容差：行盒取整会带来 1px 抖动，取 1px 上限。 */
const HEIGHT_TOLERANCE = 1
/** 地址框固定两行时的盒子高度（含内边距与描边），超出即为回归。 */
const ADDRESS_BOX_HEIGHT = { min: 40, max: 42 }
/** textarea 的封顶：6 行 × 1.45 行高 + 内外边距，留 2px 取整余量。 */
const TEXTAREA_MAX_HEIGHT = 118

/** 在页面里执行：按夹具还原弹框 DOM。 */
function applyState(fixture, initiallyHidden) {
  const $ = (id) => document.getElementById(id)
  for (const id of initiallyHidden) $(id).classList.add('hidden')
  $('server-modal').classList.remove('hidden')
  const radio = document.querySelector(`input[name="mode"][value="${fixture.mode}"]`)
  if (radio) radio.checked = true
  $('container-panel').classList.toggle('hidden', fixture.panel !== 'container')
  $('external-panel').classList.toggle('hidden', fixture.panel !== 'external')
  for (const id of fixture.show ?? []) $(id).classList.remove('hidden')
  for (const [id, value] of Object.entries(fixture.text ?? {})) $(id).textContent = value
  for (const [id, value] of Object.entries(fixture.value ?? {})) $(id).value = value
  for (const [id, value] of Object.entries(fixture.className ?? {})) $(id).className = value
  for (const [id, value] of Object.entries(fixture.disabled ?? {})) $(id).disabled = value
}

/** 在页面里执行：读出各元素的真实几何。 */
function probeGeometry() {
  const height = (selector) => {
    const el = document.querySelector(selector)
    return el ? Math.round(el.getBoundingClientRect().height) : -1
  }
  return JSON.stringify({
    card: height('.modal-card'),
    container: height('#container-panel'),
    external: height('#external-panel'),
    address: height('#server-address'),
    textarea: height('#ext-url'),
  })
}

/**
 * 在页面里执行：把市场弹框打开、塞入若干卡片，读回网格几何。
 *
 * 卡片按 app.js 的 toolCard 形制搭建，元信息行取一个很长的命令列表：该行是
 * white-space: nowrap，它的整行宽度就是卡片的最小内容宽度。轨道若用 1fr
 * （等价 minmax(auto, 1fr)），这个宽度会把轨道顶到超过均分，轨道之和超出网格宽度后
 * 网格横向溢出、右侧整列被裁掉——这条只有真实布局看得见，DOM 桩与截图都无法固定它。
 * 预览页剥掉了脚本，因此这里手搭 DOM，而不是调用 renderTools。
 * @returns {object} 网格客户宽/内容宽、轨道列宽与首行列宽。
 */
function probeMarketGrid() {
  const $ = (id) => document.getElementById(id)
  $('tools-modal').classList.remove('hidden')
  const grid = $('market-grid')
  grid.innerHTML = ''
  const meta = '现代 CLI · sqlite3 sqldiff sqlite3_analyzer sqlite3_rsync · 4.1 MB'
  for (let i = 0; i < 6; i += 1) {
    const card = document.createElement('div')
    card.className = 'tool-card-item'
    card.innerHTML = `<div class="tool-card-head"><span class="tool-card-name">工具 ${i}</span></div>`
      + '<div class="tool-card-desc">描述文本，占两行高度</div>'
      + `<div class="tool-card-meta">${meta}</div>`
      + '<div class="tool-card-actions"><select class="version-select"><option>v1.0.0 · 当前</option></select>'
      + '<button class="btn btn-danger">卸载</button></div>'
    grid.appendChild(card)
  }
  const cards = Array.from(grid.children)
  const top0 = Math.round(cards[0].getBoundingClientRect().top)
  const firstRow = cards.filter((c) => Math.round(c.getBoundingClientRect().top) === top0)
  return JSON.stringify({
    clientWidth: grid.clientWidth,
    scrollWidth: grid.scrollWidth,
    tracks: getComputedStyle(grid).gridTemplateColumns,
    rowWidths: firstRow.map((c) => Math.round(c.getBoundingClientRect().width)),
  })
}

/**
 * 找一个能跑的无头浏览器：先看 DSH_PREVIEW_BROWSER，再用 Playwright 缓存，最后 PATH。
 * 找不到时返回 undefined，由调用方决定是报错还是跳过。
 * @returns {string|undefined} 可执行文件路径。
 */
function findBrowser() {
  const fromEnv = process.env.DSH_PREVIEW_BROWSER
  if (fromEnv) return existsSync(fromEnv) ? fromEnv : undefined
  const cache = process.env.PLAYWRIGHT_BROWSERS_PATH
    ?? join(process.env.HOME ?? '', '.cache', 'ms-playwright')
  if (existsSync(cache)) {
    // Playwright 的缓存目录形如 chromium-<version>/chrome-linux64/chrome，
    // 只按前缀匹配，版本号随升级变化时不改这里。
    for (const prefix of ['chromium-', 'chromium_headless_shell-']) {
      let versions = []
      try {
        versions = readdirSync(cache).filter((name) => name.startsWith(prefix)).sort().reverse()
      } catch { /* 缓存目录不可读时继续尝试下一个来源 */ }
      for (const version of versions) {
        for (const rel of ['chrome-linux64/chrome', 'chrome-linux/headless_shell', 'chrome-linux/chrome']) {
          const candidate = join(cache, version, rel)
          if (existsSync(candidate)) return candidate
        }
      }
    }
  }
  for (const dir of (process.env.PATH ?? '').split(':')) {
    for (const name of ['chromium', 'chromium-browser', 'google-chrome']) {
      const candidate = join(dir, name)
      if (existsSync(candidate)) return candidate
    }
  }
  return undefined
}

/**
 * 生成预览页：index.html 逐字复制，只剥掉脚本并改写样式相对路径。
 * 不落一份手工维护的副本，因此它永远不会与真实页面漂移。
 * @param {string} dir - 预览页写入目录。
 * @returns {Promise<string>} 预览页的文件 URL。
 */
async function buildPreview(dir) {
  const source = await readFile(INDEX, 'utf8')
  const stripped = source
    .replace(/<script\b[^>]*><\/script>\s*/gu, '')
    .replace('href="styles.css"', `href="file://${join(FRONTEND, 'styles.css')}"`)
  const target = join(dir, 'preview.html')
  await writeFile(target, stripped)
  return `file://${target}`
}

/** 最小 CDP 客户端：发命令、收结果、等事件。 */
class Cdp {
  constructor(ws) {
    this.ws = ws
    this.next = 0
    this.pending = new Map()
    this.events = new Map()
  }

  static async connect(url) {
    const ws = new WebSocket(url)
    await new Promise((ok, fail) => { ws.onopen = ok; ws.onerror = fail })
    const client = new Cdp(ws)
    ws.onmessage = (message) => {
      const payload = JSON.parse(message.data)
      if (payload.id && client.pending.has(payload.id)) {
        const { resolve, reject } = client.pending.get(payload.id)
        client.pending.delete(payload.id)
        if (payload.error) reject(new Error(JSON.stringify(payload.error)))
        else resolve(payload.result)
      } else if (payload.method && client.events.has(payload.method)) {
        for (const listener of client.events.get(payload.method)) listener(payload.params)
        client.events.delete(payload.method)
      }
    }
    return client
  }

  send(method, params = {}) {
    const id = ++this.next
    this.ws.send(JSON.stringify({ id, method, params }))
    return new Promise((resolve, reject) => this.pending.set(id, { resolve, reject }))
  }

  once(method) {
    return new Promise((resolve) => {
      const list = this.events.get(method) ?? []
      list.push(resolve)
      this.events.set(method, list)
    })
  }
}

/**
 * 起浏览器并连上页面，回调里拿到可用的 CDP 客户端。
 * @param {string} pageUrl - 预览页文件 URL。
 * @param {string} scratchDir - 本次运行独占的浏览器状态目录（沙箱下必须可写）。
 * @param {(cdp: Cdp) => Promise<void>} body - 使用客户端的回调。
 */
async function withBrowser(pageUrl, scratchDir, body) {
  const browser = findBrowser()
  if (!browser) {
    throw new Error('未找到可用的 Chromium。用 DSH_PREVIEW_BROWSER 指定可执行文件，或安装 Playwright 浏览器缓存。')
  }
  const port = 9400 + (process.pid % 500)
  await mkdir(scratchDir, { recursive: true })
  const child = spawn(browser, [
    '--headless', '--no-sandbox', '--disable-gpu', '--hide-scrollbars',
    '--disable-dev-shm-usage', '--no-first-run', '--disable-crash-reporter',
    `--user-data-dir=${join(scratchDir, 'profile')}`,
    `--remote-debugging-port=${port}`, pageUrl,
  ], {
    stdio: 'ignore',
    // Chromium 会往 HOME 与 XDG 目录写配置；一律重定向到本次运行的临时目录内，
    // 否则在只允许写工作区的沙箱里它会卡在写 ~/.config。
    env: {
      ...process.env,
      HOME: join(scratchDir, 'home'),
      XDG_CONFIG_HOME: join(scratchDir, 'home', '.config'),
      XDG_CACHE_HOME: join(scratchDir, 'home', '.cache'),
      XDG_RUNTIME_DIR: join(scratchDir, 'home', 'run'),
    },
  })
  try {
    let targets
    for (let attempt = 0; attempt < 60; attempt += 1) {
      try {
        const response = await fetch(`http://127.0.0.1:${port}/json/list`)
        targets = await response.json()
        if (targets.some((t) => t.type === 'page')) break
      } catch { /* 端口还没起来，继续等 */ }
      await sleep(150)
    }
    if (!targets) throw new Error('Chromium 调试端口未就绪')
    const page = targets.find((t) => t.type === 'page')
    const cdp = await Cdp.connect(page.webSocketDebuggerUrl)
    await cdp.send('Page.enable')
    await cdp.send('Runtime.enable')
    await cdp.send('Emulation.setDeviceMetricsOverride', {
      width: 1180, height: 720, deviceScaleFactor: 2, mobile: false,
    })
    await body(cdp)
  } finally {
    child.kill('SIGKILL')
  }
}

/**
 * 逐个夹具渲染，回调收集每个夹具的结果。
 * @param {Cdp} cdp - 已连接的客户端。
 * @param {string} pageUrl - 预览页 URL。
 * @param {string[]} themes - 要跑的主题。
 * @param {(theme: string, name: string, fixture: object) => Promise<void>} visit - 每个夹具的回调。
 */
async function walk(cdp, pageUrl, themes, visit) {
  for (const theme of themes) {
    await cdp.send('Emulation.setEmulatedMedia', {
      media: 'screen',
      features: [{ name: 'prefers-color-scheme', value: theme }],
    })
    for (const [name, fixture] of Object.entries(FIXTURES)) {
      const loaded = cdp.once('Page.loadEventFired')
      await cdp.send('Page.navigate', { url: pageUrl })
      await loaded
      const applied = await cdp.send('Runtime.evaluate', {
        expression: `(${applyState})(${JSON.stringify(fixture)}, ${JSON.stringify(INITIALLY_HIDDEN)})`,
      })
      if (applied.exceptionDetails) {
        throw new Error(`${name}: 应用夹具失败 — ${applied.exceptionDetails.exception?.description}`)
      }
      await cdp.send('Runtime.evaluate', {
        expression: 'document.fonts.ready.then(() => 1)', awaitPromise: true,
      })
      await visit(theme, name, fixture)
    }
  }
}

/** 解析 `--key value` 形式的参数。 */
function parseArgs(argv) {
  const args = {}
  for (let i = 0; i < argv.length; i += 1) {
    if (!argv[i].startsWith('--')) continue
    const next = argv[i + 1]
    args[argv[i].slice(2)] = next === undefined || next.startsWith('--') ? true : argv[++i]
  }
  return args
}

/**
 * 产物目录：默认在 frontend/.preview（已在 .gitignore 内）。
 * 只决定截图与预览页的位置；浏览器状态固定走 SCRATCH_ROOT，不随 --out 移动。
 * @param {Record<string, string|boolean>} args - 命令行参数。
 * @returns {string} 产物目录绝对路径。
 */
function resolveOut(args) {
  return resolve(args.out && args.out !== true ? args.out : join(FRONTEND, '.preview'))
}

/**
 * 建本次运行独占的浏览器状态目录。
 *
 * 每次运行新建而非复用固定路径：并发跑两个 preview 时各自的 profile 与 HOME 不会
 * 互相踩（复用会让后者读到前者的 SingletonLock）。
 * @returns {Promise<string>} 新建的空目录绝对路径。
 */
async function createScratch() {
  await mkdir(SCRATCH_ROOT, { recursive: true })
  return mkdtemp(join(SCRATCH_ROOT, 'run-'))
}

/**
 * 执行一次 preview：生成预览页、在浏览器里量几何，再按子命令输出或断言。
 * @param {string} command - render / measure / verify 之一。
 * @param {Record<string, string|boolean>} args - 命令行参数。
 * @returns {Promise<number>} 进程退出码。
 */
async function run(command, args) {
  const out = resolveOut(args)
  await mkdir(out, { recursive: true })
  const pageUrl = await buildPreview(out)
  const themes = args.theme === 'light' || args.theme === 'dark' ? [args.theme] : ['light', 'dark']

  const measurements = []
  const market = []
  const scratch = await createScratch()
  try {
    await withBrowser(pageUrl, scratch, async (cdp) => {
      await walk(cdp, pageUrl, themes, async (theme, name) => {
        const probe = await cdp.send('Runtime.evaluate', { expression: `(${probeGeometry})()`, returnByValue: true })
        const geometry = JSON.parse(probe.result.value)
        measurements.push({ theme, name, ...geometry })
        if (command === 'render') {
          const shot = await cdp.send('Page.captureScreenshot', { format: 'png' })
          const file = join(out, `${theme}-${name}.png`)
          await writeFile(file, Buffer.from(shot.data, 'base64'))
          console.log(`已截图 ${file}`)
        }
      })
      // 市场网格与主题无关（几何相同），但仍按主题各测一次，保证两套配色下都成立。
      for (const theme of themes) {
        await cdp.send('Emulation.setEmulatedMedia', {
          media: 'screen',
          features: [{ name: 'prefers-color-scheme', value: theme }],
        })
        const loaded = cdp.once('Page.loadEventFired')
        await cdp.send('Page.navigate', { url: pageUrl })
        await loaded
        const probe = await cdp.send('Runtime.evaluate', {
          expression: `(${probeMarketGrid})()`, returnByValue: true,
        })
        market.push({ theme, ...JSON.parse(probe.result.value) })
      }
    })
  } finally {
    // profile 与 HOME/XDG 都是本次运行的一次性状态，跑完即删，避免残留累积。
    await rm(scratch, { recursive: true, force: true })
  }

  if (command === 'measure') {
    if (args.json) console.log(JSON.stringify({ measurements, market }, null, 2))
    else {
      for (const m of measurements) console.log(`${m.theme}\t${m.name}\t${JSON.stringify(m)}`)
      for (const m of market) console.log(`${m.theme}\tmarket-grid\t${JSON.stringify(m)}`)
    }
    return 0
  }
  if (command === 'render') return 0
  return verify(measurements, market)
}

/**
 * 断言布局不变量。这里断的就是「切模式/切状态是否改变弹框高度」这类只有真实布局
 * 才看得见、DOM 桩看不见的性质。
 * @param {Array<object>} measurements - 各主题各状态的实测几何。
 * @param {Array<object>} market - 各主题下市场网格的实测几何。
 * @returns {number} 进程退出码。
 */
function verify(measurements, market) {
  const failures = []
  for (const theme of [...new Set(measurements.map((m) => m.theme))]) {
    const rows = measurements.filter((m) => m.theme === theme)
    const routine = rows.filter((r) => !TALLER_BY_DESIGN.has(r.name))
    const heights = routine.map((r) => r.card)
    const spread = Math.max(...heights) - Math.min(...heights)
    console.log(`\n[${theme}] 常规状态卡片高度 ${[...new Set(heights)].sort((a, b) => a - b).join(' / ')} (极差 ${spread}px)`)
    for (const r of rows) {
      console.log(`  ${r.name.padEnd(26)} card=${r.card} container=${r.container} external=${r.external} address=${r.address} textarea=${r.textarea}`)
    }
    if (spread > HEIGHT_TOLERANCE) {
      failures.push(`${theme}: 常规状态的卡片高度极差 ${spread}px > ${HEIGHT_TOLERANCE}px`)
    }
    // 输入框自身也不该随状态变尺寸：提示行出现/消失时若靠输入框伸缩来吸收，
    // 卡片高度可能不变，但输入框会一顿一顿地长高变矮。
    const textareas = routine.map((r) => r.textarea)
    const textareaSpread = Math.max(...textareas) - Math.min(...textareas)
    if (textareaSpread > HEIGHT_TOLERANCE) {
      failures.push(`${theme}: 服务地址输入框高度在状态间变化 ${textareaSpread}px > ${HEIGHT_TOLERANCE}px`)
    }
    for (const row of rows) {
      if (row.container !== row.external) {
        failures.push(`${theme}/${row.name}: 两张面板不等高（${row.container} vs ${row.external}）——叠放没有生效`)
      }
      if (row.address < ADDRESS_BOX_HEIGHT.min || row.address > ADDRESS_BOX_HEIGHT.max) {
        failures.push(`${theme}/${row.name}: 地址框高度 ${row.address}px 超出 ${ADDRESS_BOX_HEIGHT.min}-${ADDRESS_BOX_HEIGHT.max}px——两行预留失效`)
      }
      if (row.textarea > TEXTAREA_MAX_HEIGHT) {
        failures.push(`${theme}/${row.name}: 服务地址输入框 ${row.textarea}px 超过封顶 ${TEXTAREA_MAX_HEIGHT}px`)
      }
    }
  }
  for (const m of market) {
    console.log(`[${m.theme}] 市场网格 轨道=${m.tracks} 首行列宽=${m.rowWidths.join('+')} 内容宽=${m.scrollWidth} 客户宽=${m.clientWidth}`)
    if (m.scrollWidth > m.clientWidth) {
      failures.push(`${m.theme}: 市场网格横向溢出（内容 ${m.scrollWidth} > 客户 ${m.clientWidth}）——卡片的最小内容宽度顶开了等分轨道`)
    }
    const rowSpread = Math.max(...m.rowWidths) - Math.min(...m.rowWidths)
    if (m.rowWidths.length > 1 && rowSpread > HEIGHT_TOLERANCE) {
      failures.push(`${m.theme}: 市场网格同一行列宽不等（${m.rowWidths.join(' / ')}）——轨道没有均分`)
    }
  }
  if (failures.length > 0) {
    console.error('\n布局不变量被打破：')
    for (const failure of failures) console.error(`  ✗ ${failure}`)
    return 1
  }
  console.log('\n✓ 布局不变量全部成立')
  return 0
}

const [command = 'verify', ...rest] = process.argv.slice(2)
if (!['render', 'measure', 'verify'].includes(command)) {
  console.error(`未知子命令 ${command}；可用：render | measure | verify`)
  process.exit(2)
}
try {
  process.exit(await run(command, parseArgs(rest)))
} catch (error) {
  if (error.message?.includes('未找到可用的 Chromium')) {
    console.error(`跳过：${error.message}`)
    process.exit(0)
  }
  console.error(error)
  process.exit(1)
}

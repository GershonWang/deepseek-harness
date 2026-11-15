/**
 * 工具链市场窗口的渲染逻辑。
 *
 * 页面只渲染主进程给的视图、把用户操作交回主进程；文案与派生状态（是否随包、
 * 是否已装、有无更新）全部由主进程决定，页面不自己拼字符串，也不猜测状态。
 */

/** 当前视图。 */
let view
/** 当前分类页签，空串表示「全部」。 */
let category = ''
/** 搜索关键词，小写。 */
let query = ''
/** 正在安装的工具 id，空串表示空闲。 */
let installing = ''
/** 进度报告，按工具 id 索引。 */
const progress = new Map()
/** 每张卡片的错误提示，按工具 id 索引；状态刷新时清空。 */
const errors = new Map()
/** 主进程最后一次广播的视图版本，用于丢弃乱序到达的旧视图。 */
let refreshPending = false

const el = id => document.getElementById(id)

/** 创建元素并设置属性和子节点。 */
function h(tag, props, ...children) {
  const node = document.createElement(tag)
  for (const [key, value] of Object.entries(props ?? {})) {
    if (value === undefined || value === null || value === false) continue
    if (key === 'class') node.className = value
    else if (key === 'text') node.textContent = value
    else if (key.startsWith('on')) node.addEventListener(key.slice(2), value)
    else node.setAttribute(key, value === true ? '' : String(value))
  }
  for (const child of children.flat()) {
    if (child === undefined || child === null || child === false) continue
    node.append(typeof child === 'string' ? document.createTextNode(child) : child)
  }
  return node
}

/** 按当前分类与关键词过滤工具。 */
function visibleTools() {
  const terms = query.trim().toLowerCase()
  return view.tools.filter(tool => {
    if (category !== '' && tool.categoryId !== category) return false
    if (terms === '') return true
    return tool.name.toLowerCase().includes(terms) || tool.description.toLowerCase().includes(terms)
  })
}

/** 渲染分类页签；工具数量随过滤变化，页签上显示的是该分类的总数。 */
function renderCategories() {
  const nav = el('categories')
  nav.setAttribute('aria-label', view.categoriesLabel)
  nav.replaceChildren(
    h('button', {
      class: 'tab',
      type: 'button',
      role: 'tab',
      'aria-selected': category === '' ? 'true' : 'false',
      onclick: () => { category = ''; render() },
    }, view.allCategoriesLabel, h('span', { class: 'tab-count', text: String(view.tools.length) })),
    ...view.categories.map(entry => h('button', {
      class: 'tab',
      type: 'button',
      role: 'tab',
      'aria-selected': category === entry.id ? 'true' : 'false',
      onclick: () => { category = entry.id; render() },
    }, entry.label, h('span', { class: 'tab-count', text: String(entry.count) }))),
  )
}

/** 选中版本：多线工具优先当前激活线，否则首选第一条线的最新版。 */
function selectedVersion(tool) {
  for (const line of tool.lines) {
    if (line.versions.some(entry => entry.active)) {
      return line.versions.find(entry => entry.active)?.version ?? line.latest
    }
  }
  return tool.lines[0]?.latest ?? ''
}

/** 渲染一张卡片上的进度区。 */
function renderProgress(tool) {
  const report = progress.get(tool.id)
  if (report === undefined) return null
  return h('div', { class: 'progress' },
    h('div', { class: 'progress-head' },
      h('span', { text: report.phase }, report.transferred === '' ? null : h('span', { text: report.transferred })),
      h('span', { text: `${report.percent}%` })),
    h('div', { class: `progress-track${report.determinate ? '' : ' is-indeterminate'}` },
      h('div', { class: 'progress-fill', style: `width: ${report.percent}%` })),
    report.cancellable
      ? h('div', { class: 'card-actions' },
          h('button', {
            class: 'btn btn-quiet', type: 'button', text: view.labels.cancel,
            onclick: () => { void window.dshToolchain.cancel() },
          }))
      : null)
}

/** 渲染一张卡片的主要操作。 */
function renderActions(tool) {
  if (installing === tool.id) return renderProgress(tool)
  if (tool.bundled) return null

  const actions = []
  // 版本选择只在有多条大版本线时出现；单线工具的版本在 meta 行里直接显示，
  // 没有可选的东西就不该给下拉。
  let chosen = selectedVersion(tool)
  if (tool.installed) {
    const installedVersions = tool.lines.flatMap(line => line.versions.filter(entry => entry.installed))
    if (installedVersions.length > 1) {
      const select = h('select', {
        class: 'card-select',
        'aria-label': view.labels.switchVersion,
        onchange: event => { chosen = event.target.value },
      }, ...installedVersions.map(entry => h('option', {
        value: entry.version,
        selected: entry.active,
        text: `${view.labels.version} ${entry.version}`,
      })))
      actions.push(select, h('button', {
        class: 'btn', type: 'button', text: view.labels.switchVersion,
        onclick: () => { void run(() => window.dshToolchain.activate(tool.id, select.value), tool.id) },
      }))
    }
    if (tool.updateAvailable) {
      actions.push(h('button', {
        class: 'btn btn-primary', type: 'button', text: view.labels.update,
        onclick: () => { void run(() => window.dshToolchain.install(tool.id, tool.updateVersion), tool.id) },
      }))
    }
    actions.push(h('button', {
      class: 'btn btn-quiet', type: 'button', text: view.labels.uninstall,
      onclick: () => {
        const target = tool.active === '' ? installedVersions[0]?.version : tool.active
        if (target !== undefined) void run(() => window.dshToolchain.uninstall(tool.id, target), tool.id)
      },
    }))
    return h('div', { class: 'card-actions' }, ...actions)
  }

  // 未安装：多线工具给线选择器，单线工具直接把版本写在 meta 行。
  if (tool.offersLines) {
    actions.push(h('select', {
      class: 'card-select',
      'aria-label': view.labels.version,
      onchange: event => { chosen = event.target.value },
    }, ...tool.lines.map(line => h('option', { value: line.latest, text: `${view.labels.version} ${line.latest}` }))))
  }
  actions.push(h('button', {
    class: 'btn btn-primary', type: 'button', text: view.labels.install,
    onclick: () => { void run(() => window.dshToolchain.install(tool.id, chosen), tool.id) },
  }))
  return h('div', { class: 'card-actions' }, ...actions)
}

/** 版本说明行：未装显示将要装的版本与体积，已装显示当前版本。 */
function metaText(tool) {
  const active = tool.active
  if (active !== '') {
    const entry = tool.lines.flatMap(line => line.versions).find(item => item.version === active)
    const size = entry?.size === '' ? '' : ` · ${entry?.size ?? ''}`
    return `${view.labels.installedTag} ${active}${size}`
  }
  const line = tool.lines[0]
  const entry = line?.versions.find(item => item.version === line.latest)
  const size = entry?.size === '' ? '' : ` · ${entry?.size ?? ''}`
  if (line === undefined) return ''
  // 多线工具的选择器已显示版本，这里只报体积，避免同一信息出现两次。
  return tool.offersLines ? `${size.replace(' · ', '')}` : `${view.labels.version} ${line.latest}${size}`
}

/**
 * 命令行的完整文本。
 *
 * 卡片上一行放不下 rust、sqlite 这类多命令工具，因此单行省略、悬停看全：数量、
 * 命令名与名称一致时都直接给出，不让用户为了知道装完能得到什么命令去翻文档。
 */
function commandText(tool) {
  if (tool.commands.length === 0) return ''
  return `${view.labels.commands}: ${tool.commands.join(' · ')}`
}

/** 渲染一张卡片。 */
function renderCard(tool) {
  const tag = tool.bundled
    ? h('span', { class: 'card-tag', text: view.labels.bundledTag })
    : tool.installed
      ? h('span', { class: 'card-tag is-installed', text: view.labels.installedTag })
      : null
  const meta = metaText(tool)
  const commands = commandText(tool)
  const error = errors.get(tool.id)
  return h('article', { class: 'card' },
    h('div', { class: 'card-head' },
      h('span', { class: 'card-name', text: tool.name, title: tool.name }), tag),
    h('p', { class: 'card-desc', text: tool.description }),
    commands === '' ? null : h('div', { class: 'card-commands', text: commands, title: commands }),
    meta === '' ? null : h('div', { class: 'card-meta', text: meta }),
    error === undefined ? null : h('div', { class: 'card-error', text: error }),
    renderActions(tool),
  )
}

/** 渲染整个窗口。 */
function render() {
  if (view === undefined) return
  document.documentElement.lang = view.locale
  document.title = view.title
  el('search').placeholder = view.searchPlaceholder
  el('refresh').textContent = refreshPending ? view.refreshingLabel : view.refreshLabel
  el('refresh').disabled = refreshPending
  el('notice').hidden = view.notice === ''
  el('notice').textContent = view.notice
  el('summary').replaceChildren(
    h('span', { text: view.summaryLabel }),
    h('span', { class: 'spacer' }),
    h('span', { text: view.sourceLabel }),
  )
  renderCategories()
  const tools = visibleTools()
  el('grid').replaceChildren(...tools.map(renderCard))
  el('grid').hidden = tools.length === 0
  el('empty').hidden = tools.length !== 0
  el('empty').textContent = view.tools.length === 0 ? view.emptyLabel : view.noMatchesLabel
}

/**
 * 执行一次操作，把失败显示在对应卡片上。
 *
 * 失败不弹全局对话框：一次安装失败只和那张卡片有关，弹窗会打断其余浏览。
 */
async function run(action, id) {
  errors.delete(id)
  render()
  try {
    await action()
  } catch (error) {
    if (id !== undefined) {
      const message = error instanceof Error ? error.message : String(error)
      errors.set(id, message.replace(/^Error invoking remote method '[^']+': (Error: )?/, ''))
    }
  }
  render()
}

/** 拉取最新视图并重绘。 */
async function reload() {
  view = await window.dshToolchain.status()
  // 刷新后旧的错误提示多半已经过期，清掉避免残留误导。
  errors.clear()
  render()
}

el('search').addEventListener('input', event => {
  query = event.target.value
  render()
})

el('refresh').addEventListener('click', () => {
  refreshPending = true
  render()
  void run(async () => { await window.dshToolchain.refresh() }, undefined)
    .finally(() => { refreshPending = false; render() })
})

window.dshToolchain.subscribe(next => {
  view = next
  render()
})

window.dshToolchain.onProgress(report => {
  if (report.percent >= 100) progress.delete(report.id)
  else progress.set(report.id, report)
  // 安装结束时安装态由状态广播收尾，这里只驱动进度区。
  if (report.percent >= 100) installing = ''
  else installing = report.id
  render()
})

void reload()

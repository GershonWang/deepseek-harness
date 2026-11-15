import { mkdirSync, mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { parseToolIndex, toolsLayout, type Tool } from '../src/toolchain/catalog.ts'
import { BUNDLED_TOOLS, composeProgress, composeToolchainView, type ToolchainMessages } from '../src/toolchain-view.ts'

const roots: string[] = []
function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-tcview-'))
  roots.push(root)
  return root
}
afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
})

const messages: ToolchainMessages = {
  title: '工具链市场', searchPlaceholder: '搜索工具…', refresh: '刷新索引', refreshing: '正在刷新…',
  categoriesLabel: '分类', allCategories: '全部', noMatches: '没有匹配的工具。', empty: '当前没有可用的工具。',
  summary: '已安装 {installed} / {total}', install: '安装', installing: '正在安装…', cancel: '取消',
  uninstall: '卸载', switchVersion: '切换版本', update: '更新', installedTag: '已安装',
  bundledTag: '随包', version: '版本', category: '分类', commands: '命令',
  bundled: '内置工具链', bundledShow: '查看内置清单', bundledHint: '随应用交付。',
  deactivate: '切回随包版本', bundledActive: '随包', inactive: '已停用', phaseDownloading: '下载中',
  phaseVerifying: '校验中', phaseExtracting: '解压中', sourceRemote: '索引已更新',
  sourceCache: '使用缓存的索引', sourceBuiltin: '使用内置索引（离线）', noticeOffline: '无法刷新索引。',
}

/** Build one catalog tool with the given versions. */
function tool(id: string, versions: Array<{ version: string; major?: string; size?: number }>, provides: string[] = []): Tool {
  return {
    id, name: id, category: 'modern-cli', description: `${id} description`, provides, dependencies: [],
    versions: versions.map(entry => ({
      version: entry.version,
      url: `https://e.test/${id}.tar.gz`,
      sha256: 'a'.repeat(64),
      ...(entry.major === undefined ? {} : { major: entry.major }),
      ...(entry.size === undefined ? {} : { size: entry.size }),
    })),
  }
}
/** Compose a view over the given tools and an empty tools root. */
async function compose(tools: Tool[], options: { labels?: Record<string, string>; source?: 'remote' | 'cache' | 'builtin'; offline?: boolean } = {}) {
  const root = temporaryRoot()
  const index = parseToolIndex({ version: 1, tools, ...(options.labels === undefined ? {} : { category_labels: options.labels }) })
  return await composeToolchainView({
    index,
    layout: toolsLayout(root),
    messages,
    locale: 'zh',
    source: options.source ?? 'remote',
    offline: options.offline ?? false,
  })
}

describe('toolchain view', () => {
  it('always exposes the version so the user knows what will be installed', async () => {
    const view = await compose([tool('go', [{ version: '1.27.1', size: 70_553_950 }])])
    const card = view.tools[0]
    // A single-line tool still reports its line, which is what carries the label.
    expect(card?.lines[0]?.latest).toBe('1.27.1')
    expect(card?.lines[0]?.versions[0]?.size).toBe('67 MB')
  })

  it('offers a version selector only when a tool has several feature lines', async () => {
    const single = await compose([tool('go', [{ version: '1.27.1' }])])
    const multi = await compose([tool('jdk', [{ version: '21.0.12.1', major: '21' }, { version: '17.0.20.1', major: '17' }])])
    expect(single.tools[0]?.offersLines).toBe(false)
    expect(multi.tools[0]?.offersLines).toBe(true)
  })

  it('keeps shipped tools out of the grid and out of the category tabs', async () => {
    const view = await compose([tool('maven', [{ version: '3.9.16' }], ['mvn'])])
    // 随包工具不能出现在网格里：它们既装不了也卸不了，一张卡片只会画出不存在的控件，
    // 而 python3 与提供同一命令的可安装 python 并排更像装了两份。
    expect(view.tools.map(card => card.id)).toEqual(['maven'])
    expect(view.tools.every(card => !card.bundled)).toBe(true)
    expect(view.categories.map(category => category.id)).toEqual(['modern-cli'])
    // 清单本身仍在视图里，只是换到底部面板。
    expect(view.bundled.tools.map(entry => entry.id)).toEqual(BUNDLED_TOOLS.map(entry => entry.id))
    expect(view.bundled.heading).toBe('内置工具链')
  })

  it('carries a version for a shipped tool only when its command answers', async () => {
    const view = await compose([])
    for (const entry of view.bundled.tools) {
      // 探测失败只是少一个版本号，条目本身照常列出。
      expect(typeof entry.version).toBe('string')
      expect(entry.commands.length).toBeGreaterThan(0)
    }
  })

  it('offers the shipped copy a tool would shadow as a fallback', async () => {
    // 索引里叫 python，随包条目叫 python3，两者都提供 python3：按 id 匹配永远命不中，
    // 这正是重复卡片长期没被发现的原因，所以按命令匹配。
    const view = await compose([tool('python', [{ version: '3.14.7' }], ['python3', 'python', 'pip'])])
    // 版本号取决于执行测试的机器上装了什么，因此只断言名称与"版本要么是空串要么是
    // 形如 N.N.N 的真实值"，不把某个数字写死。
    expect(view.tools[0]?.bundledFallback?.name).toBe('Python 3')
    expect(view.tools[0]?.bundledFallback?.version).toMatch(/^(?:\d+\.\d+(?:\.\d+)?)?$/u)
    // 没有任何命令重合的工具不该被塞一个回退项。
    const maven = await compose([tool('maven', [{ version: '3.9.16' }], ['mvn'])])
    expect(maven.tools[0]?.bundledFallback).toBeUndefined()
  })

  it('tells an installed-but-stood-down tool apart from one that was never installed', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    const index = parseToolIndex({ version: 1, tools: [tool('python', [{ version: '3.14.7' }], ['python3'])] })
    mkdirSync(layout.versionDir('python', '3.14.7'), { recursive: true })
    // 目录在、current 链接不在 = 用户切回了随包那份；卡片仍要能卸载这个版本。
    const view = await composeToolchainView({ index, layout, messages, locale: 'zh', source: 'remote', offline: false })
    expect(view.tools[0]?.inactive).toBe(true)
    expect(view.tools[0]?.installed).toBe(true)
  })

  it('carries the commands a tool puts on PATH so the card can name them', async () => {
    const view = await compose([tool('maven', [{ version: '3.9.16' }], ['mvn'])])
    // 装 maven 的用处就是得到 mvn；卡片要把这件事直接写出来。
    expect(view.tools[0]?.commands).toEqual(['mvn'])
    const rust = await compose([tool('rust', [{ version: '1.98.1' }], ['rustc', 'cargo', 'rustfmt'])])
    expect(rust.tools[0]?.commands).toEqual(['rustc', 'cargo', 'rustfmt'])
  })

  it('reports an update only when the catalog is newer than the installed version', async () => {
    const root = temporaryRoot()
    const layout = toolsLayout(root)
    const index = parseToolIndex({ version: 1, tools: [tool('go', [{ version: '1.27.1' }, { version: '1.26.0' }])] })
    const view = await composeToolchainView({ index, layout, messages, locale: 'zh', source: 'remote', offline: false })
    // Nothing installed yet: no update to report.
    expect(view.tools[0]?.updateAvailable).toBe(false)
    mkdirSync(layout.versionDir('go', '1.26.0'), { recursive: true })
    const { symlinkSync } = await import('node:fs')
    mkdirSync(join(root, 'current'), { recursive: true })
    symlinkSync(layout.versionDir('go', '1.26.0'), layout.currentLink('go'))
    const updated = await composeToolchainView({ index, layout, messages, locale: 'zh', source: 'remote', offline: false })
    expect(updated.tools[0]?.updateAvailable).toBe(true)
    expect(updated.tools[0]?.updateVersion).toBe('1.27.1')
    expect(updated.tools[0]?.active).toBe('1.26.0')
  })

  it('labels categories from the index and falls back to the id', async () => {
    const view = await compose([tool('go', [{ version: '1.27.1' }])], { labels: { 'modern-cli': '现代 CLI' } })
    expect(view.categories[0]?.label).toBe('现代 CLI')
    expect(view.tools[0]?.category).toBe('现代 CLI')
    const unlabelled = await compose([tool('go', [{ version: '1.27.1' }])])
    // An unlabelled category is still readable and never empty.
    expect(unlabelled.categories[0]?.label).toBe('modern-cli')
  })

  it('counts only market tools in the summary', async () => {
    const view = await compose([tool('a', [{ version: '1' }]), tool('b', [{ version: '1' }])])
    // 随包工具不参与市场计数：它们不在网格里，也不占用「已安装 N / M」这两个数。
    expect(view.summaryLabel).toBe('已安装 0 / 2')
  })

  it('reports the index source and surfaces an offline notice', async () => {
    expect((await compose([], { source: 'builtin', offline: true })).sourceLabel).toBe('使用内置索引（离线）')
    const offline = await compose([], { offline: true })
    expect(offline.notice).toBe('无法刷新索引。')
    expect((await compose([], { offline: false })).notice).toBe('')
  })
})

describe('progress view', () => {
  it('is determinate only when the transfer declares a total', () => {
    const determinate = composeProgress('go', 'downloading', 40, 28_000_000, 70_000_000, messages)
    expect(determinate.determinate).toBe(true)
    expect(determinate.transferred).toBe('27 MB / 67 MB')
    expect(determinate.cancellable).toBe(true)
    // No declared total must not be rendered as 0%.
    expect(composeProgress('go', 'downloading', 0, 1024, undefined, messages).determinate).toBe(false)
  })

  it('localizes the stage and only cancels interruptible stages', () => {
    expect(composeProgress('go', 'verifying', 75, undefined, undefined, messages)).toMatchObject({
      phase: '校验中', percent: 75, cancellable: false,
    })
    expect(composeProgress('go', 'extracting', 85, undefined, undefined, messages).cancellable).toBe(true)
  })

  it('clamps a percentage the installer reports out of range', () => {
    expect(composeProgress('go', 'downloading', 140, 1, 2, messages).percent).toBe(100)
    expect(composeProgress('go', 'downloading', -5, 1, 2, messages).percent).toBe(0)
  })
})

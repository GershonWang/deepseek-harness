/**
 * Compose the toolchain window's view from the catalog and on-disk state.
 *
 * The document renders what it receives and decides nothing: every label, every
 * category name, and every derived flag (bundled, installed, update available) is
 * decided here. That keeps a language switch and a state change on the same path, and
 * stops the page from inventing copy the dictionaries do not define.
 *
 * @module @deepseek-ai/dsh-desktop/toolchain-view
 */

import {
  activeVersion,
  listInstalled,
  listVersions,
  buildVersionGroups,
  formatBytes,
  type Tool,
  type ToolIndex,
  type ToolsLayout,
} from './toolchain/catalog.ts'
import { compareVersions } from './toolchain/version.ts'
import { bundledCommandVersion } from './toolchain/bundled-versions.ts'
import type { ToolchainBundledView, ToolchainToolView, ToolchainView } from './toolchain-contract.ts'

/**
 * Tools the application already ships, with the commands each provides.
 *
 * The index lists only tools the market can install; a bundled tool must not appear
 * there, because installing a second copy under the tools root would shadow the one the
 * package delivers.
 *
 * These entries are deliberately *not* grid cards: they carry no install, switch, or
 * uninstall control — the copy lives in the container image and out of reach of the tools
 * root — so a card would only sketch controls that cannot work, and `python3` would sit
 * in the same grid as the installable `python` that provides the very same command. The
 * window lists them behind one footer panel instead, which answers "what does the package
 * already give me" without pretending they participate in the market.
 *
 * `readVersion` names the command whose `--version` output labels the entry. It is read
 * lazily and its failure is not an error: a missing command means the entry is simply
 * listed without a version.
 */
export const BUNDLED_TOOLS: readonly {
  readonly id: string
  readonly name: string
  readonly commands: readonly string[]
  readonly readVersion?: string
}[] = [
  { id: 'git', name: 'Git', commands: ['git', 'git-lfs'], readVersion: 'git' },
  { id: 'python3', name: 'Python 3', commands: ['python3'], readVersion: 'python3' },
  { id: 'curl', name: 'curl', commands: ['curl'], readVersion: 'curl' },
  { id: 'wget', name: 'Wget', commands: ['wget'], readVersion: 'wget' },
  { id: 'archive-tools', name: 'unzip / zip', commands: ['unzip', 'zip'] },
  { id: 'jq', name: 'jq', commands: ['jq'], readVersion: 'jq' },
  { id: 'xxd', name: 'xxd', commands: ['xxd'] },
]

/** Localized strings the view needs, all supplied by the shell locale. */
export interface ToolchainMessages {
  readonly title: string
  readonly searchPlaceholder: string
  readonly refresh: string
  readonly refreshing: string
  readonly categoriesLabel: string
  readonly allCategories: string
  readonly noMatches: string
  readonly empty: string
  readonly summary: string
  readonly install: string
  readonly installing: string
  readonly cancel: string
  readonly uninstall: string
  readonly switchVersion: string
  readonly update: string
  readonly installedTag: string
  readonly bundledTag: string
  readonly version: string
  readonly category: string
  readonly commands: string
  readonly bundled: string
  readonly bundledShow: string
  readonly bundledHint: string
  readonly deactivate: string
  readonly bundledActive: string
  readonly inactive: string
  readonly phaseDownloading: string
  readonly phaseVerifying: string
  readonly phaseExtracting: string
  readonly sourceRemote: string
  readonly sourceCache: string
  readonly sourceBuiltin: string
  readonly noticeOffline: string
}

/** Inputs for one view composition. */
export interface ComposeInput {
  /** The index in effect. */
  readonly index: ToolIndex
  /** Tools layout on disk. */
  readonly layout: ToolsLayout
  /** Localized copy. */
  readonly messages: ToolchainMessages
  /** Language tag for the document. */
  readonly locale: string
  /** Which source supplied the index. */
  readonly source: 'remote' | 'cache' | 'builtin'
  /** Whether the last fetch failed, which the window reports as a notice. */
  readonly offline: boolean
}

/**
 * Relevant installed version for a tool, preferring the reported active one.
 * @param installed - every installed version.
 * @param active - the active version, empty when none.
 * @returns the version to compare against, or undefined when nothing is installed.
 */
function effectiveVersion(installed: readonly string[], active: string): string | undefined {
  if (active !== '') return active
  let best = installed[0]
  for (const version of installed) {
    if (best !== undefined && compareVersions(version, best) > 0) best = version
  }
  return best
}

/**
 * Build one tool card's data.
 * @param tool - the catalog entry.
 * @param installed - its installed versions.
 * @param active - its active version, empty when none.
 * @param bundled - whether the application already ships it.
 * @param bundledFallback - the shipped copy this tool can fall back to, when any.
 * @returns the card.
 */
function composeTool(
  tool: Tool,
  installed: readonly string[],
  active: string,
  bundled: boolean,
  bundledFallback: { readonly name: string; readonly version: string } | undefined,
): ToolchainToolView {
  const groups = buildVersionGroups(tool, installed, active)
  const lines = groups.map(group => ({
    major: group.major,
    latest: group.latest,
    versions: group.versions.map((version) => {
      const entry = tool.versions.find(candidate => candidate.version === version)
      return {
        version,
        installed: installed.includes(version),
        active: version === active,
        size: formatBytes(entry?.size),
      }
    }),
  }))
  const newest = tool.versions[0]?.version
  const current = effectiveVersion(installed, active)
  // An update exists only when the catalog offers something newer than what is installed.
  // A missing `newest` (a tool with no versions, which parsing rejects) cannot be newer.
  const pending = newest !== undefined && current !== undefined && compareVersions(newest, current) > 0
  const updateVersion = pending ? newest : ''
  return {
    id: tool.id,
    name: tool.name,
    category: '',
    categoryId: tool.category,
    description: tool.description,
    commands: tool.provides,
    bundled,
    installed: installed.length > 0,
    active,
    lines,
    offersLines: lines.length > 1,
    updateAvailable: pending,
    updateVersion,
    ...(bundledFallback === undefined ? {} : { bundledFallback }),
    // 有版本目录却没有 current 链接 = 用户主动切回了随包那份。这和"没装"必须分开：
    // 文件还在磁盘上，卸载入口仍要留着。
    inactive: installed.length > 0 && active === '',
  }
}

/**
 * Build the shipped-tool list shown behind the footer panel.
 *
 * Versions are probed only for entries that name a command: a probe spawns a child
 * process, and the list is composed again on every window repaint, so probing an entry
 * with no interpretable `--version` would cost a process per repaint for a fact that
 * cannot be shown. {@link bundledCommandVersion} caches each result for the process.
 * @returns one entry per shipped tool, in declaration order.
 */
async function composeBundledTools(): Promise<ToolchainBundledView[]> {
  const entries: ToolchainBundledView[] = []
  for (const entry of BUNDLED_TOOLS) {
    const version = entry.readVersion === undefined ? '' : await bundledCommandVersion(entry.readVersion)
    entries.push({ id: entry.id, name: entry.name, commands: entry.commands, version })
  }
  return entries
}

/**
 * Find the shipped copy an installable tool would shadow.
 *
 * The match is by command, not by id: the index names the tool `python` while the shipped
 * entry is `python3`, and both put `python3` on PATH. An id-based match — the shape an
 * earlier implementation used — therefore never fired, which is why the duplication this
 * replaces went unnoticed.
 * @param tool - the installable tool.
 * @param bundled - the shipped entries, already composed.
 * @returns the shipped copy whose commands overlap, or undefined when none does.
 */
function shadowedBundledCopy(
  tool: Tool,
  bundled: readonly ToolchainBundledView[],
): { readonly name: string; readonly version: string } | undefined {
  if (tool.provides.length === 0) return undefined
  for (const entry of bundled) {
    if (entry.commands.some(command => tool.provides.includes(command))) {
      return { name: entry.name, version: entry.version }
    }
  }
  return undefined
}

/**
 * Compose the whole view.
 *
 * Categories come from the index so a new one appears without a client release; a tool
 * whose category the index does not label falls back to the id, which is still readable
 * and never empty.
 * @param input - index, on-disk state, and localized copy.
 * @returns the view.
 */
export async function composeToolchainView(input: ComposeInput): Promise<ToolchainView> {
  const installedIds = new Set(await listInstalled(input.layout))
  // Shipped tools stay out of the grid and out of the category tabs: they cannot be
  // installed or removed, so a card would only sketch controls that cannot work, and
  // `python3` would sit beside the installable `python` that provides the same command.
  const bundled = await composeBundledTools()
  const tools: ToolchainToolView[] = []
  for (const tool of input.index.tools) {
    const installed = await listVersions(input.layout, tool.id)
    const active = installed.length === 0 ? '' : await activeVersion(input.layout, tool.id)
    // The index never lists a bundled tool, so an index card is always installable. It
    // may still shadow a shipped command (`python` provides `python3`), and then the
    // card offers that shipped copy as the fallback it can stand down to.
    const card = composeTool(tool, installed, active, false, shadowedBundledCopy(tool, bundled))
    tools.push({ ...card, category: input.index.category_labels?.[tool.category] ?? tool.category })
    if (installed.length > 0) installedIds.add(tool.id)
  }
  const counts = new Map<string, number>()
  for (const tool of tools) counts.set(tool.categoryId, (counts.get(tool.categoryId) ?? 0) + 1)
  const categories = [...counts].map(([id, count]) => ({
    id,
    count,
    label: input.index.category_labels?.[id] ?? id,
  }))
  const installedCount = tools.filter(tool => tool.installed).length
  const sourceLabel = input.source === 'remote'
    ? input.messages.sourceRemote
    : input.source === 'cache' ? input.messages.sourceCache : input.messages.sourceBuiltin
  return {
    locale: input.locale,
    title: input.messages.title,
    searchPlaceholder: input.messages.searchPlaceholder,
    refreshLabel: input.messages.refresh,
    refreshingLabel: input.messages.refreshing,
    categoriesLabel: input.messages.categoriesLabel,
    allCategoriesLabel: input.messages.allCategories,
    noMatchesLabel: input.messages.noMatches,
    emptyLabel: input.messages.empty,
    summaryLabel: input.messages.summary.replace('{installed}', String(installedCount)).replace('{total}', String(tools.length)),
    sourceLabel,
    notice: input.offline ? input.messages.noticeOffline : '',
    labels: {
      install: input.messages.install,
      installing: input.messages.installing,
      cancel: input.messages.cancel,
      uninstall: input.messages.uninstall,
      switchVersion: input.messages.switchVersion,
      update: input.messages.update,
      installedTag: input.messages.installedTag,
      bundledTag: input.messages.bundledTag,
      version: input.messages.version,
      category: input.messages.category,
      commands: input.messages.commands,
    },
    categories,
    tools,
    bundled: {
      label: input.messages.bundledShow,
      heading: input.messages.bundled,
      hint: input.messages.bundledHint,
      tools: bundled,
    },
    deactivateLabel: input.messages.deactivate,
    bundledActiveLabel: input.messages.bundledActive,
    commandsLabel: input.messages.commands,
    inactiveLabel: input.messages.inactive,
  }
}

/**
 * Compose one progress report for the document.
 * @param id - tool being installed.
 * @param phase - stage identifier reported by the installer.
 * @param percent - completion percentage.
 * @param received - bytes written so far, when known.
 * @param total - declared total bytes, when the server sent one.
 * @param messages - localized stage names.
 * @returns the progress view.
 */
export function composeProgress(
  id: string,
  phase: string,
  percent: number,
  received: number | undefined,
  total: number | undefined,
  messages: ToolchainMessages,
): {
  id: string
  phase: string
  percent: number
  determinate: boolean
  transferred: string
  cancellable: boolean
} {
  const labels: Record<string, string> = {
    downloading: messages.phaseDownloading,
    verifying: messages.phaseVerifying,
    extracting: messages.phaseExtracting,
  }
  const determinate = phase === 'downloading' && total !== undefined && total > 0
  const transferred = determinate
    ? `${formatBytes(received ?? 0)} / ${formatBytes(total)}`
    : ''
  return {
    id,
    phase: labels[phase] ?? phase,
    percent: Math.max(0, Math.min(100, Math.round(percent))),
    determinate,
    transferred,
    // Only the transfer and the extraction can be interrupted; verification is brief.
    cancellable: phase === 'downloading' || phase === 'extracting',
  }
}

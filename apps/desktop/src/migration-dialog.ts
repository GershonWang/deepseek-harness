/**
 * Native prompt importing a legacy Web-client profile into the Desktop profile.
 *
 * The prompt is an isolated `dsh-app://shell/` document, following the existing
 * update-dialog pattern: main owns every string and every accepted answer, and the
 * renderer only draws the view it receives and reports the user's choices. It is a
 * separate window rather than part of the welcome flow because the welcome window
 * owns account onboarding, and mixing the two would make each flow's failure modes
 * unreadable.
 *
 * @module @deepseek-ai/dsh-desktop/migration-dialog
 */

import { BrowserWindow, ipcMain } from 'electron'
import { classifyPatches, type MigrationSelection, type MigrationSource, type PatchConflict, type PatchEntry } from './profile-migration.ts'
import {
  MIGRATION_IPC,
  type MigrationView,
} from './migration-contract.ts'
import { hideMenuBar } from './menu-bar.ts'
import { formatDesktopMessage, type DesktopLocale } from './locale.ts'

const page = 'dsh-app://shell/migration.html'

/** Scheme and host every shell-owned document shares; a sender outside it is foreign. */
const SHELL_ORIGIN = 'dsh-app://shell/'

/**
 * Validate one submitted response against the view that produced it.
 *
 * A response may only name plugins and conflicts the prompt displayed: the
 * renderer is a distinct document, so this is a wire boundary rather than a place
 * to trust the static type.
 * @param value - untrusted response payload.
 * @param view - the view the document was showing.
 * @returns the validated selection.
 * @throws when the payload names something the view never offered.
 */
export function validateMigrationResponse(value: unknown, view: MigrationView): MigrationSelection {
  if (typeof value !== 'object' || value === null) throw new Error('migration: response must be an object')
  const record = value as Record<string, unknown>
  const action = record.action
  if (action === 'skip') return { settings: false, plugins: [], conflicts: {} }
  if (action !== 'import') throw new Error('migration: unknown action')
  if (typeof record.settings !== 'boolean') throw new Error('migration: settings must be a boolean')
  const offeredPlugins = new Set(view.plugins.items.map(item => item.name))
  const plugins = record.plugins
  if (!Array.isArray(plugins)) throw new Error('migration: plugins must be a list')
  const selected: string[] = []
  for (const name of plugins) {
    if (typeof name !== 'string' || !offeredPlugins.has(name)) throw new Error('migration: unoffered plugin')
    selected.push(name)
  }
  const offeredConflicts = new Set(view.conflicts.items.map(item => item.id))
  const rawConflicts = record.conflicts
  if (typeof rawConflicts !== 'object' || rawConflicts === null) throw new Error('migration: conflicts must be an object')
  const conflicts: Record<string, 'legacy' | 'desktop'> = {}
  for (const [id, side] of Object.entries(rawConflicts as Record<string, unknown>)) {
    if (!offeredConflicts.has(id)) throw new Error('migration: unoffered conflict')
    if (side !== 'legacy' && side !== 'desktop') throw new Error('migration: unknown conflict side')
    conflicts[id] = side
  }
  return { settings: record.settings, plugins: selected, conflicts }
}

/** One modal prompt over the main window. */
export class DesktopMigrationDialog {
  private view: MigrationView | undefined
  private resolve: ((selection: MigrationSelection | undefined) => void) | undefined
  private window: BrowserWindow | undefined
  private closeLabel = ''

  /**
   * @param preload - absolute path of the isolated migration preload.
   * @param parent - window the prompt is modal to, when one exists.
   */
  constructor(private readonly preload: string, private readonly parent?: BrowserWindow) {}

  /**
   * Show the prompt and await the user's choice.
   * @param view - the content to display.
   * @param closeLabel - label for the window close control, which declines the import.
   * @returns the chosen selection, or undefined when the window was closed.
   */
  async prompt(view: MigrationView, closeLabel: string): Promise<MigrationSelection | undefined> {
    this.view = view
    this.closeLabel = closeLabel
    const window = new BrowserWindow({
      width: 720,
      height: 660,
      minWidth: 560,
      minHeight: 480,
      modal: this.parent !== undefined,
      ...(this.parent === undefined ? {} : { parent: this.parent }),
      show: false,
      title: view.title,
      webPreferences: {
        preload: this.preload,
        nodeIntegration: false,
        contextIsolation: true,
        sandbox: true,
      },
    })
    this.window = window
    // 迁移弹窗是一问一答的本地化文档，原生 Application/Edit 行在这场对话里没有任何
    // 用处；自动隐藏又会让 Alt 把它拉回来，所以直接移除整条菜单栏。
    hideMenuBar(window)
    window.once('ready-to-show', () => { window.show() })
    const settled = new Promise<MigrationSelection | undefined>((resolve) => { this.resolve = resolve })
    window.on('closed', () => { this.finish(undefined) })
    this.installHandlers()
    try {
      await window.loadURL(page)
    } catch (error) {
      this.finish(undefined)
      throw error
    }
    return settled
  }

  /** Register the document's IPC handlers for the lifetime of this prompt. */
  private installHandlers(): void {
    ipcMain.removeHandler(MIGRATION_IPC.ready)
    ipcMain.handle(MIGRATION_IPC.ready, (event) => {
      this.assertSender(event.senderFrame?.url)
      return this.view
    })
    ipcMain.removeHandler(MIGRATION_IPC.respond)
    ipcMain.handle(MIGRATION_IPC.respond, (event, payload: unknown) => {
      this.assertSender(event.senderFrame?.url)
      const view = this.view
      if (view === undefined) throw new Error('migration: no active prompt')
      this.finish(validateMigrationResponse(payload, view))
      return undefined
    })
  }

  /**
   * Reject any sender that is not the shell-owned migration document.
   * @param url - sender frame URL, when the frame still exists.
   */
  private assertSender(url: string | undefined): void {
    if (url === undefined || !url.startsWith(SHELL_ORIGIN)) {
      throw new Error('migration: rejected foreign sender')
    }
  }

  /** Settle the prompt once and tear the window down. */
  private finish(selection: MigrationSelection | undefined): void {
    const resolve = this.resolve
    this.resolve = undefined
    ipcMain.removeHandler(MIGRATION_IPC.ready)
    ipcMain.removeHandler(MIGRATION_IPC.respond)
    const window = this.window
    this.window = undefined
    if (window !== undefined && !window.isDestroyed()) window.destroy()
    resolve?.(selection)
  }

  /** Release the window without accepting a choice. */
  dispose(): void {
    this.finish(undefined)
  }

  /** Label for the window's close control. */
  get closeControlLabel(): string { return this.closeLabel }
}

/**
 * Absolute path of the migration preload shipped beside the main bundle.
 *
 * The path is derived without `node:url`, because the migration preload imports
 * this module for its IPC channel names and a sandboxed preload may load only
 * Electron and a fixed builtin allowlist.
 * @returns the preload path.
 */
export function migrationPreloadPath(): string {
  return `${import.meta.dirname}/preload-migration.cjs`
}

/**
 * Compose the prompt content from the detected source and the active locale.
 *
 * Every string comes from the shell dictionary, so the document owns no copy of
 * its own. Plugin rows start checked: the default is to import everything the
 * legacy profile offered, and the three existing fail-soft layers (missing package,
 * version mismatch, platform mismatch) keep a bad plugin from blocking startup.
 * Only genuinely differing ids reach the conflict list; identical and one-sided
 * entries need no adjudication.
 * @param source - what the legacy profile offered.
 * @param desktopPatches - the desktop profile's current entries.
 * @param locale - active shell locale.
 * @param labels - human labels for conflicting ids, keyed by id.
 * @returns the view to display.
 */
export function buildMigrationView(
  source: MigrationSource,
  desktopPatches: readonly PatchEntry[],
  locale: DesktopLocale,
  labels: Readonly<Record<string, string>> = {},
): MigrationView {
  const messages = locale.messages
  const preview = (serialized: string): string => {
    try {
      const parsed: unknown = JSON.parse(serialized)
      const config = typeof parsed === 'object' && parsed !== null && 'config' in parsed
        ? (parsed as { config?: unknown }).config
        : undefined
      return JSON.stringify(config ?? parsed, undefined, 2)
    } catch {
      return serialized
    }
  }
  const differing = classifyPatches(source.patches, desktopPatches)
    .filter((item): item is Extract<PatchConflict, { kind: 'differing' }> => item.kind === 'differing')
  return {
    locale: locale.id,
    title: messages.migrationTitle,
    intro: messages.migrationIntro,
    settings: {
      label: messages.migrationSettingsLabel,
      detail: formatDesktopMessage(messages.migrationSettingsDetail, { count: String(source.patches.length) }),
      checked: source.patches.length > 0,
    },
    plugins: {
      heading: messages.migrationPluginsHeading,
      note: messages.migrationPluginsNote,
      items: source.plugins.map(plugin => ({
        name: plugin.name,
        detail: plugin.spec,
        checked: true,
      })),
    },
    conflicts: {
      heading: messages.migrationConflictsHeading,
      note: messages.migrationConflictsNote,
      useDesktopAll: messages.migrationUseDesktopAll,
      items: differing.map(conflict => ({
        id: conflict.id,
        label: labels[conflict.id] ?? conflict.id,
        legacy: { side: 'legacy', label: messages.migrationLegacyLabel, preview: preview(conflict.legacy.serialized) },
        desktop: { side: 'desktop', label: messages.migrationDesktopLabel, preview: preview(conflict.desktop.serialized) },
        // The import defaults to the source value, matching {@link composeMergedPatches}.
        selected: 'legacy',
      })),
    },
    retentionNote: messages.migrationRetentionNote,
    unsupportedNote: messages.migrationUnsupportedNote,
    buttons: { skip: messages.migrationSkip, import: messages.migrationImport },
  }
}

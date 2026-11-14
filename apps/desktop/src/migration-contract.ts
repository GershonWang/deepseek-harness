/**
 * Wire contract shared by the profile migration prompt and its isolated document.
 *
 * This module deliberately imports nothing: a sandboxed preload may load only
 * Electron and a fixed builtin allowlist, so the bridge cannot reach the window
 * module that owns `node` dependencies. Keeping the contract here lets both sides
 * agree on channel names and payload fields without pulling that graph in.
 *
 * @module @deepseek-ai/dsh-desktop/migration-contract
 */

/** Channels available only to the isolated migration document. */
export const MIGRATION_IPC = {
  ready: 'dsh-migration:ready',
  changed: 'dsh-migration:changed',
  respond: 'dsh-migration:respond',
} as const

/** One plugin offered for import. */
export interface MigrationPluginView {
  /** Package name, used as the selection key. */
  readonly name: string
  /** Secondary line describing the plugin. */
  readonly detail: string
  /** Initial checkbox state. */
  readonly checked: boolean
}

/** One side of a differing patch entry. */
export interface MigrationConflictSide {
  /** Which profile the value came from. */
  readonly side: 'legacy' | 'desktop'
  /** Localized label naming that profile. */
  readonly label: string
  /** The entry's configuration, formatted for reading. */
  readonly preview: string
}

/** One differing patch id the user adjudicates. */
export interface MigrationConflictView {
  /** Loader id, used as the selection key. */
  readonly id: string
  /** Human label for the entry. */
  readonly label: string
  /** The legacy profile's copy. */
  readonly legacy: MigrationConflictSide
  /** The desktop profile's copy. */
  readonly desktop: MigrationConflictSide
}

/** Everything the document renders; every string is main-owned. */
export interface MigrationView {
  readonly locale: string
  readonly title: string
  readonly intro: string
  readonly settings: { readonly label: string; readonly detail: string; readonly checked: boolean }
  readonly plugins: { readonly heading: string; readonly note: string; readonly items: readonly MigrationPluginView[] }
  readonly conflicts: {
    readonly heading: string
    readonly note: string
    readonly useDesktopAll: string
    readonly items: readonly MigrationConflictView[]
  }
  readonly retentionNote: string
  readonly unsupportedNote: string
  readonly buttons: { readonly skip: string; readonly import: string }
}

/** The renderer may submit only a selection drawn from the displayed view. */
export interface MigrationResponse {
  readonly action: 'import' | 'skip'
  readonly settings: boolean
  readonly plugins: readonly string[]
  readonly conflicts: Readonly<Record<string, 'legacy' | 'desktop'>>
}

/** Bridge exposed to the isolated document. */
export interface MigrationApi {
  ready(): Promise<MigrationView>
  respond(response: MigrationResponse): Promise<void>
  subscribe(listener: (view: MigrationView) => void): () => void
}

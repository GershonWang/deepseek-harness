/**
 * Wire contract shared by the About window and its isolated document.
 *
 * This module deliberately imports nothing: a sandboxed preload may load only
 * Electron and a fixed builtin allowlist, so the bridge cannot reach a module
 * that owns `node` dependencies. Keeping the channel names and the payload
 * fields here lets both sides agree without pulling that graph in.
 *
 * @module @deepseek-ai/dsh-desktop/about-contract
 */

/** Channels available only to the isolated About document. */
export const ABOUT_IPC = {
  status: 'dsh-about:status',
  changed: 'dsh-about:changed',
  copyPackageId: 'dsh-about:copy-package-id',
  open: 'dsh-about:open',
} as const

/** One labelled fact in the About window's field grid. */
export interface AboutFieldView {
  /** Field name, already localized to the shell language. */
  readonly label: string
  /** Field value; a repository field renders as a link instead. */
  readonly value: string
  /** Destination handed back to main for a validated external open, when this row is a link. */
  readonly url?: string
}

/**
 * Everything the About document renders.
 *
 * Every string is main-owned: the document never composes copy of its own, so
 * the window could not drift from the shell language or show an untranslated
 * label that the dictionaries do not define.
 */
export interface DesktopAboutView {
  /** Document language, so the page can set its own `lang`. */
  readonly locale: string
  /** Window title and the tray entry that opens it. */
  readonly title: string
  /** Product name. */
  readonly product: string
  /** Desktop client version, including any prerelease suffix. */
  readonly version: string
  /** The shell generation this build is, as a localized badge. */
  readonly form: string
  /** Linglong package id, also the value the copy control writes. */
  readonly packageId: string
  /** Accessible description of the brand mark. */
  readonly logoAlt: string
  /** Tooltip and accessible name of the copy control. */
  readonly copyLabel: string
  /** Transient confirmation that the copy control wrote the clipboard. */
  readonly copiedLabel: string
  /** Field grid below the identity block. */
  readonly fields: readonly AboutFieldView[]
}

/** The document can only ask for main-owned data and actions. */
export interface DesktopAboutApi {
  status(): Promise<DesktopAboutView>
  copyPackageId(): Promise<boolean>
  open(url: string): Promise<void>
  subscribe(listener: (view: DesktopAboutView) => void): () => void
}

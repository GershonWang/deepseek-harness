/**
 * Wire contract shared by the toolchain window and its isolated document.
 *
 * This module imports nothing: a sandboxed preload may load only Electron and a fixed
 * builtin allowlist, so the bridge cannot reach a module that owns `node` dependencies.
 * Keeping the channel names and payload fields here lets both sides agree without
 * pulling that graph in.
 *
 * Every string the window renders is composed in main and arrives here, so the document
 * cannot drift from the shell language or show an untranslated label.
 *
 * @module @deepseek-ai/dsh-desktop/toolchain-contract
 */

/** Channels available only to the isolated toolchain document. */
export const TOOLCHAIN_IPC = {
  /** Read the whole catalog and installation state. */
  status: 'dsh-toolchain:status',
  /** Pushed when the state changed for any reason. */
  changed: 'dsh-toolchain:changed',
  /** One progress report from a running install. */
  progress: 'dsh-toolchain:progress',
  /** Start an install. */
  install: 'dsh-toolchain:install',
  /** Cancel the running install. */
  cancel: 'dsh-toolchain:cancel',
  /** Remove an installed version. */
  uninstall: 'dsh-toolchain:uninstall',
  /** Switch the active version. */
  activate: 'dsh-toolchain:activate',
  /** Refetch the remote index. */
  refresh: 'dsh-toolchain:refresh',
} as const

/** One selectable version inside a feature line. */
export interface ToolchainVersionView {
  /** Version label, always shown so the user knows what will be installed. */
  readonly version: string
  /** Whether this exact version is on disk. */
  readonly installed: boolean
  /** Whether this version is currently active. */
  readonly active: boolean
  /** Human-readable archive size, empty when the index carries none. */
  readonly size: string
}

/** One feature line a card offers. */
export interface ToolchainLineView {
  /** Line identifier, for example `21` or `3.13`. */
  readonly major: string
  /** Newest version on this line; the one an install would fetch. */
  readonly latest: string
  /** Versions offered on this line, newest first. */
  readonly versions: readonly ToolchainVersionView[]
}

/** One tool card. */
export interface ToolchainToolView {
  /** Tool id. */
  readonly id: string
  /** Display name. */
  readonly name: string
  /** Localized category label. */
  readonly category: string
  /** Category id, for grouping. */
  readonly categoryId: string
  /** Short description. */
  readonly description: string
  /**
   * Commands this tool puts on PATH once installed.
   *
   * Empty for shipped tools, whose commands the bundled manifest lists instead. The
   * card shows them because the point of installing `maven` is getting `mvn`.
   */
  readonly commands: readonly string[]
  /**
   * Whether this tool ships with the application.
   *
   * A bundled tool is listed so the user can see the whole picture, but it cannot be
   * installed, switched, or removed here.
   */
  readonly bundled: boolean
  /** Whether any version is installed. */
  readonly installed: boolean
  /** Active version label, empty when none. */
  readonly active: string
  /** Feature lines; a single-line tool still reports one so its version stays visible. */
  readonly lines: readonly ToolchainLineView[]
  /** Whether a version selector is meaningful (more than one line). */
  readonly offersLines: boolean
  /** Whether an update is available over the installed version. */
  readonly updateAvailable: boolean
  /** Version the update control installs, empty when there is none. */
  readonly updateVersion: string
}

/** One tool the application already ships. */
export interface ToolchainBundledView {
  /** Stable id. */
  readonly id: string
  /** Display name. */
  readonly name: string
  /** Commands it puts on PATH, always non-empty. */
  readonly commands: readonly string[]
  /** Installed version when it could be read, empty when unknown. */
  readonly version: string
}

/** One progress report rendered on a card. */
export interface ToolchainProgressView {
  /** Tool being installed. */
  readonly id: string
  /** Stage now running, already localized. */
  readonly phase: string
  /** 0 to 100. */
  readonly percent: number
  /** Whether the transfer carries a determinate total. */
  readonly determinate: boolean
  /** Received and total bytes as a short label, empty when unknown. */
  readonly transferred: string
  /** Whether a cancel control applies. */
  readonly cancellable: boolean
}

/** Everything the toolchain document renders. */
export interface ToolchainView {
  /** Document language. */
  readonly locale: string
  /** Window title. */
  readonly title: string
  /** Placeholder of the search field. */
  readonly searchPlaceholder: string
  /** Label of the refresh control. */
  readonly refreshLabel: string
  /** Label shown while refreshing. */
  readonly refreshingLabel: string
  /** Label above the category tabs. */
  readonly categoriesLabel: string
  /** Label for the "all categories" tab. */
  readonly allCategoriesLabel: string
  /** Empty-state copy for a filtered list that matches nothing. */
  readonly noMatchesLabel: string
  /** Empty-state copy for an empty catalog. */
  readonly emptyLabel: string
  /** Label of the installed/total summary. */
  readonly summaryLabel: string
  /** Where the catalog came from, already localized. */
  readonly sourceLabel: string
  /** Non-fatal notice, for example that the index could not be refreshed. */
  readonly notice: string
  /** Field labels used by the cards. */
  readonly labels: {
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
    /** Label introducing the command list on a card. */
    readonly commands: string
  }
  /** Category tabs, in display order. */
  readonly categories: readonly { readonly id: string; readonly label: string; readonly count: number }[]
  /** Tool cards. */
  readonly tools: readonly ToolchainToolView[]
  /**
   * Tools the package already ships.
   *
   * Kept out of {@link tools} on purpose: they cannot be installed, switched, or
   * removed, so a card in the grid would offer controls for actions that do not exist
   * and duplicate the installable tool that provides the same command. The window lists
   * them behind one footer entry instead.
   */
  readonly bundled: {
    /** Label of the footer control that reveals the list. */
    readonly label: string
    /** Heading shown inside the panel. */
    readonly heading: string
    /** One line explaining why these entries carry no controls. */
    readonly hint: string
    /** Entries in display order. */
    readonly tools: readonly ToolchainBundledView[]
  }
  /** Route back to the bundled copy of a tool, present only when the market replaced it. */
  readonly deactivateLabel: string
  /** Tag marking the bundled copy as the one currently in effect. */
  readonly bundledActiveLabel: string
  /** Label of the command list on a bundled entry. */
  readonly commandsLabel: string
}

/** The document can only ask for main-owned data and actions. */
export interface ToolchainApi {
  /** Read the current view. */
  status(): Promise<ToolchainView>
  /** Install one version of one tool. */
  install(id: string, version: string): Promise<void>
  /** Cancel the running install. */
  cancel(): Promise<void>
  /** Remove one installed version. */
  uninstall(id: string, version: string): Promise<void>
  /** Activate one installed version. */
  activate(id: string, version: string): Promise<void>
  /** Refetch the remote index. */
  refresh(): Promise<void>
  /** Follow state changes. */
  subscribe(listener: (view: ToolchainView) => void): () => void
  /** Follow install progress. */
  onProgress(listener: (progress: ToolchainProgressView) => void): () => void
}

/**
 * Deepin desktop appearance bridge: resolve the DDE dark mode for Chromium.
 *
 * DDE keeps its appearance in its own DBus service (`org.deepin.dde.Appearance1`)
 * and does not mirror the choice into the freedesktop keys Chromium reads
 * (`org.gnome.desktop.interface color-scheme`, `gtk-theme`). On a Deepin desktop
 * those keys keep reporting the light defaults while the user has selected
 * `deepin-dark`, so Electron resolves `prefers-color-scheme: light` and the Web UI
 * renders its light palette under a dark window manager frame.
 *
 * This module reads the DDE GTK theme over DBus and reports the resolved scheme.
 * It never changes what the user selected in the application: the caller applies
 * the result only while the preference is `system`, so an explicit light or dark
 * choice keeps its own meaning.
 *
 * @module @deepseek-ai/dsh-desktop/dde-appearance
 */

import { execFile, spawn } from 'node:child_process'
import type { ChildProcess } from 'node:child_process'

/** DBus service that owns the Deepin appearance settings. */
const DDE_SERVICE = 'org.deepin.dde.Appearance1'

/** Object path of that service. */
const DDE_PATH = '/org/deepin/dde/Appearance1'

/** Property holding the selected GTK theme name. */
const GTK_THEME_PROPERTY = 'GtkTheme'

/** Theme name suffix Deepin uses for the dark variant (`deepin-dark`). */
const DARK_SUFFIX = '-dark'

/** Timeout for one DBus query; a slow or absent bus must not delay startup. */
const QUERY_TIMEOUT_MS = 1_500

/**
 * Whether a GTK theme name denotes the dark variant.
 * @param theme - GTK theme name reported by DDE, or undefined when unavailable.
 * @returns whether the theme is dark.
 */
export function isDarkGtkTheme(theme: string | undefined): boolean {
  return theme !== undefined && theme.endsWith(DARK_SUFFIX)
}

/**
 * Extract the returned string from `gdbus call` output.
 *
 * `gdbus call` prints a GVariant tuple such as `(<'deepin-dark'>,)`. Only the
 * quoted value is needed, and a malformed reply must read as "no answer" rather
 * than throwing.
 * @param stdout - raw `gdbus call` output.
 * @returns the inner string, or undefined when the output carries none.
 */
export function parseGdbusString(stdout: string): string | undefined {
  const match = /<'([^']*)'>/.exec(stdout)
  return match?.[1]
}

/**
 * Read the GTK theme name DDE currently reports.
 *
 * Any failure — no bus, no service, a timeout, an unparsable reply — reports no
 * theme. The caller then keeps the platform's own resolution, which is the
 * behavior on every non-Deepin desktop.
 * @returns the theme name, or undefined when it cannot be read.
 */
export async function readDdeGtkTheme(): Promise<string | undefined> {
  return new Promise((resolve) => {
    execFile('gdbus', [
      'call', '--session',
      '--dest', DDE_SERVICE,
      '--object-path', DDE_PATH,
      '--method', 'org.freedesktop.DBus.Properties.Get',
      DDE_SERVICE, GTK_THEME_PROPERTY,
    ], { timeout: QUERY_TIMEOUT_MS, windowsHide: true }, (error, stdout) => {
      if (error !== null) { resolve(undefined); return }
      resolve(parseGdbusString(stdout))
    })
  })
}

/**
 * Resolve whether the Deepin desktop is currently dark.
 *
 * A missing theme reads as "not dark" so the caller falls back to Chromium's own
 * answer instead of forcing a palette.
 * @returns whether the DDE GTK theme is the dark variant.
 */
export async function isDeepinDark(): Promise<boolean> {
  return isDarkGtkTheme(await readDdeGtkTheme())
}

/** Stopping handle for one appearance subscription. */
export interface DdeAppearanceWatch {
  /** Stop listening and release the child process. */
  dispose(): void
}

/**
 * Follow DDE appearance changes.
 *
 * `gdbus monitor` streams the service's `Changed` and `PropertiesChanged`
 * signals; rather than parse the signal payload, each notification triggers one
 * fresh property read, so the reported state is always the value DDE holds now.
 * Readings are serialized and coalesced: a burst of signals yields one listener
 * call with the latest answer.
 * @param onChange - receives the resolved dark flag after every reported change.
 * @returns the stopping handle.
 */
export function watchDdeAppearance(onChange: (dark: boolean) => void): DdeAppearanceWatch {
  // Held in an object so the compiler does not narrow these flags across the reads
  // that follow an `await`; they are written from signal callbacks and dispose().
  const state: { disposed: boolean; pending: boolean; latest: boolean | undefined; emitting: boolean } = {
    disposed: false, pending: false, latest: undefined, emitting: false,
  }
  let child: ChildProcess | undefined

  const flush = (): void => {
    if (state.emitting || state.latest === undefined) return
    state.emitting = true
    const value = state.latest
    state.latest = undefined
    try {
      onChange(value)
    } finally {
      state.emitting = false
    }
  }

  /**
   * Read the current theme once and hand it to the listener.
   *
   * A signal that arrives while a read is in flight sets `pending`; the in-flight
   * read then starts one more, so the last answer always reflects the newest state
   * without piling up one read per signal.
   */
  /** Whether the watcher is still live; read through a call so the compiler cannot narrow it away. */
  const isLive = (): boolean => !state.disposed

  const refresh = async (): Promise<void> => {
    if (!isLive()) return
    if (state.pending) return
    state.pending = true
    const dark = await isDeepinDark()
    state.pending = false
    // A dispose during the read makes this answer moot.
    if (!isLive()) return
    state.latest = dark
    flush()
  }

  try {
    child = spawn('gdbus', [
      'monitor', '--session',
      '--dest', DDE_SERVICE,
      '--object-path', DDE_PATH,
    ], { stdio: ['ignore', 'pipe', 'ignore'], windowsHide: true })
    child.stdout?.setEncoding('utf8')
    child.stdout?.on('data', () => { void refresh() })
    // A missing gdbus or an absent service ends the monitor; the shell then keeps
    // Chromium's own resolution, which is the pre-existing behavior.
    child.on('error', () => undefined)
    child.unref()
  } catch {
    // Spawn failure is equivalent to an absent service.
    child = undefined
  }

  return {
    dispose(): void {
      state.disposed = true
      if (child !== undefined && child.exitCode === null) child.kill('SIGTERM')
      child = undefined
    },
  }
}

/** The subset of Electron's `nativeTheme` this bridge writes. */
export interface NativeThemeTarget {
  themeSource: 'system' | 'light' | 'dark'
}

/** Stopping handle for the whole bridge. */
export interface DdeThemeBridge extends DdeAppearanceWatch {
  /**
   * Record the application's own theme preference and re-resolve the native source.
   * @param next - the preference the application currently shows.
   */
  setPreference(next: 'system' | 'light' | 'dark'): void
}

/**
 * Keep Chromium's resolved palette in step with the Deepin desktop.
 *
 * The bridge applies a concrete `light` or `dark` only while the application's own
 * preference is `system`; an explicit choice is passed through untouched, because
 * the user already decided and DDE must not override it. Every call re-evaluates
 * one stored preference, so startup and later preference changes share a single
 * code path instead of each assigning `themeSource` on its own.
 *
 * On a desktop without the DDE service, every read reports no theme and the
 * assignment is skipped, leaving `system` as Chromium's own resolution.
 * @param nativeTheme - the Electron theme target to write.
 * @param dependencies - reader and watcher overrides; defaults read the live bus.
 * @returns the bridge handle.
 */
export function bridgeDdeTheme(
  nativeTheme: NativeThemeTarget,
  dependencies: {
    /** Whether this platform defers appearance to the Deepin service; defaults to yes. */
    readonly enabled?: boolean
    readonly readTheme?: () => Promise<string | undefined>
    readonly watch?: (onChange: (dark: boolean) => void) => DdeAppearanceWatch
  } = {},
): DdeThemeBridge {
  const enabled = dependencies.enabled ?? true
  const readTheme: () => Promise<string | undefined> = dependencies.readTheme ?? readDdeGtkTheme
  const watchChanges = dependencies.watch ?? watchDdeAppearance
  // Held in an object for the same reason as the watcher flags: these fields are
  // written by setPreference and dispose, then read after an await.
  const state: { preference: 'system' | 'light' | 'dark'; disposed: boolean } = {
    preference: 'system', disposed: false,
  }
  /** Whether a resolution that started earlier is now moot. Reads both fields so neither is narrowed away. */
  const isSettled = (): boolean => state.disposed || state.preference !== 'system'

  const apply = async (): Promise<void> => {
    if (state.disposed) return
    if (state.preference !== 'system') {
      nativeTheme.themeSource = state.preference
      return
    }
    const theme = await readTheme()
    // Re-checked after the await: dispose() or a preference change may have landed
    // while the DBus round trip was in flight, and the stale answer must not win.
    if (isSettled()) return
    // An unreadable theme leaves `system` in place rather than forcing a palette.
    if (theme === undefined) {
      nativeTheme.themeSource = 'system'
      return
    }
    nativeTheme.themeSource = isDarkGtkTheme(theme) ? 'dark' : 'light'
  }

  // A disabled bridge still forwards the preference, so callers keep a single entry
  // point; it just never consults the desktop service.
  const watch = enabled ? watchChanges(() => { void apply() }) : { dispose: (): void => undefined }
  return {
    /** Record the application's own preference and re-resolve. */
    setPreference(next: 'system' | 'light' | 'dark'): void {
      state.preference = next
      if (!enabled) { nativeTheme.themeSource = next; return }
      void apply()
    },
    dispose(): void {
      state.disposed = true
      watch.dispose()
    },
  }
}

/**
 * Read the GTK theme name a GTK settings file records.
 *
 * Exposed for diagnostics only: the running desktop is authoritative, and this
 * parse exists so a report can show both sides when they disagree.
 * @param contents - `settings.ini` contents.
 * @returns the recorded theme name, or undefined when the file does not name one.
 */
export function parseGtkSettingsTheme(contents: string): string | undefined {
  for (const line of contents.split(/\r?\n/u)) {
    const match = /^\s*gtk-theme-name\s*=\s*(.+)$/u.exec(line)
    if (match?.[1] !== undefined) return match[1].trim()
  }
  return undefined
}

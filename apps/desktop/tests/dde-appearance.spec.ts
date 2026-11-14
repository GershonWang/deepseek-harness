import { describe, expect, it, vi } from 'vitest'
import {
  bridgeDdeTheme,
  isDarkGtkTheme,
  parseGdbusString,
  parseGtkSettingsTheme,
} from '../src/dde-appearance.ts'

describe('DDE theme parsing', () => {
  it('reads the quoted value out of a gdbus call reply', () => {
    expect(parseGdbusString("(<'deepin-dark'>,)")).toBe('deepin-dark')
    expect(parseGdbusString("(<'deepin'>,)")).toBe('deepin')
  })

  it('reports no theme for output that carries no quoted value', () => {
    expect(parseGdbusString('')).toBeUndefined()
    expect(parseGdbusString('(nothing,)')).toBeUndefined()
  })

  it('treats only the -dark suffix as the dark variant', () => {
    expect(isDarkGtkTheme('deepin-dark')).toBe(true)
    expect(isDarkGtkTheme('deepin')).toBe(false)
    // The suffix is the whole discriminator: a name merely mentioning dark is light.
    expect(isDarkGtkTheme('dark-blue')).toBe(false)
    expect(isDarkGtkTheme(undefined)).toBe(false)
  })

  it('reads a theme name from a GTK settings file', () => {
    expect(parseGtkSettingsTheme('[Settings]\ngtk-theme-name=deepin-dark\n')).toBe('deepin-dark')
    expect(parseGtkSettingsTheme('[Settings]\ngtk-theme-name = deepin \n')).toBe('deepin')
    expect(parseGtkSettingsTheme('[Settings]\n')).toBeUndefined()
  })
})

describe('DDE theme bridge', () => {
  /**
   * Build a bridge over a stand-in nativeTheme.
   *
   * The reader and watcher are injected rather than mocked: the bridge calls them
   * through its own closure, and the real watcher would attach `gdbus monitor` to
   * this machine's session bus, feeding the host's actual theme into these cases.
   */
  function bridgeWith(theme: string | undefined) {
    const readTheme = vi.fn(async () => theme)
    const disposeWatch = vi.fn()
    const watch = vi.fn(() => ({ dispose: disposeWatch }))
    const target = { themeSource: 'system' as 'system' | 'light' | 'dark' }
    const bridge = bridgeDdeTheme(target, { readTheme, watch })
    return { readTheme, watch, disposeWatch, target, bridge }
  }

  it('applies the dark palette a Deepin desktop reports while following the system', async () => {
    const { target, bridge } = bridgeWith('deepin-dark')
    bridge.setPreference('system')
    await vi.waitFor(() => { expect(target.themeSource).toBe('dark') })
    bridge.dispose()
  })

  it('applies the light palette when the desktop reports a light theme', async () => {
    const { target, bridge } = bridgeWith('deepin')
    bridge.setPreference('system')
    await vi.waitFor(() => { expect(target.themeSource).toBe('light') })
    bridge.dispose()
  })

  it('leaves the platform resolution alone when the desktop reports no theme', async () => {
    const { target, bridge } = bridgeWith(undefined)
    bridge.setPreference('system')
    await vi.waitFor(() => { expect(target.themeSource).toBe('system') })
    bridge.dispose()
  })

  it('never overrides an explicit preference with the desktop value', async () => {
    const { readTheme, target, bridge } = bridgeWith('deepin-dark')
    bridge.setPreference('light')
    await vi.waitFor(() => { expect(target.themeSource).toBe('light') })
    // The desktop was never consulted, so a dark desktop cannot flip a fixed choice.
    expect(readTheme).not.toHaveBeenCalled()
    bridge.dispose()
  })

  it('forwards the preference untouched on a platform that resolves appearance itself', async () => {
    const readTheme = vi.fn(async () => 'deepin-dark')
    const watch = vi.fn(() => ({ dispose: () => undefined }))
    const target = { themeSource: 'system' as 'system' | 'light' | 'dark' }
    const bridge = bridgeDdeTheme(target, { enabled: false, readTheme, watch })
    bridge.setPreference('system')
    await vi.waitFor(() => { expect(target.themeSource).toBe('system') })
    // Windows and macOS own the answer, so the Deepin service must not be consulted.
    expect(readTheme).not.toHaveBeenCalled()
    expect(watch).not.toHaveBeenCalled()
    bridge.setPreference('dark')
    expect(target.themeSource).toBe('dark')
    bridge.dispose()
  })

  it('subscribes once and stops watching when disposed', () => {
    const { watch, disposeWatch, bridge } = bridgeWith('deepin')
    expect(watch).toHaveBeenCalledOnce()
    bridge.dispose()
    expect(disposeWatch).toHaveBeenCalledOnce()
  })

  it('re-resolves when the desktop reports a change', async () => {
    const readTheme = vi.fn<(value?: string) => Promise<string | undefined>>()
    const callbacks: ((dark: boolean) => void)[] = []
    const target = { themeSource: 'system' as 'system' | 'light' | 'dark' }
    const bridge = bridgeDdeTheme(target, {
      readTheme: () => readTheme(),
      watch: (onChange) => { callbacks.push(onChange); return { dispose: () => undefined } },
    })
    readTheme.mockResolvedValue('deepin')
    bridge.setPreference('system')
    await vi.waitFor(() => { expect(target.themeSource).toBe('light') })
    readTheme.mockResolvedValue('deepin-dark')
    callbacks[0]?.(true)
    await vi.waitFor(() => { expect(target.themeSource).toBe('dark') })
    bridge.dispose()
  })
})

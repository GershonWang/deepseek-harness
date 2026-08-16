import { chmodSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { delimiter, join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import { canExecute, hasLinuxChooserBinary } from '../src/probe.ts'
import { resolveDirectoryPickerBackend, overrideDirectoryPickerBackend } from '../src/resolve.ts'
import type { DirectoryPickerHostFacts } from '../src/resolve.ts'

/** Baseline facts that resolve to `native`; each case overrides one signal (darwin never consults `linuxChooser`). */
const attended: DirectoryPickerHostFacts = {
  bindHost: '127.0.0.1',
  platform: 'darwin',
  env: {},
  linuxChooser: false,
}

describe('resolveDirectoryPickerBackend', () => {
  it('resolves native for a loopback bind on a display platform', () => {
    expect(resolveDirectoryPickerBackend(attended)).toBe('native')
    expect(resolveDirectoryPickerBackend({ ...attended, platform: 'win32' })).toBe('native')
  })

  it('resolves browse for an all-interfaces bind regardless of other signals', () => {
    expect(resolveDirectoryPickerBackend({ ...attended, bindHost: '0.0.0.0' })).toBe('browse')
  })

  it('resolves browse under an SSH launch (either env marker)', () => {
    expect(resolveDirectoryPickerBackend({ ...attended, env: { SSH_CONNECTION: '10.0.0.2 55 10.0.0.9 22' } })).toBe('browse')
    expect(resolveDirectoryPickerBackend({ ...attended, env: { SSH_TTY: '/dev/pts/3' } })).toBe('browse')
  })

  it('requires a display session and a chooser binary on linux', () => {
    const linux: DirectoryPickerHostFacts = { ...attended, platform: 'linux', linuxChooser: true }
    expect(resolveDirectoryPickerBackend(linux)).toBe('browse')
    expect(resolveDirectoryPickerBackend({ ...linux, env: { DISPLAY: ':0' } })).toBe('native')
    expect(resolveDirectoryPickerBackend({ ...linux, env: { WAYLAND_DISPLAY: 'wayland-1' } })).toBe('native')
    expect(resolveDirectoryPickerBackend({ ...linux, env: { DISPLAY: ':0' }, linuxChooser: false })).toBe('browse')
  })

  it('resolves browse on platforms the native backend cannot serve, display or not', () => {
    expect(resolveDirectoryPickerBackend({ ...attended, platform: 'freebsd', env: { DISPLAY: ':0' }, linuxChooser: true })).toBe('browse')
    expect(resolveDirectoryPickerBackend({ ...attended, platform: 'openbsd', env: { WAYLAND_DISPLAY: 'wayland-1' } })).toBe('browse')
  })

  it('treats blank env exports as unset', () => {
    expect(resolveDirectoryPickerBackend({ ...attended, env: { SSH_CONNECTION: '', SSH_TTY: '' } })).toBe('native')
    expect(resolveDirectoryPickerBackend({
      ...attended, platform: 'linux', linuxChooser: true, env: { DISPLAY: '', WAYLAND_DISPLAY: '' },
    })).toBe('browse')
  })

  it('pins browse via DSH_DIRECTORY_PICKER even on an attended display session', () => {
    const linuxNative: DirectoryPickerHostFacts = {
      ...attended, platform: 'linux', linuxChooser: true, env: { DISPLAY: ':0' },
    }
    expect(resolveDirectoryPickerBackend(linuxNative)).toBe('native')
    expect(resolveDirectoryPickerBackend({ ...linuxNative, env: { DISPLAY: ':0', DSH_DIRECTORY_PICKER: 'browse' } })).toBe('browse')
  })

  it('pins native via DSH_DIRECTORY_PICKER even without a display session', () => {
    const linuxBrowse: DirectoryPickerHostFacts = {
      ...attended, platform: 'linux', linuxChooser: true, env: {},
    }
    expect(resolveDirectoryPickerBackend(linuxBrowse)).toBe('browse')
    expect(resolveDirectoryPickerBackend({ ...linuxBrowse, env: { DSH_DIRECTORY_PICKER: 'native' } })).toBe('native')
  })

  it('ignores a blank or unknown DSH_DIRECTORY_PICKER value', () => {
    expect(resolveDirectoryPickerBackend({ ...attended, env: { DSH_DIRECTORY_PICKER: '' } })).toBe('native')
    expect(resolveDirectoryPickerBackend({ ...attended, env: { DSH_DIRECTORY_PICKER: 'bogus' } })).toBe('native')
  })

  it('overrideDirectoryPickerBackend returns only the two valid kinds', () => {
    expect(overrideDirectoryPickerBackend({})).toBeUndefined()
    expect(overrideDirectoryPickerBackend({ DSH_DIRECTORY_PICKER: 'browse' })).toBe('browse')
    expect(overrideDirectoryPickerBackend({ DSH_DIRECTORY_PICKER: 'native' })).toBe('native')
    expect(overrideDirectoryPickerBackend({ DSH_DIRECTORY_PICKER: 'bogus' })).toBeUndefined()
    expect(overrideDirectoryPickerBackend({ DSH_DIRECTORY_PICKER: '' })).toBeUndefined()
  })
})

let probeRoot: string | undefined

afterEach(() => {
  if (probeRoot !== undefined) rmSync(probeRoot, { recursive: true, force: true })
  probeRoot = undefined
})

describe('hasLinuxChooserBinary', () => {
  it('finds a chooser binary in any PATH segment, skipping empty segments', () => {
    const seen: string[] = []
    const path = ['', '/opt/none', '/usr/local/bin'].join(delimiter)
    const found = hasLinuxChooserBinary(path, (candidate) => {
      seen.push(candidate)
      return candidate === join('/usr/local/bin', 'kdialog')
    })
    expect(found).toBe(true)
    expect(seen).toEqual([
      join('/opt/none', 'zenity'), join('/opt/none', 'kdialog'),
      join('/usr/local/bin', 'zenity'), join('/usr/local/bin', 'kdialog'),
    ])
  })

  it('reports absence when no segment holds a chooser binary', () => {
    expect(hasLinuxChooserBinary(['/a', '/b'].join(delimiter), () => false)).toBe(false)
    expect(hasLinuxChooserBinary('', () => true)).toBe(false)
    expect(hasLinuxChooserBinary(undefined, () => true)).toBe(false)
  })
})

describe('canExecute', () => {
  it('accepts an executable file and rejects an absent one', () => {
    probeRoot = mkdtempSync(join(tmpdir(), 'dsh-picker-probe-'))
    const binary = join(probeRoot, 'zenity')
    writeFileSync(binary, '#!/bin/sh\n')
    chmodSync(binary, 0o755)
    expect(canExecute(binary)).toBe(true)
    expect(canExecute(join(probeRoot, 'kdialog'))).toBe(false)
  })
})

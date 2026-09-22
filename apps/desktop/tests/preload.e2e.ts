/** Built preloads must execute with only Electron's sandbox-supported require. */
import { existsSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { runInNewContext } from 'node:vm'
import { describe, expect, it, vi } from 'vitest'

const preload = (name: string): string => fileURLToPath(new URL(`../lib/${name}.cjs`, import.meta.url))

describe.skipIf(!existsSync(preload('preload-app')))('built sandboxed Desktop preloads', () => {
  it.each(['preload-app', 'preload-welcome'])('%s loads without filesystem module access', (name) => {
    const exposed = new Map<string, Record<string, unknown>>()
    const invoke = vi.fn(() => Promise.resolve({ languages: ['en-US'], preference: 'zh' }))
    const send = vi.fn()
    const electron = {
      contextBridge: { exposeInMainWorld: (key: string, value: Record<string, unknown>) => { exposed.set(key, value) } },
      ipcRenderer: { invoke, send, on: vi.fn(), off: vi.fn() },
    }
    runInNewContext(readFileSync(preload(name), 'utf8'), {
      require: (id: string) => {
        if (id !== 'electron') throw new Error(`sandbox cannot load ${id}`)
        return electron
      },
      process: { argv: ['electron', '--dsh-welcome-locale=en'] },
      location: new URL('dsh-app://app/'),
      document: { documentElement: { dataset: {} } },
      exports: {},
    })
    if (name === 'preload-app') {
      const bridge = exposed.get('__DSH_LOCALE__') as { read(): unknown; onChange(locale: string): void }
      bridge.read()
      expect(invoke).toHaveBeenCalledWith('dsh-desktop:locale-bootstrap')
      bridge.onChange('zh')
      expect(send).toHaveBeenCalledWith('dsh-desktop:locale-changed', 'zh')
    } else {
      expect(exposed.has('dshWelcome')).toBe(true)
    }
  })
})

import { describe, expect, it, vi } from 'vitest'
import { Context } from '@deepseek-ai/cordis'
import {
  createLaunchEnvironmentSnapshot, DSH_LAUNCH_ENVIRONMENT_KEY, hostEscapeOf, launchedThroughSsh,
  launchEnvironmentOf,
} from '../src/index.ts'

const layered = createLaunchEnvironmentSnapshot([
  { source: 'process', values: { SHARED: 'from-process', ONLY_PROCESS: 'p' } },
  { source: 'project-env', path: '/work/.env', values: { SHARED: 'from-project', ONLY_PROJECT: 'j' } },
  { source: 'user-env', path: '/home/.dsh/.env', values: { SHARED: 'from-user', ONLY_USER: 'u' } },
])

describe('launchedThroughSsh', () => {
  it.each(['SSH_CONNECTION', 'SSH_TTY'].flatMap(name =>
    (['process', 'project-env', 'user-env'] as const).map(source => ({ name, source })),
  ))('classifies $name from $source', ({ name, source }) => {
    const snapshot = createLaunchEnvironmentSnapshot([{ source, values: { [name]: 'ssh-marker' } }])
    expect(launchedThroughSsh(snapshot)).toBe(source === 'process')
  })

  it.each([{}, { SSH_CONNECTION: '', SSH_TTY: '' }])('keeps absent or empty inherited markers local: %j', (values) => {
    const snapshot = createLaunchEnvironmentSnapshot([
      { source: 'process', values },
      { source: 'project-env', values: { SSH_CONNECTION: 'stale-connection' } },
      { source: 'user-env', values: { SSH_TTY: 'stale-tty' } },
    ])
    expect(launchedThroughSsh(snapshot)).toBe(false)
  })
})

describe('hostEscapeOf', () => {
  const fromProcess = (values: Record<string, string>) =>
    createLaunchEnvironmentSnapshot([{ source: 'process', values }])

  it('resolves the channel a sandbox declared in the inherited process layer', () => {
    expect(hostEscapeOf(fromProcess({ DSH_HOST_ROOTFS: '/run/host/rootfs', DSH_HOST_LAUNCH: 'systemd-run' })))
      .toEqual({ hostRootfs: '/run/host/rootfs', launcher: 'systemd-run' })
  })

  it.each<[string, Record<string, string>]>([
    ['no declaration', {}],
    ['only the host root mount', { DSH_HOST_ROOTFS: '/run/host/rootfs' }],
    ['only the launcher', { DSH_HOST_LAUNCH: 'systemd-run' }],
    ['empty values', { DSH_HOST_ROOTFS: '', DSH_HOST_LAUNCH: '' }],
    ['a relative host root', { DSH_HOST_ROOTFS: 'run/host/rootfs', DSH_HOST_LAUNCH: 'systemd-run' }],
    ['an unknown launcher', { DSH_HOST_ROOTFS: '/run/host/rootfs', DSH_HOST_LAUNCH: 'flatpak-spawn' }],
  ])('reports no channel for %s', (_case, values) => {
    expect(hostEscapeOf(fromProcess(values))).toBeUndefined()
  })

  it('never accepts the declaration from a project or user .env layer', () => {
    const snapshot = createLaunchEnvironmentSnapshot([
      { source: 'project-env', path: '/work/.env', values: { DSH_HOST_ROOTFS: '/run/host/rootfs' } },
      { source: 'user-env', path: '/home/.dsh/.env', values: { DSH_HOST_LAUNCH: 'systemd-run' } },
    ])
    expect(hostEscapeOf(snapshot)).toBeUndefined()
  })
})

describe('createLaunchEnvironmentSnapshot', () => {
  it('resolves across every layer, most trusted first, and reports the winning source', () => {
    expect(layered.get('SHARED')).toEqual({ value: 'from-process', source: 'process' })
    expect(layered.get('ONLY_PROJECT')).toEqual({ value: 'j', source: 'project-env', path: '/work/.env' })
    expect(layered.get('ONLY_USER')).toEqual({ value: 'u', source: 'user-env', path: '/home/.dsh/.env' })
    expect(layered.get('ABSENT')).toBeUndefined()
  })

  it('filters layers without changing their trust order', () => {
    // The point of getFrom: a routing field that must never come from a
    // project directory cannot be reached by reordering, only by listing it.
    expect(layered.getFrom('ONLY_PROJECT', ['process', 'user-env'])).toBeUndefined()
    expect(layered.getFrom('SHARED', ['user-env', 'process']))
      .toEqual({ value: 'from-process', source: 'process' })
    expect(layered.getFrom('SHARED', [])).toBeUndefined()
  })

  it('copies each layer, so a later mutation of the source object cannot change it', () => {
    const values: Record<string, string> = { KEY: 'first' }
    const snapshot = createLaunchEnvironmentSnapshot([{ source: 'process', values }])
    values.KEY = 'second'
    values.LATE = 'added'
    expect(snapshot.get('KEY')).toEqual({ value: 'first', source: 'process' })
    expect(snapshot.get('LATE')).toBeUndefined()
  })

  it('keeps an empty value as a present value, for its owner to judge', () => {
    const snapshot = createLaunchEnvironmentSnapshot([{ source: 'process', values: { EMPTY: '' } }])
    expect(snapshot.get('EMPTY')).toEqual({ value: '', source: 'process' })
  })

  it('orders lookups canonically regardless of construction order', () => {
    const reversed = createLaunchEnvironmentSnapshot([
      { source: 'user-env', path: '/u', values: { K: 'u' } },
      { source: 'process', values: { K: 'p' } },
    ])
    expect(reversed.get('K')).toEqual({ value: 'p', source: 'process' })
  })
})

describe('launchEnvironmentOf', () => {
  it('returns the launcher snapshot when the product CLI provided one', () => {
    const ctx = new Context()
    ctx.provide(DSH_LAUNCH_ENVIRONMENT_KEY, layered)
    expect(launchEnvironmentOf(ctx)).toBe(layered)
  })

  it('falls back to the inherited environment as the only layer', () => {
    vi.stubEnv('DSH_ENV_SPEC_FALLBACK', 'ambient')
    try {
      const snapshot = launchEnvironmentOf(new Context())
      expect(snapshot.get('DSH_ENV_SPEC_FALLBACK')).toEqual({ value: 'ambient', source: 'process' })
    } finally {
      vi.unstubAllEnvs()
    }
  })
})

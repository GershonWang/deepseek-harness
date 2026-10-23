/** Sandbox-declared host-escape channel resolution. */
import { describe, expect, it } from 'vitest'
import { createLaunchEnvironmentSnapshot } from '@deepseek-ai/dsh-launch-environment'
import { hostEscapeOf } from '../src/host-escape.ts'

const fromProcess = (values: Record<string, string>) =>
  createLaunchEnvironmentSnapshot([{ source: 'process', values }])

describe('hostEscapeOf', () => {
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

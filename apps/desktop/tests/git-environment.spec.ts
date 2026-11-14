import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import {
  applyGitEnvironment,
  GIT_EXEC_PATH_ENV,
  GIT_TEMPLATE_DIR_ENV,
  hasPackagedGitHelpers,
  packagedAppRoot,
  resolveGitEnvironment,
  withLeadingPath,
} from '../src/git-environment.ts'

const roots: string[] = []
function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-git-env-'))
  roots.push(root)
  return root
}
/**
 * Build an installation root shaped like the Linglong tree.
 * @param options.helpers - whether to ship the packaged helper directory.
 * @param options.templates - whether to ship the init templates.
 */
function installation(options: { helpers: boolean; templates: boolean }): string {
  const root = temporaryRoot()
  if (options.helpers) {
    const exec = join(root, 'lib', 'git-core')
    mkdirSync(exec, { recursive: true })
    // The remote helper is the one local operations never touch, so it is the marker.
    writeFileSync(join(exec, 'git-remote-https'), '#!/bin/sh\n', { mode: 0o755 })
  }
  if (options.templates) {
    mkdirSync(join(root, 'share', 'git-core', 'templates'), { recursive: true })
  }
  return root
}
afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
})

describe('packaged git environment', () => {
  it('resolves the exec path to the packaged helper directory', async () => {
    const root = installation({ helpers: true, templates: true })
    const entries = await resolveGitEnvironment(root, {})
    expect(entries[GIT_EXEC_PATH_ENV]).toBe(join(root, 'lib', 'git-core'))
  })

  it('resolves the template directory when the packaged tree carries one', async () => {
    const root = installation({ helpers: true, templates: true })
    const entries = await resolveGitEnvironment(root, {})
    expect(entries[GIT_TEMPLATE_DIR_ENV]).toBe(join(root, 'share', 'git-core', 'templates'))
  })

  it('omits the template directory when the packaged tree carries none', async () => {
    const root = installation({ helpers: true, templates: false })
    const entries = await resolveGitEnvironment(root, {})
    expect(entries[GIT_TEMPLATE_DIR_ENV]).toBeUndefined()
  })

  it('reports no helpers for an installation without the remote helper', async () => {
    const root = installation({ helpers: false, templates: false })
    expect(await hasPackagedGitHelpers(root)).toBe(false)
    expect(await resolveGitEnvironment(root, {})).toEqual({})
  })

  it('never overwrites a value the environment already sets', async () => {
    const root = installation({ helpers: true, templates: true })
    const environment = { [GIT_EXEC_PATH_ENV]: '/custom/exec', [GIT_TEMPLATE_DIR_ENV]: '/custom/templates' }
    // A hand-set value is how a user points git at another installation.
    expect(await resolveGitEnvironment(root, environment)).toEqual({})
  })

  it('resolves the installation root two levels above the executable', () => {
    expect(packagedAppRoot('/opt/apps/x/files/electron/dsh-desktop')).toBe('/opt/apps/x/files')
  })

  it('applies entries onto the mapping the Host inherits', () => {
    const environment: NodeJS.ProcessEnv = { PATH: '/usr/bin' }
    applyGitEnvironment(environment, { [GIT_EXEC_PATH_ENV]: '/p/lib/git-core' })
    expect(environment[GIT_EXEC_PATH_ENV]).toBe('/p/lib/git-core')
    expect(environment.PATH).toBe('/usr/bin')
  })

  it('places a directory first in a PATH value', () => {
    expect(withLeadingPath('/usr/bin', '/p/bin')).toBe('/p/bin:/usr/bin')
    expect(withLeadingPath(undefined, '/p/bin')).toBe('/p/bin')
  })
})

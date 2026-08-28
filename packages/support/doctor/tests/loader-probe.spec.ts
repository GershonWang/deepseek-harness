/**
 * Tests for the loader-probe subprocess: a real Cordis boot of a profile
 * reported through its exit code (0 = loaded, 1 = failed, 2 = timed out).
 *
 * Every case spawns the probe with `node --import tsx/esm` against a scratch
 * Harness home and asserts only the exit code plus stderr diagnostics, exactly
 * the surface the doctor's dynamic-loading check consumes.
 */

import { spawn } from 'node:child_process'
import { mkdtemp, rm, mkdir, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

const probePath = fileURLToPath(new URL('../src/loader-probe.ts', import.meta.url))

interface ProbeResult {
  code: number
  stdout: string
  stderr: string
}

/** Run the probe once; the child's own `--timeout` bounds the wait. */
function runProbe(args: readonly string[]): Promise<ProbeResult> {
  const env: NodeJS.ProcessEnv = { ...process.env }
  // The `--dsh-home` argument must own the home; a stray ambient DSH_HOME
  // would make the same arguments behave differently between machines.
  delete env.DSH_HOME
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ['--import', 'tsx/esm', probePath, ...args], {
      cwd: process.cwd(),
      env,
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    let stdout = ''
    let stderr = ''
    child.stdout.on('data', (chunk) => { stdout += String(chunk) })
    child.stderr.on('data', (chunk) => { stderr += String(chunk) })
    child.on('error', reject)
    child.on('close', (code) => { resolve({ code: code ?? -1, stdout, stderr }) })
  })
}

const PROBE_TIMEOUT = 120_000

/**
 * Write a profile manifest under the home naming the given bundle layers.
 * Without a manifest the probe would auto-initialize the shipped `web`
 * template; these fixtures instead declare third-party bundles of their own.
 */
async function writeProfile(home: string, bundles: readonly string[]): Promise<void> {
  const dir = join(home, 'profiles', 'web')
  await mkdir(dir, { recursive: true })
  await writeFile(join(dir, 'package.json'), JSON.stringify({
    name: 'dsh-profile-web',
    private: true,
    dependencies: {},
    dsh: { profile: { bundles: [...bundles], patchReload: 'live' } },
  }, undefined, 2) + '\n')
}

/**
 * Write a third-party bundle package under the profile's node_modules: the
 * package.json declares its patch layer, whose insert rows make up the bundle.
 */
async function writeBundle(
  home: string, profileName: string, bundleName: string, patch: string,
): Promise<void> {
  const dir = join(home, 'profiles', profileName, 'node_modules', bundleName)
  await mkdir(dir, { recursive: true })
  await writeFile(join(dir, 'package.json'), JSON.stringify({
    name: bundleName,
    version: '1.0.0',
    private: true,
    dsh: { bundle: { patch: './cordis.patch.yml' } },
  }, undefined, 2) + '\n')
  await writeFile(join(dir, 'cordis.patch.yml'), patch)
}

describe('loader-probe', () => {
  let home: string

  beforeEach(() => {
    home = ''
  })

  afterEach(async () => {
    if (home !== '') await rm(home, { recursive: true, force: true })
  })

  it('loads a fresh profile with no third-party bundles (exit 0)', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-probe-ok-'))
    const result = await runProbe(['--dsh-home', home, '--timeout', '60000'])
    expect(result.code).toBe(0)
  }, PROBE_TIMEOUT)

  it('reports a third-party bundle whose plugin imports a missing module (exit 1)', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-probe-bad-'))
    const brokenModule = 'dep-that-does-not-exist-xyz-12345'
    await writeProfile(home, ['@deepseek-ai/dsh-sdk-minimal', 'third-party-healthy', 'third-party-bad'])
    await writeBundle(home, 'web', 'third-party-healthy', [
      '- insert:',
      '    - id: healthy-timer',
      '      name: "@deepseek-ai/cordis-plugin-timer"',
    ].join('\n') + '\n')
    await writeBundle(home, 'web', 'third-party-bad', [
      '- insert:',
      '    - id: broken-plugin',
      '      name: ./broken-plugin.js',
    ].join('\n') + '\n')
    await writeFile(
      join(home, 'profiles', 'web', 'node_modules', 'third-party-bad', 'broken-plugin.js'),
      `import { value } from '${brokenModule}'\nexport default function () { void value }\n`,
    )

    // Loading every bundle fails because the bad bundle's insert crashes.
    const all = await runProbe(['--dsh-home', home, '--timeout', '60000'])
    expect(all.code).not.toBe(0)
    expect(all.stderr).toContain(brokenModule)

    // Narrowing to the broken bundle keeps the failure and its reason.
    const bad = await runProbe(['--dsh-home', home, '--include', 'third-party-bad', '--timeout', '60000'])
    expect(bad.code).not.toBe(0)
    expect(bad.stderr).toContain(brokenModule)
  }, PROBE_TIMEOUT)

  it('loads a subset that excludes the broken bundle (exit 0)', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-probe-subset-'))
    await writeProfile(home, ['@deepseek-ai/dsh-sdk-minimal', 'third-party-healthy', 'third-party-bad'])
    await writeBundle(home, 'web', 'third-party-healthy', [
      '- insert:',
      '    - id: healthy-timer',
      '      name: "@deepseek-ai/cordis-plugin-timer"',
    ].join('\n') + '\n')
    await writeBundle(home, 'web', 'third-party-bad', [
      '- insert:',
      '    - id: broken-plugin',
      '      name: ./broken-plugin.js',
    ].join('\n') + '\n')
    await writeFile(
      join(home, 'profiles', 'web', 'node_modules', 'third-party-bad', 'broken-plugin.js'),
      "import { value } from 'dep-that-does-not-exist-xyz-12345'\nexport default function () { void value }\n",
    )

    const result = await runProbe([
      '--dsh-home', home, '--include', 'third-party-healthy', '--timeout', '60000',
    ])
    expect(result.code).toBe(0)
  }, PROBE_TIMEOUT)

  it('rejects an --include that names no third-party bundle (exit 1)', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-probe-include-'))
    const result = await runProbe(['--dsh-home', home, '--include', 'no-such-bundle'])
    expect(result.code).toBe(1)
    expect(result.stderr).toContain('not a third-party bundle')
    expect(result.stderr).toContain('no-such-bundle')
  })

  it('reports a load that never settles as a timeout (exit 2)', async () => {
    home = await mkdtemp(join(tmpdir(), 'dsh-probe-hang-'))
    await writeProfile(home, ['@deepseek-ai/dsh-sdk-minimal', 'hang-bundle'])
    await writeBundle(home, 'web', 'hang-bundle', [
      '- insert:',
      '    - id: hang-plugin',
      '      name: ./hang.js',
    ].join('\n') + '\n')
    // hang.js must be ESM (a .js file whose top-level await stalls module
    // evaluation and keeps the Loader's import task pending forever), so the
    // bundle's package.json needs "type": "module" on top of the writeBundle shape.
    await writeFile(join(home, 'profiles', 'web', 'node_modules', 'hang-bundle', 'package.json'), JSON.stringify({
      name: 'hang-bundle',
      version: '1.0.0',
      private: true,
      type: 'module',
      dsh: { bundle: { patch: './cordis.patch.yml' } },
    }, undefined, 2) + '\n')
    await writeFile(join(home, 'profiles', 'web', 'node_modules', 'hang-bundle', 'hang.js'), [
      'await new Promise(() => {})',
      'export default function () {}',
      '',
    ].join('\n'))

    const result = await runProbe(['--dsh-home', home, '--timeout', '2500'])
    expect(result.code).toBe(2)
    expect(result.stderr).toContain('timed out')
  }, PROBE_TIMEOUT)
})

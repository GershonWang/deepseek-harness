import { mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
import {
  applyHostEscapeEnvironment,
  currentHostEscape,
  declareHostEscape,
  detectLinglongCommands,
  ensureLinglongWrappers,
  linglongWrapperDir,
  pruneLegacyLinglongWrappers,
  withLinglongPath,
  wrapperScript,
} from '../src/linglong-host.ts'

const roots: string[] = []
function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-linglong-host-'))
  roots.push(root)
  return root
}
/** Create an executable stub for one host command. */
function hostCommand(rootfs: string, dir: string, name: string, mode = 0o755): void {
  mkdirSync(join(rootfs, dir), { recursive: true })
  writeFileSync(join(rootfs, dir, name), '#!/bin/sh\n', { mode })
}
afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
})

describe('linglong host wrappers', () => {
  it('detects only regular executable commands under the host root mount', async () => {
    const rootfs = temporaryRoot()
    hostCommand(rootfs, 'usr/bin', 'll-cli')
    hostCommand(rootfs, 'usr/bin', 'll-builder', 0o644)
    const detected = await detectLinglongCommands(rootfs)
    expect(detected.map(command => ({ name: command.name, hostPath: command.hostPath })))
      .toEqual([{ name: 'll-cli', hostPath: join('/', 'usr/bin', 'll-cli') }])
  })

  it('prefers the first host directory that holds a command', async () => {
    const rootfs = temporaryRoot()
    hostCommand(rootfs, 'usr/bin', 'll-cli')
    hostCommand(rootfs, 'usr/local/bin', 'll-cli')
    const detected = await detectLinglongCommands(rootfs)
    expect(detected[0]?.hostPath).toBe(join('/', 'usr/bin', 'll-cli'))
  })

  it('writes a wrapper whose host path is the host view without the mount prefix', async () => {
    const rootfs = temporaryRoot()
    hostCommand(rootfs, 'usr/bin', 'll-cli')
    const home = temporaryRoot()
    expect(await ensureLinglongWrappers(home, rootfs)).toBe(1)
    const script = readFileSync(join(linglongWrapperDir(home), 'll-cli'), 'utf8')
    expect(script).toContain(`'${join('/', 'usr/bin', 'll-cli')}'`)
    expect(script).not.toContain(rootfs)
    expect(statSync(join(linglongWrapperDir(home), 'll-cli')).mode & 0o777).toBe(0o755)
  })

  it('refuses subcommands outside the allowlist and accepts one behind a global option', () => {
    const script = wrapperScript({ name: 'll-builder', allow: ['build', 'list'], hostPath: '/usr/bin/ll-builder' })
    expect(script).toContain('build|list|"") ;;')
    expect(script).toContain('exit 2')
  })

  it('omits the allowlist check when the command permits every subcommand', () => {
    const script = wrapperScript({ name: 'tool', allow: [], hostPath: '/usr/bin/tool' })
    expect(script).not.toContain('case "$sub" in')
  })

  it('does not rewrite an unchanged wrapper', async () => {
    const rootfs = temporaryRoot()
    hostCommand(rootfs, 'usr/bin', 'll-cli')
    const home = temporaryRoot()
    await ensureLinglongWrappers(home, rootfs)
    const path = join(linglongWrapperDir(home), 'll-cli')
    const before = statSync(path).mtimeMs
    await new Promise(resolve => setTimeout(resolve, 20))
    await ensureLinglongWrappers(home, rootfs)
    expect(statSync(path).mtimeMs).toBe(before)
  })

  it('removes generated wrappers and the emptied directory when nothing is available', async () => {
    const rootfs = temporaryRoot()
    hostCommand(rootfs, 'usr/bin', 'll-cli')
    const home = temporaryRoot()
    await ensureLinglongWrappers(home, rootfs)
    const empty = temporaryRoot()
    expect(await ensureLinglongWrappers(home, empty)).toBe(0)
    expect(() => statSync(linglongWrapperDir(home))).toThrow()
  })

  it('keeps user files when pruning', async () => {
    const rootfs = temporaryRoot()
    hostCommand(rootfs, 'usr/bin', 'll-cli')
    const home = temporaryRoot()
    await ensureLinglongWrappers(home, rootfs)
    const mine = join(linglongWrapperDir(home), 'my-tool')
    writeFileSync(mine, '#!/bin/sh\necho mine\n', { mode: 0o755 })
    await ensureLinglongWrappers(home, temporaryRoot())
    expect(readFileSync(mine, 'utf8')).toContain('echo mine')
  })

  it('prunes the legacy location without touching user content', async () => {
    const home = temporaryRoot()
    const legacy = join(home, '.dsh-linglong', 'bin')
    mkdirSync(legacy, { recursive: true })
    writeFileSync(join(legacy, 'll-cli'), '# dsh-linglong:generated\n#!/bin/sh\n')
    writeFileSync(join(legacy, 'user-file'), 'keep me\n')
    await pruneLegacyLinglongWrappers(home)
    expect(readFileSync(join(legacy, 'user-file'), 'utf8')).toBe('keep me\n')
    expect(() => statSync(join(legacy, 'll-cli'))).toThrow()
  })

  it('prepends the wrapper directory to a PATH value', () => {
    const home = temporaryRoot()
    expect(withLinglongPath('/usr/bin', home)).toBe(`${linglongWrapperDir(home)}:/usr/bin`)
    expect(withLinglongPath(undefined, home)).toBe(linglongWrapperDir(home))
  })
})

describe('host escape declaration', () => {
  it('reports no channel when the environment declares neither variable', () => {
    expect(currentHostEscape({})).toBeUndefined()
  })

  it('never overwrites a user-supplied declaration', async () => {
    const environment: NodeJS.ProcessEnv = { DSH_HOST_ROOTFS: '/custom', DSH_HOST_LAUNCH: 'systemd-run' }
    let probed = false
    const fact = await declareHostEscape(environment, async () => {
      probed = true
      return { rootfs: '/probed', launcher: 'systemd-run' }
    })
    expect(fact).toEqual({ rootfs: '/custom', launcher: 'systemd-run' })
    expect(probed).toBe(false)
    expect(currentHostEscape(environment)).toEqual({ rootfs: '/custom', launcher: 'systemd-run' })
  })

  it('treats an explicitly empty declaration as disabled', () => {
    expect(currentHostEscape({ DSH_HOST_ROOTFS: '', DSH_HOST_LAUNCH: 'systemd-run' })).toBeUndefined()
  })

  it('declares the probed channel when the environment holds neither variable', async () => {
    const environment: NodeJS.ProcessEnv = {}
    const fact = await declareHostEscape(environment, async () => ({ rootfs: '/run/host/rootfs', launcher: 'systemd-run' }))
    expect(fact).toEqual({ rootfs: '/run/host/rootfs', launcher: 'systemd-run' })
    expect(currentHostEscape(environment)).toEqual({ rootfs: '/run/host/rootfs', launcher: 'systemd-run' })
  })

  it('declares nothing when the probe finds no channel', async () => {
    const environment: NodeJS.ProcessEnv = {}
    expect(await declareHostEscape(environment, async () => undefined)).toBeUndefined()
    expect(currentHostEscape(environment)).toBeUndefined()
    expect(environment).not.toHaveProperty('DSH_HOST_ROOTFS')
    expect(environment).not.toHaveProperty('DSH_HOST_LAUNCH')
  })

  it('keeps a disabled channel disabled instead of probing around the empty value', async () => {
    const environment: NodeJS.ProcessEnv = { DSH_HOST_ROOTFS: '', DSH_HOST_LAUNCH: 'systemd-run' }
    let probed = false
    const fact = await declareHostEscape(environment, async () => {
      probed = true
      return { rootfs: '/run/host/rootfs', launcher: 'systemd-run' }
    })
    expect(fact).toBeUndefined()
    expect(probed).toBe(false)
    expect(environment.DSH_HOST_ROOTFS).toBe('')
  })

  it('declares nothing for a half-declared pair', async () => {
    const environment: NodeJS.ProcessEnv = { DSH_HOST_ROOTFS: '/run/host/rootfs' }
    let probed = false
    const fact = await declareHostEscape(environment, async () => {
      probed = true
      return { rootfs: '/run/host/rootfs', launcher: 'systemd-run' }
    })
    expect(fact).toBeUndefined()
    expect(probed).toBe(false)
    expect(environment).not.toHaveProperty('DSH_HOST_LAUNCH')
  })

  // 桌面端读完登录 shell 环境后会整体替换映射，先前写在 process.env 上的注入
  // 因此丢失；这条用例固定"替换后重新应用"仍然声明通道并前置包装目录。
  it('reapplies the channel to a replacement environment mapping', async () => {
    const rootfs = temporaryRoot()
    hostCommand(rootfs, 'usr/bin', 'll-cli')
    const home = temporaryRoot()
    const environment: NodeJS.ProcessEnv = { PATH: '/usr/bin:/bin' }
    await applyHostEscapeEnvironment(environment, home, async () => ({ rootfs, launcher: 'systemd-run' }))
    expect(currentHostEscape(environment)).toEqual({ rootfs, launcher: 'systemd-run' })
    expect(environment.PATH).toBe(`${linglongWrapperDir(home)}:/usr/bin:/bin`)
  })

  // 没有通道映射时（非玲珑沙箱、或宿主无命令）不得改动替换后的 PATH，
  // 否则子进程会对着一个空目录里的命令反复失败。
  it('leaves a replacement mapping untouched without a channel', async () => {
    const home = temporaryRoot()
    const environment: NodeJS.ProcessEnv = { PATH: '/usr/bin:/bin' }
    await applyHostEscapeEnvironment(environment, home, async () => undefined)
    expect(environment.PATH).toBe('/usr/bin:/bin')
    expect(currentHostEscape(environment)).toBeUndefined()
  })

  // 宿主根挂载点存在但没有任何玲珑命令时，包装目录为空，PATH 不应被前置。
  it('skips the PATH prefix when the host root holds no wrappable command', async () => {
    const rootfs = temporaryRoot()
    const home = temporaryRoot()
    const environment: NodeJS.ProcessEnv = { PATH: '/usr/bin:/bin' }
    await applyHostEscapeEnvironment(environment, home, async () => ({ rootfs, launcher: 'systemd-run' }))
    expect(currentHostEscape(environment)).toEqual({ rootfs, launcher: 'systemd-run' })
    expect(environment.PATH).toBe('/usr/bin:/bin')
  })
})

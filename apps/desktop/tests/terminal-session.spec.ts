/**
 * 真实 node-pty 会话的本地回归。
 *
 * 宿主没有 `.node` 原生模块时自跳过：CI 的 Windows 车道与服务端构件不保证编译过
 * node-pty，而这里要验证的恰好是「原生模块能装起来并且能跑 shell」。
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  accountLoginShell,
  DesktopTerminalSession,
  loadNodePty,
  resolveTerminalEnvironment,
  resolveTerminalShell,
} from '../src/terminal-session.ts'

const sessions: DesktopTerminalSession[] = []
afterEach(() => {
  for (const session of sessions.splice(0)) session.dispose()
  vi.restoreAllMocks()
})

function nativePtyAvailable(): boolean {
  try { loadNodePty(); return true } catch (_unavailable) { return false }
}

describe('terminal shell and environment', () => {
  it('prefers the login shell variable, then the account record, then a POSIX fallback', () => {
    expect(resolveTerminalShell({ SHELL: '/bin/zsh' }, '/bin/bash')).toEqual({ path: '/bin/zsh', args: [] })
    // An empty value is a missing one: the account record still knows the real shell.
    expect(resolveTerminalShell({ SHELL: '' }, '/bin/bash')).toEqual({ path: '/bin/bash', args: [] })
    expect(resolveTerminalShell({})).toEqual({ path: '/bin/sh', args: [] })
  })

  it('always describes itself as an xterm to the child, whatever launched the application', () => {
    // A launch from a script or service may carry TERM=dumb, which makes vim and tmux refuse to start.
    const environment = resolveTerminalEnvironment({ TERM: 'dumb', PATH: '/bin' }, '/bin/bash')
    expect(environment.TERM).toBe('xterm-256color')
    expect(environment.PATH).toBe('/bin')
    // A missing SHELL is filled in, since rc files read it but bash does not export its own repair.
    expect(environment.SHELL).toBe('/bin/bash')
  })

  it('keeps a real SHELL and drops undefined slots, but leaves other empty values alone', () => {
    const environment = resolveTerminalEnvironment({ SHELL: '/bin/fish', EMPTY: '', GONE: undefined }, '/bin/bash')
    expect(environment.SHELL).toBe('/bin/fish')
    // An empty value is a setting, not an absence: variables such as GIT_EDITOR are
    // meaningfully set to the empty string.
    expect(environment.EMPTY).toBe('')
    expect('GONE' in environment).toBe(false)
  })

  it('reads the account shell without throwing where no account record exists', () => {
    expect(() => accountLoginShell()).not.toThrow()
  })
})

describe.skipIf(!nativePtyAvailable())('terminal session over a real PTY', () => {
  /** Start one session and collect its output until `settle` resolves. */
  function open() {
    const output: string[] = []
    const exits: number[] = []
    const shell = resolveTerminalShell(process.env, accountLoginShell())
    const session = new DesktopTerminalSession({
      spawn: (file, args, options) => loadNodePty().spawn(file, args, options),
      shell,
      cwd: '/tmp',
      environment: resolveTerminalEnvironment(process.env, shell.path),
      cols: 80,
      rows: 24,
      onOutput: (data) => { output.push(data) },
      onExit: (code) => { exits.push(code) },
    })
    sessions.push(session)
    session.start()
    return { session, output, exits }
  }

  it('runs the shell, echoes a command result, and reports its exit code', async () => {
    const f = open()
    // Wait for the prompt before typing: the PTY may not have the shell ready earlier.
    await vi.waitFor(() => { expect(f.output.join('')).not.toBe('') }, { timeout: 15_000 })
    f.session.write('echo dsh-terminal-$((6*7))\n')
    await vi.waitFor(() => { expect(f.output.join('')).toContain('dsh-terminal-42') }, { timeout: 15_000 })
    f.session.write('exit 7\n')
    await vi.waitFor(() => { expect(f.exits).toEqual([7]) }, { timeout: 15_000 })
  })

  it('resizes the window and stops writing once disposed', async () => {
    const f = open()
    await vi.waitFor(() => { expect(f.output.join('')).not.toBe('') }, { timeout: 15_000 })
    expect(() => { f.session.resize(100, 30) }).not.toThrow()
    // Resizing to a nonsensical grid is ignored rather than forwarded to a throwing native call.
    expect(() => { f.session.resize(0, 0) }).not.toThrow()
    f.session.dispose()
    f.session.dispose()
    expect(() => { f.session.write('after dispose\n') }).not.toThrow()
  })
})

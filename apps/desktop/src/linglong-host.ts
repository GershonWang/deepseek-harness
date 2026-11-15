/**
 * Linglong command pass-through for the Electron Desktop shell.
 *
 * A Linglong container isolates the host from the sandbox: the container has
 * neither `ll-builder`/`ll-cli` nor the host's Linglong daemon, layer repository
 * (`/var/lib/linglong`), or writable state directory. Installing those commands
 * inside the container or through the toolchain market cannot bridge that
 * coupling: a host binary called directly inside the container only prints
 * `--version`, while `ll-cli list` exits silently with 255.
 *
 * This module reproduces the Go launcher's `internal/linglonghost` behavior: it
 * wraps the declared host-escape channel (the `DSH_HOST_ROOTFS` / `DSH_HOST_LAUNCH`
 * pair) as a fixed set of shell wrappers under the client configuration
 * directory. Path injection into the Host child process happens in `main.ts`,
 * so the wrappers stay the only capability the channel adds — no
 * "run an arbitrary command on the host" primitive is introduced.
 *
 * @module @deepseek-ai/dsh-desktop/linglong-host
 */

import { chmod, mkdir, readdir, readFile, rename, rm, rmdir, stat, writeFile } from 'node:fs/promises'
import { delimiter, join, sep } from 'node:path'
/** Client configuration directory name, shared with the Desktop settings location. */
const CONFIG_DIR_NAME = 'dsh-desktop'

/** Wrapper subdirectory name under the configuration directory. */
const BIN_DIR_NAME = 'linglong-bin'

/** Legacy wrapper directory once created directly under the home directory. */
const LEGACY_DIR_NAME = '.dsh-linglong'

/** PATH-facing subdirectory name of the legacy location. */
const LEGACY_BIN_DIR_NAME = 'bin'

/**
 * Directories on the host that may hold Linglong commands, relative to the host
 * root, in priority order.
 *
 * Probe paths are expressed inside the container (host root mount + these
 * directories), while the path written into a wrapper must be the absolute path
 * the host itself sees (without the mount prefix). The wrapper runs on the host,
 * which has no `/run/host/rootfs`; writing a container path there produces
 * `exit 127` (`exec: not found`).
 */
const HOST_SEARCH_DIRS = ['usr/bin', 'usr/local/bin', 'bin'] as const

/**
 * Marker present in every generated wrapper, used to recognize "files this
 * module owns". Cleanup only removes files carrying it; anything the user placed
 * in the directory is left untouched.
 */
const MARKER = 'dsh-linglong:generated'

/** One host Linglong command to make available inside the container. */
export interface LinglongCommand {
  /** Command name inside the container. */
  readonly name: string
  /** Absolute path on the host (host view, without the host root mount prefix). */
  readonly hostPath: string
  /** Subcommand allowlist; an empty list permits any subcommand. */
  readonly allow: readonly string[]
}

/**
 * Wrapped command set, in stable order (tests and logs depend on it).
 *
 * The allowlist is a security invariant and fails closed: anything outside it, or
 * outside the first non-option argument, is refused. Widening the surface means
 * editing this table, never writing another check into the wrapper. The allowed
 * surface covers the daily development path (template creation, build, run,
 * export, install, uninstall, upgrade, and queries) while withholding the three
 * that reach beyond local machine state:
 * - `ll-builder push` reaches a remote repository and needs credentials.
 * - `ll-builder repo` and `ll-cli repo` rewrite repository configuration, which
 *   affects every later install and upgrade.
 * - `ll-cli prune` deletes the host's base/runtime layers, which other
 *   Linglong applications on the host may depend on.
 */
const COMMANDS: readonly LinglongCommand[] = [
  {
    name: 'll-builder',
    allow: ['create', 'build', 'run', 'list', 'remove', 'export', 'import', 'extract', 'clean'],
    hostPath: '',
  },
  {
    name: 'll-cli',
    allow: [
      'run', 'ps', 'enter', 'kill', 'install', 'uninstall', 'upgrade',
      'list', 'analyze', 'info', 'content', 'search',
    ],
    hostPath: '',
  },
]

/**
 * Resolve the wrapper directory (`<home>/.config/dsh-desktop/linglong-bin`).
 * @param home - user home directory.
 * @returns absolute wrapper directory path.
 */
export function linglongWrapperDir(home: string): string {
  return join(home, '.config', CONFIG_DIR_NAME, BIN_DIR_NAME)
}

/**
 * Detect the Linglong commands actually available under the host root mount, in
 * `COMMANDS` order.
 *
 * Probe paths are inside the container; the returned `hostPath` is the host's own
 * absolute path, because the wrapper runs on the host. Entries that are not
 * regular executable files are skipped: a missing capability is preferable to one
 * that is visible but fails on use.
 * @param hostRootfs - read-only mount of the host root filesystem.
 * @returns the available commands with their host-view paths.
 */
export async function detectLinglongCommands(hostRootfs: string): Promise<LinglongCommand[]> {
  const detected: LinglongCommand[] = []
  for (const command of COMMANDS) {
    for (const dir of HOST_SEARCH_DIRS) {
      const probe = join(hostRootfs, dir, command.name)
      let executable = false
      try {
        const info = await stat(probe)
        executable = info.isFile() && (info.mode & 0o111) !== 0
      } catch {
        // An absent probe path only means this directory does not hold the command.
        executable = false
      }
      if (!executable) continue
      detected.push({ ...command, hostPath: join(sep, dir, command.name) })
      break
    }
  }
  return detected
}

/**
 * Render one command's wrapper script.
 *
 * The form is fixed: `systemd-run` carries the command to the host, and
 * `--working-directory` keeps the host working directory identical to the
 * container's (a workspace is the same path on both sides). `--wait` returns the
 * host exit status unchanged (measured: 7/0 pass through, a missing host command
 * reports 1), so no cross-namespace handshake file is needed — the container's
 * `/run/user/1000` is read-only anyway. No `--unit` is set: a reused PID can
 * collide with a leftover unit name, and letting systemd choose is more stable;
 * auditing relies on the harness command record and this wrapper file itself.
 *
 * `sh -c 'exec "$0" "$@"' '<host path>' "$@"` carries the host command path in
 * `$0` and the arguments in `$@`, avoiding quote assembly in the shell so
 * arguments containing spaces or non-ASCII bytes pass through unchanged. The host
 * path is single-quoted: it is built from a fixed mount point and command name and
 * contains no single quote.
 * @param command - the command to wrap.
 * @returns the wrapper script contents.
 */
export function wrapperScript(command: LinglongCommand): string {
  const lines: string[] = [
    '#!/bin/sh',
    `# ${MARKER} 由 DeepSeek Harness 桌面客户端生成，请勿手改：`,
    '# 客户端每次启动都会按宿主探测结果重写或删除本文件。',
    `# 容器内调用 ${command.name} 会在宿主机上执行——玲珑守护进程、层仓库与可写状态都只在宿主侧。`,
    'set -u',
  ]
  if (command.allow.length > 0) {
    const names = command.allow.join('/')
    lines.push(
      `# 子命令白名单：取第一个非选项参数（${command.name} --json list 这类全局选项在前也认）。`,
      'sub=""',
      'for arg in "$@"; do case "$arg" in -*) continue ;; *) sub="$arg"; break ;; esac; done',
      'case "$sub" in',
      `  ${command.allow.join('|')}|"") ;;`,
      `  *) echo "${command.name}：容器内只允许 ${names}；『$sub』被拒绝，需要时请在宿主机上执行" >&2; exit 2 ;;`,
      'esac',
    )
  }
  lines.push(
    'exec systemd-run --user --quiet --collect --wait --pipe \\',
    '  --working-directory="$(pwd -P)" \\',
    `/bin/sh -c 'exec "$0" "$@"' '${command.hostPath}' "$@"`,
  )
  return `${lines.join('\n')}\n`
}

/**
 * Write a wrapper atomically, skipping the write when the content already matches.
 *
 * Skipping preserves the file's mtime, so a normal restart does not churn state.
 * The temporary file plus rename keeps a concurrently reading process from seeing
 * a half-written script. The mode is fixed at 0755 because the wrapper must be
 * executable.
 * @param path - wrapper destination.
 * @param content - desired script contents.
 */
async function writeIfChanged(path: string, content: string): Promise<void> {
  try {
    if (await readFile(path, 'utf8') === content) return
  } catch {
    // An absent or unreadable previous wrapper is simply replaced below.
  }
  const temporary = `${path}.tmp`
  await writeFile(temporary, content, { mode: 0o755 })
  await chmod(temporary, 0o755)
  try {
    await rename(temporary, path)
  } catch (error) {
    await rm(temporary, { force: true })
    throw error
  }
}

/**
 * Remove generated wrappers under `dir` that are not in `keep`.
 *
 * The test is the marker in the file contents, not the file name: a same-named
 * file may have been placed by the user.
 * @param dir - wrapper directory to prune.
 * @param keep - command names to retain; `undefined` removes every generated file.
 */
async function pruneGeneratedExcept(dir: string, keep?: ReadonlySet<string>): Promise<void> {
  let entries: string[]
  try {
    entries = await readdir(dir)
  } catch {
    // A directory that does not exist needs no cleanup.
    return
  }
  for (const name of entries) {
    if (keep?.has(name) === true) continue
    const path = join(dir, name)
    try {
      if ((await stat(path)).isDirectory()) continue
    } catch {
      continue
    }
    let contents: string
    try {
      contents = await readFile(path, 'utf8')
    } catch {
      continue
    }
    if (!contents.includes(MARKER)) continue
    await rm(path, { force: true })
  }
}

/**
 * Remove every generated wrapper from the wrapper directory, deleting the
 * directory itself once it becomes empty.
 *
 * Only that subdirectory is touched; the parent is the client configuration
 * directory holding Desktop settings and is never removed from here.
 * @param dir - wrapper directory.
 */
async function pruneWrapperDir(dir: string): Promise<void> {
  await pruneGeneratedExcept(dir)
  // A directory still holding user content fails to remove, which is the desired outcome.
  await rmdir(dir).catch(() => undefined)
}

/**
 * Reconcile the wrapper directory with the current host detection.
 *
 * Available commands are written; an unchanged script is not rewritten. When no
 * command is available, only marker-carrying wrappers are removed and an emptied
 * subdirectory is deleted, so path injection stops advertising a directory whose
 * commands would all fail.
 * @param home - user home directory.
 * @param hostRootfs - read-only mount of the host root filesystem.
 * @returns the number of available commands.
 */
export async function ensureLinglongWrappers(home: string, hostRootfs: string): Promise<number> {
  const dir = linglongWrapperDir(home)
  const available = await detectLinglongCommands(hostRootfs)
  if (available.length === 0) {
    await pruneWrapperDir(dir)
    return 0
  }
  await mkdir(dir, { recursive: true, mode: 0o755 })
  const keep = new Set<string>()
  for (const command of available) {
    keep.add(command.name)
    await writeIfChanged(join(dir, command.name), wrapperScript(command))
  }
  await pruneGeneratedExcept(dir, keep)
  return available.length
}

/**
 * Remove generated wrappers from the legacy location (`<home>/.dsh-linglong/bin`).
 *
 * After the location moved to the configuration directory, wrappers left there
 * are never rewritten or deleted again; they would keep the host home directory
 * holding a directory that exists only for this module and make "which wrapper is
 * actually in use" unreadable. Only marker-carrying files go, and the directories
 * are removed only when empty, so user content is never taken along. Idempotent,
 * safe to call on every start.
 * @param home - user home directory.
 */
export async function pruneLegacyLinglongWrappers(home: string): Promise<void> {
  const dir = join(home, LEGACY_DIR_NAME, LEGACY_BIN_DIR_NAME)
  try {
    await pruneGeneratedExcept(dir)
  } catch {
    return
  }
  await rmdir(dir).catch(() => undefined)
  await rmdir(join(home, LEGACY_DIR_NAME)).catch(() => undefined)
}

/**
 * Prepend the wrapper directory to a PATH value.
 *
 * Callers first confirm the directory holds generated wrappers: advertising an
 * absent or empty directory makes the model retry commands that cannot work.
 * @param path - current PATH value.
 * @param home - user home directory.
 * @returns the PATH value with the wrapper directory first.
 */
export function withLinglongPath(path: string | undefined, home: string): string {
  const dir = linglongWrapperDir(home)
  const existing = path ?? ''
  return existing === '' ? dir : `${dir}${delimiter}${existing}`
}

/** Read-only mount of the host root filesystem inside the sandbox. */
const HOST_ROOTFS_BASE = '/run/host/rootfs'

/** Launcher that starts programs on the host. */
const HOST_LAUNCHER = 'systemd-run'

/**
 * Inherited-process variable naming the sandbox's read-only host-root mount.
 *
 * A cross-language contract: `packages/host/open-in-app` reads the same pair
 * (`hostEscapeOf`), so renaming either side must update both.
 */
const HOST_ROOTFS_ENV = 'DSH_HOST_ROOTFS'

/** Inherited-process variable naming the launcher that runs programs on the host. */
const HOST_LAUNCH_ENV = 'DSH_HOST_LAUNCH'

/** A detected host-escape channel. */
export interface HostEscapeFact {
  /** Read-only mount of the host root filesystem. */
  readonly rootfs: string
  /** Launcher that starts programs on the host. */
  readonly launcher: string
}

/**
 * Probe for this run's host-escape channel.
 *
 * The criteria match the Go launcher: the host-root mount exists and the launcher
 * is on PATH. A missing half means no channel, and callers keep the sandbox-only
 * behavior.
 * @param environment - environment supplying PATH.
 * @returns the declared channel, or undefined when this run has none.
 */
export async function hostEscapeOf(environment: NodeJS.ProcessEnv = process.env): Promise<HostEscapeFact | undefined> {
  let rootfsIsDirectory = false
  try {
    rootfsIsDirectory = (await stat(HOST_ROOTFS_BASE)).isDirectory()
  } catch {
    // An absent mount is the ordinary non-sandbox case.
    rootfsIsDirectory = false
  }
  if (!rootfsIsDirectory) return undefined
  if (!(await launcherOnPath(environment))) return undefined
  return { rootfs: HOST_ROOTFS_BASE, launcher: HOST_LAUNCHER }
}

/**
 * Whether the host launcher is an executable file on the supplied PATH.
 * @param environment - environment supplying PATH.
 * @returns whether the launcher resolves.
 */
async function launcherOnPath(environment: NodeJS.ProcessEnv): Promise<boolean> {
  for (const dir of (environment.PATH ?? '').split(delimiter)) {
    if (dir === '') continue
    try {
      const info = await stat(join(dir, HOST_LAUNCHER))
      if (info.isFile() && (info.mode & 0o111) !== 0) return true
    } catch {
      // Absence in one PATH entry only defers to the next.
    }
  }
  return false
}

/**
 * Probe this run for a host-escape channel and declare it in the process environment.
 *
 * Both variables appear together, because the harness side treats the pair as one
 * fact. An existing variable is never overwritten or second-guessed: these
 * variables are also the user's manual switch, so a value already present —
 * including an empty one, which disables the channel — means the decision was
 * made elsewhere and probing would take it away. That is why the environment is
 * consulted before {@link hostEscapeOf} runs, and why a half-declared pair
 * declares nothing rather than completing itself.
 *
 * This is the only declaration entry point `main.ts` calls, and its result gates
 * both the Linglong wrappers and the open-in-app host entries. Calling
 * {@link currentHostEscape} directly instead would read back a channel nobody
 * ever wrote, leaving every sandbox feature silently unavailable.
 * @param environment - environment to inspect and write; defaults to `process.env`.
 * @param probe - channel detection, replaceable by tests that cannot mount a host root.
 * @returns the declared channel, or undefined when this run has none.
 */
export async function declareHostEscape(
  environment: NodeJS.ProcessEnv = process.env,
  probe: typeof hostEscapeOf = hostEscapeOf,
): Promise<HostEscapeFact | undefined> {
  // A complete declaration wins untouched, and any partial one means "no channel"
  // rather than an invitation to fill in the missing half.
  const declared = currentHostEscape(environment)
  if (declared !== undefined) return declared
  if (Object.hasOwn(environment, HOST_ROOTFS_ENV) || Object.hasOwn(environment, HOST_LAUNCH_ENV)) return undefined
  const fact = await probe(environment)
  if (fact === undefined) return undefined
  environment[HOST_ROOTFS_ENV] = fact.rootfs
  environment[HOST_LAUNCH_ENV] = fact.launcher
  return fact
}

/**
 * Read the channel already declared in an environment, without probing the host.
 * @param environment - environment to inspect.
 * @returns the declared channel, or undefined when it is incomplete.
 */
export function currentHostEscape(environment: NodeJS.ProcessEnv = process.env): HostEscapeFact | undefined {
  const rootfs = environment[HOST_ROOTFS_ENV]
  const launcher = environment[HOST_LAUNCH_ENV]
  if (rootfs === undefined || launcher === undefined || rootfs === '' || launcher === '') return undefined
  return { rootfs, launcher }
}

/**
 * 把宿主逃逸通道应用到一份环境映射：声明通道变量，并在包装目录确实存在生成物时
 * 把该目录前置到 PATH。
 *
 * 声明是通道存在的唯一来源，变量绝不从玲珑容器继承；PATH 前缀只在前置目录真有
 * 包装时才追加，否则会向子进程宣传一个必然失败的命令名。
 *
 * 桌面端读完登录 shell 环境后会整体替换环境映射，原先写在 process.env 上的效果
 * 随之丢失。两处调用共用本函数，第二处不必重新实现一遍探测与注入。
 * @param environment - 要写入声明并前置 PATH 的环境映射。
 * @param home - 用户家目录。
 * @param probe - 通道探测；测试可替换它以临时目录冒充宿主根挂载点。
 */
export async function applyHostEscapeEnvironment(
  environment: NodeJS.ProcessEnv,
  home: string,
  probe: typeof hostEscapeOf = hostEscapeOf,
): Promise<void> {
  const fact = await declareHostEscape(environment, probe)
  if (fact === undefined) return
  try {
    const count = await ensureLinglongWrappers(home, fact.rootfs)
    if (count > 0) environment.PATH = withLinglongPath(environment.PATH, home)
  } catch (error) {
    console.warn('desktop linglong wrappers: pass-through unavailable', error)
  }
}

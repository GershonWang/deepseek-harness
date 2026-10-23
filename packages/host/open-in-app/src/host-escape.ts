/**
 * Host-escape channel declared by the sandbox that launched this process.
 * The channel names the read-only host-root mount and the launcher that starts
 * a host program, so open-in-app can reach applications installed outside the
 * sandbox. A process without one keeps its sandbox-local behavior unchanged.
 * @module @deepseek-ai/dsh-host-open-in-app/host-escape
 */

import type { LaunchEnvironmentSnapshot } from '@deepseek-ai/dsh-launch-environment'

/** Launcher a sandbox provides for running a program on the host that started it; closed union. */
export type HostEscapeLauncher = 'systemd-run'

/**
 * The host-escape channel this run may use, as the sandbox's launcher declared
 * it. Absent outside such a sandbox, so a consumer that finds no fact keeps its
 * sandbox-local behavior unchanged.
 */
export interface HostEscapeFact {
  /** Absolute path of the sandbox's read-only mount of the host root filesystem. */
  readonly hostRootfs: string
  readonly launcher: HostEscapeLauncher
}

/** Inherited-process variable naming the sandbox's read-only host-root mount. */
const HOST_ROOTFS_VAR = 'DSH_HOST_ROOTFS'
/** Inherited-process variable naming the launcher that runs programs on the host. */
const HOST_LAUNCH_VAR = 'DSH_HOST_LAUNCH'

/**
 * Resolve the sandbox's host-escape channel. Both variables must come from the
 * inherited process layer: a project directory must never be able to declare a
 * channel that starts programs outside the sandbox, so this reads no `.env`
 * layer. A missing, empty, relative, or unknown value means the run has no such
 * channel — the declaration is optional and its producer may be older than
 * this reader.
 * @param environment - the launcher's environment snapshot.
 * @returns the declared channel, or undefined when this run has none.
 */
export function hostEscapeOf(environment: LaunchEnvironmentSnapshot): HostEscapeFact | undefined {
  const hostRootfs = environment.getFrom(HOST_ROOTFS_VAR, ['process'])?.value
  const launcher = environment.getFrom(HOST_LAUNCH_VAR, ['process'])?.value
  if (hostRootfs === undefined || launcher === undefined) return undefined
  // A POSIX sandbox mount, so an absolute path is the only usable form.
  if (!hostRootfs.startsWith('/')) return undefined
  // Closed union: a second sandbox kind joins the launcher list here.
  if (launcher !== 'systemd-run') return undefined
  return { hostRootfs, launcher }
}

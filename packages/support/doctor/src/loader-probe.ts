#!/usr/bin/env node
/**
 * Loader probe: a standalone subprocess that truly loads one DSH profile
 * through the Cordis Loader and reports the result via its exit code.
 *
 * Static checks (bundle resolution, patch composition) cannot see a plugin
 * module that imports a dependency the current installation no longer
 * provides; only a real boot surfaces that failure. This probe is that boot:
 * it resolves the profile's bundle layers, heals the module fallback the
 * Loader needs for bare import specifiers, writes the empty root config the
 * include tree patches over, mounts the tree, and waits for every entry to
 * settle. The doctor's dynamic-loading check spawns this process and reads
 * its exit code.
 *
 * Exit codes:
 *   0 - the tree loaded and every enabled entry activated.
 *   1 - loading failed (the failure reason is written to stderr).
 *   2 - the load did not settle within `--timeout` milliseconds.
 *
 * Only the exit code is a contract; runtime logs from the loaded tree go to
 * stdout/stderr unchanged so a caller can surface plugin diagnostics.
 *
 * @module @deepseek-ai/dsh-doctor/loader-probe
 */

import { writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { parseArgs } from 'node:util'
import {
  boot,
  healProfilesModuleFallback,
  loadOptionalPatches,
  loadProfile,
  PROFILE_PATCH_FILENAME,
} from '@deepseek-ai/dsh-app-boot'
import { provideCmdline } from '@deepseek-ai/dsh-cmdline'

/** Diagnostic prefix matching the other doctor surfaces. */
const BIN_NAME = 'doctor'

/** Bundle names owned by the installation; `--include` selects among the rest. */
const OFFICIAL_PREFIX = '@deepseek-ai/'

/** The root config every profile tree patches over: an empty entry list. */
const PROFILE_ROOT_CONFIG = `# dsh profile root — an empty entry list. The tree is composed as patches.
[]
`

/** Application order of the effective patch stack, mirroring the launcher's boot. */
interface ProbeOptions {
  /** The profile name to load. */
  profile: string
  /** Harness home directory. */
  home: string
  /** Third-party bundle subset to load; empty means every bundle layer. */
  include: readonly string[]
  /** How long the load may take before the probe gives up. */
  timeoutMs: number
}

/** Parse and validate the command line. */
function parseProbeArgs(argv: readonly string[]): ProbeOptions {
  const { values } = parseArgs({
    args: [...argv],
    options: {
      profile: { type: 'string' },
      'dsh-home': { type: 'string' },
      include: { type: 'string', multiple: true },
      timeout: { type: 'string' },
    },
    strict: true,
  })
  const home = values['dsh-home'] ?? process.env.DSH_HOME
  if (home === undefined || home === '') {
    throw new Error('--dsh-home <path> (or DSH_HOME) is required')
  }
  const timeoutRaw = values.timeout ?? '10000'
  if (!/^\d+$/u.test(timeoutRaw)) {
    throw new Error(`--timeout must be a positive integer, got ${JSON.stringify(timeoutRaw)}`)
  }
  const timeoutMs = Number(timeoutRaw)
  if (timeoutMs <= 0) {
    throw new Error(`--timeout must be a positive integer, got ${timeoutRaw}`)
  }
  return {
    profile: values.profile ?? 'web',
    home,
    include: values.include ?? [],
    timeoutMs,
  }
}

/** Resolve the installation anchor the way the doctor's static checks do. */
function resolveInstallAnchor(): string {
  return createRequire(import.meta.url).resolve('@deepseek-ai/dsh-web-app/package.json')
}

/**
 * Select the bundle layers to load. Without `--include` every bundle layer
 * loads; with it, official layers always remain (they are the installation's
 * own composition) and only the named third-party layers are added. A name
 * that is not one of the profile's third-party bundles is a misconfiguration
 * and fails loud rather than silently loading nothing.
 */
function selectLayers<T extends { packageName: string }>(profileName: string, layers: readonly T[], include: readonly string[]): T[] {
  if (include.length === 0) return [...layers]
  const thirdParty = new Set(
    layers.filter(layer => !layer.packageName.startsWith(OFFICIAL_PREFIX)).map(layer => layer.packageName),
  )
  for (const name of include) {
    if (!thirdParty.has(name)) {
      const available = [...thirdParty].join(', ') || 'none'
      throw new Error(`--include ${JSON.stringify(name)} is not a third-party bundle of profile ${JSON.stringify(profileName)}; available: ${available}`)
    }
  }
  const wanted = new Set(include)
  return layers.filter(layer => layer.packageName.startsWith(OFFICIAL_PREFIX) || wanted.has(layer.packageName))
}

/**
 * Load `profile` in a fresh Cordis tree and dispose it again.
 * @param options - resolved probe arguments.
 * @returns normally only when the whole tree activated; a never-settling load
 * is cut short by the caller's timeout.
 */
async function probeLoad(options: ProbeOptions): Promise<void> {
  const installAnchor = resolveInstallAnchor()
  const profile = loadProfile(BIN_NAME, options.profile, installAnchor, options.home)
  const selected = selectLayers(options.profile, profile.layers, options.include)
  // The Loader resolves bare bundle/plugin specifiers by walking up from the
  // profile directory; the module fallback materializes the installation's
  // dependency closure there, exactly as a real launch does before booting.
  await healProfilesModuleFallback({ installAnchor, profile, home: options.home })

  const rootConfig = join(profile.dir, 'cordis.yml')
  writeFileSync(rootConfig, PROFILE_ROOT_CONFIG)
  const bundlePatches = selected.flatMap(layer => layer.patches)
  const homePatches = loadOptionalPatches(BIN_NAME, join(options.home, PROFILE_PATCH_FILENAME)) ?? []
  const patches = [...bundlePatches, ...profile.patches, ...homePatches]

  let requestedExit: number | undefined
  const readyListeners = new Set<() => void>()
  const ctx = await boot(BIN_NAME, rootConfig, patches, (hostCtx) => {
    // Launcher facts every profile app expects. No invocation-level flags are
    // handed over: probing must not reject a profile whose app owns a
    // different flag family. Readiness is never committed — the probe disposes
    // the tree as soon as it settles, so app startup listeners simply stay
    // parked while they would be harmless.
    provideCmdline(hostCtx, {
      args: [],
      exit: (code) => { requestedExit = code },
      ready: {
        onReady(listener: () => void): () => void {
          readyListeners.add(listener)
          return () => { readyListeners.delete(listener) }
        },
      },
    })
  })
  await ctx.fiber.dispose()
  if (requestedExit !== undefined) {
    process.exit(requestedExit)
  }
}

/**
 * Run the probe against `argv` (defaulting to this process's arguments) and
 * map the outcome onto the documented exit code.
 * @param argv - the probe's command-line arguments, excluding the node binary.
 */
export async function main(argv: readonly string[] = process.argv.slice(2)): Promise<void> {
  let options: ProbeOptions
  try {
    options = parseProbeArgs(argv)
  } catch (error) {
    process.stderr.write(`${BIN_NAME}: loader probe: ${(error as Error).message}\n`)
    process.stderr.write('usage: node --import tsx/esm loader-probe.ts --dsh-home <path> [--profile <name>] [--include <bundle> ...] [--timeout <ms>]\n')
    process.exit(1)
  }

  const timer = setTimeout(() => {
    process.stderr.write(`${BIN_NAME}: loader probe: timed out after ${options.timeoutMs}ms waiting for profile ${options.profile} to load\n`)
    process.exit(2)
  }, options.timeoutMs)
  // Keep the timer referenced: a load stalled on promises alone would otherwise
  // drain the event loop and exit 0 instead of reporting the timeout (the same
  // hazard installFailLoud guards against); clearTimeout below releases it.

  try {
    await probeLoad(options)
    process.exit(0)
  } catch (error) {
    const detail = error instanceof Error ? error.stack ?? error.message : String(error)
    process.stderr.write(`${BIN_NAME}: loader probe: profile ${options.profile} failed to load\n${detail}\n`)
    process.exit(1)
  } finally {
    clearTimeout(timer)
  }
}

// Run directly when invoked as a script; importing the module for tests must
// not boot anything.
if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  void main()
}

/**
 * Plugin-level diagnostic checks: profile bundle resolvability,
 * opt-in bundle inventory, patch composability, and one live-load probe.
 *
 * Most checks are static; `pluginDynamicLoadCheck` additionally spawns the
 * loader-probe subprocess for a bounded real boot, catching plugin modules
 * whose imports no longer resolve after an upgrade without starting the full
 * supervisor path.
 *
 * @module @dsh-desktop/doctor/checks/plugins
 */

import { execFile } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import * as yaml from 'js-yaml'
import { entryListSchema, type PatchOptions } from '@deepseek-ai/cordis-plugin-include'
import {
  loadProfile,
  composeEntries,
  readProfileManifest,
  resolveProfileDir,
  writeProfileBundles,
  PROFILE_PATCH_FILENAME,
  type Profile,
} from '@deepseek-ai/dsh-app-boot'
import { writeFileAtomic } from '@deepseek-ai/dsh-atomic-write'
import { recordAutoDisabled } from '../auto-disabled.js'
import { bisectBy } from '../bisect-by.js'
import { optInBundles } from '../bundle-scope.js'
import { resolveInstallAnchor } from '../install-anchor.js'
import type { DoctorCheck, CheckResult, FixResult } from '../types.js'

/**
 * 以诊断视角加载 web profile：宿主为了让应用仍然启动，会把解析不到的 bundle
 * 静默剔除（只在 stderr 留一行告警），而 doctor 的检查必须看见它们——否则缺装
 * 的 bundle 会以「bundle 均已解析」或「无选装插件」的形式从报告里消失，
 * fatal 级的可解析性检查也永远不触发。启动器为此在 preflight 的子进程环境里剥离
 * DSH_SAFE_MODE，这里同样显式关闭安全模式的收紧，保证诊断看到真实组合。
 *
 * 报错沿用解析器的 `cannot resolve profile bundle` 措辞：`plugin-bundles-resolvable`
 * 靠这段文字把「缺依赖」与其它 profile 故障分开，改动措辞会静默丢掉那条提示。
 * @param dshHome - 被测的 Harness home。
 * @param installAnchor - 安装锚点清单；必须与真实启动同源，见 `resolveInstallAnchor`。
 * @returns 声明的 bundle 全部解析出层的 profile。
 * @throws 清单或用户补丁层不可读，以及某个声明的 bundle 没有对应层时。
 */
function loadDoctorProfile(dshHome: string, installAnchor: string): Profile {
  const profile = loadProfile('doctor', 'web', installAnchor, dshHome, { skipThirdPartyBundles: false })
  const declared = readProfileManifest('doctor', profile.dir).dsh?.profile?.bundles ?? []
  const resolved = new Set(profile.layers.map(layer => layer.packageName))
  const missing = [...new Set(declared)].filter(name => !resolved.has(name))
  if (missing.length === 0) return profile
  throw new Error(
    `doctor: cannot resolve profile bundle ${missing.map(name => JSON.stringify(name)).join(', ')} from the dsh installation or ${profile.dir}; `
    + 'run \'dsh plugin --profile web install\' if the dependency is not installed',
  )
}

/**
 * 用户补丁层失效条目的修复结果。
 *
 * 刻意区分"文件缺失"、"无需移除"与"已移除"三种情况，因为两个调用检查对
 * 前两者的成败判定不同：`plugin-patch-composable` 因合成告警而失败，定位
 * 不到坏条目时必须如实报告未修改；`plugin-patch-targets` 的失败前提就是
 * 存在失效 target，走到"无需移除"只可能是同一轮修复中已被前一个检查处理，
 * 属幂等成功。
 */
type OrphanRemovalOutcome =
  | { kind: 'file-missing' }
  | { kind: 'none-removed' }
  | { kind: 'removed'; removed: string[]; backupPath: string }

/**
 * 用户补丁里修复能删除的失效条目——`plugin-patch-composable` 的 `fixable`、
 * `plugin-patch-targets` 的判定与 `removeOrphanedPatchEntries` 的实际删除共用这一判据，
 * 三者因此不会漂移：check 不会承诺 fix 做不到的事。
 *
 * loader 在补丁合成期共发五类告警，本判据只覆盖其中两类——「insert 目标不存在」与
 * 「非 insert 目标不存在」，两者的共同事实都是「该条目的 id 不在基准合成结果里」。
 * 另外三类不在其中，也不该被承诺：`insert` 目标存在但不是 group、非 insert 缺 id、
 * name 与目标不符；来自随包 bundle 层的告警同样不可通过编辑用户文件消除。
 * @param profile - 已加载的 web profile。
 * @returns 失效的用户补丁条目；无失效条目时为空数组。
 */
function orphanedUserPatches(profile: Profile): PatchOptions[] {
  const baselineIds = new Set(
    composeEntries(profile.layers.map(l => l.patches))
      .map(entry => entry.id)
      .filter((id): id is string => id !== undefined)
      .map(String),
  )
  return profile.patches.filter(
    patch => patch.id !== undefined && !baselineIds.has(String(patch.id)),
  )
}

/**
 * 修复覆盖不到的告警的说明：点明能力边界与手工处置方向。
 *
 * 最常见的是 name 与目标不符：条目里的 `name` 是「目标必须叫这个名字」的守卫，插件改名后
 * 它就是过期断言，而修复不删它（目标 id 仍在基准里，删掉会误伤同一 id 的 config）。
 */
const UNFIXABLE_PATCH_HINT = '不可自动修复：修复只删除「id 已不在基准合成中」的用户补丁条目；'
  + 'insert 目标不是 group、非 insert 缺 id、name 与目标不符三类，以及来自随包 bundle 层的告警，'
  + '都需手工核对该条目的 name 与 config 是否已过期（name 不符时以告警里的 expected 为准）。'

/**
 * 逐条列出可自动删除的失效用户补丁条目：点名补丁文件、条目 id 与声明的 name，
 * 让用户不必从一行告警反推该改哪个文件。
 * @param patchPath - 用户补丁文件的绝对路径。
 * @param orphans - 失效条目。
 * @returns 每行一条的说明。
 */
function orphanPatchLines(patchPath: string, orphans: readonly PatchOptions[]): string[] {
  return orphans.map((patch) => {
    const declared = patch.name === undefined ? '' : `，声明 name: ${JSON.stringify(patch.name)}`
    return `可自动修复：${patchPath} 的条目 ${JSON.stringify(String(patch.id))}${declared}`
      + '（基准合成里已无此 id；修复会删除该条目并备份原文件）'
  })
}

/**
 * 移除用户补丁中 target 已不存在的条目，供两个补丁检查共用。
 *
 * 以"不含用户层的合成结果"为基准：用户补丁条目里 id 不在基准 entries 中的
 * 就是失效条目。加 `disabled` 无效——loader 对 target 缺失的补丁条目一视同仁
 * 地报告 warning，`disabled` 只是写到 target 上的属性，不能让缺失的 target
 * 复现，因此必须把坏条目从补丁列表移除；文件本身不删除、不改名，避免像整体
 * 改名那样把好补丁一并禁用。
 *
 * 解析复用 `loadProfile` 已按 loader 的 `entryListSchema` 解析出的 `patches`，
 * 回写使用同一 schema：用户补丁允许 `!!js` 表达式，无 schema 的解析会抛
 * `unknown tag`、无 schema 的回写会把表达式降级成普通映射，二者都会破坏
 * 用户配置。
 *
 * 备份只在确实要写回时创建：同一轮修复中可能有多个检查处理同一个文件，
 * 后执行者此时已无失效条目，提前返回可避免用已修改的内容覆盖先执行者保存
 * 的原件。
 * @param dshHome - harness home 绝对路径。
 * @param backupDir - 本轮修复的备份目录。
 * @returns 修复结果；`removed` 分支携带被移除的条目 id 与备份路径。
 */
async function removeOrphanedPatchEntries(
  dshHome: string, backupDir: string,
): Promise<OrphanRemovalOutcome> {
  const profileDir = resolveProfileDir('web', dshHome)
  const patchPath = join(profileDir, PROFILE_PATCH_FILENAME)
  // 文件不存在（从未创建，或已被 cfg-user-patch 改名为 .disabled）：无条目可处置。
  if (!existsSync(patchPath)) return { kind: 'file-missing' }

  const profile = loadDoctorProfile(dshHome, resolveInstallAnchor())
  const orphanIds = new Set(orphanedUserPatches(profile).map(patch => String(patch.id)))
  const patches: PatchOptions[] = structuredClone(profile.patches)
  const removed: string[] = []
  const kept = patches.filter((patch) => {
    if (patch.id === undefined || !orphanIds.has(String(patch.id))) return true
    removed.push(String(patch.id))
    return false
  })
  if (removed.length === 0) return { kind: 'none-removed' }

  const backupPath = join(backupDir, PROFILE_PATCH_FILENAME)
  const original = readFileSync(patchPath, 'utf8')
  await writeFileAtomic(backupPath, original, { mode: 0o600, dirMode: 0o700 })
  const updated = yaml.dump(kept, { schema: entryListSchema, noRefs: true }).trimEnd() + '\n'
  await writeFileAtomic(patchPath, updated, { mode: 0o600 })
  return { kind: 'removed', removed, backupPath }
}

const pluginBundlesResolvable: DoctorCheck = {
  id: 'plugin-bundles-resolvable',
  name: 'Profile bundles resolvable',
  category: 'plugin',
  severity: 'fatal',
  check: async (dshHome: string): Promise<CheckResult> => {
    try {
      const installAnchor = resolveInstallAnchor()
      const profile = loadDoctorProfile(dshHome, installAnchor)
      const optIn = optInBundles(installAnchor, profile.layers)
      const shipped = profile.layers.length - optIn.length
      const base = {
        ok: true,
        message: `${profile.layers.length} bundles resolved (${shipped} shipped, ${optIn.length} opt-in)`,
        fixable: optIn.length > 0,
        suggestedLevel: 1 as const,
      }
      if (optIn.length > 0) {
        return { ...base, detail: `Opt-in: ${optIn.map(t => t.packageName).join(', ')}` }
      }
      return base
    } catch (err) {
      const msg = (err as Error).message
      const bundleResolutionMentioned = msg.includes('cannot resolve profile bundle')
      const base = {
        ok: false,
        message: `Cannot resolve profile bundles: ${msg}`,
        fixable: true,
        suggestedLevel: 1 as const,
      }
      if (bundleResolutionMentioned) {
        return { ...base, detail: 'A profile-selected plugin may have broken dependencies after upgrade. Try plugin-safe mode.' }
      }
      return base
    }
  },
}

const pluginPatchComposable: DoctorCheck = {
  id: 'plugin-patch-composable',
  name: 'Profile patch layers compose cleanly',
  category: 'plugin',
  severity: 'error',
  check: async (dshHome: string): Promise<CheckResult> => {
    try {
      const profile = loadDoctorProfile(dshHome, resolveInstallAnchor())
      const allLayers = [
        ...profile.layers.map(l => l.patches),
        profile.patches,
      ]
      const warnings: string[] = []
      const entries = composeEntries(allLayers, (msg) => {
        warnings.push(msg)
      })
      if (warnings.length === 0) {
        return {
          ok: true,
          message: `${entries.length} entries composed with zero patch warnings`,
          fixable: false,
          suggestedLevel: 2,
        }
      }
      // fixable 取自 fix 的实际判据（见 orphanedUserPatches）：合成告警有五类，修复只覆盖
      // 其中两类。此前这里无条件返回 true，界面因此承诺「可修复 L2」而点击必然失败。
      const orphans = orphanedUserPatches(profile)
      const patchPath = join(resolveProfileDir('web', dshHome), PROFILE_PATCH_FILENAME)
      return {
        ok: false,
        message: orphans.length > 0
          ? `${warnings.length} patch warning(s); ${orphans.length} removable from ${PROFILE_PATCH_FILENAME}: ${orphans.map(patch => String(patch.id)).join(', ')}`
          : `${warnings.length} patch warning(s): ${warnings.slice(0, 3).join('; ')}${warnings.length > 3 ? '...' : ''}`,
        detail: [
          ...warnings,
          ...orphanPatchLines(patchPath, orphans),
          ...(orphans.length < warnings.length ? [UNFIXABLE_PATCH_HINT] : []),
        ].join('\n'),
        fixable: orphans.length > 0,
        suggestedLevel: 2,
      }
    } catch (err) {
      return {
        ok: false,
        message: `Cannot compose patches: ${(err as Error).message}`,
        fixable: true,
        suggestedLevel: 2,
      }
    }
  },
  fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
    const outcome = await removeOrphanedPatchEntries(dshHome, backupDir)
    if (outcome.kind === 'file-missing') {
      return { ok: true, message: '用户补丁文件不存在（未创建或已禁用），无需修复' }
    }
    if (outcome.kind === 'none-removed') {
      // 合成报警但识别不到坏条目（可能是 insert 子条目或跨层冲突）：
      // 不做不确定的修改，避免误伤。
      return { ok: false, message: '无法定位失效补丁条目，未做修改' }
    }
    return {
      ok: true,
      message: `已移除失效补丁条目：${outcome.removed.join('、')}（原文件已备份，其余补丁保留）`,
      backupPath: outcome.backupPath,
    }
  },
}

const pluginOptInList: DoctorCheck = {
  id: 'plugin-third-party-list',
  name: 'Opt-in plugin bundles',
  category: 'plugin',
  severity: 'info',
  check: async (dshHome: string): Promise<CheckResult> => {
    try {
      const installAnchor = resolveInstallAnchor()
      const profile = loadDoctorProfile(dshHome, installAnchor)
      const optIn = optInBundles(installAnchor, profile.layers)
      if (optIn.length === 0) {
        return {
          ok: true,
          message: 'No opt-in bundles selected',
          fixable: false,
          suggestedLevel: 1,
        }
      }
      return {
        ok: true,
        message: `${optIn.length} opt-in bundle(s): ${optIn.map(t => t.packageName).join(', ')}`,
        detail: 'If harness fails to start after upgrade, try plugin-safe mode to skip opt-in bundles.',
        fixable: optIn.length > 0,
        suggestedLevel: 1,
      }
    } catch {
      return {
        ok: true,
        message: 'Cannot list opt-in bundles (profile not loadable)',
        fixable: false,
        suggestedLevel: 1,
      }
    }
  },
}

// Verify each user-patch target id exists in the composed entry list.
const pluginPatchTargets: DoctorCheck = {
  id: 'plugin-patch-targets',
  name: 'User patch targets exist',
  category: 'plugin',
  severity: 'warning',
  check: async (dshHome: string): Promise<CheckResult> => {
    try {
      const profile = loadDoctorProfile(dshHome, resolveInstallAnchor())
      if (profile.patches.length === 0) {
        return { ok: true, message: 'No user patches', fixable: false, suggestedLevel: 2 }
      }

      // 与 fix 共用同一判据（orphanedUserPatches）：能修的就是「id 不在基准合成里」的那些。
      const missingIds = orphanedUserPatches(profile).map(patch => String(patch.id))

      if (missingIds.length === 0) {
        return {
          ok: true,
          message: `All ${profile.patches.length} user patch targets exist`,
          fixable: false,
          suggestedLevel: 2,
        }
      }

      return {
        ok: false,
        message: `${missingIds.length} user patch(es) target unknown entries: ${missingIds.slice(0, 5).join(', ')}${missingIds.length > 5 ? '...' : ''}`,
        detail: 'These patches may reference entries that were renamed or removed in the latest harness version. The patches will be silently skipped at startup.',
        fixable: true,
        suggestedLevel: 2,
      }
    } catch {
      return {
        ok: true,
        message: 'Cannot verify patch targets (profile not loadable)',
        fixable: false,
        suggestedLevel: 2,
      }
    }
  },
  fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
    const outcome = await removeOrphanedPatchEntries(dshHome, backupDir)
    if (outcome.kind === 'file-missing') {
      return { ok: true, message: '用户补丁文件不存在（未创建或已禁用），无需修复' }
    }
    // 该检查的失败前提就是存在失效 target；走到这里说明同一轮修复中已被
    // plugin-patch-composable 处理，属幂等成功而非失败。
    if (outcome.kind === 'none-removed') {
      return { ok: true, message: 'No orphaned patches to remove' }
    }
    return {
      ok: true,
      message: `Removed ${outcome.removed.length} orphaned patch(es): ${outcome.removed.slice(0, 3).join(', ')}${outcome.removed.length > 3 ? '...' : ''}`,
      backupPath: outcome.backupPath,
    }
  },
}

/** Profile the dynamic-load check probes; must match the static checks. */
const DYNAMIC_PROFILE = 'web'
/** Bound a single probe run before the probe itself reports a timeout. */
const DYNAMIC_PROBE_TIMEOUT_MS = 60_000
/** Upper bound for captured probe output (a failing load stack can be long). */
const DYNAMIC_PROBE_MAX_BUFFER = 10 * 1024 * 1024

/**
 * 定位 loader-probe 子进程入口，并说明该入口是否需要 tsx 加载。
 *
 * 按同级产物探测，而不是走包导出：doctor 已迁出 pnpm workspace 成为 launcher 私有
 * 目录包，打包态不经 node_modules 暴露该导出，`require.resolve` 必然失败。
 *
 * 两个运行面必须都覆盖，`tsc` 把 src 平铺到 `lib/types`，本文件在两个面里的同级兄弟
 * 文件不同名：
 * - 产物面（打包安装、CLI 调用）：`lib/types/checks/plugins.js` 的兄弟是
 *   `lib/types/loader-probe.js`，直接跑，不需要 tsx——打包态也没有 tsx。
 * - 源码面（vitest 直接加载 `src/checks/plugins.ts`）：兄弟是 `src/loader-probe.ts`，
 *   必须用 `--import tsx/esm` 启动。只在源码面回退到 tsx，因此不会把 tsx 依赖带进
 *   打包运行路径。
 *
 * @returns 探针脚本绝对路径，以及是否需要 tsx 加载。
 */
function loaderProbeEntry(): { path: string; needsTsx: boolean } {
  const built = new URL('../loader-probe.js', import.meta.url)
  if (existsSync(built)) return { path: fileURLToPath(built), needsTsx: false }
  return { path: fileURLToPath(new URL('../loader-probe.ts', import.meta.url)), needsTsx: true }
}

interface LoaderProbeOutcome {
  /** Exit code of the probe; -1 when the spawn itself failed. */
  code: number
  /** Captured stdout and stderr, trimmed and joined. */
  output: string
}

/**
 * Run the loader-probe subprocess against `dshHome`, loading every bundle
 * layer when `include` is empty or only the named opt-in subset.
 * @param dshHome - harness home, passed as `--dsh-home`.
 * @param include - opt-in bundle names to load (`--include` per name).
 * @param officialOnly - mount installation layers only (`--official-only`).
 * @returns the probe exit code and its captured output.
 */
function runLoaderProbe(
  dshHome: string, include: readonly string[], officialOnly = false,
): Promise<LoaderProbeOutcome> {
  const env: NodeJS.ProcessEnv = { ...process.env }
  // The explicit `--dsh-home` argument owns the home; a stray ambient
  // DSH_HOME would otherwise redirect the probe to another installation.
  delete env.DSH_HOME
  const probe = loaderProbeEntry()
  const args = probe.needsTsx
    ? ['--import', 'tsx/esm', probe.path]
    : [probe.path]
  return new Promise((resolve) => {
    execFile(
      process.execPath,
      [
        ...args,
        '--dsh-home', dshHome,
        '--profile', DYNAMIC_PROFILE,
        '--timeout', String(DYNAMIC_PROBE_TIMEOUT_MS),
        ...(officialOnly ? ['--official-only'] : []),
        ...include.flatMap(name => ['--include', name]),
      ],
      { env, maxBuffer: DYNAMIC_PROBE_MAX_BUFFER, encoding: 'utf8' },
      (error, stdout, stderr) => {
        const output = [stdout, stderr].filter(Boolean).join('\n').trim()
        if (error === null) {
          resolve({ code: 0, output })
        } else {
          // A non-zero exit rejects with the numeric exit code; the -1 arm
          // only fires when the probe binary itself cannot spawn.
          /* v8 ignore next 3 -- no test can break process.execPath; the -1 arm keeps the outcome typed. */
          resolve({
            code: typeof error.code === 'number' ? error.code : -1,
            output: output === '' ? error.message : output,
          })
        }
      },
    )
  })
}

/** One opt-in inspection pass shared by the check and its repair. */
interface LocateCulpritResult {
  /** Whether the profile manifest loaded enough to enumerate bundles. */
  loadable: boolean
  /** Opt-in bundle names currently layered in the profile. */
  optIn: string[]
  /**
   * Captured full-tree probe output, or the profile-read error when
   * `loadable` is false.
   */
  output: string
  /** Whether the full tree (every bundle layer) booted cleanly. */
  fullOk: boolean
  /** The bundle that alone breaks the load; null when none is found. */
  culprit: string | null
  /**
   * 只挂官方层的基线探测也失败：这次失败与第三方 bundle 无关，不得归因给任何一个。
   * 与 `culprit === null` 的区别是"为什么定位不到"——这里根本没进二分。
   */
  baselineFailed: boolean
}

/**
 * Enumerate the profile's opt-in bundles, boot them all through the loader
 * probe once, and bisect to the single bundle that breaks the load.
 * Shared by the check (which reports the culprit) and its repair (which
 * re-locates the culprit at fix time instead of trusting check state).
 *
 * 可疑集合必须与探针 `--include` 认的那套完全一致（见 bundle-scope）：只要有一层
 * 既进不了 `--include`、又可能坏，二分就失去单调性，定位结果会落到列表首个 bundle
 * 上，报告与修复一起错。
 *
 * 全量失败后**先证明失败与 bundle 有关**再二分：`bisectBy` 的契约要求
 * `isBad([]) === false`，而它自身从不探测空集，收尾的 `isBad([result])` 对"全局失败"
 * 同样恒真。因此这里先跑一次"只挂官方层"的基线；基线也失败即说明问题不在第三方
 * bundle 上（home 不可写、超时、探针自身异常……），直接返回 `culprit: null` 而不进二分
 * ——否则任何无关失败都会被判成"某个 bundle 有罪"，返回二分命中的第一个名字。
 * @param dshHome - harness home passed to loadProfile and the probe.
 * @returns the load outcome and the located culprit, if any.
 */
async function locateCulprit(dshHome: string): Promise<LocateCulpritResult> {
  let optIn: string[]
  try {
    const installAnchor = resolveInstallAnchor()
    const profile = loadDoctorProfile(dshHome, installAnchor)
    optIn = optInBundles(installAnchor, profile.layers).map(layer => layer.packageName)
  } catch (err) {
    return { loadable: false, optIn: [], output: (err as Error).message, fullOk: false, culprit: null, baselineFailed: false }
  }
  if (optIn.length === 0) {
    return { loadable: true, optIn, output: '', fullOk: true, culprit: null, baselineFailed: false }
  }

  const full = await runLoaderProbe(dshHome, [])
  if (full.code === 0) {
    return { loadable: true, optIn, output: full.output, fullOk: true, culprit: null, baselineFailed: false }
  }

  const baseline = await runLoaderProbe(dshHome, [], true)
  if (baseline.code !== 0) {
    return { loadable: true, optIn, output: full.output, fullOk: false, culprit: null, baselineFailed: true }
  }

  // The full tree failed while the official-only baseline passed, so a third-party
  // bundle is implicated. A subset "is bad" when loading only it still fails — with
  // the full set failing and the empty set passing, bisectBy's contract holds.
  const culprit = await bisectBy(optIn, async (subset) => {
    const result = await runLoaderProbe(dshHome, subset)
    return result.code !== 0
  })
  return { loadable: true, optIn, output: full.output, fullOk: false, culprit, baselineFailed: false }
}

/**
 * Load every opt-in bundle once; on failure, binary-search which bundle
 * breaks the load and report it. Failures only a real boot exposes (plugin
 * modules importing dependencies the installation no longer provides) land
 * here, so the report can name the culprit instead of the whole tree. Its
 * repair disables the culprit bundle by deselecting its profile layer after
 * backing up the manifest, keeps the package installed, records the disable
 * for the shell's post-startup notice, then re-boots to prove the tree loads.
 */
export const pluginDynamicLoadCheck: DoctorCheck = {
  id: 'plugin-dynamic-load',
  name: '插件运行时兼容性',
  category: 'plugin',
  severity: 'fatal',
  check: async (dshHome: string): Promise<CheckResult> => {
    const located = await locateCulprit(dshHome)
    if (!located.loadable) {
      return {
        ok: true,
        message: `Cannot list opt-in bundles (profile not loadable): ${located.output}`,
        fixable: false,
        suggestedLevel: 2,
      }
    }
    if (located.optIn.length === 0) {
      return { ok: true, message: '无选装插件', fixable: false, suggestedLevel: 2 }
    }
    if (located.fullOk) {
      return {
        ok: true,
        message: `所有 ${located.optIn.length} 个选装插件加载正常`,
        fixable: false,
        suggestedLevel: 2,
      }
    }
    if (located.baselineFailed) {
      // 只挂官方层的基线也失败：问题不在第三方 bundle 上。此时既不能点名元凶，
      // 也不提供 L2 修复——修复会去禁用 bundle，而那治不了这里的病。
      return {
        ok: false,
        message: '第三方插件不是启动失败的原因：只挂官方层的基线探测同样失败（环境或探针自身问题）',
        detail: located.output,
        fixable: false,
        suggestedLevel: 2,
      }
    }
    if (located.culprit !== null) {
      return {
        ok: false,
        message: `插件 ${located.culprit} 导致启动失败（缺少运行依赖或损坏）`,
        detail: located.output,
        fixable: true,
        suggestedLevel: 2,
      }
    }
    return {
      ok: false,
      message: '选装插件导致启动失败，未能定位',
      detail: located.output,
      fixable: false,
      suggestedLevel: 2,
    }
  },
  fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
    const profileDir = resolveProfileDir('web', dshHome)
    const manifestPath = join(profileDir, 'package.json')
    const original = readFileSync(manifestPath, 'utf8')
    const backupPath = join(backupDir, 'web-profile.package.json')

    // 循环禁用所有导致加载失败的选装 bundle：禁用一个后重新全量探测，若
    // 仍失败则继续定位下一个元凶（多个插件各自损坏时逐个处理），直到全量
    // 探测通过或无法再定位。整轮以最初 manifest 为回滚基准：任何一步的探测
    // 失败都不还原中间结果（已修好的保留），只有"全部禁用仍无法加载"才用最
    // 初备份整体还原，避免把能修的也丢回去。
    //
    // 禁用 = 只把该 bundle 从 dsh.profile.bundles 取消选择，依赖与 node_modules
    // 一律保留：安装不被破坏，用户之后可在插件页重新启用，也可自行卸载决定。
    // 不额外写 per-entry 的 disabled 行——该 bundle 的补丁层既已不参与组合，那些
    // 行指向的 target 就不存在，日后重新启用时会表现为"启用了却不生效"。
    let located = await locateCulprit(dshHome)
    if (!located.loadable) {
      return { ok: false, message: '无法读取 profile，无法自动修复' }
    }
    if (located.optIn.length === 0 || located.fullOk) {
      return { ok: true, message: '插件加载已正常，无需修复' }
    }
    if (located.baselineFailed) {
      // 禁用任何 bundle 都治不了"官方层自己也起不来"，因此直接如实返回，
      // 不写备份、不动 manifest。
      return { ok: false, message: '第三方插件不是启动失败的原因（只挂官方层的基线探测同样失败），未做修改' }
    }

    let current = readProfileManifest('doctor', profileDir)
    let currentBundles = current.dsh?.profile?.bundles ?? []
    const disabled: string[] = []
    await writeFileAtomic(backupPath, original, { mode: 0o600, dirMode: 0o700 })

    while (located.culprit !== null) {
      const culprit = located.culprit
      if (!currentBundles.includes(culprit)) break
      currentBundles = currentBundles.filter(bundle => bundle !== culprit)
      disabled.push(culprit)
      writeProfileBundles(profileDir, current, currentBundles)
      // 禁用后重新全量探测：通过则修复完成；仍失败则继续定位下一个元凶。
      const verify = await runLoaderProbe(dshHome, [])
      if (verify.code === 0) {
        await recordAutoDisabled(dshHome, disabled, backupDir)
        return {
          ok: true,
          message: `已禁用与当前版本不兼容的插件：${disabled.join('、')}（安装与依赖保留，可在插件页重新启用或自行卸载；原 manifest 已备份）`,
          backupPath,
        }
      }
      located = await locateCulprit(dshHome)
      if (!located.loadable) {
        await writeFileAtomic(manifestPath, original, { mode: 0o600 })
        return { ok: false, message: '修复过程中无法读取 profile，已还原 manifest', backupPath }
      }
      current = readProfileManifest('doctor', profileDir)
      currentBundles = current.dsh?.profile?.bundles ?? []
    }

    // 所有选装 bundle 已禁用仍无法加载，或无法再定位元凶：整体还原，
    // 保留备份供手动处理。
    await writeFileAtomic(manifestPath, original, { mode: 0o600 })
    const reason = disabled.length > 0
      ? `已禁用 ${disabled.length} 个插件仍无法加载，已还原 manifest`
      : '未能定位问题插件，无法自动修复'
    return { ok: false, message: reason, backupPath }
  },
}

export const pluginChecks: DoctorCheck[] = [
  pluginBundlesResolvable,
  pluginPatchComposable,
  pluginPatchTargets,
  pluginOptInList,
  pluginDynamicLoadCheck,
]

/**
 * Host icon extraction for resolved open-in-app applications, one strategy
 * per platform: macOS converts the resolved bundle's `.icns` to a 128px PNG
 * (`plutil` + `sips`); Windows extracts the resolved executable's associated
 * icon as a 32px PNG through a generated PowerShell script (the largest size
 * `ExtractAssociatedIcon` yields without a native addon); Linux follows the
 * spec's desktop entry `Icon=` key into the hicolor theme and pixmaps
 * directories (PNG or SVG, no subprocess), retrying an absolute path that is
 * not readable here under the sandbox's host-rootfs mount. Every failure resolves null and
 * the icon route answers 404, which the browser renders as a generic glyph.
 */

import { mkdtemp, readdir, readFile, rm, stat, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { isAbsolute, join } from 'node:path'
import { desktopApplicationIcon } from '@deepseek-ai/dsh-native-command'
import type { OpenInAppApp } from './catalog.ts'
import {
  findDesktopEntry, hostDataDirectories, output, resolveInternals, specFor, xdgDataDirectories,
  type OpenInAppInternals, type OpenInAppResolvedLaunch, type ResolvedInternals,
} from './resolver.ts'

/** One extracted icon: raw bytes plus the media type the route serves. */
export interface OpenInAppIcon {
  readonly bytes: Buffer
  readonly contentType: 'image/png' | 'image/svg+xml'
}

/**
 * Extract one bundle's icon as a 128px PNG: read `CFBundleIconFile` from
 * Info.plist (`plutil` to JSON; the value may omit the .icns extension), fall
 * back to the first `Resources/*.icns`, then convert with `sips` through a
 * fresh temp file.
 */
async function extractBundleIconPng(
  bundlePath: string, timeoutMs: number, internals: ResolvedInternals,
): Promise<Buffer | null> {
  const resources = join(bundlePath, 'Contents', 'Resources')
  let iconFile: string | null = null
  const plistJson = await output(
    'plutil', ['-convert', 'json', '-o', '-', join(bundlePath, 'Contents', 'Info.plist')], timeoutMs, internals)
  if (plistJson !== null) {
    try {
      const declared: unknown = (JSON.parse(plistJson) as { CFBundleIconFile?: unknown }).CFBundleIconFile
      if (typeof declared === 'string' && declared !== '') {
        iconFile = declared.endsWith('.icns') ? declared : `${declared}.icns`
      }
    } catch {
      // Swallows malformed plutil JSON: the Resources scan below still applies.
    }
  }
  if (iconFile === null) {
    try {
      iconFile = (await readdir(resources)).find(entry => entry.endsWith('.icns')) ?? null
    } catch {
      // Swallows a missing Resources directory: such a bundle has no icon.
      return null
    }
  }
  if (iconFile === null) return null
  const icns = join(resources, iconFile)
  try {
    await stat(icns)
  } catch {
    // Swallows ENOENT: Info.plist may declare an icon file that is not on disk.
    return null
  }
  const workDir = await mkdtemp(join(tmpdir(), 'dsh-open-in-app-'))
  try {
    const outPng = join(workDir, 'icon.png')
    if (await output('sips', ['-s', 'format', 'png', '-Z', '128', icns, '--out', outPng], timeoutMs, internals) === null) {
      return null
    }
    try {
      return await readFile(outPng)
    } catch {
      // Swallows a sips run that exited 0 without writing the output file.
      return null
    }
  } finally {
    await rm(workDir, { recursive: true, force: true })
  }
}

/**
 * The associated-icon extraction script. `-File` with positional args keeps
 * paths out of the command line's parsing (no quoting/escaping surface);
 * `ExtractAssociatedIcon` yields 32px, the most the stock .NET surface gives
 * without a native addon (README Known Limitations).
 */
const EXTRACT_ICON_PS1 = [
  'param([string]$Source, [string]$Target)',
  '$ErrorActionPreference = "Stop"',
  'Add-Type -AssemblyName System.Drawing',
  '$icon = [System.Drawing.Icon]::ExtractAssociatedIcon($Source)',
  'if ($null -eq $icon) { exit 1 }',
  '$bitmap = $icon.ToBitmap()',
  '$bitmap.Save($Target, [System.Drawing.Imaging.ImageFormat]::Png)',
  '',
].join('\n')

/** Extract one Windows executable's associated icon as a 32px PNG. */
async function extractExecutableIconPng(
  executablePath: string, timeoutMs: number, internals: ResolvedInternals,
): Promise<Buffer | null> {
  const workDir = await mkdtemp(join(tmpdir(), 'dsh-open-in-app-'))
  try {
    const script = join(workDir, 'extract-icon.ps1')
    const outPng = join(workDir, 'icon.png')
    await writeFile(script, EXTRACT_ICON_PS1, 'utf8')
    const ran = await output('powershell.exe', [
      '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', script, executablePath, outPng,
    ], timeoutMs, internals)
    if (ran === null) return null
    try {
      return await readFile(outPng)
    } catch {
      // Swallows a script run that exited 0 without writing the output file.
      return null
    }
  } finally {
    await rm(workDir, { recursive: true, force: true })
  }
}

/**
 * One Linux application's icon from one desktop entry's `Icon=` key.
 * @param desktopId - entry id without the `.desktop` suffix.
 * @param dataDirs - data directories to search (the container's own, or the
 * host's when the launch was resolved on the host side).
 * @param internals - completed platform facts.
 * @returns the icon bytes and media type, or null when the entry names none
 * that is readable here.
 */
async function extractLinuxIcon(
  desktopId: string, dataDirs: readonly string[], internals: ResolvedInternals,
): Promise<OpenInAppIcon | null> {
  const entry = await findDesktopEntry(desktopId, dataDirs)
  const icon = entry?.icon
  if (icon === undefined || icon === '') return null
  if (!isAbsolute(icon)) return await desktopApplicationIcon(icon, dataDirs)
  const direct = await desktopApplicationIcon(icon, dataDirs)
  if (direct !== null) return direct
  // An absolute icon path inside the host's own root is readable through the
  // sandbox's read-only mount of it; a shared-home path was read directly above.
  const hostRootfs = internals.hostEscape?.hostRootfs
  return hostRootfs === undefined ? null : await desktopApplicationIcon(join(hostRootfs, icon), dataDirs)
}

/**
 * 该解析结果是否存在可读取的图标来源：Linux 看宿主条目或 spec 声明的桌面条目，
 * 其余平台看解析结果自带的图标来源。判断只读解析结果、不触发提取，所以 apps
 * 路由可以据此提前告诉客户端"这个应用没有图标"，省掉必然 404 的图标请求。
 * @param app - 目录中的应用条目。
 * @param resolved - 该应用当前的解析结果。
 * @param internals - 平台与沙箱内部信息，与提取时传入的保持一致。
 * @returns 有来源时为 true；false 表示图标路由只能回 404。
 */
export function hasIconSource(
  app: OpenInAppApp,
  resolved: OpenInAppResolvedLaunch,
  internals: OpenInAppInternals = {},
): boolean {
  return iconSourceOf(app, resolved, resolveInternals(internals).platform) !== undefined
}

/**
 * 提取时要读的图标来源：Linux 是桌面条目 id，其余平台是解析结果里的图标路径。
 * 与 {@link extractAppIcon} 共用这一处判断，避免"有没有来源"与"能不能提取"两套口径。
 * @param app - 目录中的应用条目。
 * @param resolved - 该应用当前的解析结果。
 * @param platform - 已补默认值的平台。
 * @returns 来源描述；undefined 表示没有任何来源可读。
 */
function iconSourceOf(
  app: OpenInAppApp,
  resolved: OpenInAppResolvedLaunch,
  platform: NodeJS.Platform,
): { readonly desktopId: string } | { readonly icon: NonNullable<OpenInAppResolvedLaunch['icon']> } | undefined {
  if (platform === 'linux') {
    const desktopId = resolved.hostDesktopId ?? specFor(app, platform)?.desktopId
    return desktopId === undefined ? undefined : { desktopId }
  }
  return resolved.icon === undefined ? undefined : { icon: resolved.icon }
}

/**
 * 提取一个应用在宿主上的图标，供图标路由使用。
 * @param app - 已解析的目录条目（Linux 由它的规格指名桌面条目）。
 * @param resolved - 该条目已验证的启动（macOS/Windows 上同时给出图标来源）。
 * @param timeoutMs - 提取宿主命令的单次截止时间。
 * @param internals - 平台与运行器钩子，供测试固定结果。
 * @returns 图标字节与媒体类型；本宿主没有来源或提取失败时为 null。
 */
export async function extractAppIcon(
  app: OpenInAppApp,
  resolved: OpenInAppResolvedLaunch,
  timeoutMs: number,
  internals: OpenInAppInternals = {},
): Promise<OpenInAppIcon | null> {
  const completed = resolveInternals(internals)
  const source = iconSourceOf(app, resolved, completed.platform)
  if (source === undefined) return null
  if ('desktopId' in source) {
    // A host launch carries the entry it came from: its `Icon=` key lives in
    // the host's data directories, while a sandbox-local launch uses the
    // spec's own desktop id in the container's directories.
    const hostRootfs = completed.hostEscape?.hostRootfs
    const dataDirs = resolved.hostDesktopId !== undefined && hostRootfs !== undefined
      ? hostDataDirectories(hostRootfs, completed)
      : xdgDataDirectories(completed)
    return extractLinuxIcon(source.desktopId, dataDirs, completed)
  }
  if (source.icon.kind === 'app-bundle') {
    const bytes = await extractBundleIconPng(source.icon.path, timeoutMs, completed)
    return bytes === null ? null : { bytes, contentType: 'image/png' }
  }
  const bytes = await extractExecutableIconPng(source.icon.path, timeoutMs, completed)
  return bytes === null ? null : { bytes, contentType: 'image/png' }
}

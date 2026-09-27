/**
 * 安装锚点解析：doctor 的静态检查与加载探针必须与真实启动共用同一个安装根
 * （`@deepseek-ai/dsh` 包的 package.json），否则两者算出的运行时解析闭包不一致。
 *
 * 为什么锚点会决定成败：`createRuntimeResolution` 只登记锚点包依赖闭包里的包。
 * 真实启动的锚点是 CLI 包（`apps/cli/src/profile-boot.ts` 的 INSTALL_ANCHOR），
 * 它的闭包含安装自带的 optional bundle（app-boot 的 OPTIONAL_BUNDLES）；而
 * `@deepseek-ai/dsh-web-app` 的闭包不含这些包。用 web-app 作锚点，探针会把「安装
 * 自带、可正常加载」的 bundle 报成 `ERR_MODULE_NOT_FOUND`，一条误报的 fatal 就此产生。
 *
 * 解析顺序与理由：
 *  1. `DSH_DESKTOP_INSTALL_ANCHOR`：launcher 启动 doctor 时按自己的布局显式传入
 *     （打包态 `<prefix>/harness/package.json`，开发态 `apps/cli/package.json`）。
 *     显式传入却不可用时抛错，而不是静默换回 web-app——静默降级会重新引入这条误报，
 *     且让「锚点没生效」永远不可见。
 *  2. 位置推导：从本模块向上找最近的 `@deepseek-ai/dsh` 清单。打包态 doctor 位于
 *     `<harness>/doctor/lib/types/`，上溯四层即得 `<harness>/package.json`，因此
 *     直接用 `node <harness>/doctor/lib/types/cli.js` 单跑 doctor 时锚点也正确。
 *  3. 回退 `@deepseek-ai/dsh-web-app`：开发态仓库里没有可解析的安装根，保持迁移前的
 *     锚点，行为与改动前一致。
 *
 * @module @dsh-desktop/doctor/install-anchor
 */

import { existsSync, readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

/** 安装根包名；与 `apps/cli` 的 INSTALL_ANCHOR 指向同一个包。 */
const INSTALLATION_PACKAGE = '@deepseek-ai/dsh'

/** launcher 传入安装锚点的变量名；开发调试与 CI 也可直接设置。 */
export const INSTALL_ANCHOR_ENV = 'DSH_DESKTOP_INSTALL_ANCHOR'

/**
 * 位置推导的最多上溯层数：打包态从 `lib/types` 到 harness 根正好四层，
 * 再多只会走到安装目录之外，停在四层即可。
 */
const ANCHOR_SEARCH_DEPTH = 4

/**
 * 解析本次安装的锚点清单路径。
 * @returns `@deepseek-ai/dsh` 包 package.json 的绝对路径。
 * @throws 环境变量已设置但指向不可读、或不是安装根的清单（接线错误必须可见）。
 */
export function resolveInstallAnchor(): string {
  const fromEnv = process.env[INSTALL_ANCHOR_ENV]
  if (fromEnv !== undefined && fromEnv !== '') return assertInstallationManifest(fromEnv, INSTALL_ANCHOR_ENV)
  const derived = findInstallAnchorAbove(dirname(fileURLToPath(import.meta.url)))
  if (derived !== undefined) return derived
  return createRequire(import.meta.url).resolve('@deepseek-ai/dsh-web-app/package.json')
}

/**
 * 从给定目录向上寻找安装根清单。
 *
 * 独立成函数是为了能用临时目录树覆盖这条路径：打包态 doctor 位于
 * `<harness>/doctor/lib/types/`，上溯四层即安装根；开发态仓库里没有安装根，
 * 函数返回 undefined 交给调用方回退。
 * @param startDir - 起始目录，通常是本模块所在目录。
 * @returns 安装根清单的绝对路径；上溯范围内没有安装根时返回 undefined。
 */
export function findInstallAnchorAbove(startDir: string): string | undefined {
  let dir = startDir
  for (let depth = 0; depth < ANCHOR_SEARCH_DEPTH; depth += 1) {
    const candidate = join(dir, 'package.json')
    // 候选读取失败（半写入、权限）或不是安装根都只是"这一层不是"，继续上溯：
    // 诊断不该因为路径上的某个无关包而整体失败。
    if (existsSync(candidate) && readPackageName(candidate) === INSTALLATION_PACKAGE) return candidate
    const parent = dirname(dir)
    if (parent === dir) break
    dir = parent
  }
  return undefined
}

/**
 * 校验一个显式声明的锚点确实是安装根清单。
 * @param manifestPath - 候选清单的绝对路径。
 * @param source - 出错信息里的来源描述（环境变量名）。
 * @returns 原路径，供调用方直接使用。
 */
function assertInstallationManifest(manifestPath: string, source: string): string {
  const name = readPackageName(manifestPath)
  if (name === undefined) {
    throw new Error(`doctor: ${source} 指向的安装锚点不可读：${manifestPath}`)
  }
  if (name !== INSTALLATION_PACKAGE) {
    throw new Error(
      `doctor: ${source} 指向的包是 ${JSON.stringify(name)}，不是安装根 ${JSON.stringify(INSTALLATION_PACKAGE)}：${manifestPath}`,
    )
  }
  return manifestPath
}

/**
 * 读清单里的包名。
 * @param manifestPath - 清单文件路径。
 * @returns 包名字符串；文件不可读或不是合法 JSON 时返回 undefined。
 */
function readPackageName(manifestPath: string): string | undefined {
  try {
    const name: unknown = (JSON.parse(readFileSync(manifestPath, 'utf8')) as { name?: unknown }).name
    return typeof name === 'string' ? name : undefined
  } catch {
    // 不可读或非法 JSON 的候选清单不构成安装根，由调用方决定继续上溯还是报错。
    return undefined
  }
}

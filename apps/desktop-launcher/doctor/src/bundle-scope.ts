/**
 * bundle 作用域判定：把 profile 组合里的 bundle 分成「安装自带」（shipped）与
 * 「本 profile 选装」（opt-in）。
 *
 * 为什么不能用 `@deepseek-ai/` 前缀代表"安装自带"：安装自带的 optional bundle
 * （app-boot 的 OPTIONAL_BUNDLES）同样在这个命名空间下，但它们由 profile 逐个选择
 * 启用——和第三方 bundle 一样可能坏、也一样应当能被单独停用。按前缀判定会让这类
 * bundle 永远留在二分定位的"必加载"集合里：任何子集都带着真凶，定位只能落到列表
 * 第一个第三方 bundle 上，修复也就去停用无辜的插件（一次实报：dshmarket 背了
 * `@deepseek-ai/dsh-experimental-auto-review` 的锅）。
 *
 * 判定口径与运行时解析一致：能从安装锚点的 node_modules 链解析到该包，才算安装自带。
 * 由此得到的选装集合与探针的 `--include` 可疑集合同源，报告、二分、修复三者不会分叉。
 *
 * @module @dsh-desktop/doctor/bundle-scope
 */

import { dirname, join } from 'node:path'
import { OPTIONAL_BUNDLES, resolveBundleDir } from '@deepseek-ai/dsh-app-boot'

/**
 * 只问安装锚点时占位的第二锚点目录名。
 *
 * `resolveBundleDir` 的契约是"先安装、后 profile"；传一个永不存在的 profile 目录，
 * 它的答案就只剩"安装是否自带这个包"，无需在 doctor 里复制一份 node_modules 遍历。
 */
const ABSENT_PROFILE_DIR = 'doctor-no-profile'

/**
 * 列出 profile 组合里由本人选装的 bundle（安装自带组合之外的层）。
 * @param installAnchor - 安装根清单路径，见 `resolveInstallAnchor`。
 * @param layers - 已解析的 profile 层，顺序即组合顺序。
 * @returns 选装层，保持输入顺序。
 */
export function optInBundles<T extends { packageName: string }>(
  installAnchor: string, layers: readonly T[],
): T[] {
  return layers.filter(layer => isOptInBundle(layer.packageName, installAnchor))
}

/**
 * 判断单个 bundle 是否属于"本 profile 选装"。
 * @param packageName - bundle 包名。
 * @param installAnchor - 安装根清单路径，见 `resolveInstallAnchor`。
 * @returns 安装自带的 optional bundle 与非安装自带的 bundle 都为 true。
 */
export function isOptInBundle(packageName: string, installAnchor: string): boolean {
  if (OPTIONAL_BUNDLES.includes(packageName)) return true
  return !suppliedByInstallation(packageName, installAnchor)
}

/**
 * 判断安装是否自带某个包。
 * @param packageName - bundle 包名。
 * @param installAnchor - 安装根清单路径。
 * @returns 能从安装锚点解析到该包的目录时为 true。
 */
function suppliedByInstallation(packageName: string, installAnchor: string): boolean {
  try {
    resolveBundleDir('doctor', packageName, installAnchor, join(dirname(installAnchor), ABSENT_PROFILE_DIR))
    return true
  } catch {
    // 解析不到就是"安装不自带"：这是判定的正常结果，不是错误。
    return false
  }
}

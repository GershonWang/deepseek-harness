/**
 * 分发包版本：玲珑打包步骤写进应用资源目录的版本号。
 *
 * 玲珑包版本定义在打包清单 `linglong.yaml` 里，而该清单不随包安装，应用运行时读不到。
 * 所以 `build-linglong.sh` 在构建时把版本写到 `resources/linglong-version.json`，由本模块
 * 读回给「关于」窗口。上游构建与开发态没有这份文件，调用方按「未携带分发包版本」处理并
 * 回退显示桌面客户端版本。
 *
 * @module @deepseek-ai/dsh-desktop/package-version
 */

import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/** 打包步骤写在应用资源目录内的文件名。 */
export const PACKAGE_VERSION_FILENAME = 'linglong-version.json'

/** 分发包版本以数字开头，可带预发布后缀；用于拒绝损坏文件里的任意文本。 */
const PACKAGE_VERSION_PATTERN = /^[0-9][0-9A-Za-z.+_-]*$/u

/**
 * 读取本次构建的分发包版本。
 * @param resourcesPath - 运行中应用的 Electron 资源目录。
 * @returns 分发包版本；构建未携带该文件、文件损坏或取值非法时返回 undefined。
 */
export function readPackageVersion(resourcesPath: string): string | undefined {
  let parsed: unknown
  try {
    parsed = JSON.parse(readFileSync(join(resourcesPath, PACKAGE_VERSION_FILENAME), 'utf8'))
  } catch (error) {
    // 文件缺失是上游构建的正常情况，不产生诊断；其余读取或解析失败值得记录。
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
      console.warn('desktop about: could not read the distribution package version', error)
    }
    return undefined
  }
  const version = typeof parsed === 'object' && parsed !== null && 'version' in parsed ? parsed.version : undefined
  // 非法值不能顶替构建本身能担保的版本号，宁可不显示分发包版本。
  return typeof version === 'string' && PACKAGE_VERSION_PATTERN.test(version) ? version : undefined
}

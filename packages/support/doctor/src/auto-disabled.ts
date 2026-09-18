/**
 * 自动禁用留痕：doctor 在禁用坏插件后写入，桌面壳在启动成功后读取。
 *
 * doctor 是独立子进程，禁用动作发生在 harness 之外；桌面壳要在下一次启动成功
 * 后告诉用户"哪个插件被禁用、为什么、备份在哪"，就必须有一份跨进程、跨启动
 * 的落盘记录。文件位置与字段即双方的契约：doctor 只追加，壳只读并自行记录
 * "已提示"，双方各自拥有自己的状态，互不覆盖。
 *
 * 留痕只服务于提示，不参与任何判定：读取失败、内容被外部改坏、记录过多，都
 * 不能影响修复本身，也不能阻断 doctor 的其它检查。
 *
 * @module @deepseek-ai/dsh-doctor/auto-disabled
 */

import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { writeFileAtomic } from '@deepseek-ai/dsh-atomic-write'

/** 留痕保留的最大条数：桌面壳只提示最近几次禁用，超出后丢弃最旧的记录。 */
const MAX_RECORDS = 20

/**
 * 一次自动禁用的留痕。
 */
export interface AutoDisabledRecord {
  /** 禁用发生的时刻（ISO 8601）。 */
  at: string
  /** 被禁用的 bundle 包名。 */
  bundle: string
  /** 禁用原因，面向用户的一句话。 */
  reason: string
  /** 本轮修复的备份目录，内含修复前的 profile manifest。 */
  backupDir: string
}

/**
 * 留痕文件路径：`<dshHome>/doctor/auto-disabled.json`，内容为记录数组。
 * @param dshHome - harness home。
 * @returns 留痕文件的绝对路径。
 */
export function autoDisabledPath(dshHome: string): string {
  return join(dshHome, 'doctor', 'auto-disabled.json')
}

/**
 * 读取已有留痕。
 * @param path - 留痕文件路径。
 * @returns 已解析的记录；文件不存在、内容非法或结构不符时为空数组。
 */
function readAutoDisabled(path: string): AutoDisabledRecord[] {
  let raw: string
  try {
    raw = readFileSync(path, 'utf8')
  } catch {
    return [] // 尚未禁用过任何插件：首次写入。
  }
  try {
    const parsed: unknown = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed as AutoDisabledRecord[] : []
  } catch {
    return [] // 留痕被外部改坏：丢弃无法解析的历史，只保留本次记录。
  }
}

/**
 * 追加本次自动禁用的留痕。
 * @param dshHome - harness home。
 * @param disabled - 本次被禁用的 bundle 包名。
 * @param backupDir - 本轮修复的备份目录。
 * @returns 留痕写入完成；写失败由调用方的修复结果承担，不改变已发生的禁用。
 */
export async function recordAutoDisabled(
  dshHome: string, disabled: readonly string[], backupDir: string,
): Promise<void> {
  const at = new Date().toISOString()
  const reason = '探测到该插件与当前版本不兼容（加载失败）'
  const records = [
    ...readAutoDisabled(autoDisabledPath(dshHome)),
    ...disabled.map(bundle => ({ at, bundle, reason, backupDir })),
  ]
  const kept = records.slice(-MAX_RECORDS)
  await writeFileAtomic(autoDisabledPath(dshHome), JSON.stringify(kept, undefined, 2) + '\n', { mode: 0o600, dirMode: 0o700 })
}

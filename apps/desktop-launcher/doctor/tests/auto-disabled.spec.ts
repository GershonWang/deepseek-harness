/**
 * 自动禁用留痕（`<dshHome>/doctor/auto-disabled.json`）的行为测试。
 *
 * 这份文件是 doctor 与桌面壳之间的跨进程契约：doctor 只追加、壳只读。测试覆盖
 * 追加顺序与条数上限，以及"留痕被外部改坏"这类只应降级、不应抛错的边界——
 * 留痕只服务提示，任何时候都不能反过来阻断 doctor 的修复流程。
 */

import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import { readFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { autoDisabledPath, recordAutoDisabled, type AutoDisabledRecord } from '../src/auto-disabled.ts'

let home: string

beforeEach(async () => {
  home = await mkdtemp(join(tmpdir(), 'dsh-auto-disabled-'))
})

afterEach(async () => {
  await rm(home, { recursive: true, force: true })
})

/** 读取留痕文件并解析为记录数组。 */
function read(home: string): AutoDisabledRecord[] {
  return JSON.parse(readFileSync(autoDisabledPath(home), 'utf8')) as AutoDisabledRecord[]
}

describe('auto-disabled 留痕', () => {
  it('把留痕写到 home 下的固定路径，并保留可还原的字段', async () => {
    await recordAutoDisabled(home, ['third-party-bad'], '/tmp/backups/doctor-1')

    expect(autoDisabledPath(home)).toBe(join(home, 'doctor', 'auto-disabled.json'))
    const records = read(home)
    expect(records).toHaveLength(1)
    expect(records[0]!.bundle).toBe('third-party-bad')
    expect(records[0]!.backupDir).toBe('/tmp/backups/doctor-1')
    expect(records[0]!.reason).toContain('不兼容')
    // 时刻必须可被壳解析并排序。
    expect(Number.isNaN(Date.parse(records[0]!.at))).toBe(false)
  })

  it('多轮禁用按时间顺序追加，不覆盖历史', async () => {
    await recordAutoDisabled(home, ['first-bad'], '/tmp/backups/doctor-1')
    await recordAutoDisabled(home, ['second-bad'], '/tmp/backups/doctor-2')

    expect(read(home).map(record => record.bundle)).toEqual(['first-bad', 'second-bad'])
  })

  it('同一轮禁用多个插件时逐个留痕', async () => {
    await recordAutoDisabled(home, ['bad-a', 'bad-b'], '/tmp/backups/doctor-1')

    expect(read(home).map(record => record.bundle)).toEqual(['bad-a', 'bad-b'])
  })

  it('记录条数有上限，超出后丢弃最旧的记录', async () => {
    for (let index = 0; index < 25; index += 1) {
      await recordAutoDisabled(home, [`bad-${String(index)}`], '/tmp/backups/doctor-1')
    }

    const records = read(home)
    expect(records).toHaveLength(20)
    expect(records[0]!.bundle).toBe('bad-5')
    expect(records.at(-1)!.bundle).toBe('bad-24')
  })

  it('留痕内容非法或结构不符时只保留本次记录，不抛错', async () => {
    const path = autoDisabledPath(home)
    await mkdir(dirname(path), { recursive: true })
    await writeFile(path, '{ this is not json', 'utf8')
    await recordAutoDisabled(home, ['bad-after-corrupt'], '/tmp/backups/doctor-1')
    expect(read(home).map(record => record.bundle)).toEqual(['bad-after-corrupt'])

    await writeFile(path, JSON.stringify({ records: [] }), 'utf8')
    await recordAutoDisabled(home, ['bad-after-shape'], '/tmp/backups/doctor-1')
    expect(read(home).map(record => record.bundle)).toEqual(['bad-after-shape'])
  })
})

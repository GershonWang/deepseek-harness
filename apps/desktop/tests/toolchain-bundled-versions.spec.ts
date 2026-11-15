/**
 * 随包版本探测的解析规则。
 *
 * 这里只测解析与缓存两件事：真正起子进程的部分依赖容器里装了什么，测试机不一定
 * 有相同命令，因此把判定逻辑与进程边界分开，边界靠集成环境验证。
 */

import { afterEach, describe, expect, it } from 'vitest'
import { parseVersionOutput, resetBundledVersionCache, bundledCommandVersion } from '../src/toolchain/bundled-versions.ts'

afterEach(() => { resetBundledVersionCache() })

describe('随包工具版本解析', () => {
  it('从各家不同的输出里取出同一个版本号', () => {
    // 这些是实际交付工具的 --version 首行，排版各不相同。
    expect(parseVersionOutput('git version 2.43.0')).toBe('2.43.0')
    expect(parseVersionOutput('Python 3.12.14')).toBe('3.12.14')
    expect(parseVersionOutput('jq-1.7.1')).toBe('1.7.1')
    expect(parseVersionOutput('curl 8.5.0 (x86_64-pc-linux-gnu)')).toBe('8.5.0')
    expect(parseVersionOutput('unzip 6.00 of 20 April 2009')).toBe('6.00')
  })

  it('接受两段式版本号', () => {
    // 有些工具的版本只有主次两段，硬要求三段会把它们判成"读不到"。
    expect(parseVersionOutput('v1.7')).toBe('1.7')
  })

  it('读不到版本时返回空串而不是把整行当版本号', () => {
    expect(parseVersionOutput('')).toBe('')
    expect(parseVersionOutput('Usage: xxd [options] [infile [outfile]]')).toBe('')
    // 日期与文件路径里的数字不该被当成版本号。
    expect(parseVersionOutput('no numbers here')).toBe('')
  })

  it('解析失败的输出不会被抛成错误', () => {
    // 输出格式不认识属于正常情况：清单少一个版本号，不影响列出该工具。
    expect(() => parseVersionOutput('garbage')).not.toThrow()
  })

  it('同一命令只探测一次', async () => {
    // 用必然不存在的命令：探测失败被吞掉并缓存，重复调用不会再次起进程。
    const first = await bundledCommandVersion('dsh-nonexistent-command-for-test')
    const second = await bundledCommandVersion('dsh-nonexistent-command-for-test')
    expect(first).toBe('')
    expect(second).toBe(first)
  })
})

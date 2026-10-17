/**
 * 会话恢复插件的单元测试。
 *
 * 为什么单独测：这个插件的产物是一条索引注入行，它的效果只有「壳记住的会话能否在
 * 客户端启动前被写回存储」才知道后果；这里用手写假 ctx 精确驱动插入行与配置分支，
 * 不起真实 harness。真实链路的一端（overlay 行被 harness 的补丁解析器接受、并把
 * config 交给本插件）由 internal/appenv 的 Go 用例与打包态手工验证覆盖。
 *
 * 运行：node --test apps/linyaps/desktop-launcher/internal/appenv/restore_session.test.mjs
 */
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { apply, name } from './restore_session.mjs'

/** 注入的全局量名：与链接桥读取的名字一一对应。 */
const GLOBAL = '__DSH_DESKTOP_RESTORE_SESSION__'

/** 诊断前缀：只在自身出错时出现。 */
const PREFIX = 'dsh-desktop: restore-session '

/** 假 cordis 上下文：记录监听器，允许测试主动触发索引注入表的收集。 */
function makeCtx({ onThrows = false } = {}) {
  const listeners = []
  return {
    listeners,
    ctx: {
      on(event, callback) {
        if (onThrows) throw new Error('registration refused')
        listeners.push({ event, callback })
        return () => {}
      },
    },
  }
}

/** 触发一次索引注入表收集，返回收到的行。 */
function collect(listeners) {
  const table = []
  for (const listener of listeners) {
    if (listener.event === 'webserver/index-inject') listener.callback(table)
  }
  return table
}

/** 捕获一次 apply 写入 stderr 的诊断行。 */
function withCapturedStderr(run) {
  const original = process.stderr.write
  let out = ''
  process.stderr.write = (chunk) => {
    out += String(chunk)
    return true
  }
  try {
    run()
  } finally {
    process.stderr.write = original
  }
  return out
}

test('插件名与注入的全局量名是与链接桥的契约', () => {
  assert.equal(name, 'dsh-desktop-restore-session')

  // 桥按这个名字读注入值；插件或桥单方面改名都会让恢复静默失效，因此在这里连起来。
  const bridge = readFileSync(
    fileURLToPath(new URL('../../linglong/dsh-link-bridge.js', import.meta.url)),
    'utf8',
  )
  assert.ok(bridge.includes(`window.${GLOBAL}`), '链接桥必须读取本插件注入的全局量')
})

test('记住会话时注册一条索引注入行', () => {
  const h = makeCtx()
  apply(h.ctx, { sessionId: 'session-abc' })

  assert.deepEqual(collect(h.listeners), [{
    kind: 'global',
    name: GLOBAL,
    value: 'session-abc',
  }])
})

test('壳还没记住会话时不注册任何监听', () => {
  for (const config of [undefined, null, {}, { sessionId: '' }, { sessionId: 42 }]) {
    const h = makeCtx()
    apply(h.ctx, config)
    assert.deepEqual(h.listeners, [], `config=${JSON.stringify(config)} 不应注册监听`)
  }
})

test('插件不 import 任何模块：它从 launcher 运行时目录加载，解析不到裸包名', () => {
  const source = readFileSync(fileURLToPath(new URL('./restore_session.mjs', import.meta.url)), 'utf8')
  // 只允许 node: 内置前缀之外的 import 语句不存在——本插件连内置都不需要。
  assert.ok(!/^\s*import\s/m.test(source), '恢复插件不得 import 任何模块')
  assert.ok(!source.includes('require('), '恢复插件不得使用 require')
})

test('注册失败只写一行诊断，不向外抛', () => {
  const h = makeCtx({ onThrows: true })
  const out = withCapturedStderr(() => { apply(h.ctx, { sessionId: 'session-abc' }) })
  assert.equal(out, `${PREFIX}unavailable registration refused\n`)
})

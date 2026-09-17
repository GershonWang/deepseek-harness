/**
 * 启动进度上报插件的单元测试。
 *
 * 为什么单独测：这个插件的产物就是一行行 stderr 契约，其中「何时停止上报」这条
 * 规则只有壳侧解析器与加载页才知道后果（曾实测到就绪后继续上报 `161/136`）。
 * 这里用手写假 ctx 驱动，不起真实 harness；真实启动由 supervisor 的
 * `testdata/mock-progress.sh` 用例与打包态手工验证覆盖。
 *
 * 运行：node --test apps/desktop-launcher/internal/appenv/startup_progress.test.mjs
 */
import { test } from 'node:test'
import assert from 'node:assert/strict'

import { apply, name } from './startup_progress.mjs'

/** 一行进度行的前缀：与壳侧 internal/supervisor 的解析器一一对应。 */
const PREFIX = 'dsh-desktop: startup '

/** 可手动结算的 promise，用来精确控制条目何时"激活完成"。 */
function deferred() {
  let resolve
  let reject
  const promise = new Promise((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

/**
 * 造一个假 harness 上下文：条目数组可读，`internal/plugin` 监听器可被测试主动触发。
 * @param {Array<{id: string, group?: boolean, disabled?: boolean, withFiber?: boolean}>} specs
 *   条目定义；`withFiber=true` 表示激活上报器时该条目已建好 fiber（播种路径）。
 * @returns {{ctx: object, settle: (id: string) => void, spawn: (spec: object) => void}}
 *   上下文，以及"结算某个条目"和"新增一个条目"两个驱动入口。
 */
function makeHarness(specs) {
  const fibers = new Map()
  const entries = specs.map((spec) => {
    const entry = {
      id: spec.id,
      disabled: spec.disabled ?? false,
      options: { name: spec.name ?? spec.id, group: spec.group ?? false },
      fiber: undefined,
    }
    if (spec.withFiber) {
      const gate = deferred()
      fibers.set(spec.id, gate)
      entry.fiber = { entry, await: () => gate.promise }
    }
    return entry
  })

  let listener
  const ctx = {
    loader: { entries: () => entries },
    on: (event, callback) => {
      assert.equal(event, 'internal/plugin')
      listener = callback
    },
  }

  return {
    ctx,
    /** 结算某个已播种条目的 fiber。 */
    settle: (id) => fibers.get(id).resolve(),
    /** 让某个已播种条目的激活失败（fiber.await 拒绝）。 */
    fail: (id) => fibers.get(id).reject(new Error('activation failed')),
    /** 追加一个条目并触发构造事件（模拟启动后期插入的新行）。 */
    spawn: (spec) => {
      const gate = deferred()
      const entry = {
        id: spec.id,
        disabled: spec.disabled ?? false,
        options: { name: spec.name ?? spec.id, group: spec.group ?? false },
        fiber: undefined,
      }
      entry.fiber = { entry, await: () => gate.promise }
      entries.push(entry)
      listener(entry.fiber)
      return gate
    },
  }
}

/**
 * 在捕获 stderr 的前提下执行一段逻辑，返回它写出的行。
 * 先恢复再断言：断言失败的输出不能被捕获吞掉。
 * @param {() => Promise<void>|void} run - 驱动逻辑。
 * @returns {Promise<string[]>} 捕获到的完整行（去换行）。
 */
async function withCapturedStderr(run) {
  const lines = []
  const original = process.stderr.write
  process.stderr.write = (chunk) => {
    lines.push(String(chunk).trimEnd())
    return true
  }
  try {
    await run()
  } finally {
    process.stderr.write = original
  }
  return lines
}

/** 让 microtask 与 promise 回调结算。 */
const flush = () => new Promise((resolve) => setImmediate(resolve))

test('插件名与上报前缀是与壳的契约', () => {
  assert.equal(name, 'dsh-desktop-startup-progress')
})

test('激活时先报一次当前计数，分母排除 group 与 disabled', async () => {
  const h = makeHarness([
    { id: 'a' },
    { id: 'b' },
    { id: 'g', group: true },
    { id: 'd', disabled: true },
  ])
  const lines = await withCapturedStderr(() => apply(h.ctx))
  assert.deepEqual(lines, [`${PREFIX}0/2`])
})

test('逐条 settle 上报，到齐后停止上报', async () => {
  const h = makeHarness([
    { id: 'a', withFiber: true },
    { id: 'b', withFiber: true },
  ])
  const lines = await withCapturedStderr(async () => {
    apply(h.ctx)
    h.settle('a')
    await flush()
    h.settle('b')
    await flush()
  })
  assert.deepEqual(lines, [`${PREFIX}0/2`, `${PREFIX}1/2`, `${PREFIX}2/2`])
})

test('就绪后的重组不再进日志：到齐后新增条目与结算都被忽略', async () => {
  const h = makeHarness([{ id: 'a', withFiber: true }])
  const lines = await withCapturedStderr(async () => {
    apply(h.ctx)
    h.settle('a')
    await flush()
    // 就绪之后 harness 仍会重组配置树：新条目与它的结算都不该再产生进度行。
    const gate = h.spawn({ id: 'late' })
    gate.resolve()
    await flush()
  })
  assert.deepEqual(lines, [`${PREFIX}0/1`, `${PREFIX}1/1`])
})

test('激活失败（await reject）也计入已激活：那一行不再阻塞启动', async () => {
  const h = makeHarness([{ id: 'a', withFiber: true }])
  const lines = await withCapturedStderr(async () => {
    apply(h.ctx)
    h.fail('a')
    await flush()
  })
  assert.deepEqual(lines, [`${PREFIX}0/1`, `${PREFIX}1/1`])
})

test('同一个条目 id 的重复事件只计一次', async () => {
  const entries = []
  let listener
  const gate = deferred()
  const entry = { id: 'a', disabled: false, options: { name: 'a', group: false }, fiber: undefined }
  entry.fiber = { entry, await: () => gate.promise }
  entries.push(entry)
  const ctx = { loader: { entries: () => entries }, on: (_e, cb) => { listener = cb } }

  const lines = await withCapturedStderr(async () => {
    apply(ctx)
    // internal/plugin 在构造与销毁两个方向都会发射；同一 id 不得重复计数。
    listener(entry.fiber)
    gate.resolve()
    await flush()
    listener(entry.fiber)
    await flush()
  })
  assert.deepEqual(lines, [`${PREFIX}0/1`, `${PREFIX}1/1`])
})

test('没有 loader 时静默返回，不写任何行也不抛', async () => {
  const lines = await withCapturedStderr(async () => {
    apply({})
    await flush()
  })
  assert.deepEqual(lines, [])
})

test('自身出错只写一行 unavailable，不向外抛', async () => {
  const ctx = {
    loader: {
      entries: () => {
        throw new Error('boom')
      },
    },
  }
  const lines = await withCapturedStderr(async () => {
    apply(ctx)
    await flush()
  })
  assert.deepEqual(lines, [`${PREFIX}unavailable boom`])
})

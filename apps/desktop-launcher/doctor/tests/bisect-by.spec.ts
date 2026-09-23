/**
 * Tests for the generic bisectBy binary-search framework.
 */

import { describe, it, expect } from 'vitest'
import { bisectBy } from '../src/bisect-by.js'

describe('bisectBy', () => {
  it('returns null for empty input', async () => {
    const result = await bisectBy([], async () => true)
    expect(result).toBeNull()
  })

  it('finds the only item in a single-element array', async () => {
    const result = await bisectBy(['a'], async subset => subset.length > 0)
    expect(result).toBe('a')
  })

  it('finds culprit at the beginning', async () => {
    const items = ['a', 'b', 'c', 'd', 'e']
    const culprit = 'a'
    const result = await bisectBy(items, async subset => subset.includes(culprit))
    expect(result).toBe(culprit)
  })

  it('finds culprit at the end', async () => {
    const items = ['a', 'b', 'c', 'd', 'e']
    const culprit = 'e'
    const result = await bisectBy(items, async subset => subset.includes(culprit))
    expect(result).toBe(culprit)
  })

  it('finds culprit in the middle', async () => {
    const items = ['a', 'b', 'c', 'd', 'e']
    const culprit = 'c'
    const result = await bisectBy(items, async subset => subset.includes(culprit))
    expect(result).toBe(culprit)
  })

  it('works with an even number of items', async () => {
    const items = ['a', 'b', 'c', 'd']
    const culprit = 'd'
    const result = await bisectBy(items, async subset => subset.includes(culprit))
    expect(result).toBe(culprit)
  })

  it('returns null when verification fails (no single culprit)', async () => {
    const items = ['a', 'b']
    // Both items together cause the problem, but neither alone does.
    const result = await bisectBy(items, async subset => subset.length === 2)
    expect(result).toBeNull()
  })

  it('uses log2(n)+1-ish calls for a single culprit', async () => {
    const items = Array.from({ length: 16 }, (_, i) => `item-${i}`)
    const culprit = 'item-11'
    let calls = 0
    const result = await bisectBy(items, async (subset) => {
      calls += 1
      return subset.includes(culprit)
    })
    expect(result).toBe(culprit)
    // 16 items → 4 binary-search steps + 1 verification = 5 calls max
    expect(calls).toBeLessThanOrEqual(6)
  })

  it('works with custom object types', async () => {
    const items = [
      { id: 1, name: 'first' },
      { id: 2, name: 'second' },
      { id: 3, name: 'third' },
    ]
    const culprit = items[1]!
    const result = await bisectBy(items, async subset => subset.some(s => s.id === culprit.id))
    expect(result).toBe(culprit)
  })
})

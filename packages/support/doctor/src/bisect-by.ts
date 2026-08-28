/**
 * Generic binary-search isolation framework.
 *
 * Given a set of items and an async predicate that determines whether the
 * "bad" behavior is still present when only a subset of items is active,
 * narrows down to the single item responsible.
 *
 * The predicate contract:
 *  - `isBad(items)` → `true` means the bad behavior is present.
 *  - `isBad([])` must be `false` (no items → no problem).
 *  - `isBad(fullSet)` must be `true` (all items → problem reproduces).
 *  - Exactly one item in the set is the culprit; adding it to any subset
 *    that previously passed will make it fail.
 *
 * @module @deepseek-ai/dsh-doctor/bisect-by
 */

/**
 * Run binary search over `items` to find the single item whose presence
 * causes the bad behavior.
 *
 * @template T - item type.
 * @param items - the full set of suspects; must contain exactly one culprit.
 * @param isBad - async predicate returning `true` when the bad behavior
 *   is observed with the given subset of items active.
 * @returns the identified culprit, or `null` if the search is inconclusive.
 */
export async function bisectBy<T>(
  items: T[],
  isBad: (subset: T[]) => Promise<boolean>,
): Promise<T | null> {
  if (items.length === 0) return null

  let low = 0
  let high = items.length - 1

  while (low < high) {
    const mid = Math.floor((low + high) / 2)
    // Test the first half (low..mid) as the only active suspects.
    const firstHalf = items.slice(low, mid + 1)
    const firstHalfBad = await isBad(firstHalf)

    if (firstHalfBad) {
      // Culprit is in the first half.
      high = mid
    } else {
      // Culprit is in the second half.
      low = mid + 1
    }
  }

  const result = items[low]
  if (result === undefined) return null

  // Verify: the identified item alone should reproduce the bad behavior.
  const verifyBad = await isBad([result])
  if (!verifyBad) return null

  return result
}

/**
 * Version-label ordering for toolchain entries.
 *
 * The index mixes styles that no single automatic split handles: Go publishes
 * `1.27.1`, JDK 8 uses Adoptium's release label `8u504`, JDK 21 uses
 * `21.0.12.1`, and Rust's `1.98`/`1.99` carry a compatibility promise that must
 * not be split into separate feature lines. Comparison therefore works on numeric
 * segments, while the feature line a card groups by is declared by the index and
 * only falls back to a derived value for older indexes.
 *
 * @module @deepseek-ai/dsh-desktop/toolchain/version
 */

/**
 * Largest digit count one numeric run may contribute.
 *
 * No real version label reaches this size; the cap exists so a malformed label with a
 * very long digit run cannot overflow and turn "which is newer" into its opposite,
 * which is the one answer this module exists to get right.
 */
const MAX_VERSION_DIGITS = 9

/**
 * Split a label into numeric segments.
 *
 * Fields are separated by `.`, `-`, `+`, and `_`; every digit run inside a field is a
 * segment of its own, so `v18` yields 18 and `8u504` yields (8, 504) — keeping JDK 8's
 * build number comparable. A label with no digit at all reports `undefined` and the
 * caller decides the fallback.
 *
 * Known boundary: a prerelease suffix such as `1.0.0-rc1` contributes its digit as an
 * ordinary segment, so `rc1` sorts above `1.0.0`. The shipped index carries no such
 * label; if one appears, the suffix needs its own ordering rather than letting rc's
 * digit pass for a patch level.
 * @param version - version label.
 * @returns numeric segments, or undefined when the label carries no digit.
 */
export function versionSegments(version: string): number[] | undefined {
  const fields = version.split(/[.\-+_]/u)
  const segments: number[] = []
  let any = false
  for (const field of fields) {
    const numbers = fieldNumbers(field)
    if (numbers.length === 0) {
      segments.push(0)
      continue
    }
    any = true
    segments.push(...numbers)
  }
  return any ? segments : undefined
}

/**
 * Collect every digit run in one field, in order of appearance.
 * @param field - one dot-separated field of a version label.
 * @returns decimal values of the runs.
 */
function fieldNumbers(field: string): number[] {
  const out: number[] = []
  let index = 0
  while (index < field.length) {
    const code = field.charCodeAt(index)
    if (code < 48 || code > 57) { index += 1; continue }
    let value = 0
    let digits = 0
    while (index < field.length) {
      const next = field.charCodeAt(index)
      if (next < 48 || next > 57) break
      if (digits < MAX_VERSION_DIGITS) value = value * 10 + (next - 48)
      digits += 1
      index += 1
    }
    out.push(value)
  }
  return out
}

/**
 * Compare two version labels numerically.
 *
 * Missing trailing segments count as zero, so `1.2` equals `1.2.0`. When either label
 * carries no digit at all the comparison falls back to byte order: callers need a
 * stable, explainable order, and guessing "which is newer" from a non-version label
 * is the worse answer. That branch also covers the empty string and labels such as
 * `latest`.
 * @param left - first label.
 * @param right - second label.
 * @returns negative when `left` is older, zero when equal, positive when newer.
 */
export function compareVersions(left: string, right: string): number {
  const leftSegments = versionSegments(left)
  const rightSegments = versionSegments(right)
  if (leftSegments === undefined || rightSegments === undefined) {
    return left < right ? -1 : left > right ? 1 : 0
  }
  const length = Math.max(leftSegments.length, rightSegments.length)
  for (let index = 0; index < length; index += 1) {
    const a = leftSegments[index] ?? 0
    const b = rightSegments[index] ?? 0
    if (a !== b) return a < b ? -1 : 1
  }
  return 0
}

/**
 * Sort version labels from newest to oldest, in place.
 *
 * Card display and the fallback choice after an uninstall share this order, so the
 * same set never appears in opposite orders in the two places.
 * @param versions - labels to sort; mutated.
 */
export function sortVersionsDesc(versions: string[]): void {
  versions.sort((left, right) => compareVersions(right, left))
}
/**
 * Pick the newest label from a set.
 * @param versions - candidate labels.
 * @returns the newest label, or undefined for an empty set.
 */
export function highestVersion(versions: readonly string[]): string | undefined {
  let best: string | undefined
  for (const version of versions) {
    if (best === undefined || compareVersions(version, best) > 0) best = version
  }
  return best
}

/**
 * Resolve the feature line a version belongs to.
 *
 * An index that declares `major` is authoritative: no automatic split is universal.
 * Go's feature version is the second segment (1.26 and 1.27 are separate lines)
 * while Node's is the first (24.20 and 24.21 share one), JDK 8 uses a dotless
 * release label, and Rust must not be split at all.
 *
 * Without a declaration the first digit segment stands in, or the whole label when it
 * holds no digit — grouping at least keeps identical labels together.
 * @param declared - the index's explicit `major`, when present.
 * @param version - version label.
 * @returns the feature line identifier.
 */
export function featureLine(declared: string | undefined, version: string): string {
  if (declared !== undefined && declared !== '') return declared
  const segments = versionSegments(version)
  if (segments === undefined || segments.length === 0) return version
  // A leading 0 means the feature version is the second segment (0.2.x style labels).
  if (segments[0] === 0 && segments.length >= 2) return `${segments[0]}.${segments[1]}`
  return String(segments[0])
}

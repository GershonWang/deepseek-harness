import { describe, expect, it } from 'vitest'
import {
  compareVersions,
  featureLine,
  highestVersion,
  sortVersionsDesc,
  versionSegments,
} from '../src/toolchain/version.ts'

describe('version segments', () => {
  it('reads plain dotted labels', () => {
    expect(versionSegments('1.27.1')).toEqual([1, 27, 1])
  })

  it('keeps a build number inside one field comparable', () => {
    // Adoptium publishes JDK 8 as `8u504`; the build number decides which is newer.
    expect(versionSegments('8u504')).toEqual([8, 504])
  })

  it('reports no segments for a label without digits', () => {
    expect(versionSegments('latest')).toBeUndefined()
    expect(versionSegments('')).toBeUndefined()
  })

  it('caps a very long digit run so it cannot overflow into a wrong order', () => {
    const segments = versionSegments(`1.${'9'.repeat(30)}`)
    expect(segments?.[1]).toBeLessThanOrEqual(999_999_999)
  })
})

describe('version comparison', () => {
  it('orders patch releases', () => {
    expect(compareVersions('1.27.1', '1.26.0')).toBeGreaterThan(0)
    expect(compareVersions('1.26.0', '1.27.1')).toBeLessThan(0)
    expect(compareVersions('1.2', '1.2.0')).toBe(0)
  })

  it('orders JDK release labels', () => {
    expect(compareVersions('8u504', '8u302')).toBeGreaterThan(0)
    expect(compareVersions('21.0.12.1', '8u504')).toBeGreaterThan(0)
  })

  it('falls back to byte order for labels without digits', () => {
    // Callers need a stable order; guessing "newer" from a non-version label is worse.
    expect(compareVersions('latest', 'nightly')).toBeLessThan(0)
    expect(compareVersions('', 'latest')).toBeLessThan(0)
  })

  it('sorts a set from newest to oldest', () => {
    const versions = ['1.26.0', '1.27.1', '1.25.9']
    sortVersionsDesc(versions)
    expect(versions).toEqual(['1.27.1', '1.26.0', '1.25.9'])
  })

  it('picks the highest label', () => {
    expect(highestVersion(['1.26.0', '1.27.1'])).toBe('1.27.1')
    expect(highestVersion([])).toBeUndefined()
  })
})

describe('feature lines', () => {
  it('prefers the index declaration', () => {
    // No automatic split is universal: Go splits at the second segment, Node at the first.
    expect(featureLine('1', '1.27.1')).toBe('1')
    expect(featureLine('0.26', '0.26.1')).toBe('0.26')
  })

  it('derives the first segment when the index declares none', () => {
    expect(featureLine(undefined, '24.21.0')).toBe('24')
    expect(featureLine(undefined, '8u504')).toBe('8')
  })

  it('keeps a zero-leading label on its second segment', () => {
    expect(featureLine(undefined, '0.26.1')).toBe('0.26')
  })

  it('groups a label without digits by the whole label', () => {
    expect(featureLine(undefined, 'latest')).toBe('latest')
  })
})

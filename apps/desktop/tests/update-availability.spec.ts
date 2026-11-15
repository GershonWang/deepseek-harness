import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, expect, it } from 'vitest'
import { UPDATE_FEED_FILENAME, hasPackagedUpdateSource } from '../src/update-availability.ts'

const roots: string[] = []

function resources(withFeed: boolean): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-update-availability-'))
  roots.push(root)
  if (withFeed) writeFileSync(join(root, UPDATE_FEED_FILENAME), 'provider: generic\n')
  return root
}

afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
})

it('offers updates only to a packaged build that ships the feed descriptor', () => {
  expect(hasPackagedUpdateSource(true, resources(true))).toBe(true)
  expect(hasPackagedUpdateSource(true, resources(false))).toBe(false)
})

it('never offers updates to an unpackaged run, which has no installed application to replace', () => {
  expect(hasPackagedUpdateSource(false, resources(true))).toBe(false)
})

it('treats an unreadable resources directory as unavailable instead of failing the entry point', () => {
  const root = resources(false)
  expect(hasPackagedUpdateSource(true, join(root, 'absent'))).toBe(false)
  // A resources path that is itself a file cannot contain the descriptor either.
  const file = join(root, 'not-a-directory')
  writeFileSync(file, '')
  expect(hasPackagedUpdateSource(true, file)).toBe(false)
})

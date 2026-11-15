import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PACKAGE_VERSION_FILENAME, readPackageVersion } from '../src/package-version.ts'

const roots: string[] = []
function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), 'dsh-package-version-'))
  roots.push(root)
  return root
}
function writeVersion(root: string, value: string): void {
  writeFileSync(join(root, PACKAGE_VERSION_FILENAME), value)
}
afterEach(() => {
  for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true })
})

describe('distribution package version', () => {
  it('reads the version the packaging step wrote beside the resources', () => {
    const root = temporaryRoot()
    writeVersion(root, '{"version":"0.2.0.9"}\n')
    expect(readPackageVersion(root)).toBe('0.2.0.9')
  })

  it('propagates a missing file as no distribution version', () => {
    // An upstream or development build carries no such file at all; that is not a failure.
    expect(readPackageVersion(temporaryRoot())).toBeUndefined()
  })

  it('rejects a malformed file instead of reporting its text as a version', () => {
    const root = temporaryRoot()
    // A corrupted file must not replace the version the build itself can vouch for.
    writeVersion(root, '{"version":42}\n')
    expect(readPackageVersion(root)).toBeUndefined()
  })

  it('rejects a value that is not shaped like a version', () => {
    const root = temporaryRoot()
    writeVersion(root, '{"version":"<html>not a version</html>"}\n')
    expect(readPackageVersion(root)).toBeUndefined()
  })

  it('falls back when the file is unreadable JSON and reports the malformed file', () => {
    const root = temporaryRoot()
    writeVersion(root, '{ not json\n')
    // 损坏文件值得留下诊断，但窗口仍必须打开，所以只警告并回退。
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    expect(readPackageVersion(root)).toBeUndefined()
    expect(warn).toHaveBeenCalledOnce()
    warn.mockRestore()
  })
})

import { afterEach, expect, it, vi } from 'vitest'
import { hideMenuBar } from '../src/menu-bar.ts'

afterEach(() => { vi.restoreAllMocks() })

/** A window carrying only the method the helper uses, so the assertion covers that call. */
function window() {
  return { setMenuBarVisibility: vi.fn<(visible: boolean) => void>() }
}

it.each(['win32', 'linux'] as const)('hides the native menu bar on %s', (platform) => {
  vi.spyOn(process, 'platform', 'get').mockReturnValue(platform)
  const target = window()
  hideMenuBar(target)
  expect(target.setMenuBarVisibility.mock.calls).toEqual([[false]])
})

it('leaves the macOS system menu bar alone', () => {
  vi.spyOn(process, 'platform', 'get').mockReturnValue('darwin')
  const target = window()
  hideMenuBar(target)
  expect(target.setMenuBarVisibility).not.toHaveBeenCalled()
})

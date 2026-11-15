/**
 * 壳窗口的菜单栏行为。
 *
 * 这些窗口（工具链市场、关于、终端、迁移）都是壳拥有的本地化文档，没有自己需要
 * 的原生菜单项。`autoHideMenuBar` 只是"默认收起"，按 Alt 会把 Application / Edit
 * 两行重新拉出来——系统截图快捷键恰好带 Alt，于是截出来的界面顶部多出两行英文。
 * 本文件断言这些窗口改为移除整条菜单栏，并且选项里不再残留 autoHideMenuBar。
 */

import { beforeEach, describe, expect, it, vi } from 'vitest'

/** BrowserWindow 的最小桩：只提供这些窗口在创建阶段真正用到的方法。 */
function fakeWindow() {
  const window = {
    webContents: {
      isDestroyed: () => false,
      send: vi.fn(),
      setWindowOpenHandler: vi.fn(),
      on: vi.fn(),
      mainFrame: {},
    },
    loadURL: vi.fn(async () => undefined),
    isDestroyed: () => false,
    isMinimized: () => false,
    restore: vi.fn(),
    show: vi.fn(),
    focus: vi.fn(),
    destroy: vi.fn(),
    setTitle: vi.fn(),
    once: vi.fn(),
    on: vi.fn(),
    setMenuBarVisibility: vi.fn<(visible: boolean) => void>(),
  }
  return window
}

const created: ReturnType<typeof fakeWindow>[] = []
const options: Record<string, unknown>[] = []

/**
 * 构造器桩。
 *
 * 这里必须能被 `new` 调用（生产代码写的是 `new BrowserWindow(...)`），同时又要
 * 把每次创建的窗口与选项捕获下来供断言，因此用普通函数而不是 class：一个只包含
 * 构造函数的 class 会被 `no-extraneous-class` 判为多余。
 * @param init - 生产代码传入的窗口选项。
 * @returns 供断言使用的窗口桩。
 */
function constructWindow(init: Record<string, unknown>): ReturnType<typeof fakeWindow> {
  options.push(init)
  const window = fakeWindow()
  created.push(window)
  return window
}

vi.mock('electron', () => ({
  BrowserWindow: constructWindow,
  ipcMain: { handle: vi.fn(), removeHandler: vi.fn() },
  clipboard: { writeText: vi.fn() },
  shell: { openExternal: vi.fn(async () => undefined) },
}))

const { DesktopToolchainWindow } = await import('../src/toolchain-window.ts')
const { DesktopAboutWindow } = await import('../src/about-window.ts')
const { resolveDesktopLocale } = await import('../src/locale.ts')

beforeEach(() => {
  created.length = 0
  options.length = 0
})

describe('壳窗口的菜单栏', () => {
  it.each(['win32', 'linux'] as const)('工具链市场在 %s 上移除整条菜单栏', (platform) => {
    vi.spyOn(process, 'platform', 'get').mockReturnValue(platform)
    const window = new DesktopToolchainWindow({
      preload: 'preload-toolchain.cjs',
      locale: () => resolveDesktopLocale('zh-CN'),
      view: async () => { throw new Error('not used here') },
      install: async () => undefined,
      uninstall: async () => undefined,
      activate: async () => undefined,
      refresh: async () => undefined,
    })
    window.show()
    // 选项里不能再有 autoHideMenuBar：它保留 Alt 唤回菜单栏的能力。
    expect(options[0]?.autoHideMenuBar).toBeUndefined()
    expect(created[0]?.setMenuBarVisibility.mock.calls).toEqual([[false]])
    window.dispose()
  })

  it('关于窗口同样移除整条菜单栏', () => {
    vi.spyOn(process, 'platform', 'get').mockReturnValue('linux')
    const window = new DesktopAboutWindow({
      preload: 'preload-about.cjs',
      locale: () => resolveDesktopLocale('zh-CN'),
      version: () => '1.0.0',
      harnessVersion: () => '1.0.0',
    })
    window.show()
    expect(options[0]?.autoHideMenuBar).toBeUndefined()
    expect(created[0]?.setMenuBarVisibility.mock.calls).toEqual([[false]])
    window.dispose()
  })

  it('macOS 保留系统菜单栏，不被这条规则影响', () => {
    vi.spyOn(process, 'platform', 'get').mockReturnValue('darwin')
    const window = new DesktopAboutWindow({
      preload: 'preload-about.cjs',
      locale: () => resolveDesktopLocale('zh-CN'),
      version: () => '1.0.0',
      harnessVersion: () => '1.0.0',
    })
    window.show()
    expect(created[0]?.setMenuBarVisibility).not.toHaveBeenCalled()
    window.dispose()
  })
})

// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  base64ToImageFile, isShellEmbedded, requestClipboardImage,
} from '../src/client/desktop-clipboard.ts'

/** Force the module to believe we live inside the shell iframe. */
function fakeParent(): void {
  const parent = { postMessage: vi.fn() } as unknown as Window
  Object.defineProperty(window, 'parent', { value: parent, configurable: true })
}

/** Deliver a shell reply as if the parent window posted it. */
function replyFromParent(data: string): void {
  window.dispatchEvent(new MessageEvent('message', {
    data: { dshDesktop: true, type: 'clipboard-image-result', data },
    source: window.parent,
  }))
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('isShellEmbedded', () => {
  it('is false in a top-level tab (parent === window)', () => {
    expect(isShellEmbedded()).toBe(false)
  })

  it('is true when a parent window exists', () => {
    fakeParent()
    expect(isShellEmbedded()).toBe(true)
  })
})

describe('requestClipboardImage', () => {
  it('resolves null immediately when not embedded', async () => {
    expect(await requestClipboardImage()).toBeNull()
  })

  it('posts a clipboard-read-image request and resolves the base64 reply', async () => {
    fakeParent()
    const promise = requestClipboardImage()
    expect(window.parent.postMessage).toHaveBeenCalledWith(
      { dshDesktop: true, type: 'clipboard-read-image' }, '*')
    replyFromParent('QUJD')
    expect(await promise).toBe('QUJD')
  })

  it('resolves null when the shell replies empty', async () => {
    fakeParent()
    const promise = requestClipboardImage()
    replyFromParent('')
    expect(await promise).toBeNull()
  })

  it('ignores replies that do not match the protocol or source', async () => {
    fakeParent()
    const promise = requestClipboardImage()
    window.dispatchEvent(new MessageEvent('message', {
      data: { dshDesktop: true, type: 'clipboard-image-result', data: 'XXXX' },
      source: { postMessage: vi.fn() } as unknown as Window,
    }))
    replyFromParent('QUJD')
    expect(await promise).toBe('QUJD')
  })

  it('times out to null', async () => {
    vi.useFakeTimers()
    fakeParent()
    const promise = requestClipboardImage(100)
    vi.advanceTimersByTime(150)
    expect(await promise).toBeNull()
  })
})

describe('base64ToImageFile', () => {
  it('decodes base64 into an image/png File', () => {
    const file = base64ToImageFile('iVBORw0KGgo=')
    expect(file).not.toBeNull()
    expect(file?.type).toBe('image/png')
    expect(file?.name).toBe('clipboard-image.png')
    expect(file?.size).toBeGreaterThan(0)
  })

  it('returns null for corrupt base64', () => {
    expect(base64ToImageFile('%%%not-base64%%%')).toBeNull()
  })
})

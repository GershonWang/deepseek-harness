// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  base64ToImageFile, isShellEmbedded, requestClipboardImage,
} from '../src/client/desktop-clipboard.ts'

const originalParent = window.parent

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
  Object.defineProperty(window, 'parent', { value: originalParent, configurable: true })
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
    // 1x1 PNG: 89 50 4E 47 0D 0A 1A 0A ...
    const pngBase64 = 'iVBORw0KGgo='
    const file = base64ToImageFile(pngBase64)
    expect(file).not.toBeNull()
    expect(file?.type).toBe('image/png')
    expect(file?.name).toBe('clipboard-image.png')
    expect(file?.size).toBeGreaterThan(0)
  })

  it('detects JPEG from magic bytes and sets the correct MIME type', () => {
    // FF D8 FF (JPEG SOI + marker)
    const jpegBase64 = btoa(String.fromCharCode(0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 0x4a, 0x46))
    const file = base64ToImageFile(jpegBase64)
    expect(file).not.toBeNull()
    expect(file?.type).toBe('image/jpeg')
    expect(file?.name).toBe('clipboard-image.jpg')
  })

  it('detects WebP from magic bytes and sets the correct MIME type', () => {
    // RIFF....WEBP
    const webpBytes = [0x52, 0x49, 0x46, 0x46, 0, 0, 0, 0, 0x57, 0x45, 0x42, 0x50]
    const webpBase64 = btoa(String.fromCharCode(...webpBytes))
    const file = base64ToImageFile(webpBase64)
    expect(file).not.toBeNull()
    expect(file?.type).toBe('image/webp')
    expect(file?.name).toBe('clipboard-image.webp')
  })

  it('detects BMP from magic bytes and sets the correct MIME type', () => {
    // 42 4D = "BM"
    const bmpBase64 = btoa(String.fromCharCode(0x42, 0x4d, 0, 0, 0, 0, 0, 0))
    const file = base64ToImageFile(bmpBase64)
    expect(file).not.toBeNull()
    expect(file?.type).toBe('image/bmp')
    expect(file?.name).toBe('clipboard-image.bmp')
  })

  it('detects GIF from magic bytes and sets the correct MIME type', () => {
    // GIF89a
    const gifBase64 = btoa(String.fromCharCode(0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0, 0))
    const file = base64ToImageFile(gifBase64)
    expect(file).not.toBeNull()
    expect(file?.type).toBe('image/gif')
    expect(file?.name).toBe('clipboard-image.gif')
  })

  it('falls back to PNG when magic bytes are unrecognized', () => {
    const unknownBase64 = btoa(String.fromCharCode(0x00, 0x01, 0x02, 0x03))
    const file = base64ToImageFile(unknownBase64)
    expect(file).not.toBeNull()
    expect(file?.type).toBe('image/png')
    expect(file?.name).toBe('clipboard-image.png')
  })

  it('returns null for corrupt base64', () => {
    expect(base64ToImageFile('%%%not-base64%%%')).toBeNull()
  })
})

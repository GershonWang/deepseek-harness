/**
 * PostMessage bridge between the harness page and the dsh-desktop shell.
 *
 * The packaged shell loads the harness web app inside a WebKitGTK iframe where
 * the engine never surfaces clipboard images to the page (unlike Chromium).
 * The shell process itself can read the host X11 CLIPBOARD, so the composer
 * asks it for the current image over the same { dshDesktop: true } protocol
 * the external-link bridge uses, and receives base64-encoded bytes back.
 */

/** True while running inside the packaged shell iframe (not a plain browser tab). */
export function isShellEmbedded(): boolean {
  return typeof window !== 'undefined' && window.parent !== window
}

const REQUEST_TYPE = 'clipboard-read-image'
const RESPONSE_TYPE = 'clipboard-image-result'

let inFlight: Promise<string | null> | undefined

/**
 * Ask the shell for the current CLIPBOARD image.
 * @param timeoutMs - how long to wait for the shell reply.
 *   Defaults to 15 s: the shell probes CLIPBOARD, PRIMARY, text/uri-list and
 *   Wayland in sequence, each with its own X11 deadline, and leveling the
 *   client wait below that chain would make slow-but-good pastes report as
 *   "no image".
 * @returns base64 image data, or null when absent, refused, or timed out.
 * Concurrent callers share one request.
 */
export function requestClipboardImage(timeoutMs = 15000): Promise<string | null> {
  if (!isShellEmbedded()) return Promise.resolve(null)
  if (inFlight !== undefined) return inFlight
  inFlight = new Promise<string | null>((resolve) => {
    let settled = false
    const finish = (data: string | null): void => {
      if (settled) return
      settled = true
      window.removeEventListener('message', onMessage)
      if (timer !== undefined) clearTimeout(timer)
      resolve(data)
    }
    const onMessage = (event: MessageEvent): void => {
      const d = event.data ?? {}
      if (d.dshDesktop !== true || d.type !== RESPONSE_TYPE) return
      if (event.source !== window.parent) return
      finish(typeof d.data === 'string' && d.data !== '' ? d.data : null)
    }
    window.addEventListener('message', onMessage)
    const timer = setTimeout(() => finish(null), timeoutMs)
    window.parent.postMessage({ dshDesktop: true, type: REQUEST_TYPE }, '*')
  })
  void inFlight.then(() => { inFlight = undefined })
  return inFlight
}

/** Raster format detection result from magic-number inspection. */
interface DetectedImageFormat {
  /** Standardized MIME media type. */
  mediaType: 'image/png' | 'image/jpeg' | 'image/bmp' | 'image/tiff' | 'image/webp' | 'image/gif'
  /** Suggested filename extension (no leading dot). */
  ext: string
}

/**
 * Detect a raster image's format from its leading bytes.
 * @param bytes - raw image bytes (at least 12 bytes for reliable detection).
 * @returns the detected format, or null when no known signature matches.
 */
function detectImageFormat(bytes: Uint8Array): DetectedImageFormat | null {
  if (bytes.length >= 8 &&
    bytes[0] === 0x89 && bytes[1] === 0x50 && bytes[2] === 0x4e && bytes[3] === 0x47 &&
    bytes[4] === 0x0d && bytes[5] === 0x0a && bytes[6] === 0x1a && bytes[7] === 0x0a) {
    return { mediaType: 'image/png', ext: 'png' }
  }
  if (bytes.length >= 3 &&
    bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) {
    return { mediaType: 'image/jpeg', ext: 'jpg' }
  }
  if (bytes.length >= 12 &&
    bytes[0] === 0x52 && bytes[1] === 0x49 && bytes[2] === 0x46 && bytes[3] === 0x46 &&
    bytes[8] === 0x57 && bytes[9] === 0x45 && bytes[10] === 0x42 && bytes[11] === 0x50) {
    return { mediaType: 'image/webp', ext: 'webp' }
  }
  if (bytes.length >= 2 && bytes[0] === 0x42 && bytes[1] === 0x4d) {
    return { mediaType: 'image/bmp', ext: 'bmp' }
  }
  if (bytes.length >= 4 &&
    ((bytes[0] === 0x49 && bytes[1] === 0x49 && bytes[2] === 0x2a && bytes[3] === 0x00) ||
     (bytes[0] === 0x4d && bytes[1] === 0x4d && bytes[2] === 0x00 && bytes[3] === 0x2a))) {
    return { mediaType: 'image/tiff', ext: 'tiff' }
  }
  if (bytes.length >= 6 &&
    bytes[0] === 0x47 && bytes[1] === 0x49 && bytes[2] === 0x46 && bytes[3] === 0x38 &&
    (bytes[4] === 0x37 || bytes[4] === 0x39) && bytes[5] === 0x61) {
    return { mediaType: 'image/gif', ext: 'gif' }
  }
  return null
}

/**
 * Decode a base64 image into a browser File the attachment rail can take.
 * The file's MIME type and name are derived from the actual bytes' magic
 * number instead of being hard-coded to PNG, so JPEG/BMP/TIFF/WebP payloads
 * from the host X11 clipboard still validate correctly on the service side.
 */
export function base64ToImageFile(data: string): File | null {
  try {
    const binary = atob(data)
    const bytes = Uint8Array.from(binary, ch => ch.charCodeAt(0))
    const detected = detectImageFormat(bytes)
    const mediaType = detected?.mediaType ?? 'image/png'
    const ext = detected?.ext ?? 'png'
    return new File([bytes], `clipboard-image.${ext}`, { type: mediaType })
  } catch {
    return null
  }
}

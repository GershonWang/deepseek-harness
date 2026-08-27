/**
 * PostMessage bridge between the harness page and the dsh-desktop shell.
 *
 * The packaged shell loads the harness web app inside a WebKitGTK iframe where
 * the engine never surfaces clipboard images to the page (unlike Chromium).
 * The shell process itself can read the host X11 CLIPBOARD, so the composer
 * asks it for the current image over the same { dshDesktop: true } protocol
 * the external-link bridge uses, and receives a base64 PNG back.
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
 * @returns base64 PNG data, or null when absent, refused, or timed out.
 * Concurrent callers share one request.
 */
export function requestClipboardImage(timeoutMs = 2500): Promise<string | null> {
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

/** Decode a base64 PNG into a browser File the attachment rail can take. */
export function base64ToImageFile(data: string): File | null {
  try {
    const binary = atob(data)
    const bytes = Uint8Array.from(binary, ch => ch.charCodeAt(0))
    return new File([bytes], 'clipboard-image.png', { type: 'image/png' })
  } catch {
    return null
  }
}

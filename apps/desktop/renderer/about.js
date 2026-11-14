/**
 * Renderer for the About window.
 *
 * Main owns every string, every version, and every destination: this document
 * draws the view it receives and hands repository URLs back for validation.
 */
'use strict'

const api = window.dshAbout

/** How long the copy control confirms it wrote the clipboard, matching the shell's other copy feedback. */
const COPY_FEEDBACK_MS = 1200

/** Replace an element's text content. */
function setText(id, value) {
  const element = document.getElementById(id)
  if (element === null) return
  element.textContent = value == null ? '' : String(value)
}

/** Build one fact row: a repository opens externally, everything else is plain text. */
function fieldRow(field) {
  const row = document.createElement('div')
  row.className = 'row'
  const key = document.createElement('dt')
  key.textContent = field.label
  const value = document.createElement('dd')
  if (field.url === undefined) {
    value.className = 'mono selectable'
    value.textContent = field.value
  } else {
    const link = document.createElement('a')
    link.className = 'link mono'
    link.href = '#'
    link.textContent = field.value
    link.addEventListener('click', (event) => {
      event.preventDefault()
      // The document never opens a destination by itself; main validates the one it offered.
      void api.open(field.url).catch((error) => { console.warn('about: failed to open repository:', error) })
    })
    value.append(link)
  }
  row.append(key, value)
  return row
}

let current = null
let copyTimer

/** Draw the whole view. */
function render(view) {
  if (view === null || view === undefined) return
  current = view
  document.documentElement.lang = view.locale
  document.title = view.title
  document.getElementById('logo').setAttribute('aria-label', view.logoAlt)
  setText('product', view.product)
  setText('version', view.version)
  setText('form', view.form)
  setText('package-id', view.packageId)
  const copy = document.getElementById('copy')
  copy.title = view.copyLabel
  copy.setAttribute('aria-label', view.copyLabel)
  const fields = document.getElementById('fields')
  fields.replaceChildren(...view.fields.map(fieldRow))
  document.getElementById('about').hidden = false
}

/** Copy the package ID; this control is the reason the ID is shown as selectable text. */
function bindCopy() {
  const button = document.getElementById('copy')
  button.addEventListener('click', async () => {
    if (current === null) return
    let copied = false
    try {
      copied = await api.copyPackageId()
    } catch (error) {
      console.warn('about: failed to copy package id:', error)
      return
    }
    if (!copied) return
    const original = current.copyLabel
    button.title = current.copiedLabel
    button.setAttribute('aria-label', current.copiedLabel)
    clearTimeout(copyTimer)
    copyTimer = setTimeout(() => {
      button.title = original
      button.setAttribute('aria-label', original)
    }, COPY_FEEDBACK_MS)
  })
}

/** Escape closes the window, the same path as its title bar control. */
function bindEscape() {
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') window.close()
  })
}

/** Hide WebKit's built-in navigation menu, as the other shell documents do. */
function bindContextMenu() {
  document.addEventListener('contextmenu', (event) => event.preventDefault(), true)
}

let received = false
const unsubscribe = api.subscribe((view) => { received = true; render(view) })
window.addEventListener('pagehide', unsubscribe, { once: true })
bindCopy()
bindEscape()
bindContextMenu()
// A failed read leaves the window empty rather than inventing copy: main owns every
// string here, and it cannot describe a failure it did not send a message for.
void api.status()
  .then((view) => { if (!received) render(view) })
  .catch((error) => { console.error('about: failed to load data:', error) })

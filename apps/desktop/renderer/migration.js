/**
 * Renderer for the profile migration prompt.
 *
 * Main owns every decision: this document only renders the view it receives and
 * reports the user's choices back. Every string is supplied by main so the copy
 * stays locale-owned, and the renderer holds no path or plugin identity of its own.
 */
'use strict'

const api = window.dshMigration

/** Replace an element's text when the value is present. */
function setText(id, value) {
  const element = document.getElementById(id)
  if (element === null) return
  element.textContent = value
}

/** Build one labelled plugin row carrying the package name in its dataset. */
function pluginRow(plugin) {
  const item = document.createElement('li')
  item.className = 'row selectable'
  const checkbox = document.createElement('input')
  checkbox.type = 'checkbox'
  checkbox.checked = plugin.checked
  checkbox.dataset.plugin = plugin.name
  const label = document.createElement('label')
  const strong = document.createElement('strong')
  strong.textContent = plugin.name
  const small = document.createElement('small')
  small.textContent = plugin.detail
  label.append(strong, small)
  item.append(checkbox, label)
  item.addEventListener('click', (event) => {
    if (event.target !== checkbox) checkbox.checked = !checkbox.checked
  })
  checkbox.addEventListener('change', () => { importButton.disabled = false })
  return item
}

/** Build one conflict block offering the legacy and desktop copies. */
function conflictRow(conflict, onChoose) {
  const block = document.createElement('div')
  block.className = 'conflict'
  const title = document.createElement('strong')
  title.textContent = conflict.label
  block.append(title)
  for (const option of [conflict.legacy, conflict.desktop]) {
    const label = document.createElement('label')
    const radio = document.createElement('input')
    radio.type = 'radio'
    radio.name = `conflict-${conflict.id}`
    radio.value = option.side
    radio.checked = option.side === conflict.selected
    radio.addEventListener('change', () => onChoose(conflict.id, option.side))
    const body = document.createElement('span')
    const heading = document.createElement('span')
    heading.textContent = option.label
    const preview = document.createElement('pre')
    preview.textContent = option.preview
    body.append(heading, preview)
    label.append(radio, body)
    block.append(label)
  }
  return block
}

let importButton = document.getElementById('import')
let currentView = null

/** Draw the whole view; called on every push from main. */
function render(view) {
  if (view === null) return
  currentView = view
  document.documentElement.lang = view.locale
  document.title = view.title
  setText('title', view.title)
  setText('intro', view.intro)

  document.getElementById('settings-toggle').checked = view.settings.checked
  setText('settings-label', view.settings.label)
  setText('settings-detail', view.settings.detail)

  setText('plugin-heading', view.plugins.heading)
  setText('plugin-note', view.plugins.note)
  const list = document.getElementById('plugin-list')
  list.replaceChildren(...view.plugins.items.map(pluginRow))
  document.getElementById('plugin-section').hidden = view.plugins.items.length === 0

  const conflictSection = document.getElementById('conflict-section')
  conflictSection.hidden = view.conflicts.items.length === 0
  setText('conflict-heading', view.conflicts.heading)
  setText('conflict-note', view.conflicts.note)
  setText('conflict-all-desktop', view.conflicts.useDesktopAll)
  const choose = (id, side) => {
    for (const conflict of view.conflicts.items) {
      if (conflict.id === id) conflict.selected = side
    }
  }
  document.getElementById('conflict-list').replaceChildren(
    ...view.conflicts.items.map(item => conflictRow(item, choose)),
  )

  setText('retention-note', view.retentionNote)
  setText('unsupported-note', view.unsupportedNote)
  setText('skip', view.buttons.skip)
  setText('import', view.buttons.import)
  importButton.disabled = false
  document.getElementById('migration').hidden = false
  document.getElementById('migration').focus()
}

/** Collect the current choices and submit them. */
function submitImport() {
  if (currentView === null) return
  const plugins = [...document.querySelectorAll('#plugin-list input[data-plugin]')]
    .filter(input => input.checked)
    .map(input => input.dataset.plugin)
  const conflicts = {}
  for (const conflict of currentView.conflicts.items) {
    const picked = document.querySelector(`input[name="conflict-${conflict.id}"]:checked`)
    if (picked !== null) conflicts[conflict.id] = picked.value
  }
  importButton.disabled = true
  void api.respond({
    action: 'import',
    settings: document.getElementById('settings-toggle').checked,
    plugins,
    conflicts,
  })
}

document.getElementById('skip').addEventListener('click', () => {
  importButton.disabled = true
  void api.respond({ action: 'skip', settings: false, plugins: [], conflicts: {} })
})

importButton.addEventListener('click', submitImport)

document.getElementById('conflict-all-desktop').addEventListener('click', () => {
  if (currentView === null) return
  for (const conflict of currentView.conflicts.items) {
    conflict.selected = 'desktop'
  }
  render(currentView)
})

void api.ready().then(render)
api.subscribe(render)

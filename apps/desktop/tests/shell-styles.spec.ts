import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/**
 * Shell documents share one design-token sheet.
 *
 * A page's own stylesheet only references `--dsw-alias-*`; it does not define them. Two
 * failures follow from that and neither shows up at build time: a page that forgets the
 * `<link>` renders with every colour and border missing while its hardcoded layout still
 * works, and a token spelled wrong silently falls back to nothing. Both look like "the UI
 * lost half its styling", so both are checked here.
 */

const renderer = fileURLToPath(new URL('../renderer/', import.meta.url))
const tokenSheet = fileURLToPath(new URL('../lib/welcome/design-platform.css', import.meta.url))

/** Tokens defined by the shared sheet. */
function definedTokens(): Set<string> {
  const css = readFileSync(tokenSheet, 'utf8')
  return new Set([...css.matchAll(/--dsw-alias-[a-z0-9-]+:/gu)].map(match => match[0].slice(0, -1)))
}

/** Every stylesheet in the shell renderer directory. */
function stylesheets(): string[] {
  return readdirSync(renderer).filter(name => name.endsWith('.css'))
}

describe('shell document styles', () => {
  it('references only tokens the shared sheet defines', () => {
    const defined = definedTokens()
    // A misspelled token resolves to nothing, which the browser reports nowhere.
    expect(defined.size).toBeGreaterThan(100)
    for (const sheet of stylesheets()) {
      const css = readFileSync(`${renderer}${sheet}`, 'utf8')
      const used = [...css.matchAll(/var\((--dsw-alias-[a-z0-9-]+)/gu)].map(match => match[1])
      const missing = [...new Set(used)].filter(token => !defined.has(token))
      expect(missing, `${sheet} uses undefined tokens`).toEqual([])
    }
  })

  it('reaches the shared sheet from every page that uses a token', () => {
    // The page still renders without it — the layout is hardcoded — so the failure is a
    // silent loss of colour and borders rather than a blank window. A page may take the
    // tokens directly or through another shell sheet that imports them, so the linked
    // stylesheets are followed rather than requiring one exact href.
    const withTokens = stylesheets().filter((sheet) => {
      const css = readFileSync(`${renderer}${sheet}`, 'utf8')
      return css.includes('var(--dsw-alias-')
    })
    expect(withTokens.length).toBeGreaterThan(0)
    for (const sheet of withTokens) {
      const page = `${sheet.replace(/\.css$/u, '')}.html`
      let html: string
      try { html = readFileSync(`${renderer}${page}`, 'utf8') } catch { continue }
      const hrefs = [...html.matchAll(/<link[^>]+href="([^"]+\.css)"/gu)].map(match => match[1])
      const reachesTokens = hrefs.some((href) => {
        // `design-platform.css` is served from `lib/welcome` by the shell protocol, not
        // from this directory, so that one name maps to the built asset.
        const path = href === 'design-platform.css'
          ? resolve(renderer, '../lib/welcome/design-platform.css')
          : resolve(renderer, href)
        try {
          // Only a definition counts: a page's own sheet mentions every token in
          // `var(...)`, so matching the bare prefix would let a page vouch for itself.
          return /--dsw-alias-[a-z0-9-]+:/u.test(readFileSync(path, 'utf8'))
        } catch { return false }
      })
      expect(reachesTokens, `${page} must reach design-platform.css through a linked sheet`).toBe(true)
    }
  })

  it('links the shared palette script wherever a page follows the app theme', () => {
    // shell-theme.js owns the dark attribute; a page that sets it itself duplicates the
    // media query and drifts when the app palette changes.
    for (const sheet of stylesheets()) {
      const css = readFileSync(`${renderer}${sheet}`, 'utf8')
      if (!css.includes('data-ds-dark-theme')) continue
      const page = `${sheet.replace(/\.css$/u, '')}.html`
      let html: string
      try { html = readFileSync(`${renderer}${page}`, 'utf8') } catch { continue }
      expect(html, `${page} must load shell-theme.js`).toContain('src="shell-theme.js"')
    }
  })
})

import { readFileSync } from 'node:fs'
import { expect, it } from 'vitest'
import { createElectronBuilderConfig } from '../scripts/electron-builder-config.mjs'

const manifest = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8')) as {
  dependencies: Record<string, string>
}
const root = new URL('../', import.meta.url)

/**
 * Build the unsigned Linux configuration the Linglong bundle packages.
 * @returns electron-builder configuration for linux x64.
 */
function linuxConfig(): {
  readonly files: readonly (string | { readonly from: string; readonly to: string })[]
  readonly asarUnpack: readonly string[]
} {
  return createElectronBuilderConfig({
    DSH_DESKTOP_APP_ID: 'com.example.shell-window-test',
    DSH_DESKTOP_UNSIGNED: '1',
  }, 'linux', 'x64') as never
}

it.each(['lib/preload-terminal.cjs', 'lib/preload-toolchain.cjs', 'lib/preload-about.cjs', 'lib/terminal/**/*'])(
  'ships %s inside the packaged application', (pattern) => {
    // electron-builder copies only what `files` names: a missing entry is a window that fails
    // to load in the installed app while passing every source test.
    expect(linuxConfig().files).toContain(pattern)
  })

it('ships the terminal page bundle produced by the desktop build', () => {
  // The page is a separate Vite output, so it needs both its directory entry and the
  // preload that exposes the bridge; either one missing yields a blank terminal window.
  expect(linuxConfig().files).toContain('lib/terminal/**/*')
  expect(linuxConfig().files).toContain('lib/preload-terminal.cjs')
})

it('keeps node-pty as a packaged dependency so the main-process bundle resolves it', () => {
  // The main bundle's external-package allowlist is this manifest: a dependency missing here
  // fails the bundle, and a devDependency would resolve in the repository but not when packaged.
  expect(manifest.dependencies['node-pty']).toBe('1.2.0-beta.15')
})

it('unpacks native modules so node-pty loads from app.asar.unpacked', () => {
  // A `.node` inside app.asar cannot be dlopen'd; the unpack rule is what makes it loadable.
  expect(linuxConfig().asarUnpack).toContain('**/*.{node,dylib,dll,so,exe}')
})

it('unpacks the Linux Landlock launcher the sandbox spawns', () => {
  // dsh spawns this extensionless ELF as the sandbox's second rung. An executable inside app.asar
  // is not spawnable (ENOTDIR), which probes the backend 'unusable' and fails every confined command
  // closed — the failure the extension globs cannot cover.
  expect(linuxConfig().asarUnpack).toContain('**/node_modules/@deepseek-ai/node-addon-system-*/bin/landlock-run')
})

it('declares the terminal page among the renderer documents', () => {
  // The page HTML lives in `renderer/`, which the package already copies wholesale; this check
  // records that the document exists where the window loads it from.
  expect(readFileSync(new URL('renderer/terminal.html', root), 'utf8')).toContain('lib/terminal/terminal.js')
})

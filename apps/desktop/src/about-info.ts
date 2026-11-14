/**
 * Facts the About window presents and the view it receives.
 *
 * The values are the ones the Electron shell's Linglong packaging gives the
 * application: the same identity the previous Go launcher showed, so a user who
 * upgrades keeps reading the same package ID and the same two repositories.
 * Kept in one module so the tray entry, the window, and the tests cannot drift.
 *
 * @module @deepseek-ai/dsh-desktop/about-info
 */

import type { DesktopLocale } from './locale.ts'
import type { AboutFieldView, DesktopAboutView } from './about-contract.ts'
import { formatDesktopMessage } from './locale.ts'

/** Distribution identity of this shell; not the upstream release identity. */
export const ABOUT_IDENTITY = {
  /** Product name, language-independent. */
  product: 'DeepSeek Harness',
  /** Linglong package ID, shown verbatim because bug reports must quote it. */
  packageId: 'com.deepseek.dsh-desktop',
  /** Who packaged this fork; unrelated to the upstream authors. */
  packager: 'Jokul',
  /** Upstream repository, where product defects belong. */
  upstreamRepo: 'https://github.com/deepseek-ai/deepseek-harness',
  /** Packaging repository of this fork, where Linglong-specific changes belong. */
  forkRepo: 'https://github.com/GershonWang/deepseek-harness',
} as const

/** Repositories the About window may open in the system browser. */
export const ABOUT_REPOSITORIES: readonly string[] = [ABOUT_IDENTITY.upstreamRepo, ABOUT_IDENTITY.forkRepo]

/** Inputs of {@link composeAboutView}; every value is owned outside this module. */
export interface AboutViewInput {
  /** Shell dictionary the window is rendered in. */
  readonly locale: DesktopLocale
  /** Desktop client version, including any prerelease suffix. */
  readonly version: string
  /** DSH runtime version bundled with this build. */
  readonly harnessVersion: string
}

/**
 * Compose the About view for one locale.
 *
 * Each row states one fact; the two repositories carry a `url` so the document
 * renders them as links while main still decides which destinations may open.
 * @param input - locale and the two versions to report.
 * @returns the view the document draws.
 */
export function composeAboutView(input: AboutViewInput): DesktopAboutView {
  const { messages } = input.locale
  const fields: AboutFieldView[] = [
    { label: messages.aboutHarnessVersion, value: input.harnessVersion },
    { label: messages.aboutUpstream, value: ABOUT_IDENTITY.upstreamRepo, url: ABOUT_IDENTITY.upstreamRepo },
    { label: messages.aboutPackager, value: ABOUT_IDENTITY.packager },
    { label: messages.aboutFork, value: ABOUT_IDENTITY.forkRepo, url: ABOUT_IDENTITY.forkRepo },
  ]
  return {
    locale: input.locale.id,
    title: messages.aboutMenu,
    product: ABOUT_IDENTITY.product,
    version: input.version,
    form: formatDesktopMessage(messages.aboutForm, { form: messages.aboutFormElectron }),
    packageId: ABOUT_IDENTITY.packageId,
    logoAlt: messages.aboutLogoAlt,
    copyLabel: messages.aboutCopyPackageId,
    copiedLabel: messages.aboutCopied,
    fields,
  }
}

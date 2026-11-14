/**
 * Archive extraction for toolchain installs.
 *
 * Downloads arrive as `tar.gz`, `tar.xz`, or `zip`. Rather than link a decompression
 * library, extraction shells out to the system `tar`/`unzip`: the package already
 * ships `tar`, `xz`, and `unzip`, and GNU tar reads all three of those formats
 * through one command. That keeps the dependency surface at zero and makes a
 * corrupt-archive diagnostic the tool's own message instead of a hand-written parser.
 *
 * @module @deepseek-ai/dsh-desktop/toolchain/archive
 */

import { execFile } from 'node:child_process'
import { mkdir } from 'node:fs/promises'
import { promisify } from 'node:util'

const run = promisify(execFile)

/** Archive formats an install can carry. */
export type ArchiveFormat = 'tar.gz' | 'tar.xz' | 'zip'

/** Generous bound for one extraction: a 200 MB JDK takes seconds, not minutes. */
const EXTRACT_TIMEOUT_MS = 10 * 60 * 1000

/**
 * Infer the archive format from a download URL.
 *
 * The query string is stripped first: signed CDN URLs carry parameters after the file
 * name. An unrecognized suffix falls back to `tar.gz`, which every historical tool in
 * the index uses.
 * @param url - download URL.
 * @returns the archive format.
 */
export function archiveFormat(url: string): ArchiveFormat {
  const withoutQuery = url.split('?')[0]?.split('#')[0] ?? ''
  const lower = withoutQuery.toLowerCase()
  if (lower.endsWith('.tar.xz') || lower.endsWith('.txz')) return 'tar.xz'
  if (lower.endsWith('.zip')) return 'zip'
  return 'tar.gz'
}

/**
 * Whether a format expects a single container directory.
 * @param format - archive format.
 * @returns whether `tar` reads it directly (a zip needs `unzip`).
 */
function usesTar(format: ArchiveFormat): boolean {
  return format === 'tar.gz' || format === 'tar.xz'
}

/**
 * Extract one archive into a directory that must already exist and be empty.
 *
 * `tar` is given `--no-same-owner`: an index entry cannot dictate ownership, and as a
 * non-root that flag makes extraction fail outright. `-x`/`-o` are deliberately absent:
 * the archive supplies its own modes, which is how binaries keep their executable bit.
 * @param archivePath - absolute path of the downloaded archive.
 * @param format - archive format.
 * @param destination - existing directory to extract into.
 * @throws when the tool is missing or reports a malformed archive.
 */
export async function extractArchive(
  archivePath: string,
  format: ArchiveFormat,
  destination: string,
): Promise<void> {
  await mkdir(destination, { recursive: true })
  // `tar` writes into an existing directory; `unzip` refuses `-d` when it does not exist
  // yet, so the directory is created above and passed the same way to both.
  const command = usesTar(format) ? 'tar' : 'unzip'
  const args = usesTar(format)
    ? ['-xf', archivePath, '-C', destination, '--no-same-owner']
    : ['-q', '-o', archivePath, '-d', destination]
  try {
    await run(command, args, { timeout: EXTRACT_TIMEOUT_MS, maxBuffer: 8 * 1024 * 1024 })
  } catch (error) {
    // The tool's own stderr names the real problem (truncated archive, wrong format,
    // missing binary); rethrowing it verbatim beats inventing a classification.
    const detail = error instanceof Error ? error.message : String(error)
    throw new Error(`toolchain: ${format} extraction failed: ${detail}`, { cause: error })
  }
}

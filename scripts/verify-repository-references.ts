/** Reject maintained references to repository commits and the disallowed organization URL. */

import { execFileSync } from 'node:child_process'
import { existsSync, lstatSync, readFileSync, readlinkSync } from 'node:fs'
import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'
import { canonicalReferenceText } from './verify-public-repository-links.ts'

const root = resolve(import.meta.dirname, '..')
const organization = ['deepseek', 'harness'].join('-')
const organizationUrl = new RegExp(`\\bgithub\\.com/${organization}(?![a-z0-9-])`)
// The independent kit repository owns the engine source and documentation.
const kitRepositoryUrl = new RegExp(`\\bgithub\\.com/${organization}/libreoffice-kit(?:\\.git)?(?=/|[^a-zA-Z0-9_.-]|$)`, 'g')
const commitCandidate = /(?<![a-z0-9])[\da-f]{7,40}(?![a-z0-9])/gi
const excludedPrefixes = ['vendor/', '.agents/notes/archived/']
/**
 * fork 专属的取证与不可变引用文件，理由与本上游口径的差异在于"哈希就是内容本身"：
 * - `AUDIT.md`、`fork-divergence.md`、`i18n.md`、`index.md` 以「分支 + 基点提交 + tree 哈希」
 *   记录桌面启动器与玲珑打包链路的审计基准，抹掉哈希等于抹掉追溯能力；
 * - `remote.go` 的 `defaultIndexURL` 有意钉在不可变提交上（见该常量上方的威胁模型），
 *   属于运行时的功能依赖而非文档引用：仓库标签可被强推移动，改成标签会让"索引内容由
 *   客户端自身的发布过程锚定"这一性质退化，因此保留提交哈希。
 * 这是本 fork 的本地豁免，不改变上游文件的检查口径。
 */
const excludedFiles = [
  'apps/desktop-launcher/docs/AUDIT.md',
  'apps/desktop-launcher/docs/fork-divergence.md',
  'apps/desktop-launcher/docs/i18n.md',
  'apps/desktop-launcher/docs/index.md',
  'apps/desktop-launcher/internal/toolchain/remote.go',
]
const gitOutputLimit = 64 * 1024 * 1024

/** One prohibited reference in a maintained source file. */
export interface RepositoryReference {
  /** Repository-relative path, with forward slashes. */
  file: string
  /** One-based source line containing the reference. */
  line: number
  /** Whether the line names a repository commit or the disallowed organization URL. */
  kind: 'commit-hash' | 'organization-url'
}

function isMaintained(file: string): boolean {
  return !excludedPrefixes.some(prefix => file.startsWith(prefix)) && !excludedFiles.includes(file)
}

/**
 * Inspect a maintained source file against known commit identifiers.
 * @param file - Repository-relative path used in diagnostics and exclusions.
 * @param source - File text or a symlink's stored target.
 * @param commits - Lowercase, unambiguous full or abbreviated commit identifiers.
 * @returns One finding per line and reference kind; digests and other Git object types are accepted.
 */
export function findRepositoryReferences(
  file: string,
  source: string,
  commits: ReadonlySet<string>,
): RepositoryReference[] {
  if (!isMaintained(file)) return []
  const references: RepositoryReference[] = []
  for (const [index, line] of source.split('\n').entries()) {
    if (organizationUrl.test(canonicalReferenceText(line).replace(kitRepositoryUrl, ''))) {
      references.push({ file, line: index + 1, kind: 'organization-url' })
    }
    if ([...line.matchAll(commitCandidate)].some(match => commits.has(match[0].toLowerCase()))) {
      references.push({ file, line: index + 1, kind: 'commit-hash' })
    }
  }
  return references
}

function readMaintainedFiles(repoRoot: string): Map<string, string> {
  const files = execFileSync('git', ['ls-files', '--cached', '--others', '--exclude-standard', '-z'], {
    cwd: repoRoot,
    encoding: 'utf8',
    maxBuffer: gitOutputLimit,
  }).split('\0').filter(file => file !== '' && isMaintained(file))
  const sources = new Map<string, string>()
  for (const file of files) {
    const path = resolve(repoRoot, file)
    const stat = lstatSync(path, { throwIfNoEntry: false })
    if (stat?.isSymbolicLink() === true) sources.set(file, readlinkSync(path))
    else if (stat?.isFile() === true) sources.set(file, readFileSync(path, 'utf8'))
  }
  return sources
}

function repositoryCommits(repoRoot: string, sources: Iterable<string>): Set<string> {
  const candidates = [...new Set([...sources].flatMap(source =>
    [...source.matchAll(commitCandidate)].map(match => match[0].toLowerCase())))]
  if (candidates.length === 0) return new Set()
  const results = execFileSync('git', ['cat-file', '--batch-check=%(objectname) %(objecttype)'], {
    cwd: repoRoot,
    env: { ...process.env, GIT_NO_LAZY_FETCH: '1' },
    encoding: 'utf8',
    input: `${candidates.join('\n')}\n`,
    maxBuffer: gitOutputLimit,
    stdio: ['pipe', 'pipe', 'pipe'],
  }).trimEnd().split('\n')
  // Git resolves prefixes across all available objects, including unreachable ones.
  // Ambiguous prefixes do not identify one object and cannot establish a commit reference.
  return new Set(candidates.filter((candidate, index) => {
    const [object, type] = results[index]?.split(' ') ?? []
    return type === 'commit' && object?.startsWith(candidate) === true
  }))
}

/**
 * Scan tracked and nonignored new files using only the local Git object database.
 * @param repoRoot - Working tree whose files and Git objects are inspected.
 * @returns Prohibited references outside vendor, frozen Agent Notes, and the listed fork exemptions; absent objects cannot match.
 */
export function scanRepositoryReferences(repoRoot: string): RepositoryReference[] {
  const sources = readMaintainedFiles(repoRoot)
  const commits = repositoryCommits(repoRoot, sources.values())
  return [...sources].flatMap(([file, source]) => findRepositoryReferences(file, source, commits))
}

const invokedPath = process.argv[1]
if (invokedPath !== undefined && import.meta.url === pathToFileURL(resolve(invokedPath)).href) {
  // 豁免清单必须指向真实存在的文件：条目因重命名或删除而失效时，闸门要立刻报出来，
  // 而不是让一条死豁免悄悄放宽检查口径。
  const missingExclusions = excludedFiles.filter(file => !existsSync(resolve(root, file)))
  if (missingExclusions.length > 0) {
    console.error('verify-repository-references: exemption entries no longer name a file:')
    for (const file of missingExclusions) console.error(`  ${file}`)
    process.exitCode = 1
  }
  const references = scanRepositoryReferences(root)
  if (references.length === 0) {
    console.log('verify-repository-references: maintained files contain no repository commit identifiers or disallowed organization URLs.')
  } else {
    console.error('verify-repository-references: use release tags or maintained repository links:')
    for (const { file, line, kind } of references) console.error(`  ${file}:${String(line)} ${kind}`)
    process.exitCode = 1
  }
}

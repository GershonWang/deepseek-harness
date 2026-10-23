/**
 * 拒绝启动器壳前端里硬编码的中文文案。
 *
 * 为什么独立于上游的 `verify-client-ui-i18n`：那条门禁的归属是 `packages/client`
 * 与上游各 app 的源码树，`apps/linyaps/` 是本 fork 自有的带壳前端，只有本仓库
 * 需要它。壳前端没有构建器，`frontend/locales/*.js` 是文案的唯一归属地；
 * `app.js`/`i18n.js` 的字符串与 `index.html` 的可见文本一旦出现 CJK，切换语言时
 * 就会漏翻。这里只复制需要的两条规则（JS 字符串字面量与 HTML 文本），不引入
 * 上游门禁的整套 JSX 规则：壳前端里嵌的 SVG 图标与包装模板会误命中那些规则。
 *
 * 注释里的中文是有意保留的说明，不算文案：HTML 注释先整体抹掉（保留换行以便报
 * 出行号），JS 侧由 TypeScript 语法树天然跳过。
 *
 * 用法：在仓库根运行
 *   node apps/linyaps/desktop-launcher/tools/verify-frontend-i18n.mjs [frontend 目录]
 */
import { readFileSync, readdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const ROOT = resolve(HERE, '..', '..', '..', '..')
const require = createRequire(join(ROOT, 'package.json'))
const ts = require('typescript')

/** CJK 统一表意文字及其标点：英文数据与字典键都是 ASCII。 */
const CJK_TEXT = /[\u3000-\u303f\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\ufe30-\ufe4f\uff00-\uffef]/u

/** 收集 JS 字符串字面量、无替换模板与模板表达式里的中文。 */
function jsViolations(file, source) {
  const sourceFile = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS)
  const violations = []
  const report = (node, text) => {
    if (!CJK_TEXT.test(text)) return
    const position = sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile))
    violations.push({ column: position.character + 1, file, line: position.line + 1, text: text.trim() })
  }
  const visit = node => {
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
      report(node, node.text)
    } else if (ts.isTemplateExpression(node)) {
      report(node, [node.head.text, ...node.templateSpans.map(span => span.literal.text)].join(''))
    }
    ts.forEachChild(node, visit)
  }
  visit(sourceFile)
  return violations
}

/** 收集 HTML 可见文本里的中文；注释按原长度抹成空格以保持行号。 */
function htmlViolations(file, source) {
  const withoutComments = source.replaceAll(
    /<!--[\s\S]*?-->/gu,
    comment => comment.replace(/[^\n]/gu, ' '),
  )
  const violations = []
  withoutComments.split('\n').forEach((line, index) => {
    const match = CJK_TEXT.exec(line)
    if (match !== null) {
      violations.push({ column: match.index + 1, file, line: index + 1, text: line.trim() })
    }
  })
  return violations
}

const frontendDir = process.argv[2] === undefined ? join(ROOT, 'apps/linyaps/desktop-launcher/frontend') : resolve(process.argv[2])
// 只扫壳自身的源码：locales/ 是文案归属地，vendor/ 是第三方产物，tools/ 不随包。
const files = readdirSync(frontendDir, { withFileTypes: true })
  .filter(entry => entry.isFile() && (entry.name.endsWith('.js') || entry.name.endsWith('.html')))
  .filter(entry => !entry.name.startsWith('test-'))
  .map(entry => join(frontendDir, entry.name))
  .sort()

const violations = files.flatMap(path => {
  const file = (path.startsWith(ROOT + sep) ? path.slice(ROOT.length + 1) : path).replaceAll('\\', '/')
  const source = readFileSync(path, 'utf8')
  return path.endsWith('.html') ? htmlViolations(file, source) : jsViolations(file, source)
})

if (violations.length > 0) {
  console.error(`verify-frontend-i18n: ${violations.length} hard-coded Chinese string(s):`)
  for (const violation of violations) {
    console.error(`  ${violation.file}:${violation.line}:${violation.column} ${JSON.stringify(violation.text)}`)
  }
  process.exitCode = 1
} else {
  console.log(`verify-frontend-i18n: ${files.length} shell source file(s) use locale-owned copy.`)
}

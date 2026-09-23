#!/usr/bin/env node
/**
 * 构建 apps/desktop-launcher/doctor。
 *
 * doctor 不是 pnpm workspace 成员，也不在 tsdown 的 workspace globs 内，因此根
 * `pnpm run build` 既不编译它、也不为它产出单文件 bundle。它的运行形态就是
 * `tsc` 平铺产物：`lib/types/index.js` 是库入口、`lib/types/cli.js` 是命令行入口，
 * `lib/types/checks/plugins.js` 按同级相对路径找到 `lib/types/loader-probe.js`。
 *
 * 为什么用 `tsc -p` 而不是 `tsc -b`：`-b` 会沿 project references 遍历并构建整张
 * 引用图（实测从 doctor 出发覆盖 23 个项目），而 `-p` 只编译 doctor 自身。doctor 是
 * fork 私有包，它的构建不应该存在任何可能改写上游包构建产物的路径；`-p` 把这条路径
 * 彻底删掉，代价是必须先跑过根构建。
 *
 * 前置：doctor 依赖的 workspace 包必须已构建。缺失时本脚本直接报错并指出该跑什么，
 * 而不是自己去构建它们——那正是上面要避免的行为。
 */
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const toolsDir = dirname(fileURLToPath(import.meta.url))
const launcherDir = resolve(toolsDir, '..')
const repoRoot = resolve(launcherDir, '..', '..')
const doctorDir = join(launcherDir, 'doctor')
const doctorTsconfig = join(doctorDir, 'tsconfig.json')

/**
 * 运行一个子命令，继承 stdio 以便失败时看到编译错误原文。
 * @param command - 可执行文件路径。
 * @param args - 参数列表。
 */
function run(command, args) {
  execFileSync(command, args, { cwd: repoRoot, stdio: 'inherit' })
}

run(process.execPath, [join(toolsDir, 'doctor-link-deps.mjs')])

// 引用项目的产物必须先存在。doctor 的 tsconfig 用 project references 而非路径别名解析
// 这些包，`tsc -p` 不会代建它们；缺产物时 tsc 只会报一串 TS6305，读不出"该跑根构建"
// 这个结论，所以在这里先给出可执行的提示。
const tsconfig = JSON.parse(readFileSync(doctorTsconfig, 'utf8'))
const unbuilt = (tsconfig.references ?? [])
  .map(reference => resolve(doctorDir, reference.path))
  .filter(dir => !existsSync(join(dir, 'lib', 'types', 'index.d.ts')))
  .map(dir => dir.slice(repoRoot.length + 1))
if (unbuilt.length > 0) {
  console.error(`doctor-build: 以下引用项目尚未构建：${unbuilt.join('、')}`)
  console.error('doctor-build: 先运行 pnpm run build（doctor 不代建上游包）')
  process.exit(1)
}

run(process.execPath, [join(repoRoot, 'node_modules', 'typescript', 'bin', 'tsc'), '-p', doctorTsconfig])

// 入口缺失是最难排查的失败形态：构建退出码为 0，但 launcher 起来后只在运行期报
// ERR_MODULE_NOT_FOUND。这里显式断言三个入口都在。
const required = ['lib/types/index.js', 'lib/types/loader-probe.js', 'lib/types/cli.js']
const missing = required.filter(relative => !existsSync(join(doctorDir, relative)))
if (missing.length > 0) {
  console.error(`doctor-build: 构建产物缺少入口 ${missing.join('、')}；检查 tsconfig 的 outDir 与 include`)
  process.exit(1)
}
console.log(`doctor-build: 完成，入口 ${required.join('、')}`)

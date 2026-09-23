/**
 * doctor 的测试配置。
 *
 * 为什么需要独立配置：根 vitest.config.ts 的 include 只覆盖 `apps` 下**一级**目录的
 * tests（形如 `apps/<name>/tests/...`），doctor 位于
 * `apps/desktop-launcher/doctor/tests/`，根配置收集不到它——不建这份配置，doctor 的
 * 全部测试会在 `pnpm run test` 里静默消失。改根配置则要在上游文件里长期维护 fork 的
 * 接线，与"改动收敛在 apps/desktop-launcher/ 内"相悖。
 *
 * 为什么复用根 tsconfig.base.json 做路径解析：doctor 的测试按包名 import 上游包。
 * 不加这个插件，解析会落到 doctor/node_modules 指向的**已构建产物**，测试就变成了对
 * 产物而非对源码的验证（见仓库"源码面与产物面不可混用"约定）。指向根 tsconfig 让这些
 * import 回到各包的 src。
 *
 * @module @dsh-desktop/doctor/vitest.config
 */
import { fileURLToPath } from 'node:url'
import tsconfigPaths from 'vite-tsconfig-paths'
import { defineConfig } from 'vitest/config'

/** 仓库根的类型解析门面；路径相对本文件而非 cwd，避免受启动目录影响。 */
const repoFacade = fileURLToPath(new URL('../../../tsconfig.base.json', import.meta.url))

export default defineConfig({
  plugins: [tsconfigPaths({ projects: [repoFacade] })],
  test: {
    include: ['tests/**/*.spec.ts'],
  },
})

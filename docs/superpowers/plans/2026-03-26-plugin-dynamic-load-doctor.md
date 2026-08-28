# 插件动态加载检查与启动失败自动诊断 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 doctor 能检测出第三方插件导致的运行时启动崩溃（静态检查查不出来的那种），定位到具体 bundle，并提供一键修复；启动连续失败时自动弹出诊断；重试期间前端显示一致的加载状态。

**Architecture:** 三个独立可交付的阶段：(1) doctor 包新增 `plugin-dynamic-load` 检查，通过子进程跑 Cordis Loader 验证插件加载，失败则二分定位 + L2 级禁用修复；(2) 桌面启动器前端新增启动加载页和启动失败页，重试期间防抖不闪烁，失败页带诊断和安全模式快捷入口；(3) supervisor 进入 failed 状态时自动后台跑 doctor，前端自动弹出诊断结果。

**Tech Stack:** TypeScript（Node.js doctor 包，ESM）、Go（Wails 桌面启动器）、原生 HTML/CSS/JS（前端）、Cordis Loader（动态加载验证）、`@deepseek-ai/dsh-app-boot`（profile 加载）、`vitest`（测试）。

---

## 文件结构总览

| 阶段 | 文件 | 职责 | 新建/修改 |
|---|---|---|---|
| 1 | `packages/support/doctor/src/loader-probe.ts` | 子进程探测脚本：加载 profile + 指定 bundle，退出码表示结果 | 新建 |
| 1 | `packages/support/doctor/src/checks/plugins.ts` | 新增 plugin-dynamic-load 检查项 | 修改 |
| 1 | `packages/support/doctor/src/bisect.ts` | 二分法工具（已存在，验证复用性） | 修改（按需） |
| 1 | `packages/support/doctor/tests/loader-probe.spec.ts` | 动态加载检查测试 | 新建 |
| 2 | `apps/desktop-launcher/frontend/index.html` | 新增启动加载页、启动失败页结构 | 修改 |
| 2 | `apps/desktop-launcher/frontend/app.js` | 状态渲染逻辑 + 防抖 + 失败页交互 | 修改 |
| 2 | `apps/desktop-launcher/frontend/styles.css` | 启动加载页、失败页样式 | 修改 |
| 3 | `apps/desktop-launcher/internal/app/app.go` | 启动失败自动触发 doctor，状态字段扩展 | 修改 |
| 3 | `apps/desktop-launcher/frontend/app.js` | 自动诊断状态感知 + 自动弹窗 | 修改 |

---

# Phase 1: Doctor 动态加载检查

## 前置知识

- doctor 包已存在于 `packages/support/doctor/`，检查项通过 `registerCheck()` 注册，按 category 分文件（env/config/plugin/data）
- 每个检查实现 `DoctorCheck` 接口：`check(dshHome)` 返回 `CheckResult`，可选 `fix(dshHome, backupDir)` 返回 `FixResult`
- `loadProfile()` 在 `@deepseek-ai/dsh-app-boot` 的 `profile.ts` 中，支持 `skipThirdPartyBundles` 和 `extraPatchFiles` 选项
- 已有 `bisectThirdPartyBundles()` 工具在 `bisect.ts`，但当前是静态分析版，需要改造为可注入自定义判定函数

---

### Task 1.1: 改造 bisect 工具为通用二分框架

**Files:**
- Modify: `packages/support/doctor/src/bisect.ts`
- Test: `packages/support/doctor/tests/bisect.spec.ts`（验证已有）

**目标：** 让 `bisectThirdPartyBundles` 接受一个自定义的判定函数，而不是硬编码静态检查逻辑。这样动态加载检查可以复用二分框架。

- [ ] **Step 1: 读 bisect.ts 确认当前签名**

  读取 `packages/support/doctor/src/bisect.ts`，确认当前函数签名和参数。

- [ ] **Step 2: 改造为通用二分函数**

  将 `bisectThirdPartyBundles()` 改为接受一个 `isBad(bundleNames: string[]): Promise<boolean>` 判定函数作为参数。保留原有静态检查逻辑作为默认或单独的包装函数。

  目标签名：
  ```ts
  export async function bisectBy<T>(
    items: T[],
    isBad: (subset: T[]) => Promise<boolean>,
  ): Promise<T | null>
  ```

  再保留 `bisectThirdPartyBundles` 作为便捷封装（调用 `bisectBy` + 静态检查判定）。

- [ ] **Step 3: 运行现有 bisect 测试确保不回归**

  运行：`pnpm test --filter @deepseek-ai/dsh-doctor -- bisect`
  预期：全部通过。

- [ ] **Step 4: 提交**

  ```bash
  git add packages/support/doctor/src/bisect.ts packages/support/doctor/tests/bisect.spec.ts
  git commit -m "refactor(doctor): extract generic bisectBy helper"
  ```

---

### Task 1.2: loader-probe 子进程探测脚本

**Files:**
- Create: `packages/support/doctor/src/loader-probe.ts`
- Test: `packages/support/doctor/tests/loader-probe.spec.ts`

**目标：** 一个可被 `node` 直接执行的脚本，加载指定 profile + 第三方 bundle 子集，通过退出码报告结果。

- [ ] **Step 1: 写失败测试**

  新建 `packages/support/doctor/tests/loader-probe.spec.ts`：

  ```ts
  import { describe, it, expect, beforeEach, afterEach } from 'vitest'
  import { mkdir, rm, writeFile } from 'node:fs/promises'
  import { join } from 'node:path'
  import { execFile } from 'node:child_process'
  import { promisify } from 'node:util'

  const exec = promisify(execFile)

  const tmpDir = join(process.cwd(), 'profiles', 'loader-probe-test')

  describe('loader-probe', () => {
    beforeEach(async () => {
      await rm(tmpDir, { recursive: true, force: true })
      await mkdir(join(tmpDir, 'profiles', 'web'), { recursive: true })
    })

    afterEach(async () => {
      await rm(tmpDir, { recursive: true, force: true })
    })

    it('exits 0 when profile loads successfully with no third-party bundles', async () => {
      // 基础 profile 能正常加载（没有第三方插件）
      const { exitCode } = await exec(
        process.execPath,
        [
          '--import', 'tsx/esm',
          join(__dirname, '../src/loader-probe.ts'),
          '--profile', 'web',
          '--dsh-home', tmpDir,
          '--timeout', '10000',
        ],
        { timeout: 15000 },
      ).catch((e) => ({ exitCode: e.code ?? 1, stdout: e.stdout, stderr: e.stderr }))
      expect(exitCode).toBe(0)
    }, 20000)

    it('exits non-zero when a bundle imports a missing module', async () => {
      // 创建一个故意 import 不存在模块的 fake bundle
      const fakeBundleDir = join(tmpDir, 'node_modules', 'fake-bad-bundle')
      await mkdir(fakeBundleDir, { recursive: true })
      await writeFile(
        join(fakeBundleDir, 'package.json'),
        JSON.stringify({
          name: 'fake-bad-bundle',
          type: 'module',
          main: 'index.js',
          cordis: {
            plugins: ['./index.js'],
          },
        }),
      )
      await writeFile(
        join(fakeBundleDir, 'index.js'),
        "import 'this-module-does-not-exist-xyz'\n",
      )

      const { exitCode, stderr } = await exec(
        process.execPath,
        [
          '--import', 'tsx/esm',
          join(__dirname, '../src/loader-probe.ts'),
          '--profile', 'web',
          '--dsh-home', tmpDir,
          '--include', 'fake-bad-bundle',
          '--timeout', '10000',
        ],
        { timeout: 15000 },
      ).catch((e) => ({ exitCode: e.code ?? 1, stdout: e.stdout || '', stderr: e.stderr || '' }))
      expect(exitCode).not.toBe(0)
      expect(stderr).toContain('this-module-does-not-exist-xyz')
    }, 20000)
  })
  ```

  注意：根据项目实际的 profile 加载方式调整测试细节。关键是验证成功和失败两种场景。

- [ ] **Step 2: 运行测试确认失败**

  运行：`pnpm test --filter @deepseek-ai/dsh-doctor -- loader-probe`
  预期：FAIL（loader-probe.ts 还不存在）

- [ ] **Step 3: 实现 loader-probe.ts**

  新建 `packages/support/doctor/src/loader-probe.ts`：

  ```ts
  #!/usr/bin/env node
  /**
   * Subprocess probe: loads a profile through Cordis Loader
   * and reports success/failure via exit code.
   *
   * Usage: node loader-probe.js --profile web --dsh-home /path --include bundle1 --include bundle2 --timeout 10000
   *
   * Exit codes:
   *   0 - load succeeded
   *   1 - load failed (error on stderr)
   *   2 - timed out
   *
   * @module
   */

  import { parseArgs } from 'node:util'
  import { loadProfile } from '@deepseek-ai/dsh-app-boot/profile.js'
  import { BOOT_PREFIXES } from '@deepseek-ai/dsh-app-boot/index.js'

  const { values, positionals } = parseArgs({
    options: {
      profile: { type: 'string', default: 'web' },
      'dsh-home': { type: 'string' },
      include: { type: 'string', multiple: true, default: [] },
      timeout: { type: 'string', default: '10000' },
    },
    strict: true,
  })

  const profileName = values.profile
  const dshHome = values['dsh-home'] || process.env.DSH_HOME
  const includes = values.include
  const timeoutMs = parseInt(values.timeout || '10000', 10)

  if (!dshHome) {
    console.error('loader-probe: --dsh-home or DSH_HOME is required')
    process.exit(1)
  }

  // 超时防护：如果加载卡住了，强制退出
  const timeoutId = setTimeout(() => {
    console.error('loader-probe: timed out after ' + timeoutMs + 'ms')
    process.exit(2)
  }, timeoutMs)
  timeoutId.unref?.()

  async function main() {
    try {
      // 加载 profile，跳过正常的第三方 bundle，只加载 --include 指定的
      const profile = await loadProfile({
        profile: profileName,
        dshHome,
        skipThirdPartyBundles: true,
        extraPatchFiles: includes.map((name) => ({
          name,
          // bundle 通过 node_modules 解析
          // 这里需要构造一个临时 patch 或者直接 include bundle
        })),
      })

      // 如果 profile 有 loader，真正 load 一下
      // 具体加载方式取决于 loadProfile 的返回结构
      // 需要先看一下实际的 loadProfile 返回什么

      clearTimeout(timeoutId)
      process.exit(0)
    } catch (err) {
      clearTimeout(timeoutId)
      const message = err instanceof Error ? err.stack || err.message : String(err)
      console.error('loader-probe: load failed')
      console.error(message)
      process.exit(1)
    }
  }

  void main()
  ```

  **注意：** 上面的实现是骨架。实际实现时需要先读 `packages/boot/app-boot/src/profile.ts` 确认 `loadProfile()` 的真实返回结构和参数，再填充正确的加载逻辑。

  关键点：
  - 用 `skipThirdPartyBundles: true` 跳过正常第三方 bundle
  - 用 `extraPatchFiles` 或类似机制注入 `--include` 指定的 bundle
  - 真正走 Cordis Loader compose + load 流程
  - 加载完后立即 dispose / 清理，不启动任何服务

- [ ] **Step 4: 运行测试看结果**

  运行：`pnpm test --filter @deepseek-ai/dsh-doctor -- loader-probe`
  预期：根据实际实现调整，最终全部通过。

- [ ] **Step 5: 提交**

  ```bash
  git add packages/support/doctor/src/loader-probe.ts packages/support/doctor/tests/loader-probe.spec.ts
  git commit -m "feat(doctor): add loader-probe subprocess for dynamic plugin load verification"
  ```

---

### Task 1.3: plugin-dynamic-load 检查项

**Files:**
- Modify: `packages/support/doctor/src/checks/plugins.ts`
- Test: `packages/support/doctor/tests/plugins-dynamic-load.spec.ts`（新建）

**目标：** 注册 `plugin-dynamic-load` 检查项：全量加载 → 二分定位 → 返回结果。

- [ ] **Step 1: 写失败测试**

  新建 `packages/support/doctor/tests/plugins-dynamic-load.spec.ts`：

  ```ts
  import { describe, it, expect, beforeEach, afterEach } from 'vitest'
  import { mkdir, rm, writeFile } from 'node:fs/promises'
  import { join } from 'node:path'
  import { runDiagnosis, _resetRegistry } from '@deepseek-ai/dsh-doctor'
  // 需要先注册检查项，或者从 checks/plugins 导入

  const tmpDir = join(process.cwd(), 'profiles', 'dynamic-load-test')

  describe('plugin-dynamic-load check', () => {
    beforeEach(async () => {
      _resetRegistry()
      await rm(tmpDir, { recursive: true, force: true })
      await mkdir(join(tmpDir, 'profiles', 'web'), { recursive: true })
      // 注册 plugin checks
      await import('../src/checks/plugins.js')
    })

    afterEach(async () => {
      await rm(tmpDir, { recursive: true, force: true })
    })

    it('passes when no third-party bundles are present', async () => {
      const report = await runDiagnosis(tmpDir)
      const check = report.checks.find((c) => c.id === 'plugin-dynamic-load')
      expect(check).toBeDefined()
      expect(check!.result.ok).toBe(true)
    }, 30000)

    it('fails and identifies the bad bundle when one third-party bundle is broken', async () => {
      // 创建一个坏 bundle
      const badBundleDir = join(tmpDir, 'node_modules', 'test-bad-bundle')
      await mkdir(badBundleDir, { recursive: true })
      await writeFile(
        join(badBundleDir, 'package.json'),
        JSON.stringify({
          name: 'test-bad-bundle',
          type: 'module',
          main: 'index.js',
          cordis: { plugins: ['./index.js'] },
        }),
      )
      await writeFile(
        join(badBundleDir, 'index.js'),
        "import 'definitely-nonexistent-module-abc'\n",
      )

      // 在用户 patch 中引入这个 bundle
      const patchPath = join(tmpDir, 'profiles', 'web', 'cordis.patch.yml')
      await writeFile(
        patchPath,
        'plugins:\n  - test-bad-bundle\n',
      )

      const report = await runDiagnosis(tmpDir)
      const check = report.checks.find((c) => c.id === 'plugin-dynamic-load')
      expect(check).toBeDefined()
      expect(check!.result.ok).toBe(false)
      expect(check!.result.message).toContain('test-bad-bundle')
      expect(check!.result.fixable).toBe(true)
      expect(check!.result.suggestedLevel).toBe(2)
    }, 60000)
  })
  ```

  注意：根据项目实际的 patch 格式和 bundle 引入方式调整。

- [ ] **Step 2: 运行测试确认失败**

  运行：`pnpm test --filter @deepseek-ai/dsh-doctor -- plugins-dynamic-load`
  预期：FAIL（检查项还没实现）

- [ ] **Step 3: 实现检查项**

  在 `packages/support/doctor/src/checks/plugins.ts` 中新增 `pluginDynamicLoadCheck`：

  ```ts
  // 加到现有 pluginChecks 数组里
  export const pluginDynamicLoadCheck: DoctorCheck = {
    id: 'plugin-dynamic-load',
    name: '插件运行时兼容性',
    category: 'plugin',
    severity: 'fatal',

    async check(dshHome) {
      // 1. 先获取第三方 bundle 列表
      const bundles = await listThirdPartyBundles(dshHome)
      if (bundles.length === 0) {
        return {
          ok: true,
          message: '未检测到第三方插件',
          fixable: false,
          suggestedLevel: 2,
        }
      }

      // 2. 全量加载测试
      const fullResult = await probeLoad(dshHome, bundles)
      if (fullResult.ok) {
        return {
          ok: true,
          message: `所有 ${bundles.length} 个第三方插件加载正常`,
          fixable: false,
          suggestedLevel: 2,
        }
      }

      // 3. 二分定位
      const culprit = await bisectBy(bundles, async (subset) => {
        const r = await probeLoad(dshHome, subset)
        return !r.ok
      })

      return {
        ok: false,
        message: culprit
          ? `插件 ${culprit} 导致启动失败`
          : '第三方插件导致启动失败，未能定位具体插件',
        detail: fullResult.error,
        fixable: !!culprit,
        suggestedLevel: 2,
      }
    },

    async fix(dshHome, backupDir) {
      // 先再跑一次检查确认状态 + 获取 culprit
      const result = await this.check(dshHome)
      if (result.ok) {
        return { ok: true, message: '插件加载已正常，无需修复' }
      }
      // 从 result 里提取 culprit 名称...
      // 备份 patch 文件
      // 注释掉对应 bundle 的引入行
      // 重新跑检查验证
      // 返回结果
    },
  }
  ```

  辅助函数：
  - `probeLoad(dshHome, bundles)` —— 起子进程跑 loader-probe，返回 `{ ok, error }`
  - `listThirdPartyBundles(dshHome)` —— 从 patch 文件解析第三方 bundle 列表（复用已有逻辑）

- [ ] **Step 4: 运行测试验证通过**

  运行：`pnpm test --filter @deepseek-ai/dsh-doctor -- plugins-dynamic-load`
  预期：全部通过。

- [ ] **Step 5: 运行全部 doctor 测试确保不回归**

  运行：`pnpm test --filter @deepseek-ai/dsh-doctor`
  预期：全部通过。

- [ ] **Step 6: 提交**

  ```bash
  git add packages/support/doctor/src/checks/plugins.ts packages/support/doctor/tests/plugins-dynamic-load.spec.ts
  git commit -m "feat(doctor): add plugin-dynamic-load check with bisection and L2 repair"
  ```

---

### Task 1.4: 修复逻辑实现（L2 禁用坏 bundle）

**Files:**
- Modify: `packages/support/doctor/src/checks/plugins.ts`
- Test: 已有测试文件中补充修复测试

**目标：** 实现 `fix()` 方法：备份 patch 文件 → 注释掉坏 bundle → 验证修复。

- [ ] **Step 1: 补充修复失败测试**

  在 `plugins-dynamic-load.spec.ts` 中添加：

  ```ts
  import { runRepair } from '@deepseek-ai/dsh-doctor'
  import { readFile } from 'node:fs/promises'

  it('L2 repair disables the bad bundle and restores loadability', async () => {
    // 准备：一个坏 bundle + 一个好 bundle
    // ... 构造测试环境

    // 先确认检查失败
    const preReport = await runDiagnosis(tmpDir)
    const preCheck = preReport.checks.find((c) => c.id === 'plugin-dynamic-load')
    expect(preCheck!.result.ok).toBe(false)

    // 执行 L2 修复
    const repairReport = await runRepair(2, tmpDir)
    const applied = repairReport.applied.find((a) => a.checkId === 'plugin-dynamic-load')
    expect(applied).toBeDefined()

    // 验证：patch 文件被修改了，坏 bundle 被注释掉
    const patchContent = await readFile(
      join(tmpDir, 'profiles', 'web', 'cordis.patch.yml'),
      'utf-8',
    )
    expect(patchContent).toContain('#') // 有注释

    // 验证：修复后检查通过
    const postReport = await runDiagnosis(tmpDir)
    const postCheck = postReport.checks.find((c) => c.id === 'plugin-dynamic-load')
    expect(postCheck!.result.ok).toBe(true)
  }, 60000)
  ```

- [ ] **Step 2: 实现 fix 方法**

  修复步骤（方案 A：编辑 profile manifest —— 第三方 bundle 是 profile 的 bundle 层，位于 `package.json` 的 `dsh.profile.bundles`，不在 `cordis.patch.yml` 里）：
  1. 调用共享的 `locateCulprit()` 定位 culprit bundle
  2. 备份 `profiles/web/package.json` 原始字节到 `backupDir/web-profile.package.json`（`writeFileAtomic`，字节级可逆）
  3. 用 `writeProfileManifest` 从 `dsh.profile.bundles` 移除 culprit（其余字段 spread 保留）
  4. 重新跑全量探测验证
  5. 验证失败 → 还原备份字节，返回失败；验证通过 → 返回成功

  与 `DSH_SAFE_MODE=plugins` 的跳过模型一致（同样排除非官方 bundle）。

- [ ] **Step 3: 运行测试验证**

  运行：`pnpm test --filter @deepseek-ai/dsh-doctor -- plugins-dynamic-load`
  预期：全部通过。

- [ ] **Step 4: 提交**

  ```bash
  git add packages/support/doctor/src/checks/plugins.ts packages/support/doctor/tests/plugins-dynamic-load.spec.ts
  git commit -m "feat(doctor): implement L2 repair for plugin-dynamic-load check"
  ```

---

### Task 1.5: Typecheck + 完整测试

- [ ] **Step 1: 运行类型检查**

  运行：`pnpm typecheck --filter @deepseek-ai/dsh-doctor`
  预期：无错误。

- [ ] **Step 2: 运行完整 doctor 测试**

  运行：`pnpm test --filter @deepseek-ai/dsh-doctor`
  预期：全部通过。

- [ ] **Step 3: 构建验证**

  运行：`pnpm build --filter @deepseek-ai/dsh-doctor`
  预期：构建成功。

---

# Phase 2: 启动中 UI 优化

## 前置知识

- 桌面启动器前端在 `apps/desktop-launcher/frontend/`，纯原生 HTML/CSS/JS，无框架
- 状态通过 `harness:status` 事件推送，前端在 `applyStatus(s)` 中渲染
- 当前主舞台逻辑：有 `s.Target` 显示 iframe，否则显示引导页
- 状态有 `starting` / `running` / `stopped` / `failed` 四种

---

### Task 2.1: 新增启动加载页和启动失败页结构

**Files:**
- Modify: `apps/desktop-launcher/frontend/index.html`

- [ ] **Step 1: 在 stage-card 里添加两个新 section**

  在 `#guidance` 之后、`</div>` 之前添加：

  ```html
  <section id="loading-page" class="loading-page hidden">
    <div class="loading-spinner"></div>
    <h2>正在启动...</h2>
    <p class="hint">DeepSeek Harness 正在加载插件和服务，请稍候</p>
  </section>

  <section id="failed-page" class="failed-page hidden">
    <div class="failed-icon">⚠️</div>
    <h2>启动失败</h2>
    <p id="failed-reason" class="failed-reason"></p>
    <div class="failed-actions">
      <button id="btn-failed-doctor" class="btn btn-primary">诊断问题</button>
      <button id="btn-failed-safe-mode" class="btn">以安全模式启动</button>
    </div>
    <p class="hint">完整日志: ~/.cache/dsh-desktop/harness.log</p>
  </section>
  ```

- [ ] **Step 2: 提交（结构部分）**

  ```bash
  git add apps/desktop-launcher/frontend/index.html
  git commit -m "feat(desktop): add startup loading and failure page structure"
  ```

---

### Task 2.2: 新增样式

**Files:**
- Modify: `apps/desktop-launcher/frontend/styles.css`

- [ ] **Step 1: 添加启动加载页样式**

  在 styles.css 末尾添加：

  ```css
  /* 启动加载页 */
  .loading-page {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    gap: 16px;
  }

  .loading-spinner {
    width: 40px;
    height: 40px;
    border: 3px solid var(--border-color);
    border-top-color: var(--accent-color);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .loading-page h2 {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
  }

  /* 启动失败页 */
  .failed-page {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    gap: 12px;
    text-align: center;
  }

  .failed-icon {
    font-size: 48px;
  }

  .failed-page h2 {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    color: var(--danger-color, #f48771);
  }

  .failed-reason {
    margin: 0;
    color: var(--text-secondary);
    font-size: 13px;
    max-width: 400px;
    word-break: break-all;
  }

  .failed-actions {
    display: flex;
    gap: 12px;
    margin-top: 8px;
  }
  ```

  注意：颜色变量需要和现有主题对齐，实际实现时参考 styles.css 里已有的变量定义。

- [ ] **Step 2: 提交（样式部分）**

  ```bash
  git add apps/desktop-launcher/frontend/styles.css
  git commit -m "feat(desktop): add startup loading and failure page styles"
  ```

---

### Task 2.3: 前端状态渲染逻辑调整 + 防抖

**Files:**
- Modify: `apps/desktop-launcher/frontend/app.js`

**目标：**
1. `starting` 状态显示启动加载页
2. `failed` 状态显示启动失败页
3. 只有手动停止的 `stopped` 才显示引导页
4. 重试期间短暂的 stopped 状态用防抖过滤，不闪烁

- [ ] **Step 1: 新增防抖状态变量**

  在 `state` 对象中新增：
  ```js
  const state = {
    status: null,
    prevConnectError: "",
    _stoppedTimer: null, // 防抖计时器
  };
  ```

- [ ] **Step 2: 重写 applyStatus 中的主舞台渲染逻辑**

  把原来的：
  ```js
  const frame = $("#harness");
  const guide = $("#guidance");
  if (s.Target) {
    if (frame.getAttribute("src") !== s.Target) frame.setAttribute("src", s.Target);
    frame.classList.remove("hidden");
    guide.classList.add("hidden");
  } else {
    frame.classList.add("hidden");
    frame.removeAttribute("src");
    guide.classList.remove("hidden");
  }
  ```

  替换为：
  ```js
  const frame = $("#harness");
  const guide = $("#guidance");
  const loadingPage = $("#loading-page");
  const failedPage = $("#failed-page");

  // 隐藏所有页面
  frame.classList.add("hidden");
  guide.classList.add("hidden");
  loadingPage.classList.add("hidden");
  failedPage.classList.add("hidden");

  // 外部模式或运行中 → iframe
  if (s.Target) {
    if (frame.getAttribute("src") !== s.Target) frame.setAttribute("src", s.Target);
    frame.classList.remove("hidden");
    clearTimeout(state._stoppedTimer);
    state._stoppedTimer = null;
  }
  // starting → 加载页
  else if (s.State === "starting") {
    loadingPage.classList.remove("hidden");
    // 更新加载页的安全模式提示
    const loadingHint = loadingPage.querySelector(".hint");
    if (loadingHint) {
      loadingHint.textContent = s.SafeMode
        ? "安全模式下启动，已跳过第三方插件"
        : "DeepSeek Harness 正在加载插件和服务，请稍候";
    }
    clearTimeout(state._stoppedTimer);
    state._stoppedTimer = null;
  }
  // failed → 失败页
  else if (s.State === "failed") {
    failedPage.classList.remove("hidden");
    $("#failed-reason").textContent = s.LastExit || "未知错误";
    clearTimeout(state._stoppedTimer);
    state._stoppedTimer = null;
  }
  // stopped → 引导页（带 1s 防抖，避免重试期间闪烁）
  else {
    if (!state._stoppedTimer) {
      state._stoppedTimer = setTimeout(() => {
        if (state.status && state.status.State === "stopped") {
          guide.classList.remove("hidden");
        }
        state._stoppedTimer = null;
      }, 1000);
    }
    // 1 秒内不改变显示，由计时器决定
    // 如果当前显示的是 loading 或 failed，保持显示直到防抖超时
  }
  ```

- [ ] **Step 3: 绑定失败页按钮事件**

  在 `bindUI()` 函数中添加：
  ```js
  $("#btn-failed-doctor").addEventListener("click", () => {
    openModal("doctor-modal");
    runDoctor();
  });

  $("#btn-failed-safe-mode").addEventListener("click", async () => {
    applyStatus(await api().StartSafeMode());
  });
  ```

  注意：`runDoctor()` 当前是 init 内的局部函数，需要调整作用域或重新组织代码。最简单的方式是把失败页按钮的绑定放在 init 里。

- [ ] **Step 4: 验证（浏览器预览）**

  打开 `apps/desktop-launcher/frontend/index.html` 确认页面结构正常，没有 JS 报错。

- [ ] **Step 5: 提交**

  ```bash
  git add apps/desktop-launcher/frontend/app.js
  git commit -m "feat(desktop): startup loading/failure pages with debounce"
  ```

---

# Phase 3: 启动失败自动诊断

## 前置知识

- `App` 在 `app.go` 中，状态通过 `tick()` 每秒推送一次 `harness:status` 事件
- `RunDoctor()` 已经实现，通过 `exec.Command` 跑 `dsh doctor --json`
- supervisor 进入 `StateFailed` 时停止重试，状态变为 failed
- 前端通过 `runtime.EventsOn("harness:status", callback)` 监听状态变化

---

### Task 3.1: Go 端 - 启动失败自动触发 doctor

**Files:**
- Modify: `apps/desktop-launcher/internal/app/app.go`

**目标：** 当 supervisor 进入 `StateFailed` 状态且为容器模式时，后台自动运行 doctor 诊断，结果通过状态事件推送。

- [ ] **Step 1: App 结构体新增字段**

  在 `App` 结构体中新增：
  ```go
  startupDoctorResult   *DoctorReport // 自动诊断结果缓存
  startupDoctorRunning  bool          // 是否正在进行自动诊断
  startupDoctorDoneOnce bool          // 本次失败周期是否已触发过诊断
  ```

- [ ] **Step 2: 状态快照新增字段**

  在 `FrontendStatus` 中新增：
  ```go
  StartupDiagnosing  bool   // 是否正在进行启动失败自动诊断
  StartupDoctorReady bool   // 自动诊断结果是否已就绪
  ```

  在 `snapshot()` 中填充这两个字段。

- [ ] **Step 3: 状态变化检测 + 自动触发**

  在 `tick()` 或 `emitStatus()` 之前检测状态变化：当状态从非 failed 变为 failed 且容器模式且未诊断过时，触发后台诊断。

  触发逻辑：
  ```go
  func (a *App) maybeStartStartupDoctor(prevState, newState domain.HarnessState) {
    if newState == domain.StateFailed && prevState != domain.StateFailed {
      if a.startupDoctorDoneOnce {
        return
      }
      a.startupDoctorDoneOnce = true
      a.startupDoctorRunning = true
      a.emitStatus()
      go func() {
        result := a.RunDoctor()
        a.startupDoctorRunning = false
        a.startupDoctorResult = &result
        a.emitStatus()
      }()
    }
    // 如果从 failed 变回其他状态，重置标记
    if newState != domain.StateFailed && prevState == domain.StateFailed {
      a.startupDoctorDoneOnce = false
      a.startupDoctorResult = nil
    }
  }
  ```

  注意：需要保存上一次的状态来做边沿检测。可以在 `App` 里加一个 `prevState` 字段，或者在 `tick()` 中比较。

- [ ] **Step 4: 编译验证**

  运行：`cd apps/desktop-launcher && go build ./...`
  预期：编译成功。

- [ ] **Step 5: 提交**

  ```bash
  git add apps/desktop-launcher/internal/app/app.go
  git commit -m "feat(desktop): auto-run doctor on startup failure"
  ```

---

### Task 3.2: 前端 - 自动诊断感知 + 自动弹窗

**Files:**
- Modify: `apps/desktop-launcher/frontend/app.js`

**目标：** 前端检测到自动诊断就绪时，自动弹出 doctor 弹窗并显示结果。

- [ ] **Step 1: applyStatus 中检测自动诊断状态**

  在 `applyStatus()` 中添加逻辑：

  ```js
  // 启动失败自动诊断：结果就绪时自动弹窗
  if (s.State === "failed" && s.StartupDoctorReady) {
    if (!state._startupDoctorShown) {
      state._startupDoctorShown = true;
      // 打开 doctor 弹窗并拉取结果
      openModal("doctor-modal");
      runDoctor(); // 或者直接用缓存的结果（如果有 API 的话）
      // 在摘要区加提示条
      setTimeout(() => {
        const summary = $("#doctor-summary");
        if (summary && !summary.textContent.includes("自动诊断")) {
          summary.innerHTML =
            '<div class="doctor-auto-hint">检测到启动失败，已为你自动诊断</div>' +
            summary.innerHTML;
        }
      }, 100);
    }
  }
  // 离开 failed 状态时重置标记
  if (s.State !== "failed") {
    state._startupDoctorShown = false;
  }
  ```

  注意：更好的方式是 Go 端把诊断结果存在状态里，前端直接用。但因为 doctor 结果数据量大，走状态事件可能太重。可以加一个单独的方法 `GetStartupDoctorReport()`，或者复用 `RunDoctor()`（结果已经缓存了也不会慢多少）。

  实现时选择最简单可行的方案。

- [ ] **Step 2: 启动失败页增加自动诊断状态**

  在失败页显示"正在诊断..."或"诊断完成，查看结果"的状态提示。

- [ ] **Step 3: 验证（浏览器预览）**

  打开 index.html 确认无 JS 报错。

- [ ] **Step 4: 提交**

  ```bash
  git add apps/desktop-launcher/frontend/app.js
  git commit -m "feat(desktop): auto-open doctor on startup failure diagnosis"
  ```

---

### Task 3.3: 玲珑构建验证

- [ ] **Step 1: 运行玲珑构建脚本**

  运行：`./build-linglong.sh`
  预期：构建成功（TS + Go 都编译通过）。

- [ ] **Step 2: 如有错误，修复后重新构建**

- [ ] **Step 3: 提交（如有修复）**

---

## 收尾

### Task Final: 最终检查 + 提交

- [ ] **Step 1: 运行 doctor 测试**
  `pnpm test --filter @deepseek-ai/dsh-doctor`

- [ ] **Step 2: 运行类型检查**
  `pnpm typecheck --filter @deepseek-ai/dsh-doctor`

- [ ] **Step 3: Go 编译检查**
  `cd apps/desktop-launcher && go build ./...`

- [ ] **Step 4: 最终提交（如有未提交内容）**

  ```bash
  git add -A
  git commit -m "feat: plugin dynamic load check + startup auto-diagnosis + UI polish"
  ```

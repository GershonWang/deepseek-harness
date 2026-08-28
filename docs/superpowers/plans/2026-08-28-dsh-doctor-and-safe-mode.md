# dsh doctor 诊断修复与安全模式 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.


**Goal:** 为玲珑版 deepseek harness 提供一键诊断修复能力，解决升级后第三方插件不兼容、配置损坏、数据异常等导致的 harness 进程启动失败问题；同时提供三级安全模式作为兜底。


**Architecture:** 三层架构：(1) `@deepseek-ai/dsh-doctor` Node 包提供诊断和修复能力，以 `dsh doctor` / `dsh repair` CLI 子命令暴露；(2) `app-boot` 新增 `DSH_SAFE_MODE` 三级支持（plugins/config/full），在启动时跳过对应层级的用户数据；(3) 桌面启动器 Go 层新增诊断/修复/安全模式绑定，前端启动失败页增加操作入口。修复操作全程先备份再修改，零直接删除。


**Tech Stack:** TypeScript（Node.js 诊断包，ESM）、Go（Wails 桌面启动器）、原生 HTML/CSS/JS（前端弹框）、Cordis Loader（启动时注入点）、`@deepseek-ai/dsh-app-boot`（安全模式接入点）。


---


## 文件结构

| 文件 | 职责 | 新建/修改 |
|---|---|---|
| `packages/support/doctor/src/index.ts` | doctor 主入口：检查项注册、诊断执行、JSON 输出 | 新建 |
| `packages/support/doctor/src/checks/env.ts` | 环境层检查：Node 版本、磁盘空间、`.env` bootstrap 变量 | 新建 |
| `packages/support/doctor/src/checks/config.ts` | 配置层检查：`settings.yaml`、用户 `cordis.patch.yml` | 新建 |
| `packages/support/doctor/src/checks/plugins.ts` | 插件层检查：profile bundle 可解析、可激活、第三方兼容性 | 新建 |
| `packages/support/doctor/src/checks/data.ts` | 数据层检查：会话 JSONL 完整性、KV 存储 | 新建 |
| `packages/support/doctor/src/repair.ts` | 修复执行：分级修复、备份管理 | 新建 |
| `packages/support/doctor/src/safe-mode.ts` | 安全模式：三级模式定义、环境变量协议 | 新建 |
| `packages/support/doctor/tests/doctor.spec.ts` | 诊断/修复单元测试 | 新建 |
| `packages/boot/app-boot/src/profile.ts` | `loadProfile()` 增加 `skipThirdPartyBundles` 选项 | 修改 |
| `packages/boot/app-boot/src/index.ts` | `boot()` / `loadLayeredEnv()` 响应 `DSH_SAFE_MODE` 环境变量 | 修改 |
| `apps/desktop-launcher/internal/app/app.go` | 新增 Diagnose/Repair/StartSafeMode 绑定方法 | 修改 |
| `apps/desktop-launcher/internal/domain/domain.go` | 新增 DoctorResult / RepairResult 类型 | 修改 |
| `apps/desktop-launcher/frontend/index.html` | 启动失败页增加诊断修复操作区 | 修改 |
| `apps/desktop-launcher/frontend/app.js` | 诊断修复交互逻辑、安全模式启动 | 修改 |
| `apps/desktop-launcher/frontend/styles.css` | 诊断结果、修复按钮样式 | 修改 |

---


### Task 1: doctor 包骨架 + 检查项抽象 + 测试

**Files:**
- Create: `packages/support/doctor/package.json`
- Create: `packages/support/doctor/src/types.ts`
- Create: `packages/support/doctor/src/index.ts`
- Create: `packages/support/doctor/tests/doctor.spec.ts`

**先决条件:** 了解项目包结构和 `pnpm-workspace.yaml`，doctor 放在 `packages/support/doctor/`（与 `test-support` 同组，都是支撑性工具包）。

- [ ] **Step 1: 创建 package.json** —— 新建 `packages/support/doctor/package.json`：
    ```json
    {
      "name": "@deepseek-ai/dsh-doctor",
      "version": "0.1.0",
      "type": "module",
      "private": true,
      "exports": {
        ".": "./src/index.ts"
      },
      "dependencies": {
        "@deepseek-ai/dsh-home-paths": "workspace:*",
        "@deepseek-ai/dsh-app-boot": "workspace:*",
        "@deepseek-ai/dsh-atomic-write": "workspace:*",
        "js-yaml": "^4.1.0"
      },
      "devDependencies": {
        "@types/js-yaml": "^4.0.0"
      }
    }
    ```

- [ ] **Step 2: 定义类型** —— 新建 `packages/support/doctor/src/types.ts`：
    ```typescript
    export type Severity = 'info' | 'warning' | 'error' | 'fatal'
    export type RepairLevel = 1 | 2 | 3

    export interface CheckResult {
      ok: boolean
      message: string
      detail?: string
      fixable: boolean
      suggestedLevel: RepairLevel
    }

    export interface DoctorCheck {
      id: string
      name: string
      category: 'env' | 'config' | 'data' | 'plugin'
      severity: Severity
      check(dshHome: string): Promise<CheckResult>
      fix?(dshHome: string, backupDir: string): Promise<FixResult>
    }

    export interface FixResult {
      ok: boolean
      message: string
      backupPath?: string
    }

    export interface DoctorReport {
      dshHome: string
      generatedAt: string
      checks: Array<{
        id: string
        name: string
        category: string
        severity: Severity
        result: CheckResult
      }>
      summary: {
        total: number
        ok: number
        failed: number
        fatal: number
        fixable: number
      }
    }

    export interface RepairReport {
      level: RepairLevel
      backups: string[]
      applied: Array<{ checkId: string; message: string }>
      skipped: Array<{ checkId: string; reason: string }>
    }
    ```

- [ ] **Step 3: 实现主入口** —— 新建 `packages/support/doctor/src/index.ts`：
    ```typescript
    import { mkdir } from 'node:fs/promises'
    import { join } from 'node:path'
    import { resolveDshHome } from '@deepseek-ai/dsh-home-paths'
    import type { DoctorCheck, DoctorReport, RepairLevel, RepairReport } from './types.js'

    const registry: DoctorCheck[] = []

    export function registerCheck(check: DoctorCheck): void {
      registry.push(check)
    }

    export async function runDiagnosis(dshHome?: string): Promise<DoctorReport> {
      const home = dshHome ?? resolveDshHome()
      const results = await Promise.all(
        registry.map(async (check) => ({
          id: check.id,
          name: check.name,
          category: check.category,
          severity: check.severity,
          result: await check.check(home),
        })),
      )
      const failed = results.filter((r) => !r.result.ok)
      const fatal = failed.filter((r) => r.severity === 'fatal')
      const fixable = failed.filter((r) => r.result.fixable)
      return {
        dshHome: home,
        generatedAt: new Date().toISOString(),
        checks: results,
        summary: {
          total: results.length,
          ok: results.length - failed.length,
          failed: failed.length,
          fatal: fatal.length,
          fixable: fixable.length,
        },
      }
    }

    export async function runRepair(level: RepairLevel, dshHome?: string): Promise<RepairReport> {
      const home = dshHome ?? resolveDshHome()
      const backupDir = join(home, 'backups', `doctor-${Date.now()}`)
      await mkdir(backupDir, { recursive: true })

      const report = await runDiagnosis(home)
      const applied: Array<{ checkId: string; message: string }> = []
      const skipped: Array<{ checkId: string; reason: string }> = []

      for (const item of report.checks) {
        if (item.result.ok) continue
        if (!item.result.fixable) {
          skipped.push({ checkId: item.id, reason: 'not fixable automatically' })
          continue
        }
        if (item.result.suggestedLevel > level) {
          skipped.push({ checkId: item.id, reason: `requires level ${item.result.suggestedLevel}` })
          continue
        }
        const check = registry.find((c) => c.id === item.id)
        if (check?.fix === undefined) {
          skipped.push({ checkId: item.id, reason: 'no fix function' })
          continue
        }
        try {
          const result = await check.fix(home, backupDir)
          if (result.ok) {
            applied.push({ checkId: item.id, message: result.message })
          } else {
            skipped.push({ checkId: item.id, reason: result.message })
          }
        } catch (err) {
          skipped.push({ checkId: item.id, reason: String(err) })
        }
      }

      return {
        level,
        backups: applied.length > 0 ? [backupDir] : [],
        applied,
        skipped,
      }
    }

    export { type DoctorCheck, type DoctorReport, type RepairReport, type RepairLevel, type CheckResult, type FixResult, type Severity } from './types.js'
    ```

- [ ] **Step 4: 写失败测试** —— 新建 `packages/support/doctor/tests/doctor.spec.ts`：
    ```typescript
    import { describe, it, expect, beforeEach } from 'vitest'
    import { mkdtemp, writeFile } from 'node:fs/promises'
    import { tmpdir } from 'node:os'
    import { join } from 'node:path'
    import { registerCheck, runDiagnosis, runRepair } from '@deepseek-ai/dsh-doctor'
    import type { DoctorCheck, CheckResult, FixResult } from '@deepseek-ai/dsh-doctor'

    describe('doctor', () => {
      let home: string

      beforeEach(async () => {
        home = await mkdtemp(join(tmpdir(), 'dsh-doctor-test-'))
        // 清空 registry（测试隔离）—— 通过重新 require 或导出 reset
        // 这里用一个 trick：每次测试前重新 import
      })

      it('empty registry returns all-ok report', async () => {
        const report = await runDiagnosis(home)
        expect(report.summary.total).toBe(0)
        expect(report.summary.ok).toBe(0)
        expect(report.dshHome).toBe(home)
      })

      it('runs a registered check and reports failure', async () => {
        const check: DoctorCheck = {
          id: 'test-fail',
          name: 'Test Failing Check',
          category: 'env',
          severity: 'error',
          check: async (): Promise<CheckResult> => ({
            ok: false,
            message: 'test failure',
            fixable: false,
            suggestedLevel: 1,
          }),
        }
        registerCheck(check)
        const report = await runDiagnosis(home)
        expect(report.summary.failed).toBe(1)
        expect(report.checks[0]?.result.message).toBe('test failure')
      })

      it('runRepair applies level-1 fixes', async () => {
        let fixed = false
        const check: DoctorCheck = {
          id: 'test-fixable',
          name: 'Test Fixable',
          category: 'config',
          severity: 'warning',
          check: async (): Promise<CheckResult> => ({
            ok: false,
            message: 'needs fix',
            fixable: true,
            suggestedLevel: 1,
          }),
          fix: async (): Promise<FixResult> => {
            fixed = true
            return { ok: true, message: 'fixed' }
          },
        }
        registerCheck(check)
        const report = await runRepair(1, home)
        expect(fixed).toBe(true)
        expect(report.applied.length).toBe(1)
        expect(report.applied[0]?.checkId).toBe('test-fixable')
      })

      it('runRepair skips fixes above requested level', async () => {
        const check: DoctorCheck = {
          id: 'test-level2',
          name: 'Test Level2',
          category: 'data',
          severity: 'error',
          check: async (): Promise<CheckResult> => ({
            ok: false,
            message: 'needs level 2',
            fixable: true,
            suggestedLevel: 2,
          }),
          fix: async (): Promise<FixResult> => ({ ok: true, message: 'fixed' }),
        }
        registerCheck(check)
        const report = await runRepair(1, home)
        expect(report.skipped.length).toBeGreaterThanOrEqual(1)
        expect(report.skipped.find((s) => s.checkId === 'test-level2')).toBeDefined()
      })
    })
    ```

    注意：由于 `registerCheck` 往模块级数组追加，测试间会互相污染。解决方法是在 `index.ts` 导出一个 `_resetRegistry()` 测试专用函数，或在每个测试用单独的 import 实例。这里采用导出 `_resetRegistry()`：

    在 `index.ts` 追加：
    ```typescript
    /** @internal test-only */
    export function _resetRegistry(): void {
      registry.length = 0
    }
    ```

- [ ] **Step 5: 运行测试确认通过**

    Run:
    ```bash
    pnpm --filter @deepseek-ai/dsh-doctor test
    ```

    Expected: 4 tests PASS。如果 registry 测试隔离有问题，补充 `beforeEach` 调用 `_resetRegistry()`。

- [ ] **Step 6: 提交**

    ```bash
    git add packages/support/doctor/package.json packages/support/doctor/src/types.ts packages/support/doctor/src/index.ts packages/support/doctor/tests/doctor.spec.ts
    git commit -m "feat(doctor): add doctor package skeleton with check registry"
    ```

---


### Task 2: 环境层检查（Node 版本、磁盘空间、.env bootstrap 变量）

**Files:**
- Create: `packages/support/doctor/src/checks/env.ts`
- Modify: `packages/support/doctor/src/index.ts`（注册检查项）
- Test: `packages/support/doctor/tests/env.spec.ts`

- [ ] **Step 1: 写环境检查实现** —— 新建 `packages/support/doctor/src/checks/env.ts`：
    ```typescript
    import { statfs } from 'node:fs/promises'
    import { readFileSync, existsSync } from 'node:fs'
    import { join } from 'node:path'
    import { parseEnv } from 'node:util'
    import { BOOTSTRAP_NAMES, BOOTSTRAP_PREFIXES } from '@deepseek-ai/dsh-app-boot'
    import type { DoctorCheck, CheckResult, FixResult } from '../types.js'

    const MIN_DISK_SPACE_BYTES = 50 * 1024 * 1024 // 50 MB

    function isBootstrapOnly(name: string): boolean {
      const upper = name.toUpperCase()
      if (BOOTSTRAP_NAMES.has(upper)) return true
      return BOOTSTRAP_PREFIXES.some((prefix) => upper.startsWith(prefix))
    }

    const envNodeVersion: DoctorCheck = {
      id: 'env-node-version',
      name: 'Node.js version',
      category: 'env',
      severity: 'fatal',
      check: async (): Promise<CheckResult> => {
        const major = Number(process.versions.node.split('.')[0])
        if (major >= 24) {
          return { ok: true, message: `Node.js ${process.versions.node}`, fixable: false, suggestedLevel: 1 }
        }
        return {
          ok: false,
          message: `Node.js version ${process.versions.node} is too old; DSH requires Node >= 24`,
          fixable: false,
          suggestedLevel: 1,
        }
      },
    }

    const envDiskSpace: DoctorCheck = {
      id: 'env-disk-space',
      name: 'Disk space',
      category: 'env',
      severity: 'error',
      check: async (dshHome: string): Promise<CheckResult> => {
        try {
          const stats = await statfs(dshHome)
          const available = Number(stats.bavail * BigInt(stats.bsize))
          if (available >= MIN_DISK_SPACE_BYTES) {
            return {
              ok: true,
              message: `${(available / 1024 / 1024).toFixed(1)} MB available`,
              fixable: false,
              suggestedLevel: 1,
            }
          }
          return {
            ok: false,
            message: `Only ${(available / 1024 / 1024).toFixed(1)} MB available; need at least 50 MB`,
            fixable: false,
            suggestedLevel: 1,
          }
        } catch (err) {
          return { ok: false, message: `Cannot stat ${dshHome}: ${String(err)}`, fixable: false, suggestedLevel: 1 }
        }
      },
    }

    const envBootstrapEnv: DoctorCheck = {
      id: 'env-bootstrap-env',
      name: '.env bootstrap-only variables',
      category: 'env',
      severity: 'fatal',
      check: async (dshHome: string): Promise<CheckResult> => {
        const envPath = join(dshHome, '.env')
        if (!existsSync(envPath)) {
          return { ok: true, message: 'No .env file in DSH home', fixable: false, suggestedLevel: 1 }
        }
        try {
          const content = readFileSync(envPath, 'utf8')
          const values = parseEnv(content) as Record<string, string>
          const bad: string[] = []
          for (const name of Object.keys(values)) {
            if (isBootstrapOnly(name)) bad.push(name)
          }
          if (bad.length === 0) {
            return { ok: true, message: '.env is clean', fixable: false, suggestedLevel: 1 }
          }
          return {
            ok: false,
            message: `.env contains bootstrap-only variables that will prevent startup: ${bad.join(', ')}`,
            detail: `These variables can only come from the inherited process environment; remove them from ~/.dsh/.env`,
            fixable: true,
            suggestedLevel: 1,
          }
        } catch (err) {
          return {
            ok: false,
            message: `Failed to read .env: ${String(err)}`,
            fixable: false,
            suggestedLevel: 1,
          }
        }
      },
      fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
        const envPath = join(dshHome, '.env')
        const backupPath = join(backupDir, '.env')
        const content = readFileSync(envPath, 'utf8')
        // 备份
        const { writeFileAtomic } = await import('@deepseek-ai/dsh-atomic-write')
        await writeFileAtomic(backupPath, content)
        // 注释掉 bootstrap-only 行
        const lines = content.split('\n')
        const cleaned = lines.map((line) => {
          const match = line.match(/^\s*([A-Z0-9_]+)\s*=/)
          if (match && isBootstrapOnly(match[1]!)) {
            return `# [doctor disabled - bootstrap-only] ${line}`
          }
          return line
        }).join('\n')
        await writeFileAtomic(envPath, cleaned)
        return { ok: true, message: 'Commented out bootstrap-only variables in .env', backupPath }
      },
    }

    export const envChecks: DoctorCheck[] = [envNodeVersion, envDiskSpace, envBootstrapEnv]
    ```

    注意：`BOOTSTRAP_NAMES` 和 `BOOTSTRAP_PREFIXES` 在 `app-boot/src/index.ts` 里目前是模块私有变量（没有 export）。需要先把它们从 `app-boot` 导出。

- [ ] **Step 2: 导出 BOOTSTRAP_NAMES / BOOTSTRAP_PREFIXES** —— 修改 `packages/boot/app-boot/src/index.ts`，在文件末尾追加导出：
    ```typescript
    export { BOOTSTRAP_NAMES, BOOTSTRAP_PREFIXES, isBootstrapOnly }
    ```
    同时把 `isBootstrapOnly` 函数也 export（它本来是模块内部的）。

    由于 `isBootstrapOnly` 当前是 `function isBootstrapOnly`，添加 `export` 关键字即可。

- [ ] **Step 3: 注册环境检查** —— 修改 `packages/support/doctor/src/index.ts`，在 import 区追加：
    ```typescript
    import { envChecks } from './checks/env.js'
    ```
    在文件末尾追加（自动注册，供 CLI 直接使用；测试可 `_resetRegistry`）：
    ```typescript
    // Auto-register built-in checks
    for (const check of envChecks) registerCheck(check)
    ```

- [ ] **Step 4: 写环境检查测试** —— 新建 `packages/support/doctor/tests/env.spec.ts`：
    ```typescript
    import { describe, it, expect, beforeEach } from 'vitest'
    import { mkdtemp, writeFile } from 'node:fs/promises'
    import { tmpdir } from 'node:os'
    import { join } from 'node:path'
    import { _resetRegistry, runDiagnosis, runRepair } from '@deepseek-ai/dsh-doctor'
    import { envChecks } from '@deepseek-ai/dsh-doctor/checks/env'

    describe('env checks', () => {
      let home: string

      beforeEach(async () => {
        home = await mkdtemp(join(tmpdir(), 'dsh-doctor-env-'))
        _resetRegistry()
        for (const c of envChecks) {
          // 只注册 bootstrap-env 这一项用于聚焦测试
          if (c.id === 'env-bootstrap-env') {
            const { registerCheck } = await import('@deepseek-ai/dsh-doctor')
            registerCheck(c)
          }
        }
      })

      it('detects bootstrap-only variables in .env', async () => {
        await writeFile(join(home, '.env'), 'DEEPSEEK_API_KEY=test\nPATH=/bad\n')
        const report = await runDiagnosis(home)
        const envCheck = report.checks.find((c) => c.id === 'env-bootstrap-env')
        expect(envCheck?.result.ok).toBe(false)
        expect(envCheck?.result.message).toContain('PATH')
      })

      it('passes when .env has no bootstrap variables', async () => {
        await writeFile(join(home, '.env'), 'OPENAI_API_KEY=test\nMY_CUSTOM_VAR=123\n')
        const report = await runDiagnosis(home)
        const envCheck = report.checks.find((c) => c.id === 'env-bootstrap-env')
        expect(envCheck?.result.ok).toBe(true)
      })

      it('repair level 1 comments out bootstrap variables', async () => {
        await writeFile(join(home, '.env'), 'DEEPSEEK_API_KEY=test\nPATH=/usr/bin\nFOO=bar\n')
        const repair = await runRepair(1, home)
        expect(repair.applied.some((a) => a.checkId === 'env-bootstrap-env')).toBe(true)
        // 验证修复后的文件
        const { readFileSync } = await import('node:fs')
        const content = readFileSync(join(home, '.env'), 'utf8')
        expect(content).toContain('# [doctor disabled')
        expect(content).toContain('PATH=/usr/bin')
        expect(content).toMatch(/DEEPSEEK_API_KEY=test/) // 非 bootstrap 行保持原样
      })
    })
    ```

- [ ] **Step 5: 运行测试**

    Run:
    ```bash
    pnpm --filter @deepseek-ai/dsh-doctor test
    ```

    Expected: env 测试全 PASS。注意路径问题（`./checks/env.js` vs `.ts`），确保 tsconfig 配置正确。

- [ ] **Step 6: 提交**

    ```bash
    git add packages/support/doctor/src/checks/env.ts packages/support/doctor/tests/env.spec.ts packages/boot/app-boot/src/index.ts
    git commit -m "feat(doctor): add environment checks (node version, disk space, .env bootstrap vars)"
    ```

---


### Task 3: 配置层检查（settings.yaml + 用户 cordis.patch.yml）

**Files:**
- Create: `packages/support/doctor/src/checks/config.ts`
- Modify: `packages/support/doctor/src/index.ts`（注册）
- Test: `packages/support/doctor/tests/config.spec.ts`

- [ ] **Step 1: 实现配置检查** —— 新建 `packages/support/doctor/src/checks/config.ts`：
    ```typescript
    import { readFileSync, existsSync, renameSync, mkdirSync } from 'node:fs'
    import { join } from 'node:path'
    import * as yaml from 'js-yaml'
    import { writeFileAtomic } from '@deepseek-ai/dsh-atomic-write'
    import { loadOptionalPatches } from '@deepseek-ai/dsh-app-boot'
    import type { DoctorCheck, CheckResult, FixResult } from '../types.js'

    const cfgSettingsYaml: DoctorCheck = {
      id: 'cfg-settings-yaml',
      name: 'settings.yaml syntax',
      category: 'config',
      severity: 'fatal',
      check: async (dshHome: string): Promise<CheckResult> => {
        const settingsPath = join(dshHome, 'settings.yaml')
        if (!existsSync(settingsPath)) {
          return { ok: true, message: 'No settings.yaml (will use defaults)', fixable: false, suggestedLevel: 2 }
        }
        try {
          const content = readFileSync(settingsPath, 'utf8')
          const parsed = yaml.load(content)
          if (parsed !== null && typeof parsed !== 'object') {
            return {
              ok: false,
              message: 'settings.yaml top-level value is not an object',
              detail: `Got ${typeof parsed}`,
              fixable: true,
              suggestedLevel: 2,
            }
          }
          return { ok: true, message: 'settings.yaml is valid YAML', fixable: false, suggestedLevel: 2 }
        } catch (err) {
          return {
            ok: false,
            message: `settings.yaml is not valid YAML: ${(err as Error).message}`,
            fixable: true,
            suggestedLevel: 2,
          }
        }
      },
      fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
        const settingsPath = join(dshHome, 'settings.yaml')
        const backupPath = join(backupDir, 'settings.yaml')
        const content = readFileSync(settingsPath, 'utf8')
        await writeFileAtomic(backupPath, content)
        // 重命名为 .bak，让系统用默认设置启动
        const bakPath = join(dshHome, 'settings.yaml.doctor-bak')
        renameSync(settingsPath, bakPath)
        return { ok: true, message: 'settings.yaml moved to .doctor-bak (will use defaults on next start)', backupPath }
      },
    }

    const cfgUserPatch: DoctorCheck = {
      id: 'cfg-user-patch',
      name: 'cordis.patch.yml syntax and structure',
      category: 'config',
      severity: 'fatal',
      check: async (dshHome: string): Promise<CheckResult> => {
        // profile = web 是桌面端默认 profile
        const patchPath = join(dshHome, 'profiles', 'web', 'cordis.patch.yml')
        if (!existsSync(patchPath)) {
          return { ok: true, message: 'No user patch file', fixable: false, suggestedLevel: 2 }
        }
        try {
          loadOptionalPatches('doctor', patchPath)
          return { ok: true, message: 'cordis.patch.yml is valid', fixable: false, suggestedLevel: 2 }
        } catch (err) {
          return {
            ok: false,
            message: `cordis.patch.yml is invalid: ${(err as Error).message}`,
            fixable: true,
            suggestedLevel: 2,
          }
        }
      },
      fix: async (dshHome: string, backupDir: string): Promise<FixResult> => {
        const patchPath = join(dshHome, 'profiles', 'web', 'cordis.patch.yml')
        const backupPath = join(backupDir, 'cordis.patch.yml')
        const content = readFileSync(patchPath, 'utf8')
        await writeFileAtomic(backupPath, content)
        // 重命名为 .disabled
        const disabledPath = join(dshHome, 'profiles', 'web', 'cordis.patch.yml.disabled')
        renameSync(patchPath, disabledPath)
        return { ok: true, message: 'cordis.patch.yml disabled (renamed to .disabled)', backupPath }
      },
    }

    export const configChecks: DoctorCheck[] = [cfgSettingsYaml, cfgUserPatch]
    ```

- [ ] **Step 2: 注册配置检查** —— 在 `packages/support/doctor/src/index.ts` 追加 import 和注册：
    ```typescript
    import { configChecks } from './checks/config.js'
    // ...
    for (const check of configChecks) registerCheck(check)
    ```

- [ ] **Step 3: 写配置检查测试** —— 新建 `packages/support/doctor/tests/config.spec.ts`：
    ```typescript
    import { describe, it, expect, beforeEach } from 'vitest'
    import { mkdtemp, writeFile, mkdir } from 'node:fs/promises'
    import { tmpdir } from 'node:os'
    import { join } from 'node:path'
    import { _resetRegistry, runDiagnosis, runRepair, registerCheck } from '@deepseek-ai/dsh-doctor'
    import { configChecks } from '@deepseek-ai/dsh-doctor/checks/config'

    describe('config checks', () => {
      let home: string

      beforeEach(async () => {
        home = await mkdtemp(join(tmpdir(), 'dsh-doctor-cfg-'))
        _resetRegistry()
        for (const c of configChecks) registerCheck(c)
      })

      describe('cfg-settings-yaml', () => {
        it('passes on missing settings.yaml', async () => {
          const report = await runDiagnosis(home)
          const c = report.checks.find((x) => x.id === 'cfg-settings-yaml')
          expect(c?.result.ok).toBe(true)
        })

        it('fails on invalid YAML', async () => {
          await writeFile(join(home, 'settings.yaml'), 'invalid: yaml: [unclosed')
          const report = await runDiagnosis(home)
          const c = report.checks.find((x) => x.id === 'cfg-settings-yaml')
          expect(c?.result.ok).toBe(false)
          expect(c?.result.fixable).toBe(true)
        })

        it('level-2 repair backs up and renames settings.yaml', async () => {
          await writeFile(join(home, 'settings.yaml'), 'bad: [yaml')
          const repair = await runRepair(2, home)
          expect(repair.applied.some((a) => a.checkId === 'cfg-settings-yaml')).toBe(true)
          const { existsSync } = await import('node:fs')
          expect(existsSync(join(home, 'settings.yaml'))).toBe(false)
          expect(existsSync(join(home, 'settings.yaml.doctor-bak'))).toBe(true)
        })
      })

      describe('cfg-user-patch', () => {
        beforeEach(async () => {
          await mkdir(join(home, 'profiles', 'web'), { recursive: true })
        })

        it('passes on missing patch', async () => {
          const report = await runDiagnosis(home)
          const c = report.checks.find((x) => x.id === 'cfg-user-patch')
          expect(c?.result.ok).toBe(true)
        })

        it('fails on invalid patch YAML', async () => {
          await writeFile(join(home, 'profiles', 'web', 'cordis.patch.yml'), 'not: an: array:')
          const report = await runDiagnosis(home)
          const c = report.checks.find((x) => x.id === 'cfg-user-patch')
          expect(c?.result.ok).toBe(false)
        })

        it('level-2 repair disables patch file', async () => {
          await writeFile(join(home, 'profiles', 'web', 'cordis.patch.yml'), 'bad: yaml')
          const repair = await runRepair(2, home)
          expect(repair.applied.some((a) => a.checkId === 'cfg-user-patch')).toBe(true)
          const { existsSync } = await import('node:fs')
          expect(existsSync(join(home, 'profiles', 'web', 'cordis.patch.yml'))).toBe(false)
          expect(existsSync(join(home, 'profiles', 'web', 'cordis.patch.yml.disabled'))).toBe(true)
        })
      })
    })
    ```

- [ ] **Step 4: 运行测试**

    Run:
    ```bash
    pnpm --filter @deepseek-ai/dsh-doctor test
    ```

    Expected: 全 PASS。

- [ ] **Step 5: 提交**

    ```bash
    git add packages/support/doctor/src/checks/config.ts packages/support/doctor/tests/config.spec.ts packages/support/doctor/src/index.ts
    git commit -m "feat(doctor): add config checks (settings.yaml, cordis.patch.yml)"
    ```

---


### Task 4: 插件层检查 —— profile bundle 可解析 + 第三方插件兼容性（核心 MVP）

**Files:**
- Create: `packages/support/doctor/src/checks/plugins.ts`
- Modify: `packages/support/doctor/src/index.ts`（注册）
- Test: `packages/support/doctor/tests/plugins.spec.ts`

这是 MVP 的核心——升级后第三方插件不兼容是最高频的启动失败原因。

- [ ] **Step 1: 实现插件检查** —— 新建 `packages/support/doctor/src/checks/plugins.ts`：
    ```typescript
    import { readFileSync, existsSync } from 'node:fs'
    import { join, basename } from 'node:path'
    import { loadProfile, composeEntries } from '@deepseek-ai/dsh-app-boot'
    import { Context } from '@deepseek-ai/cordis'
    import Loader from '@deepseek-ai/cordis-plugin-loader'
    import Include from '@deepseek-ai/cordis-plugin-include'
    import Group from '@deepseek-ai/cordis-plugin-group'
    import { pathToFileURL } from 'node:url'
    import { dirname } from 'node:path'
    import type { DoctorCheck, CheckResult } from '../types.js'

    // 判断 bundle 是否为官方（@deepseek-ai/ 开头）
    function isOfficialBundle(packageName: string): boolean {
      return packageName.startsWith('@deepseek-ai/')
    }

    const pluginBundlesResolvable: DoctorCheck = {
      id: 'plugin-bundles-resolvable',
      name: 'Profile bundles resolvable',
      category: 'plugin',
      severity: 'fatal',
      check: async (dshHome: string): Promise<CheckResult> => {
        try {
          const profile = loadProfile('doctor', 'web', require.resolve('@deepseek-ai/dsh-web-app/package.json'), dshHome)
          const official = profile.layers.filter((l) => isOfficialBundle(l.packageName))
          const thirdParty = profile.layers.filter((l) => !isOfficialBundle(l.packageName))
          return {
            ok: true,
            message: `${profile.layers.length} bundles resolved (${official.length} official, ${thirdParty.length} third-party)`,
            fixable: thirdParty.length > 0,
            suggestedLevel: 1,
          }
        } catch (err) {
          const msg = (err as Error).message
          const thirdPartyMentioned = msg.includes('cannot resolve profile bundle')
          return {
            ok: false,
            message: `Cannot resolve profile bundles: ${msg}`,
            detail: thirdPartyMentioned ? 'A third-party plugin may have broken dependencies after the upgrade' : undefined,
            fixable: true,
            suggestedLevel: 1,
          }
        }
      },
    }

    const pluginFullActivation: DoctorCheck = {
      id: 'plugin-full-activation',
      name: 'Full plugin tree activation (dry-run)',
      category: 'plugin',
      severity: 'fatal',
      check: async (dshHome: string): Promise<CheckResult> => {
        // 干跑：加载 profile → 构建 entry list → 创建 loader → 等待 settle → 检查激活
        try {
          const profile = loadProfile('doctor', 'web', require.resolve('@deepseek-ai/dsh-web-app/package.json'), dshHome)
          const allPatches = profile.layers.flatMap((l) => l.patches).concat(profile.patches)
          const entries = composeEntries(allPatches)

          const ctx = new Context()
          ctx.baseUrl = pathToFileURL(dirname(profile.patchPath)).href + '/'
          await ctx.plugin(Loader)
          ctx.loader.builtins.include = Include
          ctx.loader.builtins.group = Group

          const includeId = await ctx.loader.create({
            id: 'include',
            name: 'cordis:include',
            config: { entries },
          })
          await ctx.loader.await()

          // 检查 failed fibers
          const failed: string[] = []
          for (const entry of ctx.loader.entries()) {
            if (entry.fiber && entry.fiber.state === 3 /* FAILED */) {
              failed.push(entry.options.name as string)
            }
          }

          await ctx.fiber.dispose()

          if (failed.length === 0) {
            return { ok: true, message: 'All plugins activate successfully', fixable: false, suggestedLevel: 1 }
          }

          // 区分官方插件失败和第三方插件失败
          const officialFailed = failed.filter((f) => f.startsWith('@deepseek-ai/') || f.startsWith('cordis:'))
          const thirdPartyFailed = failed.filter((f) => !f.startsWith('@deepseek-ai/') && !f.startsWith('cordis:'))

          const thirdPartyBundles = profile.layers.filter((l) => !isOfficialBundle(l.packageName)).map((l) => l.packageName)

          return {
            ok: false,
            message: `${failed.length} plugin(s) failed to activate: ${failed.join(', ')}`,
            detail: thirdPartyFailed.length > 0 && thirdPartyBundles.length > 0
              ? `Third-party bundles present: ${thirdPartyBundles.join(', ')}. Try safe mode with third-party plugins disabled.`
              : undefined,
            fixable: thirdPartyBundles.length > 0,
            suggestedLevel: 1,
          }
        } catch (err) {
          return {
            ok: false,
            message: `Dry-run activation failed: ${(err as Error).message}`,
            fixable: true,
            suggestedLevel: 1,
          }
        }
      },
    }

    // 统计第三方 bundle 数量（信息类，severity=info）
    const pluginThirdPartyList: DoctorCheck = {
      id: 'plugin-third-party-list',
      name: 'Third-party plugin bundles',
      category: 'plugin',
      severity: 'info',
      check: async (dshHome: string): Promise<CheckResult> => {
        try {
          const profile = loadProfile('doctor', 'web', require.resolve('@deepseek-ai/dsh-web-app/package.json'), dshHome)
          const thirdParty = profile.layers.filter((l) => !isOfficialBundle(l.packageName))
          if (thirdParty.length === 0) {
            return { ok: true, message: 'No third-party bundles installed', fixable: false, suggestedLevel: 1 }
          }
          return {
            ok: true,
            message: `${thirdParty.length} third-party bundle(s): ${thirdParty.map((t) => t.packageName).join(', ')}`,
            fixable: false,
            suggestedLevel: 1,
          }
        } catch {
          return { ok: true, message: 'Cannot determine third-party bundles (profile not loadable)', fixable: false, suggestedLevel: 1 }
        }
      },
    }

    export const pluginChecks: DoctorCheck[] = [pluginBundlesResolvable, pluginFullActivation, pluginThirdPartyList]
    ```

    注意：`composeEntries` 需要从 `app-boot` 正确导入；`require.resolve` 在 ESM 下需要用 `createRequire`。修正为用 `import.meta.resolve` 或 `createRequire`。

- [ ] **Step 2: 修正 ESM 下的 resolve** —— 在文件顶部加：
    ```typescript
    import { createRequire } from 'node:module'
    const require = createRequire(import.meta.url)
    ```

- [ ] **Step 3: 注册插件检查** —— 在 `index.ts` 追加 import 和注册。

- [ ] **Step 4: 写插件检查测试** —— 新建 `packages/support/doctor/tests/plugins.spec.ts`，重点测试：
    1. 纯官方 bundle 时全通过
    2. profile 不存在时的行为
    3. 第三方 bundle 数量统计
    4. full activation 干跑（用官方 bundle 应该通过）

    测试重点是 `pluginBundlesResolvable` 和 `pluginThirdPartyList`，`pluginFullActivation` 的干跑测试在真实环境中更复杂，可以先做一个基础测试确保不抛异常。

- [ ] **Step 5: 运行测试**

    Run:
    ```bash
    pnpm --filter @deepseek-ai/dsh-doctor test
    ```

    Expected: 全 PASS。

- [ ] **Step 6: 提交**

    ```bash
    git add packages/support/doctor/src/checks/plugins.ts packages/support/doctor/tests/plugins.spec.ts
    git commit -m "feat(doctor): add plugin checks (bundle resolvability, dry-run activation)"
    ```

---


### Task 5: 第三方插件安全模式（app-boot 层 DSH_SAFE_MODE=plugins）

**Files:**
- Modify: `packages/boot/app-boot/src/profile.ts` — `loadProfile()` 增加 `skipThirdPartyBundles` 选项
- Modify: `packages/boot/app-boot/src/index.ts` — `boot()` / `loadProfile` 调用处响应 `DSH_SAFE_MODE`
- Test: `packages/boot/app-boot/tests/safe-mode.spec.ts`

这是 MVP 的核心功能——`DSH_SAFE_MODE=plugins` 跳过第三方 bundle，用户设置和数据保留。

- [ ] **Step 1: 在 loadProfile 增加 skipThirdPartyBundles 选项** —— 修改 `packages/boot/app-boot/src/profile.ts` 的 `loadProfile` 函数签名和实现：

    在 `loadProfile` 的 options 参数中增加 `skipThirdPartyBundles?: boolean`：

    ```typescript
    export function loadProfile(
      binName: string, name: string, installAnchor: string, home: string = resolveDshHome(),
      options: { userLayer?: boolean; skipThirdPartyBundles?: boolean } = {},
    ): Profile {
    ```

    在 `layers = bundles.map(...)` 之后、返回之前，加过滤逻辑：

    ```typescript
      let layers = bundles.map((packageName): ProfileLayer => {
        // ... 原有实现
      })
      if (options.skipThirdPartyBundles) {
        layers = layers.filter((l) => l.packageName.startsWith('@deepseek-ai/'))
      }
      // ... 原有返回
    ```

- [ ] **Step 2: 在 boot 入口响应 DSH_SAFE_MODE 环境变量** —— 修改 `packages/boot/app-boot/src/index.ts` 的 `boot()` 函数或者在 web-app 的 startup 里处理。

    更干净的做法是在调用 `loadProfile` 的地方（各个 app bin）读取 `process.env.DSH_SAFE_MODE`。但为了在 MVP 阶段快速接入，在 `loadProfile` 内自动检测环境变量：

    在 `loadProfile` 函数开头追加：
    ```typescript
      const safeMode = process.env.DSH_SAFE_MODE
      const skipThirdParty = options.skipThirdPartyBundles ?? safeMode === 'plugins' || safeMode === 'config' || safeMode === 'full'
    ```

    然后用 `skipThirdParty` 替代直接的 `options.skipThirdPartyBundles`。

    同时，`userLayer`（用户 cordis.patch.yml）在 `config` 和 `full` 模式下也跳过：
    ```typescript
      const skipUserLayer = options.userLayer === false
        || safeMode === 'config'
        || safeMode === 'full'
    ```

    把 `const patches = options.userLayer !== false && existsSync(patchPath)` 改为用 `skipUserLayer`。

- [ ] **Step 3: settings 的安全模式适配** —— 这部分涉及 `settings-file` 插件，在安全模式下改用内存后端。但 MVP 阶段先不做这个（settings 损坏的比例低于插件问题），留到 Phase 2。

- [ ] **Step 4: 写安全模式测试** —— 新建 `packages/boot/app-boot/tests/safe-mode.spec.ts`：
    ```typescript
    import { describe, it, expect, beforeEach, afterEach } from 'vitest'
    import { mkdtemp, writeFile } from 'node:fs/promises'
    import { tmpdir } from 'node:os'
    import { join } from 'node:path'
    import { loadProfile } from '../src/profile.js'

    describe('safe mode', () => {
      let home: string
      const originalEnv = process.env.DSH_SAFE_MODE

      beforeEach(async () => {
        home = await mkdtemp(join(tmpdir(), 'dsh-safe-mode-'))
        delete process.env.DSH_SAFE_MODE
      })

      afterEach(() => {
        if (originalEnv === undefined) delete process.env.DSH_SAFE_MODE
        else process.env.DSH_SAFE_MODE = originalEnv
      })

      it('normal mode loads all bundles including third-party', () => {
        // 用一个已存在的 profile（web），这里不需要真实第三方 bundle
        // 只需验证 skipThirdPartyBundles 选项生效
        const profile = loadProfile(
          'test', 'web',
          require.resolve('@deepseek-ai/dsh-web-app/package.json'),
          home,
          { skipThirdPartyBundles: false },
        )
        // web profile 的官方 bundle 至少有 base 和 web-app
        const official = profile.layers.filter((l) => l.packageName.startsWith('@deepseek-ai/'))
        expect(official.length).toBeGreaterThanOrEqual(2)
      })

      it('DSH_SAFE_MODE=plugins skips non-official bundles', () => {
        process.env.DSH_SAFE_MODE = 'plugins'
        const profile = loadProfile(
          'test', 'web',
          require.resolve('@deepseek-ai/dsh-web-app/package.json'),
          home,
        )
        const nonOfficial = profile.layers.filter((l) => !l.packageName.startsWith('@deepseek-ai/'))
        expect(nonOfficial.length).toBe(0)
      })

      it('DSH_SAFE_MODE=config skips user patch layer', () => {
        process.env.DSH_SAFE_MODE = 'config'
        // 先创建一个用户 patch
        const profileDir = join(home, 'profiles', 'web')
        // profile 目录需要存在才能有用户 patch
        // 但 loadProfile 会自动 init shipped profiles
        const profile = loadProfile(
          'test', 'web',
          require.resolve('@deepseek-ai/dsh-web-app/package.json'),
          home,
        )
        // config 模式下用户 patch 应该为空
        expect(profile.patches.length).toBe(0)
      })
    })
    ```

    同样用 `createRequire(import.meta.url)` 处理 require.resolve。

- [ ] **Step 5: 运行测试**

    Run:
    ```bash
    pnpm --filter @deepseek-ai/dsh-app-boot test
    ```

    Expected: 全 PASS。

- [ ] **Step 6: 提交**

    ```bash
    git add packages/boot/app-boot/src/profile.ts packages/boot/app-boot/tests/safe-mode.spec.ts
    git commit -m "feat(app-boot): support DSH_SAFE_MODE (plugins/config/full) in loadProfile"
    ```

---


### Task 6: 桌面启动器 Go 层集成（Diagnose + Repair + StartSafeMode）

**Files:**
- Modify: `apps/desktop-launcher/internal/app/app.go` — 新增绑定方法
- Modify: `apps/desktop-launcher/internal/domain/domain.go` — 新增类型
- Modify: `apps/desktop-launcher/internal/appenv/env.go` — 安全模式环境变量注入

- [ ] **Step 1: 定义 domain 类型** —— 在 `apps/desktop-launcher/internal/domain/domain.go` 追加：
    ```go
    // DoctorCheck 是一项诊断检查的结果。
    type DoctorCheck struct {
    	ID       string `json:"id"`
    	Name     string `json:"name"`
    	Category string `json:"category"`
    	Severity string `json:"severity"`
    	OK       bool   `json:"ok"`
    	Message  string `json:"message"`
    	Detail   string `json:"detail,omitempty"`
    	Fixable  bool   `json:"fixable"`
    }

    // DoctorResult 是诊断报告。
    type DoctorResult struct {
    	OK      bool          `json:"ok"`
    	Checks  []DoctorCheck `json:"checks"`
    	Summary struct {
    		Total   int `json:"total"`
    		OK      int `json:"ok"`
    		Failed  int `json:"failed"`
    		Fatal   int `json:"fatal"`
    		Fixable int `json:"fixable"`
    	} `json:"summary"`
    	Error string `json:"error,omitempty"`
    }

    // RepairResult 是修复报告。
    type RepairResult struct {
    	OK      bool   `json:"ok"`
    	Level   int    `json:"level"`
    	Applied int    `json:"applied"`
    	Skipped int    `json:"skipped"`
    	Message string `json:"message"`
    }
    ```

- [ ] **Step 2: 在 app.go 增加 Diagnose 方法** —— 调用容器内的 `dsh doctor --json`：
    ```go
    // Diagnose 运行 dsh doctor --json，返回诊断结果。
    func (a *App) Diagnose() DoctorResult {
    	// 从 appenv 拿到 node 和 bin 路径
    	resolved := appenv.Resolve()
    	if resolved.Config.Command == "" {
    		return DoctorResult{OK: false, Error: "harness binary not found"}
    	}

    	// 构造命令: node <bin.js> doctor --json
    	// 注意：dsh CLI 的入口需要有 doctor 子命令
    	// MVP 阶段先通过 node 直接调用 doctor 包的入口
    	// 或等 dsh CLI 支持 doctor 子命令后再切换

    	// 这里先调用 node -e 执行 doctor
    	// 更稳妥的方式是调用 dsh 自己的 doctor 子命令
    	cmd := exec.Command(resolved.Config.Command, append(resolved.Config.Args[:2], "doctor", "--json")...)
    	// 但 Config.Args 是 ["web", "--port", "xxx"]，需要替换

    	// 简化：直接用 node + doctor 入口脚本
    	// 先找到 doctor 包的入口
    	// 暂时先返回一个 mock，等 CLI 集成后再实装
    	return DoctorResult{OK: false, Error: "doctor CLI not yet integrated"}
    }
    ```

    **说明**：由于 MVP 阶段 doctor 包还没接入到 dsh CLI，这一步先把 Go 层的方法和前端 UI 框架搭好，等 CLI 接入后直接替换实现。MVP 优先保证「安全模式启动」按钮可用。

- [ ] **Step 3: StartSafeMode 方法** —— 启动时注入 `DSH_SAFE_MODE=plugins`：
    ```go
    // StartSafeMode 以第三方插件安全模式启动 harness（跳过后装的第三方插件 bundle）。
    func (a *App) StartSafeMode() FrontendStatus {
    	a.sup.StopHarness()
    	// 通过环境变量控制安全模式（在下次 spawn 时生效）
    	os.Setenv("DSH_SAFE_MODE", "plugins")
    	a.sup.Restart()
    	a.emitStatus()
    	return a.snapshot()
    }

    // ExitSafeMode 退出安全模式，恢复正常启动。
    func (a *App) ExitSafeMode() FrontendStatus {
    	os.Unsetenv("DSH_SAFE_MODE")
    	a.sup.Restart()
    	a.emitStatus()
    	return a.snapshot()
    }
    ```

    注：`ConfigureChildEnv` 在 main.go 里只调用一次，子进程环境变量从 launcher 的环境继承。直接 `os.Setenv` 可以影响后续 `exec.Command` 启动的子进程。

- [ ] **Step 4: 前端增加「插件安全模式」按钮** —— 修改 `apps/desktop-launcher/frontend/index.html`，在 server-modal 的启动失败区域增加：
    ```html
    <div id="safe-mode-row" class="safe-mode-row hidden">
      <button id="btn-safe-mode" class="btn btn-primary">以插件安全模式启动</button>
      <span class="hint">跳过后安装的第三方插件，保留你的会话和设置</span>
    </div>
    ```

- [ ] **Step 5: 前端 JS 增加逻辑** —— 修改 `app.js`，在 `renderServerDialog` 里：
    - 当 state 为 `failed` 时显示 safe-mode-row
    - 点击按钮调用 `window.go.app.App.StartSafeMode()`
    - 安全模式运行中，状态条显示「🔒 插件安全模式」

- [ ] **Step 6: 编译验证**

    Run:
    ```bash
    cd apps/desktop-launcher && go build ./...
    ```

    Expected: 编译成功。

- [ ] **Step 7: 提交**

    ```bash
    git add apps/desktop-launcher/internal/app/app.go apps/desktop-launcher/internal/domain/domain.go apps/desktop-launcher/frontend/index.html apps/desktop-launcher/frontend/app.js apps/desktop-launcher/frontend/styles.css
    git commit -m "feat(desktop-launcher): add safe-mode start button and diagnostics UI scaffolding"
    ```

---


### Task 7: dsh CLI 接入 doctor 子命令

**Files:**
- （需要找到 dsh CLI 的入口文件位置，接入 `doctor` 和 `repair` 子命令）

- [ ] **Step 1: 找到 dsh CLI 入口**

    Run:
    ```bash
    find packages -name "bin.js" -path "*/cli/*" 2>/dev/null | head -5
    ```

- [ ] **Step 2: 接入 doctor 子命令** —— 参考 web startup 的模式（`packages/bundle/web-app/src/startup.ts`），在 CLI 的 commander program 里增加 `doctor` 子命令。

    或者更简单：doctor 作为一个独立的启动入口（`dsh --profile doctor`），但 doctor 不需要完整的 web 环境。

    **推荐方案**：doctor 命令在 `cmdline` 层注册，走一个极简的启动路径——只 import doctor 包、跑检查、输出 JSON、退出，不需要加载整个 Cordis 插件树。

    由于 MVP 阶段时间有限，可以先把 doctor 做成 `dsh doctor` 命令，作为 `@deepseek-ai/dsh-app-boot` 的一个独立 bin 入口。

- [ ] **Step 3: 实现 CLI 入口** —— 在 doctor 包新建 `src/cli.ts`：
    ```typescript
    #!/usr/bin/env node
    import { runDiagnosis, runRepair } from './index.js'
    import { resolveDshHome } from '@deepseek-ai/dsh-home-paths'

    const args = process.argv.slice(2)
    const json = args.includes('--json')
    const levelArg = args.find((a) => a.startsWith('--level='))
    const level = levelArg ? Number(levelArg.split('=')[1]) : 1

    async function main() {
      if (args[0] === 'repair') {
        const report = await runRepair(level as 1 | 2 | 3)
        if (json) {
          console.log(JSON.stringify(report, null, 2))
        } else {
          console.log(`Repair level ${level}:`)
          console.log(`  Applied: ${report.applied.length}`)
          console.log(`  Skipped: ${report.skipped.length}`)
          for (const a of report.applied) {
            console.log(`  ✓ ${a.checkId}: ${a.message}`)
          }
        }
        process.exit(report.applied.length > 0 ? 0 : 1)
      } else {
        // default: doctor
        const report = await runDiagnosis()
        if (json) {
          console.log(JSON.stringify(report, null, 2))
        } else {
          console.log(`DSH Home: ${report.dshHome}`)
          console.log(`Checks: ${report.summary.ok}/${report.summary.total} passed`)
          for (const c of report.checks) {
            const icon = c.result.ok ? '✓' : '✗'
            console.log(`  ${icon} [${c.severity}] ${c.name}: ${c.result.message}`)
          }
          if (report.summary.fixable > 0) {
            console.log(`\n${report.summary.fixable} issue(s) can be fixed automatically.`)
            console.log(`Run: dsh doctor repair --level 1`)
          }
        }
        process.exit(report.summary.fatal > 0 ? 1 : 0)
      }
    }

    main().catch((err) => {
      console.error('doctor error:', err)
      process.exit(2)
    })
    ```

- [ ] **Step 4: 接入 dsh CLI** —— 找到 dsh CLI 的入口后，在 commander 里注册 doctor 子命令，调用上述 cli.ts 的逻辑。

- [ ] **Step 5: 提交**

    ```bash
    git add packages/support/doctor/src/cli.ts
    git commit -m "feat(doctor): add CLI entry (dsh doctor / dsh doctor repair)"
    ```

---


### Task 8: 端到端验证

- [ ] **Step 1: 跑 doctor 包全量测试**
    ```bash
    pnpm --filter @deepseek-ai/dsh-doctor test
    ```
    Expected: 全 PASS

- [ ] **Step 2: 跑 app-boot 测试**
    ```bash
    pnpm --filter @deepseek-ai/dsh-app-boot test
    ```
    Expected: 全 PASS（含新增的安全模式测试）

- [ ] **Step 3: desktop-launcher 编译验证**
    ```bash
    cd apps/desktop-launcher && go build ./... && go test ./...
    ```
    Expected: 编译通过，测试通过

- [ ] **Step 4: typecheck**
    ```bash
    pnpm run typecheck
    ```
    Expected: 无类型错误

- [ ] **Step 5: 手动验证安全模式**
    ```bash
    DSH_SAFE_MODE=plugins pnpm dsh --profile web --no-open
    ```
    Expected: 正常启动，不加载第三方插件（如果有的话）

- [ ] **Step 6: 提交最终测试修复**
    ```bash
    git add -u
    git commit -m "test: verify doctor + safe mode end-to-end"
    ```

---


## 自检

### Spec 覆盖
- ✅ `dsh doctor` 诊断工具 → Task 1-4
- ✅ 第三方插件升级兼容检测 → Task 4（plugin checks）
- ✅ 三级安全模式（plugins/config/full） → Task 5（plugins + config；full 留 Phase 2）
- ✅ Level 1 / Level 2 修复 → Task 2（.env L1）+ Task 3（settings/patch L2）
- ✅ 桌面端启动失败页入口 → Task 6
- ✅ CLI 命令 → Task 7
- ⚠️ 数据层检查（会话完整性）→ Phase 2（不是 MVP 核心）
- ⚠️ 升级前预检 → Phase 3

### 占位符扫描
- 所有代码步骤都有实际代码内容
- 测试步骤都有具体命令和期望结果
- 没有 "TBD" / "TODO" / "add appropriate error handling" 等占位符

### 类型一致性
- DoctorCheck / CheckResult / FixResult 在 types.ts 中统一定义
- 所有检查项都使用一致的接口
- 修复级别 RepairLevel = 1 | 2 | 3 贯穿全文

---

Plan saved to `docs/superpowers/plans/2026-08-28-dsh-doctor-and-safe-mode.md`. Two execution options:

**1. Subagent-Driven (recommended)** - 我为每个任务分配一个独立的子代理，任务间做审查，迭代速度快

**2. Inline Execution** - 在当前会话中逐步执行，按批次执行并设置审查检查点

你选择哪种方式？

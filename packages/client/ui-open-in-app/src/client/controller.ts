/** Browser availability/choice state and the launch carrier for the split button. */

import { createSnapshotStore, type SnapshotStore } from '@deepseek-ai/dsh-client-store'
import {
  OPEN_IN_APP_APPS_ROUTE, OPEN_IN_APP_OPEN_ROUTE,
  type OpenInAppAppsPayload, type OpenInAppOpenPayload,
} from '@deepseek-ai/dsh-host-open-in-app/shared'

import { APP_LABEL_KEY } from './applications.ts'

/** Shared launch status for controls targeting the captured workspace path. */
export interface OpenInAppLaunchState {
  readonly phase: 'idle' | 'busy' | 'error'
  readonly path: string | null
}

type Fetch = (input: string | URL, init?: RequestInit) => Promise<Response>

/**
 * Owns the availability read, the persisted last choice, and the launch POST.
 * Availability and choice publish through uSES-safe sources so every Session
 * header shares one truth.
 */
export class OpenInAppController {
  /** Installed app ids in host menu order; null until the host answered. */
  readonly apps: SnapshotStore<readonly string[] | null> = createSnapshotStore<readonly string[] | null>(null)
  /** Last chosen app id, or empty before the first choice, shared across sessions and browser restarts. */
  readonly choice: SnapshotStore<string> = createSnapshotStore<string>('', {
    persist: { name: 'dsh.open-in-app.choice' },
  })

  /** Current launch, shared by pointer and keyboard gestures. */
  readonly operation = createSnapshotStore<OpenInAppLaunchState>({ phase: 'idle', path: null })

  /** 没有图标来源的应用 id；与 {@link apps} 同一次读取一起换成新值。 */
  private iconless: readonly string[] = []

  /**
   * 宿主是否可能为这个应用提供图标。宿主报告没有图标来源时为 false：图标路由对这类应用
   * （只有启动命令、没有桌面条目的）只能回 404，调用方据此跳过请求、直接画通用图标。
   * 只在 {@link apps} 发布后读取，因此应用清单变化一定会把新结论带到渲染。
   * @param appId - 目录中的应用 id。
   * @returns 有可能提供图标时为 true。
   */
  hasIconSource(appId: string): boolean {
    return !this.iconless.includes(appId)
  }

  /**
   * Resolve the remembered nameable installed application, with the button's first-app fallback.
   * @returns the installed app id, or undefined while unavailable.
   */
  currentApp(): string | undefined {
    const apps = (this.apps.getSnapshot() ?? []).filter(id => APP_LABEL_KEY[id] !== undefined)
    const choice = this.choice.getSnapshot()
    return apps.includes(choice) ? choice : apps[0]
  }

  private loading: Promise<void> | undefined

  /**
   * @param fetcher - HTTP carrier for the apps read and the launch POST.
   */
  constructor(private readonly fetcher: Fetch = (input, init) => fetch(input, init)) {}

  /**
   * Read availability once per controller life; concurrent calls share the read.
   * A failed read publishes an empty list, which renders no button at all.
   * @returns after availability is published.
   */
  load(): Promise<void> {
    this.loading ??= this.run()
    return this.loading
  }

  /**
   * Read availability again, before a menu opens: the host re-resolves the
   * applications it can escape to, so one installed or removed on the host
   * since the last read reaches the menu. The published list stays until the
   * fresh read lands, so an open menu never empties while the host answers.
   * @returns after the fresh availability is published.
   */
  refresh(): Promise<void> {
    this.loading = this.run()
    return this.loading
  }

  /**
   * Remember one picked app id.
   * @param appId - catalog id from the availability list.
   */
  choose(appId: string): void {
    if (this.operation.getSnapshot().phase !== 'busy') this.choice.set(appId)
  }

  /**
   * Launch one installed app on a workspace directory.
   * @param appId - catalog id from the availability list.
   * @param path - the session's absolute workspace directory.
   * Concurrent gestures are ignored until the current Host request settles.
   * @returns after the host acknowledged the launch; rejects on any failure.
   */
  async launch(appId: string, path: string): Promise<void> {
    if (this.operation.getSnapshot().phase === 'busy') return
    this.operation.set({ phase: 'busy', path })
    const body: OpenInAppOpenPayload = { app: appId, path }
    try {
      const response = await this.fetcher(OPEN_IN_APP_OPEN_ROUTE, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!response.ok) throw new Error(`open failed: HTTP ${String(response.status)}`)
      this.operation.set({ phase: 'idle', path })
    } catch (error) {
      this.operation.set({ phase: 'error', path })
      throw error
    }
  }

  private async run(): Promise<void> {
    let apps: readonly string[] = []
    let iconless: readonly string[] = []
    try {
      const response = await this.fetcher(OPEN_IN_APP_APPS_ROUTE, {
        headers: { accept: 'application/json' },
      })
      if (response.ok) {
        const payload = await response.json() as OpenInAppAppsPayload
        if (Array.isArray(payload.apps)) apps = payload.apps.filter(id => typeof id === 'string')
        // 字段缺失（旧宿主或缓存的旧 bundle）时按"可能有图标"处理，退回改动前的行为。
        if (Array.isArray(payload.iconless)) iconless = payload.iconless.filter(id => typeof id === 'string')
      }
    } catch {
      // Swallows network failures: an unreachable host reads as no apps, and
      // the header simply shows no button rather than a broken one.
    }
    this.iconless = iconless
    this.apps.set(apps)
  }
}

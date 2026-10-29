/** Shared file and directory opener: one default action, application menu, and one reported outcome per gesture. */
import { useRef, useState } from 'react'
import type { ReactNode } from 'react'
import {
  IconChevronDownOutlineRegular, IconFolderOpenOutlineRegular, IconRightUpOutlineRegular, Menu, Tooltip,
} from '@deepseek-ai/dsh-client-ui-primitives'
import type { ShortcutCatalogEntry } from '@deepseek-ai/dsh-client-shortcuts/client'
import type { TranslateNS } from '@deepseek-ai/dsh-client-ui-slots'
import type { OpenInAppOutcome } from './outcome.ts'
import type { NS } from './locales.ts'
import css from './OpenTargetButton.module.css'

/** Application metadata supplied by either the directory catalog or a file association query. */
export interface OpenTargetApplication {
  readonly id: string
  readonly name: string
  readonly icon: string | null
}

/** Opening intent; default selection remains owned by the target's adapter. */
export type OpenTargetOperation = { readonly kind: 'default' } | { readonly kind: 'application'; readonly id: string } | { readonly kind: 'reveal' }

/** Seat a contribution sends one settled gesture to for app-wide announcement. */
export interface OpenTargetReporter {
  /** Report the settled outcome; `{ kind: 'skipped' }` announces nothing. */
  readonly report: (outcome: OpenInAppOutcome) => void
}

/** Inputs shared by both target adapters and the file empty-state action. */
export interface OpenTargetButtonProps extends OpenTargetReporter {
  readonly shortcut?: Pick<ShortcutCatalogEntry, 'keys' | 'aria'> | undefined
  readonly kind: 'file' | 'directory'
  readonly applications: readonly OpenTargetApplication[]
  readonly defaultId: string | undefined
  readonly failed: boolean
  readonly busy?: boolean
  readonly loading?: boolean
  readonly prominent?: boolean
  readonly t: TranslateNS<typeof NS>
  /** Resolve every gesture to an outcome, including Host and transport failures; never reject. */
  readonly execute: (operation: OpenTargetOperation) => Promise<OpenInAppOutcome>
  readonly refresh?: () => void
}

/**
 * Serialize gestures and report the outcome each one settles on to the app-wide
 * announcer, which outlives this control and the panel holding it.
 * @param execute - target adapter that settles on the gesture's outcome.
 * @param report - announcement seat the settled outcome is sent to.
 * @returns the pending state and the guarded action callback.
 */
export function useOpenTargetGesture(
  execute: OpenTargetButtonProps['execute'], report: OpenTargetButtonProps['report'],
): {
  pending: boolean
  act: (operation: OpenTargetOperation) => void
} {
  const [pending, setPending] = useState(false)
  const inFlight = useRef(false)
  return {
    pending,
    act: (operation) => {
      if (inFlight.current) return
      inFlight.current = true
      setPending(true)
      void execute(operation).then(report).finally(() => { inFlight.current = false; setPending(false) })
    },
  }
}

/**
 * 本页取图标失败过的地址。宿主没有图标来源的应用已经不再请求图标，剩下的失败发生在提取阶段，
 * 而图标控件每次挂载都会重新请求同一个地址：这里把失败记到本页，同一个地址只请求一次，
 * 之后直接画通用图标（刷新页面即重新尝试）。
 */
const failedIcons = new Set<string>()

/**
 * 一个应用图标：有地址就渲染图片，没有地址或本页已取失败时退回通用图标。
 * @param props - 图标地址（宿主不提供时为 null）与像素尺寸。
 * @returns 图标元素；没有地址或本页已取失败时为通用图标。
 */
function ApplicationIcon({ source, size = 14 }: { source: string | null; size?: number }): ReactNode {
  const [failed, setFailed] = useState(() => source !== null && failedIcons.has(source))
  const shown = source !== null && !failed ? source : null
  return shown === null
    ? <IconRightUpOutlineRegular size={size} />
    : <img src={shown} width={size} height={size} className={css.appIcon} alt="" draggable={false} onError={() => { failedIcons.add(shown); setFailed(true) }} />
}

/**
 * Render identical split buttons for files and directories. File reveal always
 * stays last; it is the default only when no application is registered.
 * @param props - target applications, default selection, and operations.
 * @returns the control alone: its settled outcomes are announced by the seat it reports to.
 */
export function OpenTargetButton(props: OpenTargetButtonProps): ReactNode {
  const { applications, defaultId, kind, t } = props
  const [menuOpen, setMenuOpen] = useState(false)
  const { pending, act } = useOpenTargetGesture(props.execute, props.report)
  // Files open through an application whenever one is registered: the OS-marked
  // default when the Host reported one, else the first entry in the Shell's
  // preference order. Reveal is only promoted with no registered application, so
  // a missing default marker never hides the applications behind it.
  const preferred = kind === 'file'
    ? applications.find(app => app.id === defaultId) ?? applications[0]
    : applications.find(app => app.id === defaultId)
  const disabled = pending || props.busy === true || props.loading === true
  const hasMenu = props.loading === true || props.failed || applications.length + (kind === 'file' ? 1 : 0) > 1
  const revealDefault = kind === 'file' && preferred === undefined && props.loading !== true
  const primaryLabel = preferred === undefined ? t('path.reveal') : t('open.title', { app: preferred.name })
  const run = (operation: OpenTargetOperation): void => { setMenuOpen(false); act(operation) }
  const primary = (): void => {
    if (revealDefault) { run({ kind: 'reveal' }); return }
    // The Host's default marker is best effort. With none, request the application
    // this control names instead of letting the OS resolve the association again.
    if (preferred !== undefined && preferred.id !== defaultId) { run({ kind: 'application', id: preferred.id }); return }
    run({ kind: 'default' })
  }
  const icon = props.loading === true && preferred === undefined
    ? <span className={css.skeleton} data-open-target-skeleton aria-hidden="true" style={{ width: props.prominent ? 18 : 13, height: props.prominent ? 18 : 13 }} />
    : revealDefault
      ? <IconFolderOpenOutlineRegular size={props.prominent ? 18 : 13} />
      : <ApplicationIcon key={preferred?.icon} source={preferred?.icon ?? null} size={props.prominent ? 18 : 13} />
  return (
    <Menu
      className={css.menuAnchor}
      open={menuOpen && !disabled && hasMenu}
      autoFocus
      portal
      dense
      align="end"
      onClose={() => { setMenuOpen(false) }}
      items={[
        ...applications.map(app => ({
          id: `app:${app.id}`,
          icon: <ApplicationIcon key={app.icon} source={app.icon} />,
          label: app.id === preferred?.id ? t('path.appDefault', { app: app.name }) : app.name,
        })),
        ...(props.failed ? [{ id: 'unavailable', label: t('path.appsError'), disabled: true }] : []),
      ]}
      footer={kind === 'file' ? [{
        id: 'reveal', icon: <IconFolderOpenOutlineRegular />,
        label: revealDefault ? t('path.appDefault', { app: t('path.reveal') }) : t('path.reveal'),
      }] : []}
      onSelect={(id) => { run(id === 'reveal' ? { kind: 'reveal' } : { kind: 'application', id: id.slice(4) }) }}
      anchor={(
        <div className={css.split} data-open-target={kind} data-size={props.prominent ? 'large' : 'compact'}
          data-open-path={kind === 'file' && !props.prominent ? '' : undefined} data-state={disabled ? 'busy' : 'idle'}>
          <Tooltip portal label={primaryLabel} shortcutKeys={props.shortcut?.keys} side="bottom" delayMs={500}>
            <button type="button" className={css.main} disabled={disabled} aria-label={props.prominent ? undefined : primaryLabel}
              aria-keyshortcuts={props.shortcut?.aria}
              data-open-path-open={kind === 'file' && !props.prominent ? '' : undefined}
              data-open-path-unpreviewable={props.prominent ? '' : undefined} onClick={primary}>
              {icon}{(props.prominent) && (revealDefault ? t('path.reveal') : t('path.open'))}
            </button>
          </Tooltip>
          {hasMenu && <button
            type="button" className={css.chevron} disabled={disabled}
            aria-haspopup="menu" aria-expanded={menuOpen && !disabled} aria-label={t('path.more')}
            data-open-path-more={kind === 'file' && !props.prominent ? '' : undefined}
            onClick={() => {
              if (!menuOpen) props.refresh?.()
              setMenuOpen(value => !value)
            }}
          >
            <IconChevronDownOutlineRegular size={props.prominent ? 14 : 10} />
          </button>}
        </div>
      )}
    />
  )
}

/**
 * 托盘终端的右键菜单：终端页面内自绘的深色浮层。
 *
 * 为什么不用 Electron 的原生菜单：Linux 上它由 GTK 主题绘制，配色、圆角与悬停
 * 高亮都跟随系统，浅色系统里会出现一块白菜单压在终端唯一的深色画布上。终端本来
 * 就不随宿主主题翻转（见 `terminal.css`），菜单也必须留在同一套深色里。
 *
 * 文案不在这里产生：调用方从主进程下发的视图里取「复制」「粘贴」，本模块只负责
 * 呈现、定位、键盘与关闭。动作也只由调用方给出，菜单不碰剪贴板与终端。
 *
 * @module @deepseek-ai/dsh-desktop/terminal-menu
 */

/** 菜单里的一项：文案、可选快捷键提示、是否可用，以及选中后要做的事。 */
export interface TerminalMenuAction {
  /** 稳定的标识，供测试与调用方区分动作。 */
  readonly id: 'copy' | 'paste'
  /** 显示文案，取自壳语言字典。 */
  readonly label: string
  /** 右侧的快捷键提示，只做展示。 */
  readonly hint?: string
  /** 不可用时不响应点击与键盘。 */
  readonly disabled: boolean
  /** 执行动作；菜单会先关闭再调用它。 */
  readonly run: () => void
}

/** 菜单锚点，取右键位置的视口坐标。 */
export interface MenuPoint {
  readonly x: number
  readonly y: number
}

/** 菜单自身尺寸。 */
export interface MenuSize {
  readonly width: number
  readonly height: number
}

/** 窗口内容区尺寸。 */
export interface MenuViewport {
  readonly width: number
  readonly height: number
}

/** 菜单与窗口边缘之间的最小留白，避免贴着边框或被裁掉。 */
export const MENU_MARGIN = 8

/**
 * 把菜单框夹进窗口内。
 *
 * 只做夹取、不做翻转：菜单项很少，夹取后的位置仍指向用户右键的地方，而且不会在
 * 两个候选位置之间来回跳。窗口比菜单还小时贴住左上留白。
 * @param anchor - 右键位置。
 * @param size - 菜单实测尺寸。
 * @param viewport - 窗口内容区尺寸。
 * @param margin - 与边缘的最小距离。
 * @returns 固定定位用的 left 与 top。
 */
export function menuPosition(anchor: MenuPoint, size: MenuSize, viewport: MenuViewport,
  margin = MENU_MARGIN): { left: number; top: number } {
  return {
    left: Math.round(clamp(anchor.x, margin, viewport.width - size.width - margin)),
    top: Math.round(clamp(anchor.y, margin, viewport.height - size.height - margin)),
  }
}

/**
 * 把值收进区间；区间本身倒挂时返回下界。
 * @param value - 待收敛的值。@param min - 下界。@param max - 上界。
 * @returns 区间内的值。
 */
function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), Math.max(min, max))
}

/**
 * 终端页面的右键菜单。
 *
 * 菜单元素挂在页面根节点上而不是终端容器里：xterm 自己会裁剪与滚动内容，挂在里面
 * 会被裁掉。它只在打开期间存在，关闭即移除，页面不保留任何菜单状态。
 */
export class TerminalContextMenu {
  private element: HTMLElement | undefined

  /**
   * @param host - 菜单挂载的父节点，通常是 `document.body`。
   * @param restoreFocus - 菜单关闭后是否该把焦点还给终端；外部点击不抢焦点。
   */
  constructor(private readonly host: HTMLElement, private readonly restoreFocus: (restore: boolean) => void) {}

  /** @returns 菜单当前是否打开。 */
  get isOpen(): boolean {
    return this.element !== undefined
  }

  /**
   * 在锚点处打开菜单。
   * @param anchor - 右键位置的视口坐标。
   * @param items - 菜单项，按显示顺序。
   */
  openAt(anchor: MenuPoint, items: readonly TerminalMenuAction[]): void {
    this.close(false)
    const element = this.build(items)
    this.element = element
    this.host.append(element)
    // 先以不可见状态测量，避免在锚点处闪一帧再跳到夹取后的位置。
    element.style.visibility = 'hidden'
    const rect = element.getBoundingClientRect()
    const { left, top } = menuPosition(anchor, { width: rect.width, height: rect.height },
      { width: window.innerWidth, height: window.innerHeight })
    element.style.left = `${left}px`
    element.style.top = `${top}px`
    element.style.visibility = ''
    const first = element.querySelector<HTMLButtonElement>('.terminal-menu-item:not(:disabled)')
    first?.focus()
    document.addEventListener('pointerdown', this.onPointerDown, true)
    document.addEventListener('keydown', this.onKeyDown, true)
    window.addEventListener('blur', this.onWindowBlur)
    window.addEventListener('resize', this.onWindowResize)
  }

  /**
   * 关闭菜单并移除元素。
   * @param restoreFocus - 是否把焦点还给终端；外部点击时传 false。
   */
  close(restoreFocus = true): void {
    const element = this.element
    if (element === undefined) return
    this.element = undefined
    document.removeEventListener('pointerdown', this.onPointerDown, true)
    document.removeEventListener('keydown', this.onKeyDown, true)
    window.removeEventListener('blur', this.onWindowBlur)
    window.removeEventListener('resize', this.onWindowResize)
    const keepFocus = restoreFocus && element.contains(document.activeElement)
    element.remove()
    this.restoreFocus(keepFocus)
  }

  /** 页面卸载时移除菜单与监听。 */
  dispose(): void {
    this.close(false)
  }

  /**
   * 构建菜单元素。
   * @param items - 菜单项。
   * @returns 尚未挂载的菜单。
   */
  private build(items: readonly TerminalMenuAction[]): HTMLElement {
    const element = document.createElement('div')
    element.className = 'terminal-menu'
    element.setAttribute('role', 'menu')
    for (const item of items) {
      const row = document.createElement('button')
      row.type = 'button'
      row.className = 'terminal-menu-item'
      row.dataset.menuId = item.id
      row.setAttribute('role', 'menuitem')
      row.disabled = item.disabled
      const label = document.createElement('span')
      label.textContent = item.label
      row.append(label)
      if (item.hint !== undefined) {
        const hint = document.createElement('span')
        hint.className = 'terminal-menu-hint'
        hint.setAttribute('aria-hidden', 'true')
        hint.textContent = item.hint
        row.append(hint)
      }
      row.addEventListener('click', () => {
        if (item.disabled) return
        this.close()
        item.run()
      })
      element.append(row)
    }
    return element
  }

  /** 点击菜单外部时关闭，让点击目标自己拿焦点。 */
  private readonly onPointerDown = (event: PointerEvent): void => {
    const element = this.element
    if (element === undefined || !(event.target instanceof Node)) return
    if (element.contains(event.target)) return
    this.close(false)
  }

  /** 菜单自己的键盘导航：方向键移动、Enter/空格执行、Esc 关闭。 */
  private readonly onKeyDown = (event: KeyboardEvent): void => {
    const element = this.element
    if (element === undefined) return
    const rows = [...element.querySelectorAll<HTMLButtonElement>('.terminal-menu-item:not(:disabled)')]
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      this.close()
      return
    }
    if (rows.length === 0) return
    const current = rows.findIndex(row => row === document.activeElement)
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      const step = event.key === 'ArrowDown' ? 1 : -1
      const next = current < 0 ? 0 : (current + step + rows.length) % rows.length
      rows[next]?.focus()
      return
    }
    if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault()
      rows[event.key === 'Home' ? 0 : rows.length - 1]?.focus()
      return
    }
    if (event.key === 'Enter' || event.key === ' ') {
      const row = rows[current]
      if (row === undefined) return
      event.preventDefault()
      row.click()
    }
  }

  /** 窗口失去焦点时收起菜单；焦点不还给终端，避免把窗口再拉回前台。 */
  private readonly onWindowBlur = (): void => { this.close(false) }

  /** 尺寸变化后锚点已不可信，直接收起。 */
  private readonly onWindowResize = (): void => { this.close(false) }
}

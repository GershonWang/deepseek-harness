/**
 * 托盘「终端」窗口的页面脚本：一个 xterm 实例 + 主进程下发的状态行。
 *
 * 页面不持有会话状态：尺寸上报、输入转发、退出与否全由主进程决定，这里只做
 * 「把 PTY 输出画出来、把按键原样送回去」，以及安全地显示一行本地化状态。
 *
 * 键盘归属：只有三组粘贴组合（Ctrl+V、Ctrl+Shift+V、Shift+Insert）放行给系统的
 * 默认粘贴，其余一切——**包括 Esc**——都属于 PTY。Esc 是 vim 退出插入模式、
 * readline 转义序列的前缀，抢走它等于让交互程序不可用。
 */
import '@xterm/xterm/css/xterm.css'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import type { DesktopTerminalApi, DesktopTerminalView } from '../terminal-contract.ts'
import { isClipboardPasteShortcut } from './terminal-keys.ts'

declare global {
  interface Window {
    dshTerminal: DesktopTerminalApi
  }
}

/**
 * 取一个必需的页面元素。
 *
 * 文档是随包固定的一份，缺元素属于构建错误而不是运行期状况：与其在后面每一处都
 * 重复判空，不如在入口一次性失败，把问题暴露在加载时。
 * @param id - 元素 id。@returns 该元素。
 * @throws 元素不存在时抛出。
 */
function requireElement(id: string): HTMLElement {
  const element = document.getElementById(id)
  if (element === null) throw new Error(`desktop terminal: missing #${id}`)
  return element
}

/**
 * 终端配色。
 *
 * xterm 的 `theme` 只接受具体色值、不解析 CSS 变量，因此这里静态映射一份；
 * `terminal.css` 只画画布之外那一层（窗口底色、状态行），两处同色需一起改。
 */
const THEME = {
  background: '#1a1a1a',
  foreground: '#d4d4d4',
  cursor: '#d4d4d4',
  cursorAccent: '#1a1a1a',
  selectionBackground: '#3b4252',
  black: '#000000',
  red: '#f14c4c',
  green: '#23d18b',
  yellow: '#e2e210',
  blue: '#3b8eea',
  magenta: '#bc3fbc',
  cyan: '#29b7d3',
  white: '#e5e5e5',
  brightBlack: '#666666',
  brightRed: '#f14c4c',
  brightGreen: '#23d18b',
  brightYellow: '#f5f543',
  brightBlue: '#3b8eea',
  brightMagenta: '#bc3fbc',
  brightCyan: '#29b7d3',
  brightWhite: '#ffffff',
} as const

/** 回滚缓冲：与右侧栏终端同量级，够翻长构建输出又不吃内存。 */
const SCROLLBACK = 5000

/** 字号；取值落在壳内终端字号区间（10–20）的常用值上。 */
const FONT_SIZE = 13

/**
 * 等宽字体栈。
 *
 * 只点名通用等宽族：壳文档不加载 UI 主题字体，点名一个未随包的族会拿到半套字形，
 * 而 xterm 的行列由主字体的单元格宽度实测而来，字形一漂就整列错位。
 */
const FONT_FAMILY = 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace'

const api = window.dshTerminal
const screen = requireElement('screen')
const status = requireElement('status')
const statusText = requireElement('status-text')
const restart = requireElement('restart')

const terminal = new Terminal({
  theme: THEME,
  fontFamily: FONT_FAMILY,
  fontSize: FONT_SIZE,
  fontWeight: 500,
  lineHeight: 1.25,
  letterSpacing: 0,
  cursorBlink: true,
  scrollback: SCROLLBACK,
})
const fit = new FitAddon()
terminal.loadAddon(fit)
terminal.open(screen)
// 粘贴组合放行给 Chromium：返回 false 让 xterm 不再把 Ctrl+V 编成 0x16，默认粘贴
// 随后派发 paste 事件，由 xterm 按 bracketed-paste 规则写进 PTY。这里刻意不调用
// preventDefault，否则默认粘贴也会被拦掉。其余按键（含 Esc）继续交给 PTY。
terminal.attachCustomKeyEventHandler(event => !isClipboardPasteShortcut(event))

/** 会话是否已拉起：未拉起时不转发 resize，拉起之后每次都同步（SIGWINCH）。 */
let started = false

/**
 * 渲染主进程下发的视图。
 *
 * 失败与退出共用同一条状态行与同一颗重开按钮：对用户来说两者都只是「这里现在没有
 * 活着的 shell」，区别由文案说明，不需要两套控件。页面因此不拼任何一句文案。
 * @param next - 新视图。
 */
function render(next: DesktopTerminalView): void {
  document.documentElement.lang = next.locale
  document.title = next.title
  terminal.textarea?.setAttribute('aria-label', next.screenLabel)
  const ended = next.phase !== 'running'
  status.hidden = !ended
  statusText.textContent = next.status ?? ''
  restart.textContent = next.title
  restart.hidden = !ended
}

/**
 * 量出真实行列并上报：第一次同时拉起会话，之后复用同一个会话。
 *
 * 容器隐藏或布局未就绪时 fit 读到 0 尺寸会自行跳过，所以不需要额外的可见性判断；
 * 但刚变化的布局要先等一帧，否则量到的是旧尺寸。
 */
function reportSize(): void {
  fit.fit()
  if (terminal.cols <= 0 || terminal.rows <= 0) return
  started = true
  void api.start(terminal.cols, terminal.rows)
    .then(render)
    .catch((error: unknown) => { console.error('desktop terminal: could not start the session', error) })
}

terminal.onData((data) => {
  void api.write(data).catch((_gone: unknown) => {
    // 会话可能刚退出：状态行由主进程推送，这里不重复报错。
  })
})

terminal.onResize(({ cols, rows }) => {
  if (!started || cols <= 0 || rows <= 0) return
  void api.resize(cols, rows).catch((_gone: unknown) => {
    // 同上：会话结束时 resize 必然失败，不是可行动的错误。
  })
})

// 拖拽窗口时每帧都重算行列既无收益，还会让 PTY 收到一串没有意义的 SIGWINCH。
let resizeTimer: ReturnType<typeof setTimeout> | undefined
window.addEventListener('resize', () => {
  clearTimeout(resizeTimer)
  resizeTimer = setTimeout(() => {
    resizeTimer = undefined
    fit.fit()
  }, 120)
})

restart.addEventListener('click', () => {
  // 重开走同一条上报路径：主进程发现当前会话已退出，就会拉起新会话。
  started = false
  reportSize()
})

// 订阅先于首次上报：shell 的提示符可能在会话拉起后立刻到达，晚订阅会丢掉开头那一行。
const unsubscribeView = api.subscribe(render)
const unsubscribeOutput = api.output((data) => { terminal.write(data) })
window.addEventListener('pagehide', () => {
  unsubscribeView()
  unsubscribeOutput()
  terminal.dispose()
}, { once: true })

void api.status()
  .then(render)
  .catch((error: unknown) => { console.error('desktop terminal: could not read the initial view', error) })

// 等一帧让样式与字号生效，再量格子与上报；焦点交给终端，打开即可直接输入。
requestAnimationFrame(() => {
  reportSize()
  terminal.focus()
})

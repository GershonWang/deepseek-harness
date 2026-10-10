package tray

import (
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/i18n"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
)

// render 把一份状态翻译成托盘项的可视内容。
//
// 渲染与 D-Bus 分离：菜单标签、提示文字与图标选择都是纯函数，可以在没有会话总线的
// 环境里断言，而 D-Bus 导出只负责把结果发出去。
// @param iconPrefix - 安装前缀，用于定位随包图标。
// @returns 图标位图、提示与 SNI 状态。
func (s trayState) render(iconPrefix string) render {
	text := modeName(s.locale, s.mode)
	if s.switching {
		text += "…"
	}
	// 出错时用 SNI 的 NeedsAttention 让宿主把图标标记为需要关注：托盘没有窗口贴错误
	// 提示，这是唯一能让用户注意到「刚才那一下没成功」的标准手段。
	status := "Active"
	if s.err != "" {
		status = "NeedsAttention"
	}
	return render{
		pixmaps: pixmaps(iconPrefix, s.mode),
		tooltip: toolTip{
			Title: i18n.T(s.locale, "tray.tooltip"),
			Text:  text,
		},
		status: status,
	}
}

// nextMode 给出「切换到另一个形态」的目标：有客户端在跑时是另一个形态，
// 没有客户端时是默认形态之外的另一个——两种情况下用户拿到的都是「另一个」。
// @returns 目标形态。
func (s trayState) nextMode() switchboard.Mode {
	current := s.mode
	if current == "" {
		current = s.defaultMode
	}
	if current == switchboard.ModeOfficial {
		return switchboard.ModeShell
	}
	return switchboard.ModeOfficial
}

// modeName 给出形态的本地化名字。
// @param locale - 语言。
// @param mode - 形态；空串表示没有客户端在运行。
// @returns 用户可见的名字。
func modeName(locale i18n.Locale, mode switchboard.Mode) string {
	switch mode {
	case switchboard.ModeShell:
		return i18n.T(locale, "tray.modeShell")
	case switchboard.ModeOfficial:
		return i18n.T(locale, "tray.modeOfficial")
	default:
		return i18n.T(locale, "tray.noClient")
	}
}

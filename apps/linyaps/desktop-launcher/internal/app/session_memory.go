// Package app - 「上次打开的会话」的记忆
//
// 真源是 GUI：用户选了哪个会话只有页面知道，因此 id 由页面里的链接桥上报、经前端
// 转发到这里存进 config.json。之所以要壳来记：GUI 把它写在 localStorage 里，而内嵌
// iframe 的这份存储退出后不落盘（见 apps/linyaps/desktop-launcher/README.zh.md 的
// 渲染层一节），客户端自己记不住。下次启动由 appenv 经 overlay 的注入插件送回页面。
package app

import (
	"log"
)

// maxSessionIDLength 是记录会话 id 的长度上限。会话 id 形如 `session-<uuid>`，
// 128 已远超真实长度，超出即视为损坏或伪造。
const maxSessionIDLength = 128

// validSessionID 判定会话 id 的形状：长度有界，且只用 id 允许的字符集。
//
// 空串合法，含义是「当前没有会话」，由调用方解释为清除记录。非空值来自页面，是 wire
// 边界上的输入，随后还会被写进 overlay 的插件 config；与其指望下游各自防御，不如在
// 入口处只放行这一个字符集。
//
// @param id 链接桥上报的会话 id。
// @returns 该 id 是否可以记录。
func validSessionID(id string) bool {
	if len(id) > maxSessionIDLength {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

// SaveLastSession 记录 GUI 当前打开的会话；空串清除记录。
//
// 只在取值变化时落盘：链接桥按变化上报，但同一 id 仍可能重复到达（页面重载、消息
// 重放），而每次启动、每次切换都重写 config.json 没有意义。
//
// @param home 用户家目录（配置文件的锚点）。
// @param sessionID 要记录的会话 id，空串表示清除。
// @returns 写盘失败时的错误；无变化时返回 nil。
func SaveLastSession(home, sessionID string) error {
	cfg, _ := LoadAppConfig(home)
	if cfg.LastSessionID == sessionID {
		return nil
	}
	cfg.LastSessionID = sessionID
	return SaveAppConfig(home, cfg)
}

// RememberSession 记录 GUI 当前打开的会话，由 Wails 绑定给前端调用。
//
// 参数为空串表示客户端此刻没有选中任何会话（例如当前会话被归档），此时清除记录：
// 留着上一个 id 会让下次启动打开一段用户已经离开的对话。
//
// 形状非法的 id 一律丢弃并留痕，绝不落盘：它来自页面，而落盘之后会被送进 harness 的
// 插件 config。记忆本身只是体验增强，写失败不打扰用户（前端也忽略返回）。
//
// @param sessionID 链接桥上报的会话 id；空串表示清除。
func (a *App) RememberSession(sessionID string) {
	if !validSessionID(sessionID) {
		log.Printf("丢弃形状非法的会话 id: %q", sessionID)
		return
	}
	if err := SaveLastSession(a.home, sessionID); err != nil {
		log.Printf("会话记忆未落盘: %v", err)
	}
}

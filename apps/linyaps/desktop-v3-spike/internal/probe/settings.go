// 本文件是「壳设置改存 Go 侧」的原型：客户端现在把 locale、字体、终端字号存在壳
// origin 的 localStorage 里，而 v3 的壳 origin 从 wails://wails 变成
// wails://localhost，localStorage 按 origin 隔离，升级后这些值必然读不到。
//
// 为什么用它替代 localStorage：设置属于「应用配置」而不是「页面状态」，存在 Go 侧
// 才能跨框架、跨 origin 稳定；客户端已有 AppConfig 承载窗口几何/主题/端口，
// 扩展它是同一条路。本文件只验证读写的往返与首次迁移的判定逻辑。
package probe

import (
	"os"
	"path/filepath"
)

// settingsPathDefault 是默认落点。容器内 $HOME 只读，因此默认放宿主与容器共享的
// /tmp：它能在多次运行之间存活，正好用来验证「首跑写入、次跑读回」。
const settingsPathDefault = "/tmp/dsh-v3-spike-settings.json"

// SettingsPath 返回壳设置的落盘路径，可用 DSH_V3_SPIKE_SETTINGS 覆盖。
func SettingsPath() string {
	if p := os.Getenv("DSH_V3_SPIKE_SETTINGS"); p != "" {
		return p
	}
	return settingsPathDefault
}

// LoadShellSettings 读取壳设置原文；不存在时返回空串。
//
// 返回原文而不解码：设置结构由壳定义，Go 侧只负责存取，避免两端各维护一份 schema。
func (s *Service) LoadShellSettings() string {
	data, err := os.ReadFile(SettingsPath())
	if err != nil {
		return ""
	}
	return string(data)
}

// SaveShellSettings 写入壳设置原文，返回落盘路径或错误描述。
func (s *Service) SaveShellSettings(payload string) string {
	path := SettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "error: " + err.Error()
	}
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		return "error: " + err.Error()
	}
	return path
}

// Package app - 应用配置持久化
//
// 统一管理桌面端的持久化配置，包括：
//   - 外部服务地址（externalUrl）
//   - 窗口状态（尺寸、最大化等）
//
// 所有配置存储在同一个文件中：~/.config/dsh-desktop/config.json
// 读取时做容错：字段缺失或格式损坏时，对应项回退默认值。
package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WindowState 描述窗口的持久化状态。
type WindowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized"`
	// X、Y 是窗口左上角坐标；同时为 0 表示「没有记录」，因此用 omitempty 省略。
	// 坐标本身可以是负的（副屏在主屏左侧），所以不能用「小于 0 即未记录」来判断。
	X int `json:"x,omitempty"`
	Y int `json:"y,omitempty"`
}

// DefaultWindowState 返回默认窗口状态。
func DefaultWindowState() WindowState {
	return WindowState{
		Width:     1280,
		Height:    800,
		Maximized: false,
	}
}

// AppConfig 是桌面端配置文件的完整结构。
// 所有字段使用指针/零值语义，缺失时回退默认值。
type AppConfig struct {
	ExternalURL string      `json:"externalUrl,omitempty"`
	Window      WindowState `json:"window"`
	// WebPort 是 harness Web 服务最近一次实际使用的监听端口（0 表示没有记录）。
	//
	// 它只服务于**跨启动的身份**：harness Web GUI 的界面状态（含「上次会话」）
	// 按 origin 存在 localStorage 里，而 origin 含端口；端口每次换一个，GUI 就
	// 每次落在空存储里，只能新建会话。因此下次启动优先复用同一个端口，见
	// appenv.Resolve 的 resolvePort。
	WebPort int `json:"webPort,omitempty"`
	// LastSessionID 是 GUI 上次打开的会话 id（空串表示没有记录）。
	//
	// 它只服务于**跨启动的还原**：GUI 把「上次打开的会话」写在 localStorage 里，
	// 而内嵌 iframe 的这份存储退出后不落盘（见 README 的渲染层一节），客户端自己
	// 记不住。壳因此记在自己这里，下次启动由 appenv 的注入插件送回页面；页面若已
	// 打不开这段对话（被删除、归档或换了 harness），客户端退回自己的回退策略。
	LastSessionID string `json:"lastSessionId,omitempty"`
	// Theme 是前端回推的界面主题，取值 "dark"/"light"；空串表示尚未记录。
	//
	// 它只服务于**首帧**：窗口背景色必须在页面绘制之前定下来，而那时前端还没起来，
	// Go 侧读不到 prefers-color-scheme，只能沿用上次记录的值（审计 28）。
	Theme string `json:"theme,omitempty"`
}

// DefaultAppConfig 返回默认配置。
func DefaultAppConfig() AppConfig {
	return AppConfig{
		Window: DefaultWindowState(),
	}
}

// AppConfigFilePath 返回配置文件路径。
func AppConfigFilePath(home string) string {
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			home = h
		} else {
			home = "."
		}
	}
	return filepath.Join(home, ".config", "dsh-desktop", "config.json")
}

// LoadAppConfig 从磁盘读取配置。
//
// 文件不存在或格式损坏时返回默认配置和 nil 错误（静默降级）。
// 单个字段缺失时，该字段使用默认值，其他字段保留读取到的值。
func LoadAppConfig(home string) (AppConfig, error) {
	path := AppConfigFilePath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultAppConfig(), nil
		}
		return DefaultAppConfig(), err
	}

	cfg := DefaultAppConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		// 配置损坏时回退默认值，不打断启动
		return DefaultAppConfig(), nil
	}

	// 窗口尺寸合理性校验
	if cfg.Window.Width < 400 || cfg.Window.Height < 300 {
		cfg.Window = DefaultWindowState()
	}

	// 端口越界（文件被手工改坏）按「没有记录」处理：交给 appenv 另挑一个，
	// 而不是把一个非法端口透传给 harness 让它启动失败。
	if cfg.WebPort < 1 || cfg.WebPort > 65535 {
		cfg.WebPort = 0
	}

	// 会话 id 形状非法同理按「没有记录」处理：这个值会被写进 overlay 的插件 config，
	// 宁可这次不还原，也不让一次手改污染下次启动的启动参数。
	if !validSessionID(cfg.LastSessionID) {
		cfg.LastSessionID = ""
	}

	return cfg, nil
}

// SaveAppConfig 保存配置到磁盘。
//
// 使用原子写入（临时文件 + 重命名），避免进程意外退出时配置损坏。
func SaveAppConfig(home string, cfg AppConfig) error {
	path := AppConfigFilePath(home)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// LoadWindowState 读取配置中的窗口状态。
// 单独的便捷函数，供 main.go 启动时调用。
func LoadWindowState(home string) (WindowState, error) {
	cfg, err := LoadAppConfig(home)
	if err != nil {
		return DefaultWindowState(), err
	}
	return cfg.Window, nil
}

// SaveWindowState 更新配置中的窗口状态并保存。
// 读取现有配置，只修改 window 字段，保留 externalUrl 等其他字段不变。
func SaveWindowState(home string, state WindowState) error {
	cfg, _ := LoadAppConfig(home)
	cfg.Window = state
	return SaveAppConfig(home, cfg)
}

// SaveWebPort 更新配置中记录的 harness 监听端口并保存。
//
// 端口失效（被别的进程占用）时 appenv 会另挑一个，这里记录的是**本次实际使用**的
// 那个：记忆必须收敛到最后的有效端口，否则一旦偏好端口被长期占用，用户会永久停留在
// 「每次启动都是新 origin」的降级状态。port 为 0（本次没拿到具体端口）时不写。
func SaveWebPort(home string, port int) error {
	if port == 0 {
		return nil
	}
	cfg, _ := LoadAppConfig(home)
	if cfg.WebPort == port {
		return nil
	}
	cfg.WebPort = port
	return SaveAppConfig(home, cfg)
}

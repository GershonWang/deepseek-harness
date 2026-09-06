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

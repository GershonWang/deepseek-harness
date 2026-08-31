package toolchain

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectConfig 是项目根目录 .dsh-toolchain.yml 的内容。
type ProjectConfig struct {
	// Tools 是工具 ID → 期望版本 的映射（版本为精确值，如 "1.23.2"）。
	Tools map[string]string
	// AutoSwitch 是否在检测到配置时自动切换已装版本（默认 true）。
	AutoSwitch bool
	// AutoPrompt 是否在缺工具时提示安装（默认 true）。
	AutoPrompt bool
}

// ConfigFileName 是项目级工具配置的文件名。
const ConfigFileName = ".dsh-toolchain.yml"

// ProjectPin 是 ResolveProject 对单个 pin 的判定结果。
type ProjectPin struct {
	ID        string // 工具 ID
	Version   string // 期望版本
	Installed bool   // 期望版本是否已安装
	Active    bool   // 期望版本是否已是激活版本
}

// ResolveProject 向上查找 start 目录的项目配置，并把每个 pin 与 home 下已装
// 工具对照，返回配置与判定结果。未找到配置时返回 nil config、nil pins、nil error。
func ResolveProject(start, home string) (*ProjectConfig, []ProjectPin, error) {
	path, cfg, err := FindProjectConfig(start)
	if err != nil {
		return nil, nil, err
	}
	if cfg == nil {
		return nil, nil, nil
	}
	_ = path
	dir := InstallDir(home)
	pins := make([]ProjectPin, 0, len(cfg.Tools))
	for id, ver := range cfg.Tools {
		p := ProjectPin{ID: id, Version: ver, Installed: IsInstalled(dir, id, ver)}
		p.Active = p.Installed && ActiveVersion(dir, id) == ver
		pins = append(pins, p)
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].ID < pins[j].ID })
	return cfg, pins, nil
}

// ApplyProject 应用项目配置：对已装但未激活的 pin 切换激活版本（受
// auto_switch 控制）。返回实际执行了切换的工具 ID 列表（按字母序），
// 不处理缺失的工具（由调用方提示安装）。
func ApplyProject(start, home string, autoSwitch bool) (switched []string, err error) {
	_, pins, err := ResolveProject(start, home)
	if err != nil || pins == nil {
		return nil, err
	}
	if !autoSwitch {
		return nil, nil
	}
	dir := InstallDir(home)
	for _, p := range pins {
		if p.Installed && !p.Active {
			if err := SetActiveVersion(dir, p.ID, p.Version); err != nil {
				return switched, fmt.Errorf("switch %s to %s: %w", p.ID, p.Version, err)
			}
			switched = append(switched, p.ID)
		}
	}
	return switched, nil
}

// FindProjectConfig 从 start 目录向上逐级查找 .dsh-toolchain.yml，返回配置
// 文件绝对路径与解析结果。未找到时返回 ("", nil, nil)。
func FindProjectConfig(start string) (string, *ProjectConfig, error) {
	dir := start
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", nil, err
		}
	}
	dir = filepath.Clean(dir)
	for {
		candidate := filepath.Join(dir, ConfigFileName)
		if data, err := os.ReadFile(candidate); err == nil {
			cfg, perr := ParseProjectConfig(data)
			if perr != nil {
				return candidate, nil, fmt.Errorf("parse %s: %w", candidate, perr)
			}
			return candidate, cfg, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil, nil // 已到根目录
		}
		dir = parent
	}
}

// ParseProjectConfig 解析 .dsh-toolchain.yml 的已知子集：
//
//	# 注释
//	tools:
//	  go: "1.23.2"      # 内联注释
//	  gcc: 12.3.0
//	auto_switch: true
//	auto_prompt: false
//
// 支持单/双引号包裹的值、空行、内联 # 注释；未知键被忽略，未知的顶层
// 布尔键不影响解析。解析失败返回错误，调用方据此提示用户修正配置。
func ParseProjectConfig(data []byte) (*ProjectConfig, error) {
	cfg := &ProjectConfig{Tools: map[string]string{}, AutoSwitch: true, AutoPrompt: true}
	inTools := false
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		indented := len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t')
		key, val, ok := splitKV(line)
		if !ok {
			return nil, fmt.Errorf("line %d: 无法解析 %q", i+1, line)
		}
		if key == "tools" {
			if strings.TrimSpace(val) != "" {
				return nil, fmt.Errorf("line %d: tools 应为 section 头（tools:）", i+1)
			}
			inTools = true
			continue
		}
		if inTools && indented {
			// tools section 内缩进的键值对：工具 ID → 版本。
			cfg.Tools[key] = unquote(val)
			continue
		}
		// 非缩进行：离开 tools section，按顶层键处理。
		inTools = false
		switch key {
		case "auto_switch":
			b, err := parseBool(val)
			if err != nil {
				return nil, fmt.Errorf("line %d: auto_switch: %w", i+1, err)
			}
			cfg.AutoSwitch = b
		case "auto_prompt":
			b, err := parseBool(val)
			if err != nil {
				return nil, fmt.Errorf("line %d: auto_prompt: %w", i+1, err)
			}
			cfg.AutoPrompt = b
		default:
			// 未知键忽略（向前兼容）。
		}
	}
	return cfg, nil
}

// splitKV 拆 "key: value"；去掉行尾注释与引号。value 为空合法（如 "tools:"）。
func splitKV(line string) (key, val string, ok bool) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:i])
	val = strings.TrimSpace(line[i+1:])
	if key == "" {
		return "", "", false
	}
	return key, stripComment(val), true
}

// stripComment 去掉行尾 # 注释（引号内的 # 保留，已足够处理本 schema）。
func stripComment(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// unquote 去掉首尾成对的单/双引号。
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// parseBool 解析 true/false（大小写不敏感）。
func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("需要 true 或 false，得到 %q", s)
	}
}

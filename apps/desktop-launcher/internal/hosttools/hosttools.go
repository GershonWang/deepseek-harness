// Package hosttools 管理"宿主命令挂载"：把宿主机已安装的工具链目录通过
// linglong config.d 挂载进沙箱，让容器内命令可用。只在玲珑打包（沙箱）环境有意义。
//
// 挂载约定：宿主路径绑定到容器内 /opt/host-tools/<name>，launcher 启动时把
// /opt/host-tools/*/bin 注入子进程 PATH（优先级高于按需安装与内置）。
package hosttools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// AppID 是玲珑应用 id（config.d 目录按它定位）。
const AppID = "com.deepseek.dsh-desktop"

// MountBase 是容器内宿主工具链的挂载基址。
const MountBase = "/opt/host-tools"

// filePrefix 是本模块写入的配置文件前缀。
const filePrefix = "30-host-tools-"

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Entry 是一条宿主工具链挂载配置。
type Entry struct {
	Name   string // 标识（配置文件与挂载目录名）
	Source string // 宿主路径
	Target string // 容器内挂载路径 /opt/host-tools/<name>
}

// mountJSON 与 linglong config.d 的挂载配置格式一致。
type mountJSON struct {
	Mounts []struct {
		Type        string   `json:"type"`
		Source      string   `json:"source"`
		Destination string   `json:"destination"`
		Options     []string `json:"options"`
	} `json:"mounts"`
}

// ConfigDir 返回玲珑每应用 config.d 目录。
func ConfigDir(home string) string {
	return filepath.Join(home, ".config", "linglong", "apps", AppID, "config.d")
}

func targetFor(name string) string { return filepath.Join(MountBase, name) }

func fileFor(dir, name string) string { return filepath.Join(dir, filePrefix+name+".json") }

// SanitizeName 归一化挂载名称（派生自宿主路径时用）：非字母数字字符转 '-'、
// 折叠连续 '-'、截断到 32 字符内。
func SanitizeName(raw string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !ok {
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
			continue
		}
		prevDash = false
		b.WriteRune(r)
		if b.Len() >= 32 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// SuggestName 从宿主路径末段派生默认名称。
func SuggestName(source string) string {
	return SanitizeName(filepath.Base(strings.TrimRight(source, "/")))
}

// Add 校验宿主路径并把挂载配置写入 config.d。name 为空时由 source 派生。
func Add(home, name, source string) (Entry, error) {
	if name == "" {
		name = SuggestName(source)
	}
	name = SanitizeName(name)
	if !namePattern.MatchString(name) {
		return Entry{}, fmt.Errorf("挂载名称非法（仅允许小写字母/数字/连字符，长度≤32）: %q", name)
	}
	info, err := os.Stat(source)
	if err != nil {
		return Entry{}, fmt.Errorf("宿主路径不可访问: %v", err)
	}
	if !info.IsDir() {
		return Entry{}, fmt.Errorf("宿主路径不是目录: %s", source)
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return Entry{}, err
	}
	dir := ConfigDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Entry{}, err
	}
	m := mountJSON{
		Mounts: []struct {
			Type        string   `json:"type"`
			Source      string   `json:"source"`
			Destination string   `json:"destination"`
			Options     []string `json:"options"`
		}{{Type: "bind", Source: abs, Destination: targetFor(name), Options: []string{"ro", "bind"}}},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Entry{}, err
	}
	if err := os.WriteFile(fileFor(dir, name), data, 0o644); err != nil {
		return Entry{}, err
	}
	return Entry{Name: name, Source: abs, Target: targetFor(name)}, nil
}

// List 读取 config.d 里已有的宿主工具链挂载配置。
func List(home string) []Entry {
	dir := ConfigDir(home)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Entry
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), filePrefix) || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(e.Name(), filePrefix), ".json")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m mountJSON
		if json.Unmarshal(data, &m) != nil || len(m.Mounts) == 0 {
			continue
		}
		out = append(out, Entry{Name: name, Source: m.Mounts[0].Source, Target: m.Mounts[0].Destination})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// EffectiveBin 返回挂载源的"生效 bin 目录"：优先 <source>/bin，否则当
// source 本身（bin 目录或单二进制目录）直接含可执行文件时用 source。
// 与 appenv 对容器内 /opt/host-tools/<name> 的扫描规则保持一致。
func EffectiveBin(source string) string {
	sub := filepath.Join(source, "bin")
	if info, err := os.Stat(sub); err == nil && info.IsDir() {
		return sub
	}
	if hasExecutable(source) {
		return source
	}
	return sub
}

// hasExecutable 判断目录是否直接含至少一个可执行文件。
func hasExecutable(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

// Remove 删除指定挂载配置。
func Remove(home, name string) error {
	name = SanitizeName(name)
	if !namePattern.MatchString(name) {
		return fmt.Errorf("挂载名称非法: %q", name)
	}
	err := os.Remove(fileFor(ConfigDir(home), name))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

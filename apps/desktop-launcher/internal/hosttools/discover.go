package hosttools

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Discovered 是宿主工具链扫描发现的一个候选根目录。
type Discovered struct {
	Name    string // 建议挂载名（由根目录名派生）
	Source  string // 宿主根目录绝对路径
	Tool    string // 探测到的主命令名（如 go / node）
	Version string // 探测到的版本（未探测到为空）
}

// maxDiscovered 是单次扫描结果上限，防目录异常导致海量条目。
const maxDiscovered = 100

// probeTimeout 是单次版本探测上限。
const probeTimeout = 3 * time.Second

// DefaultRootDirs 返回常见宿主工具链根目录候选。固定 home 相对项直接展开，
// /opt、/usr/local、~/.rustup/toolchains、~/.nvm/versions/node 按子目录枚举。
func DefaultRootDirs(home string) []string {
	dirs := []string{
		filepath.Join(home, "tools"),
		filepath.Join(home, "sdk"),
		filepath.Join(home, "go"),
		filepath.Join(home, "miniconda3"),
		filepath.Join(home, "anaconda3"),
		filepath.Join(home, ".cargo"),
	}
	dirs = append(dirs, subdirs("/opt")...)
	dirs = append(dirs, subdirs("/usr/local")...)
	dirs = append(dirs, subdirs(filepath.Join(home, ".rustup", "toolchains"))...)
	dirs = append(dirs, subdirs(filepath.Join(home, ".nvm", "versions", "node"))...)
	return dirs
}

// subdirs 列出目录的直接子目录（按名排序），不存在时返回空。
func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// Discover 扫描候选目录，返回含可执行命令的工具链根目录及其版本。跳过
// 不存在的路径、无 bin/ 且无直接可执行文件的目录；结果按名称排序。
func Discover(dirs []string) []Discovered {
	var out []Discovered
	seen := map[string]bool{}
	for _, dir := range dirs {
		if len(out) >= maxDiscovered {
			break
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		tool, exe := primaryExecutable(abs)
		if exe == "" {
			continue
		}
		d := Discovered{Name: SuggestName(abs), Source: abs, Tool: tool}
		if v, ok := Probe(exe); ok {
			d.Version = v
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// primaryExecutable 找根目录的主命令：优先 <root>/bin/<basename>，再取
// <root>/bin 下第一个可执行文件，最后取根目录直接的可执行文件。返回
// (命令名, 绝对路径)，未找到返回 ("", "")。
func primaryExecutable(root string) (string, string) {
	base := filepath.Base(root)
	bin := filepath.Join(root, "bin")
	if info, err := os.Stat(bin); err == nil && info.IsDir() {
		if exe := filepath.Join(bin, base); isExecutable(exe) {
			return base, exe
		}
		if exe := firstExecutable(bin); exe != "" {
			return filepath.Base(exe), exe
		}
	}
	if exe := filepath.Join(root, base); isExecutable(exe) {
		return base, exe
	}
	if exe := firstExecutable(root); exe != "" {
		return filepath.Base(exe), exe
	}
	return "", ""
}

// firstExecutable 返回目录下第一个可执行文件的绝对路径，无则空。
func firstExecutable(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if isExecutable(p) {
			return p
		}
	}
	return ""
}

// isExecutable 判断路径是否是可执行的普通文件。
func isExecutable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// Probe 探测可执行文件的版本：运行 "<exe> --version"（3s 超时），返回
// 首行文本。命令不存在、超时或非零退出均视为未探测到。
func Probe(exe string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--version")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", false
	}
	out := strings.TrimSpace(buf.String())
	if out == "" {
		return "", false
	}
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	return strings.TrimSpace(out), true
}

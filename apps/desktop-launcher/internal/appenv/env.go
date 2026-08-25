// Package appenv 解析 launcher 的运行环境：harness 可执行文件、端口、日志
// 目录，并为子进程准备环境变量。纯 Go，无 GUI 依赖。
package appenv

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/hosttools"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/supervisor"
)

// Resolved 是一次环境解析的结果。
type Resolved struct {
	Config supervisor.Config
	Port   string
}

// Resolve 按优先级解析子进程环境：
//  1. DSH_DESKTOP_DSH_BIN 环境变量（开发调试）
//  2. $PREFIX/harness/lib/bin.js 存在（打包态）
//  3. repo 内 apps/cli/lib/bin.js 存在（开发态）
//  4. 回退：node + 当前目录 bin.js
func Resolve() Resolved {
	port := resolvePort()
	logDir := resolveLogDir()

	if bin := os.Getenv("DSH_DESKTOP_DSH_BIN"); bin != "" {
		return Resolved{
			Config: supervisor.Config{Command: bin, Args: []string{"web", "--port", port}, LogDir: logDir},
			Port:   port,
		}
	}

	exe, _ := os.Executable()
	prefix := filepath.Dir(filepath.Dir(exe)) // .../files/bin -> .../files
	packagedBin := filepath.Join(prefix, "harness", "lib", "bin.js")
	if _, err := os.Stat(packagedBin); err == nil {
		// 打包态优先用捆绑的 Node（harness 需要 Node >=24，宿主 PATH 的 node
		// 可能是 20.x 跑不起来）。
		node := filepath.Join(prefix, "node", "bin", "node")
		if _, statErr := os.Stat(node); statErr != nil {
			node = resolveNode()
		}
		return Resolved{
			Config: supervisor.Config{Command: node, Args: []string{packagedBin, "web", "--port", port}, LogDir: logDir},
			Port:   port,
		}
	}

	cwd, _ := os.Getwd()
	devBin := filepath.Join(cwd, "..", "cli", "lib", "bin.js")
	if _, err := os.Stat(devBin); err == nil {
		return Resolved{
			Config: supervisor.Config{Command: resolveNode(), Args: []string{devBin, "web", "--port", port}, LogDir: logDir},
			Port:   port,
		}
	}

	return Resolved{
		Config: supervisor.Config{Command: resolveNode(), Args: []string{"bin.js", "web", "--port", port}, LogDir: logDir},
		Port:   port,
	}
}

// resolvePort 稳定端口：harness 重启时复用同一端口，GUI 才能重连。
func resolvePort() string {
	port := os.Getenv("DSH_DESKTOP_PORT")
	if port == "" || port == "0" {
		port = reservePort()
	}
	return port
}

// resolveLogDir 返回 harness 日志目录。
func resolveLogDir() string {
	if dir := os.Getenv("DSH_DESKTOP_LOG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "dsh-desktop")
}

// resolveNode 返回 node 可执行文件路径；DSH_DESKTOP_NODE 优先，否则用 PATH。
func resolveNode() string {
	if n := os.Getenv("DSH_DESKTOP_NODE"); n != "" {
		return n
	}
	if runtime.GOOS == "windows" {
		return "node.exe"
	}
	return "node"
}

// hostToolsBase 是宿主工具链的容器内挂载基址（测试可覆盖）。
var hostToolsBase = hosttools.MountBase

// packagedGitExecPath 返回随包 git-core 目录（形如 <files>/lib/git-core，由
// 可执行文件位置推导，任意机器一致）；仅当该目录存在时返回 true。打包态 git
// 的编译期 exec-path 指向 /usr/lib/git-core（容器内不存在），必须显式指回包内。
func packagedGitExecPath(exe string) (string, bool) {
	prefix := filepath.Dir(filepath.Dir(exe))
	gitCore := filepath.Join(prefix, "lib", "git-core")
	info, err := os.Stat(gitCore)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return gitCore, true
}

// ConfigureChildEnv 设置子进程（harness 及其后代）需要的环境变量。
// PATH 优先级：宿主挂载(/opt/host-tools/*/bin) > 按需安装(~/.dsh-tools/bin) > 现有 PATH。
func ConfigureChildEnv(home string) {
	_ = os.Setenv("GTK_A11Y", "none")
	_ = os.Setenv("DSH_DIRECTORY_PICKER", "browse")

	if exe, err := os.Executable(); err == nil {
		if gitCore, ok := packagedGitExecPath(exe); ok {
			_ = os.Setenv("GIT_EXEC_PATH", gitCore)
		}
	}

	segs := []string{}
	if bins := hostToolBins(hostToolsBase); len(bins) > 0 {
		segs = append(segs, bins...)
	}
	bin, lib := dshToolsEnv(home)
	if info, err := os.Stat(bin); err == nil && info.IsDir() {
		segs = append(segs, bin)
	}
	if len(segs) > 0 {
		_ = os.Setenv("PATH", strings.Join(append(segs, os.Getenv("PATH")), string(os.PathListSeparator)))
	}
	if info, err := os.Stat(lib); err == nil && info.IsDir() {
		if old := os.Getenv("LD_LIBRARY_PATH"); old != "" {
			_ = os.Setenv("LD_LIBRARY_PATH", lib+string(os.PathListSeparator)+old)
		} else {
			_ = os.Setenv("LD_LIBRARY_PATH", lib)
		}
	}
}

// hostToolBins 扫描宿主挂载基址下各工具链的生效 bin 目录（按名字排序）。
// 优先 <dir>/bin；若目录本身直接含可执行文件（用户粘贴的 bin 目录），
// 则用目录本身。
func hostToolBins(base string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		bin := filepath.Join(dir, "bin")
		if info, err := os.Stat(bin); err == nil && info.IsDir() {
			out = append(out, bin)
			continue
		}
		if hasExecutable(dir) {
			out = append(out, dir)
		}
	}
	sort.Strings(out)
	return out
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

// dshToolsEnv 返回按需工具目录的 PATH 与 LD_LIBRARY_PATH 段（home/.dsh-tools）。
func dshToolsEnv(home string) (pathSeg, ldSeg string) {
	return filepath.Join(home, ".dsh-tools", "bin"), filepath.Join(home, ".dsh-tools", "lib")
}

// reservePort 选一个空闲的 loopback 端口并返回其字符串。
// 监听随即关闭，之后子进程再 bind 存在极小竞态，对本地 harness 足够稳。
func reservePort() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "0"
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return fmt.Sprintf("%d", port)
}

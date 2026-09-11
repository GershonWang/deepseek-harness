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
//
// 四条分支都经 harnessArgs 组装参数，以保证监护声明 overlay 无一遗漏。
func Resolve() Resolved {
	port := resolvePort()
	logDir := resolveLogDir()

	if bin := os.Getenv("DSH_DESKTOP_DSH_BIN"); bin != "" {
		return Resolved{
			Config: supervisor.Config{Command: bin, Args: harnessArgs("", port), LogDir: logDir},
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
			Config: supervisor.Config{Command: node, Args: harnessArgs(packagedBin, port), LogDir: logDir},
			Port:   port,
		}
	}

	cwd, _ := os.Getwd()
	devBin := filepath.Join(cwd, "..", "cli", "lib", "bin.js")
	if _, err := os.Stat(devBin); err == nil {
		return Resolved{
			Config: supervisor.Config{Command: resolveNode(), Args: harnessArgs(devBin, port), LogDir: logDir},
			Port:   port,
		}
	}

	return Resolved{
		Config: supervisor.Config{Command: resolveNode(), Args: harnessArgs("bin.js", port), LogDir: logDir},
		Port:   port,
	}
}

// supervisorOverlayName 是 launcher 写入运行时目录、并在每次 spawn 时传给
// harness 的 patch overlay 文件名。
const supervisorOverlayName = "supervisor-overlay.yml"

// supervisorOverlayBody 是 overlay 的内容：声明 harness 由本 launcher 的
// Supervisor 监护，因此插件市场不得再自行重启。
//
// 必须禁用的原因：dsh-market 插件的"立即重启"端点会 spawn 一个 detached
// helper，用复用的 argv（含同一个稳定 --port，见 resolvePort）拉起替代进程；
// 而本 launcher 的监护循环在 harness 退出后同样会用该端口重启。两者对同一个
// 端口竞态，先 bind 的胜出，另一方以 EADDRINUSE 退出。若监护循环屡次败给对方，
// 它会在 StartupTimeoutMs 后进入失败态并弹诊断，而诊断只检查自己 spawn 的
// 进程，看不到 helper 写在 tmpdir 的 dsh-market-restart-*.err.log——用户因此
// 看到"服务启动失败"却又"检测不到故障点"。让监护循环成为唯一的重启者，
// 竞态的前提即不存在。
//
// 依赖的第三方契约：entry id `dsh-market` 与配置字段 `allowRestart`。上游若改名
// 或移除，patch 匹配不到行只会产生一条 include 警告（不会阻止启动），但本
// 保护随之失效——这是选择用 overlay 而非改 dsh 源码的代价，改这里需同步核对
// dsh-market 的 settings 命名空间。
const supervisorOverlayBody = `# 由 dsh-desktop-launcher 生成，请勿手工编辑。
# harness 由 launcher 的 Supervisor 监护（spawn、重启、端口都归它管），
# 插件市场不得再自行重启：两者会用同一个 --port 竞态，先 bind 的胜出，
# 另一方 EADDRINUSE 退出。
- id: dsh-market
  config:
    allowRestart: false
`

// harnessArgs 组装 harness 的启动参数：入口脚本（可为空）、web 子命令、声明
// 监护关系的 overlay，以及稳定端口。
//
// 集中在一处是因为 Resolve 有四条入口分支，任何一条漏掉 overlay 都会让那条
// 路径重新引入与 dsh-market 的端口竞态。
//
// 顺序不可调换：`web` 子命令启用了 passThroughOptions，而 `--port` 是 web app
// 自己的 flag 而非 launcher 的选项，它一旦出现，其后所有参数都会原样透传给
// app——排在它之后的 `--patch` 不会再被解析，overlay 静默失效（实测报
// `error: unknown option '--patch'`）。因此 launcher 自己的 flag 必须先于
// `--port`。
//
// overlay 写失败时静默省略该参数：监护本身仍然有效，只是回到"两个重启者"
// 的旧行为，比让 harness 因缺失参数而启动不起来更可取。
func harnessArgs(entry, port string) []string {
	args := make([]string, 0, 6)
	if entry != "" {
		args = append(args, entry)
	}
	args = append(args, "web")
	if overlay, err := writeSupervisorOverlay(); err == nil {
		args = append(args, "--patch", overlay)
	}
	return append(args, "--port", port)
}

// writeSupervisorOverlay 把监护声明写入 launcher 运行时目录，返回其路径。
// 内容固定，每次启动覆写即可，无需比较或保留旧版本。
func writeSupervisorOverlay() (string, error) {
	path := filepath.Join(resolveLogDir(), supervisorOverlayName)
	if err := os.WriteFile(path, []byte(supervisorOverlayBody), 0o644); err != nil {
		return "", err
	}
	return path, nil
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

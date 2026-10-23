// Package appenv 解析 launcher 的运行环境：harness 可执行文件、端口、日志
// 目录，并为子进程准备环境变量。纯 Go，无 GUI 依赖。
package appenv

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/hosttools"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/linglonghost"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/supervisor"
)

// Preferences 是 launcher 跨启动记住的客户端状态，决定本次如何起 harness 与 GUI。
//
// 两项都不是「用户偏好」，而是本次运行要还原的事实：端口是 harness Web GUI 的
// origin 身份，会话 id 是 GUI 上次打开的那段对话。零值表示没有记录。
type Preferences struct {
	// Port 是上次实际使用的监听端口（0 表示没有记录）。
	Port int
	// SessionID 是上次 GUI 中打开的会话（空串表示没有记录）。
	SessionID string
}

// Resolved 是一次环境解析的结果。
type Resolved struct {
	Config supervisor.Config
	Port   string
	// RememberPort 表示 Port 应当被调用方记为下次启动的首选端口：端口决定 harness
	// Web GUI 的 origin，而 GUI 的界面状态（含「上次会话」）按 origin 存在
	// localStorage 里，端口每次变就等于每次从空存储开始。
	//
	// 环境变量覆盖时为 false：那是本次运行的显式指定（`0` 还要求随机端口），写回
	// 会把一次调试选定的端口变成后续所有启动的稳定端口。
	RememberPort bool
	// DoctorNode 运行 doctor 的 node 可执行文件。
	DoctorNode string
	// DoctorCLI doctor 的 cli.js 路径；空表示本次安装未随包提供 doctor。
	DoctorCLI string
	// InstallAnchor 安装根清单（`@deepseek-ai/dsh` 的 package.json）路径；空表示
	// 本次布局里找不到安装根，doctor 自行按位置推导。它决定 doctor 的插件检查与
	// 真实启动是否用同一个安装根，进而决定运行时解析闭包（详见 DoctorEnv）。
	InstallAnchor string
}

// DoctorEnv 返回必须显式传给 doctor 子进程的环境条目。
//
// 目前只有安装锚点 `DSH_DESKTOP_INSTALL_ANCHOR`：doctor 用它区分「安装自带」与
// 「本 profile 选装」的 bundle，两者分界错了，检查会把能正常加载的 bundle 报成
// 缺依赖，修复也会去停用无辜的插件。锚点为空（找不到安装根）时不传，让 doctor
// 回退到自己的位置推导——预检是尽力而为的前置检查，不该因为这一条而中断。
//
// @returns 形如 `KEY=VALUE` 的环境条目；没有可传的内容时返回 nil。
func (r Resolved) DoctorEnv() []string {
	if r.InstallAnchor == "" {
		return nil
	}
	return []string{InstallAnchorEnv + "=" + r.InstallAnchor}
}

// Resolve 按优先级解析子进程环境：
//  1. DSH_DESKTOP_DSH_BIN 环境变量（开发调试）
//  2. $PREFIX/harness/lib/bin.js 存在（打包态）
//  3. repo 内 apps/cli/lib/bin.js 存在（开发态）
//  4. 回退：node + 当前目录 bin.js
//
// 四条分支都经 harnessArgs 组装参数，以保证监护声明 overlay 无一遗漏。
//
// prefs 是调用方上次记下的客户端状态：端口只在环境变量未覆盖时参与选择（见
// resolvePort，选中的端口是否需要记回去由 Resolved.RememberPort 说明），会话 id
// 则交给注入插件的 overlay 行（见 restore_session.go）。
//
// doctor 的运行环境独立解析（见 resolveDoctor）：它不再挂在 dsh 的 argv 上，
// 因此不能从上面的分支里顺带推导。doctor 的安装锚点同理（见 resolveInstallAnchor）：
// 它必须指向上面这些分支实际启动的那次安装，而从分支里顺带推导会把两者绑死在
// 同一段代码上，改一处就得同时改另一处。
func Resolve(prefs Preferences) Resolved {
	port, rememberPort := resolvePort(prefs.Port)
	logDir := resolveLogDir()
	doctorNode, doctorCLI := resolveDoctor()
	installAnchor := resolveInstallAnchor()

	if bin := os.Getenv("DSH_DESKTOP_DSH_BIN"); bin != "" {
		return Resolved{
			Config:       supervisor.Config{Command: bin, Args: harnessArgs("", port, prefs.SessionID), LogDir: logDir},
			Port:         port,
			RememberPort: rememberPort,
			DoctorNode:   doctorNode,
			DoctorCLI:    doctorCLI,
			// 安装锚点与 harness 入口同源：打包态是 harness 树，开发态是 apps/cli。
			InstallAnchor: installAnchor,
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
			Config:       supervisor.Config{Command: node, Args: harnessArgs(packagedBin, port, prefs.SessionID), LogDir: logDir},
			Port:         port,
			RememberPort: rememberPort,
			DoctorNode:   node,
			DoctorCLI:    doctorCLI,
			// 安装锚点与 harness 入口同源：打包态是 harness 树，开发态是 apps/cli。
			InstallAnchor: installAnchor,
		}
	}

	cwd, _ := os.Getwd()
	devBin := filepath.Join(cwd, "..", "cli", "lib", "bin.js")
	if _, err := os.Stat(devBin); err == nil {
		return Resolved{
			Config:       supervisor.Config{Command: resolveNode(), Args: harnessArgs(devBin, port, prefs.SessionID), LogDir: logDir},
			Port:         port,
			RememberPort: rememberPort,
			DoctorNode:   resolveNode(),
			DoctorCLI:    doctorCLI,
			// 安装锚点与 harness 入口同源：打包态是 harness 树，开发态是 apps/cli。
			InstallAnchor: installAnchor,
		}
	}

	return Resolved{
		Config:        supervisor.Config{Command: resolveNode(), Args: harnessArgs("bin.js", port, prefs.SessionID), LogDir: logDir},
		Port:          port,
		RememberPort:  rememberPort,
		DoctorNode:    resolveNode(),
		DoctorCLI:     doctorCLI,
		InstallAnchor: installAnchor,
	}
}

// doctorNodeEnv / doctorCLIEnv 允许在非标准布局下显式指定 doctor 的运行环境
// （开发调试、CI 冒烟）。两者独立：只覆盖 node 而不覆盖 cli.js（或反之）是
// 合法用法，因此不做成对校验。
const (
	doctorNodeEnv = "DSH_DESKTOP_DOCTOR_NODE"
	doctorCLIEnv  = "DSH_DESKTOP_DOCTOR_CLI"
)

// resolveDoctor 解析 doctor 的运行环境，返回 node 可执行文件与 cli.js 路径。
//
// doctor 不再经 `dsh doctor` 子命令分发，因此它的位置与 dsh 的入口脚本位置
// 无关，必须独立探测。两个布局的 doctor 位置不同：
//   - 打包态：doctor 由 prepare-offline.sh stage 到 harness 树内
//     （<prefix>/harness/doctor/...）。放在 harness 树**内部**而不是旁边，是为了
//     让 doctor 对 @deepseek-ai/dsh-* 的 import 靠 Node 逐级向上查找直接命中
//     <prefix>/harness/node_modules，无需任何额外符号链接。
//   - 开发态：launcher 的 cwd 是 apps/linyaps/desktop-launcher，doctor 就在它下面
//     （与上面 devBin 分支同一前提）。
//
// 找不到时返回空 CLI 而不是报错：预检是尽力而为的前置检查，doctor 缺失应降级为
// 一条可呈现的诊断失败（preflight.DoctorNotConfigured），而不是让 launcher 起不来。
func resolveDoctor() (node, cli string) {
	node = resolveNode()
	if v := os.Getenv(doctorNodeEnv); v != "" {
		node = v
	}
	if v := os.Getenv(doctorCLIEnv); v != "" {
		return node, v
	}

	exe, _ := os.Executable()
	prefix := filepath.Dir(filepath.Dir(exe))
	packaged := filepath.Join(prefix, "harness", "doctor", "lib", "types", "cli.js")
	if _, err := os.Stat(packaged); err == nil {
		return node, packaged
	}

	cwd, _ := os.Getwd()
	dev := filepath.Join(cwd, "doctor", "lib", "types", "cli.js")
	if _, err := os.Stat(dev); err == nil {
		return node, dev
	}

	return node, ""
}

// InstallAnchorEnv 允许在非标准布局下显式指定安装锚点（开发调试、CI 冒烟）。
// 导出是因为它由本包解析、由 preflight 组装进 doctor 子进程环境：跨进程契约只能
// 有一个字面量来源，两处各写一遍迟早会分叉。
const InstallAnchorEnv = "DSH_DESKTOP_INSTALL_ANCHOR"

// resolveInstallAnchor 解析安装锚点：安装根 `@deepseek-ai/dsh` 的 package.json。
//
// doctor 用锚点算 bundle 的运行时解析闭包，并划出「安装自带」与「本 profile 选装」
// 的分界；真实启动的锚点是 CLI 包自己的清单（apps/cli/src/profile-boot.ts 的
// INSTALL_ANCHOR）。两者必须是同一个包，否则安装自带的 optional bundle 会被判成
// 依赖缺失，检查报出并不存在的启动失败。
//
// 解析顺序与 harness 入口一致：显式覆盖 → 打包态 <prefix>/harness → 开发态
// apps/cli。返回空表示本次布局里找不到安装根：doctor 于是回退到自己的位置推导，
// 这条降级路径的理由同 resolveDoctor——预检是尽力而为的前置检查。
//
// 只确认路径存在，不校验清单里的包名：名字不符由 doctor fail loud（它的报错带
// 上路径与包名），launcher 不做第二处校验，避免同一约束两个判定点。
func resolveInstallAnchor() string {
	if v := os.Getenv(InstallAnchorEnv); v != "" {
		return v
	}

	exe, _ := os.Executable()
	prefix := filepath.Dir(filepath.Dir(exe))
	packaged := filepath.Join(prefix, "harness", "package.json")
	if _, err := os.Stat(packaged); err == nil {
		return packaged
	}

	cwd, _ := os.Getwd()
	dev := filepath.Join(cwd, "..", "cli", "package.json")
	if _, err := os.Stat(dev); err == nil {
		return dev
	}

	return ""
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
// extraRows 是本层附加的插入行（当前只有启动进度上报插件，见
// startup_progress.go）；传空串表示该能力不可用，overlay 仍保留监护声明。
func supervisorOverlayBody(extraRows string) string {
	return `# 由 dsh-desktop-launcher 生成，请勿手工编辑。
# harness 由 launcher 的 Supervisor 监护（spawn、重启、端口都归它管），
# 插件市场不得再自行重启：两者会用同一个 --port 竞态，先 bind 的胜出，
# 另一方 EADDRINUSE 退出。
- id: dsh-market
  config:
    allowRestart: false
` + directoryPickerOverlayRows + extraRows
}

// directoryPickerOverlayRows 把目录选择器固定为壳内嵌的浏览列表。
//
// 上游 web-app bundle 装的是 `directory-picker-auto`：在 loopback + 本地图形会话
// 下它会选宿主原生对话框，而对话框在不同发行版的 WebKitGTK 与沙箱里表现不一，
// 壳无法控制。disable + insert 与上游 apps/web/tests/pin-browse-picker.overlay.yml
// 完全一致，因此只依赖上游已发布的两个后端包，不改 harness 源码。
const directoryPickerOverlayRows = `- id: directory-picker
  disabled: true
- insert:
    - id: directory-picker-browse
      name: '@deepseek-ai/dsh-host-directory-picker-browse'
    - id: ui-directory-picker-browse
      name: '@deepseek-ai/dsh-client-ui-directory-picker-browse'
`

// harnessArgs 组装 harness 的启动参数：入口脚本（可为空）、web 子命令、声明
// 监护关系的 overlay（内含目录选择器固定、启动进度上报与「上次会话」恢复三个
// 注入项）、稳定端口，以及关闭「打开默认浏览器」。
//
// 集中在一处是因为 Resolve 有四条入口分支，任何一条漏掉 overlay 都会让那条
// 路径重新引入与 dsh-market 的端口竞态。
//
// 顺序不可调换：`web` 子命令启用了 passThroughOptions，而 `--port` 与 `--no-open`
// 都是 web app 自己的 flag 而非 launcher 的选项，它们一旦出现，其后所有参数都会
// 原样透传给 app——排在 `--port` 之后的 `--patch` 不会再被解析，overlay 静默失效
// （实测报 `error: unknown option '--patch'`）。因此 launcher 自己的 flag 必须先于
// `--port`，而 web app 的 flag 只能排在 `--port` 之后。
//
// 带上 `--no-open`：GUI 由壳嵌在自己的 iframe 里，让 harness 再去拉起系统默认
// 浏览器只会在每次启动时多开一个窗口，而那个窗口的 origin 是本次才挑的端口，
// 与壳里的界面互不相干。
//
// overlay 写失败时静默省略该参数：监护本身仍然有效，只是回到"两个重启者"
// 的旧行为，比让 harness 因缺失参数而启动不起来更可取。
func harnessArgs(entry, port, sessionID string) []string {
	args := make([]string, 0, 7)
	if entry != "" {
		args = append(args, entry)
	}
	args = append(args, "web")
	if overlay, err := writeSupervisorOverlay(sessionID); err == nil {
		args = append(args, "--patch", overlay)
	}
	return append(args, "--port", port, "--no-open")
}

// writeSupervisorOverlay 把监护声明写入 launcher 运行时目录，返回其路径。
// 内容每次启动覆写即可，无需比较或保留旧版本。
//
// 三个注入项（目录选择器固定、启动进度上报、会话恢复）在同一处写入并作为插入行
// 附加：各自独立降级，任一步失败都只失去该能力（监护仍有效），不阻断 harness 启动。
func writeSupervisorOverlay(sessionID string) (string, error) {
	path := filepath.Join(resolveLogDir(), supervisorOverlayName)
	progressPath, _ := writeStartupProgressPlugin()
	restorePath, _ := writeRestoreSessionPlugin()
	rows := startupProgressOverlayRow(progressPath) + restoreSessionOverlayRow(restorePath, sessionID)
	if err := os.WriteFile(path, []byte(supervisorOverlayBody(rows)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// portEnv 覆盖本次运行的监听端口：显式值直接使用，`0` 表示由系统挑一个空闲端口。
// 两者都不参与端口记忆，理由见 resolvePort。
const portEnv = "DSH_DESKTOP_PORT"

// resolvePort 选择 harness 的监听端口，并说明该端口是否应被调用方记下来。
//
// 优先级：
//  1. `DSH_DESKTOP_PORT=0`：本次随机挑一个空闲端口，不读也不写端口记忆；
//  2. `DSH_DESKTOP_PORT=<n>`：本次就用 n（非法值照旧原样透传给 harness 报错），
//     同样不写记忆——环境变量是本次运行的显式指定；
//  3. preferredPort（上次记下的端口）仍然空闲：复用它；
//  4. 其余情况：另挑一个空闲端口。
//
// 为什么要跨启动记住端口：harness Web GUI 把界面状态存在 localStorage，而
// localStorage 按 origin 分区，origin 里含端口。端口每次冷启动都换一个，GUI 就
// 每次落在空的存储里，读不到 `dsh.sessions.current`，只能按「没有上次会话」新建
// 一个——这正是「每次启动都开新会话」的来源。同一次运行内 harness 重启仍复用
// 同一端口（记忆值或本次挑的值），GUI 因此可以重连。
func resolvePort(preferredPort int) (string, bool) {
	switch override := os.Getenv(portEnv); {
	case override == "0":
		return reservePort(), false
	case override != "":
		return override, false
	}
	if preferredPort > 0 && preferredPort <= 65535 && portAvailable(preferredPort) {
		return strconv.Itoa(preferredPort), true
	}
	return reservePort(), true
}

// portAvailable 判断 loopback 上的端口当前是否空闲；探测用的监听随即关闭。
func portAvailable(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// resolveLogDir 返回 harness 日志目录。
func resolveLogDir() string {
	if dir := os.Getenv("DSH_DESKTOP_LOG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "dsh-desktop")
}

// RuntimeDir 返回启动器的运行时目录（默认 ~/.cache/dsh-desktop，DSH_DESKTOP_LOG_DIR
// 可覆盖）：harness 日志、启动进度插件与监护 overlay 都落在这里，壳自己的状态文件
// 也放同一处。单独导出而不是让调用方拼接路径：目录位置只保留一个来源，测试也能用
// 同一个环境变量把落盘重定向到临时目录、不碰真实用户目录。
func RuntimeDir() string {
	return resolveLogDir()
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

// hostRootfsBase 是沙箱内宿主根文件系统的只读挂载点（测试可覆盖）。
var hostRootfsBase = "/run/host/rootfs"

// lookPath 解析 PATH 上的可执行文件（测试可覆盖）。
var lookPath = exec.LookPath

// 宿主逃逸通道的声明变量。这是一份跨语言契约：harness 侧的
// dsh-launch-environment 用同一对名字解析事实，改名必须两侧同步。
const (
	hostRootfsEnv   = "DSH_HOST_ROOTFS"
	hostLauncherEnv = "DSH_HOST_LAUNCH"
	hostLauncher    = "systemd-run"
)

// HostEscapeFact 是本次运行的宿主逃逸通道：宿主根文件系统的只读挂载点，加在宿主机上
// 启动程序的启动器。它和 harness 侧 packages/util/launch-environment 的 HostEscapeFact
// 描述同一份事实（跨语言契约，变量名见 hostRootfsEnv/hostLauncherEnv）。
type HostEscapeFact struct {
	// Rootfs 是宿主根文件系统的只读挂载点。
	Rootfs string
	// Launcher 是在宿主机上启动程序的启动器（systemd-run）。
	Launcher string
}

// HostEscape 探测本次运行是否具备宿主逃逸通道，供"把宿主能力接到容器内"的功能使用
// （例如 linglonghost 的透传包装）。判据与声明通道时一致：宿主根挂载点存在，且启动器
// 在 PATH 上。缺少任一即视为没有通道，调用方必须保持容器内的既有行为。
func HostEscape() (HostEscapeFact, bool) {
	if !dirExists(hostRootfsBase) {
		return HostEscapeFact{}, false
	}
	if _, err := lookPath(hostLauncher); err != nil {
		return HostEscapeFact{}, false
	}
	return HostEscapeFact{Rootfs: hostRootfsBase, Launcher: hostLauncher}, true
}

// configureHostEscapeEnv 声明本沙箱的宿主逃逸通道：宿主根的只读挂载点，加上
// 在宿主机上运行程序的启动器。两者成对出现，harness 侧据此判断本次运行能否把
// 工作区交给宿主应用打开；缺失时 harness 保持沙箱内的既有行为，因此宿主根不可读
// 或转发启动器不在 PATH 上都不设置，绝不声明一条用不了的通道。
//
// 已存在的同名变量一律不覆盖：这两个变量也是使用者手工调试与临时关闭该通道的入口，
// 覆盖会把开关从使用者手里拿走（把 DSH_HOST_ROOTFS 设为空即关闭）。
func configureHostEscapeEnv() {
	fact, ok := HostEscape()
	if !ok {
		return
	}
	setenvIfUnset(hostRootfsEnv, fact.Rootfs)
	setenvIfUnset(hostLauncherEnv, fact.Launcher)
}

// setenvIfUnset 仅在变量完全缺失时写入；空值也视为使用者的显式取值。
func setenvIfUnset(key, value string) {
	if _, ok := os.LookupEnv(key); !ok {
		_ = os.Setenv(key, value)
	}
}

// ConfigureChildEnv 设置子进程（harness 及其后代）需要的环境变量。
// PATH 优先级：宿主挂载(/opt/host-tools/*/bin) > 玲珑宿主包装(~/.config/dsh-desktop/linglong-bin) >
// 按需安装(~/.dsh-tools/bin) > 现有 PATH。
//
// 玲珑宿主包装排在按需安装之前是刻意的：它透传的是宿主那份 ll-builder/ll-cli，而玲珑
// 命令离不开宿主的守护进程、层仓库与可写状态；市场里若出现同名的自包含安装，那份在
// 容器内反而用不了。用户自己声明的宿主导入仍排在它之前——显式选择优先。
func ConfigureChildEnv(home string) {
	_ = os.Setenv("GTK_A11Y", "none")
	configureHostEscapeEnv()

	if exe, err := os.Executable(); err == nil {
		if gitCore, ok := packagedGitExecPath(exe); ok {
			_ = os.Setenv("GIT_EXEC_PATH", gitCore)
		}
	}

	segs := []string{}
	if bins := hostToolBins(hostToolsBase); len(bins) > 0 {
		segs = append(segs, bins...)
	}
	if dir := linglonghost.WrapperDir(home); dirExists(dir) {
		segs = append(segs, dir)
	}
	bin, lib := dshToolsEnv(home)
	if dirExists(bin) {
		segs = append(segs, bin)
	}
	prependPathEnv("PATH", segs)
	if dirExists(lib) {
		prependPathEnv("LD_LIBRARY_PATH", []string{lib})
	}
}

// dirExists 判断路径存在且是目录。PATH/LD_LIBRARY_PATH 只注入真实存在的目录：注入
// 不存在的目录既让子进程多走一次无效查找，也让"这个能力到底有没有"变得不可读。
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// prependPathEnv 把 segs 按给定顺序前置进名为 key 的路径列表变量，并先剔除同名旧段。
//
// ConfigureChildEnv 在安装、切换、卸载工具后会被反复调用。若只是无条件前置，固定
// 段每轮都会再加一份，PATH 与 LD_LIBRARY_PATH 随操作次数线性膨胀且永不收敛，子进程
// 的搜索路径也随之变长。只剔除与 segs 完全相同的段：用户自己加进 PATH 的其它目录
// 原样保留。空段一并丢弃——POSIX 下空段表示当前目录，把它带进子进程等于允许从
// 任意工作目录执行命令，且这里没有任何理由保留它。
func prependPathEnv(key string, segs []string) {
	if len(segs) == 0 {
		return
	}
	drop := make(map[string]bool, len(segs))
	for _, seg := range segs {
		drop[seg] = true
	}
	kept := make([]string, 0, len(segs)+4)
	for _, old := range filepath.SplitList(os.Getenv(key)) {
		if old == "" || drop[old] {
			continue
		}
		kept = append(kept, old)
	}
	_ = os.Setenv(key, strings.Join(append(append([]string{}, segs...), kept...), string(os.PathListSeparator)))
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

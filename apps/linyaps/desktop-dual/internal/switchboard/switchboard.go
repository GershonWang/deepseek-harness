// Package switchboard 实现玲珑包的客户端切换器：在同包内随带的客户端形态
// ——Go + Wails 薄壳客户端与官方 Electron 客户端——之间互斥切换，并记录默认形态。
//
// 随带哪些形态是安装事实，由构造时探测包内可执行文件得出：两个都在时提供完整切换，
// 只随带薄壳时切换没有目标，对外只报告 Switchable=false，由托盘收敛菜单。探测而不是
// 编译期开关，是为了让「含 Electron」与「不含 Electron」两个包共用同一份切换器代码，
// 不存在会各自漂移的第二份实现。
//
// 为什么必须分成进程：切换器是常驻的托盘进程，客户端是它拉起的子进程。切换器要能在
// 客户端自行退出后继续存活（否则用户再无入口管理后台客户端），而客户端有自己完整的
// 优雅退出路径，两者不能共用一个进程。
//
// 互斥语义：任一时刻至多一个客户端在运行。切换即「停掉当前客户端的整棵进程组，
// 再拉起另一个」，因此正在进行的会话会被中断——这是产品选择，不是缺陷。
package switchboard

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"
)

// Mode 是客户端形态。
type Mode string

const (
	// ModeShell 是随包的 Go + Wails 薄壳客户端，窗口内嵌 dsh web 的 Web GUI。
	ModeShell Mode = "shell"
	// ModeOfficial 是随包的官方 Electron 客户端。
	ModeOfficial Mode = "official"
)

// officialExecutable 是包内官方客户端主程序的固定名字。electron-builder 默认按
// productName 生成名字（实测为 `@deepseek-aidsh-desktop`，含 @ 与连字符），打包时
// 已重命名，切换器因此不依赖上游的命名规则。
const officialExecutable = "dsh-desktop-official"

// shellExecutable 是薄壳客户端的主程序名。切换器与它同处 ${PREFIX}/bin，按自身
// 可执行文件所在目录解析：容器内 PATH 不保证含该目录，经 PATH 查找会把「装在哪」
// 变成运行期才知道的事。
const shellExecutable = "dsh-desktop-launcher"

// hideOnCloseEnv 是告诉薄壳客户端「关窗只隐藏窗口」的环境变量名。
//
// 名字在 desktop-launcher 的 internal/app 里另有一份字面量（见那边的 HideOnCloseEnv）：
// 两个模块各自独立，切换器无法 import 它，为共享一个字符串引入第三方模块不划算。
// 两边互相点名，并各有一个用例把字面量钉死——改名时至少有一个模块的测试失败，而不是
// 两边悄悄错开、表现为「点托盘显示没反应」这种没有报错也没有日志的失效。
const hideOnCloseEnv = "DSH_DESKTOP_HIDE_ON_CLOSE"

// showSocketRel 是薄壳客户端监听唤回请求的 Unix socket 路径，相对用户主目录。
//
// 为什么用 socket 而不是信号：薄壳客户端基于 Wails，Linux 上用的是 WebKit 的
// JavaScriptCore，而 JSC 自己安装 SIGUSR1 处理器做 GC 线程的挂起/恢复。JSC 一旦接管，
// Go 的 signal.Notify 就再也收不到这个信号，发出去的 SIGUSR1 等于直接打进 JSC 的 GC
// 机制——实测第一次点托盘「显示」时客户端进程被立刻杀死、无任何日志，harness 沦为孤儿。
// socket 客户端自己监听，触发语义与所有权都在我们手里，不再与运行时抢信号。
//
// 路径在 desktop-launcher 侧另有一份字面量（两个模块互相无法 import），两边互为镜像，
// 各有一个用例把字面量钉死：改名时至少有一边的测试会红，而不是悄悄错开、表现为「点显示
// 没反应」这种既没有报错也没有日志的失效。
const showSocketRel = ".cache/dsh-desktop/show.sock"

// showPayload 是拨通唤回 socket 后写入的字节。
//
// 服务端只把「收到连接」当作唤回请求，内容不参与语义；固定写 "show\n" 是为了抓包或日志里
// 一眼能看出这条连接的用途，同时避免服务端哪天改成按行读取时收到一个没有结束符的请求。
const showPayload = "show\n"

// showDialTimeout 是连接唤回 socket 的超时。
//
// 2s 远大于本机 Unix socket 的连接耗时，只用来兜住客户端正在退出、socket 文件已不可连的
// 窗口期：没有超时的话 Present 会一直挂着，而它持着 s.mu，托盘之后的每次刷新与点击都会被
// 一起堵死。
const showDialTimeout = 2 * time.Second

// dialShow 是唤回薄壳客户端的实现，抽成变量以便单测断言「显示」走的是拨号唤回，
// 而不必真的拉起客户端或监听 socket。
var dialShow = dialShowSocket

// dialShowSocket 拨通 path 上的唤回 socket 并写入 showPayload。
//
// 只负责把请求送达：写完即关闭，不等服务端回应——客户端收到连接后显示窗口，那条路径与
// 这里无关。连接或写入失败都原样返回，调用方必须报给用户，绝不回退成启动新客户端（薄壳
// 客户端没有单实例锁，回退会与已在运行的那个抢同一个 dsh 端口与 ~/.dsh 状态）。
// @param path - 唤回 socket 的绝对路径。
// @returns 连接或写入失败的原因。
func dialShowSocket(path string) error {
	conn, err := net.DialTimeout("unix", path, showDialTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(showPayload)); err != nil {
		return err
	}
	return nil
}

// stopGrace 是终止客户端进程组时等待优雅退出的时长；超时后整组强杀。
//
// 20s 不是估出来的，而是按客户端（薄壳启动器）自身有界的收尾时间推导出的下界。前提是
// 启动器的 Shutdown() 先停 harness、再收拾 doctor 与终端，即次序为
// a.sup.Stop() → a.stopDoctor() → term.CloseAll()：
//
//   - Supervisor.Stop()：KillTimeoutMs(5000) 等优雅退出，再以同样的 5000 等 SIGKILL 后
//     回收，最长 10s；
//   - App.stopDoctor()：取消并等待 doctor 退出，最长 2s；
//   - terminal CloseAll()：每个会话最长 3s 优雅退出 + 2s 强杀回收共 5s，且串行遍历全部
//     会话，会话数不定，因此这一段的上界不可预估。
//
// 前两段合计 12s，20s 留出余量。次序在这里是前提：启动器先停 harness，所以 20s 到达时
// harness 早就退出了，SIGKILL 打掉的是仍在收尾的启动器及其终端会话，而不是一个正在存盘
// 或还在跑的 harness。这条次序前提跨模块，本文件的用例只钉得住时长下界、拦不下它，因此
// 启动器哪天把 harness 挪到最后再停，必须同时回头核对这个数字。
//
// 也正因最后一段上界不可预估，stopGrace 只能是兜底网，不是「客户端一定干净退出」的硬
// 保证：真正的退出保证由客户端自己的收尾负责（它在收到 SIGTERM 后主动停 harness），这里
// 只兜住客户端卡死的情形——用户点了退出或切换之后，托盘不能无限期挂着。调小它必须重新
// 核对上面三段：短于客户端收尾时间会在收尾中途强杀，留下占着 dsh 端口的孤儿 harness，
// 表现为「退出不干净 / ll-cli ps 里还有进程」。switchboard_test.go 有用例钉住这个下界。
const stopGrace = 20 * time.Second

// Config 是切换器的持久化配置。字段缺失时取零值，由 normalize 补全。
//
// 只保留默认形态：托盘形态没有窗口，原先随悬浮球一起持久化的屏幕坐标已随悬浮球
// 一并移除。旧配置文件里的额外字段会被 JSON 解码忽略，不影响升级。
type Config struct {
	// DefaultMode 是切换器启动时自动拉起的客户端形态。
	DefaultMode Mode `json:"defaultMode"`
}

// Status 是切换器对外的状态快照。托盘按固定间隔读取它并据此重绘图标、提示与菜单，
// 因此这里不做事件推送——状态量极小，轮询不会漏掉任何一种变化（包括客户端被用户
// 直接关窗这类在切换器之外发生的变化）。
type Status struct {
	// Mode 是当前正在运行的客户端形态；没有客户端在跑时为空串。
	Mode Mode `json:"mode"`
	// DefaultMode 是下次启动时使用的形态。
	DefaultMode Mode `json:"defaultMode"`
	// Switching 表示一次切换正在进行，菜单据此禁用重复触发。
	Switching bool `json:"switching"`
	// Error 保留最近一次失败的原因，成功后清空。
	Error string `json:"error"`
	// Switchable 表示包里随带了两种客户端形态，托盘据此决定菜单给不给「切换」与
	// 「默认启动」。它是安装事实而不是运行状态：包一旦装好就不会变，因此只在构造时
	// 探测一次，菜单结构在一次进程生命周期内也不会变形。
	//
	// 只有一个客户端时这个值为假，菜单收敛为「显示 / 关于 / 退出」——切换没有目标，
	// 默认形态也没有第二种可选，留着只会是点了必然失败或被置灰的入口。
	Switchable bool `json:"switchable"`
}

// Switchboard 管理客户端进程与切换状态。所有导出方法都可能被托盘回调并发调用，
// 内部用互斥锁串行化。
type Switchboard struct {
	mu         sync.Mutex
	home       string
	configPath string
	cfg        Config
	current    Mode
	child      *exec.Cmd
	// exited 在当前客户端进程结束时关闭。切换必须等它，而不是等 s.child 变 nil——
	// 后者由 reap 在拿到锁之后才设置，而切换自己正持着锁，会白等到强杀超时：实测每次
	// 切换都要卡满 stopGrace，表现为「点了没反应」。
	exited    chan struct{}
	switching bool
	lastError string
	// available 是包里实际随带的客户端形态，顺序即菜单优先级（薄壳在前）。
	// 它是安装事实，构造时探测一次后不再变化。
	available []Mode
}

// New 构造切换器并读取持久化配置。配置文件缺失或损坏时回退默认值，
// 不拦启动：切换器不可用比配置损坏严重得多。
// @param home - 用户主目录，容器内与宿主是同一目录，因此配置可跨启动保留。
// @returns 就绪的切换器。
func New(home string) *Switchboard {
	return newSwitchboard(home, detectAvailableModes())
}

// newSwitchboard 按给定的可用形态构造切换器。可用形态由调用方给出而不是在这里探测，
// 是为了让「默认形态回退」这条规则能脱离真实安装布局被断言。
// @param home - 用户主目录。
// @param available - 包里随带的客户端形态；为空表示探测不到。
// @returns 就绪的切换器。
func newSwitchboard(home string, available []Mode) *Switchboard {
	s := &Switchboard{
		home:       home,
		configPath: filepath.Join(home, ".config", "dsh-desktop", "switch.json"),
		cfg:        Config{DefaultMode: ModeShell},
		available:  available,
	}
	if raw, err := os.ReadFile(s.configPath); err == nil {
		var loaded Config
		if json.Unmarshal(raw, &loaded) == nil {
			s.cfg = normalize(loaded, available)
		}
	}
	return s
}

// detectAvailableModes 探测当前安装里随带的客户端形态。
// @returns 随包的形态；无法解析自身路径时为空切片，由调用方走保守路径。
func detectAvailableModes() []Mode {
	exe, err := switcherExecutable()
	if err != nil {
		return nil
	}
	return availableModes(exe)
}

// availableModes 列出 switcherExe 所在安装前缀里实际存在的客户端形态。
//
// 判据复用 clientCommand：它解析时已经确认对应可执行文件存在，因此「这个形态在不在包里」
// 只有一处定义——另写一份 os.Stat 迟早会和解析规则漂移，而漂移的结果是菜单给出一个
// 必然失败的入口。
// @param switcherExe - 切换器自身的可执行文件路径。
// @returns 按菜单优先级排列的形态；一个都没有时为空切片。
func availableModes(switcherExe string) []Mode {
	var modes []Mode
	for _, mode := range []Mode{ModeShell, ModeOfficial} {
		if _, _, err := clientCommand(switcherExe, mode); err == nil {
			modes = append(modes, mode)
		}
	}
	return modes
}

// normalize 把缺失或越界的配置收敛到可用值。
//
// 默认形态不在 available 里时必须回退。配置文件住在宿主家目录、跨包保留：用户可能先装过
// 含官方客户端的包，再换成只含薄壳的包，此时旧的 defaultMode=official 会让每次启动都去
// 拉一个包内不存在的二进制；而单客户端形态的菜单里没有「默认形态」入口可以改回来，
// 这里因此是唯一能自愈的地方。
// @param cfg - 从磁盘读到的配置。
// @param available - 包里随带的客户端形态；为空表示探测不到，只做合法性收敛。
// @returns 可直接使用的配置。
func normalize(cfg Config, available []Mode) Config {
	if cfg.DefaultMode != ModeShell && cfg.DefaultMode != ModeOfficial {
		cfg.DefaultMode = ModeShell
		return cfg
	}
	if len(available) > 0 && !slices.Contains(available, cfg.DefaultMode) {
		cfg.DefaultMode = available[0]
	}
	return cfg
}

// ConfigPath 返回配置文件路径，供测试与诊断使用。
// @returns 切换器配置的绝对路径。
func (s *Switchboard) ConfigPath() string { return s.configPath }

// Start 拉起默认形态的客户端。已经有一个客户端在运行时不做任何事，避免切换器
// 重启后叠出第二个客户端。
// @returns 启动失败的原因；失败时切换器仍在运行，用户可再从托盘菜单触发一次。
func (s *Switchboard) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.child != nil {
		return nil
	}
	return s.launchLocked(s.cfg.DefaultMode)
}

// Present 把客户端显示给用户，是托盘左键与菜单「显示」的唯一入口。
//
// 两种客户端的关闭语义不同，「显示」的含义因此也不同：官方 Electron 客户端有自己的
// 单实例锁，再启动一次它会把请求转交给已运行的实例去还原窗口、随后自己退出；薄壳客户端
// 没有单实例锁，再启动会真的多出一个进程去抢同一个 dsh 端口与 ~/.dsh 状态，所以要靠
// 唤回 socket（showShellLocked）让已在运行的那个把窗口显示出来。
//
// 薄壳客户端只有在由本切换器拉起时才关窗隐藏（见 hideOnCloseEnv）：其余情形关窗即退出，
// 那时 runningLocked 已经是假，这里会走启动分支。
// @returns 启动或唤回失败的原因。
func (s *Switchboard) Present() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runningLocked() {
		if s.current == ModeOfficial {
			return s.focusOfficialLocked()
		}
		return s.showShellLocked()
	}
	return s.launchLocked(s.cfg.DefaultMode)
}

// showShellLocked 让已在运行的薄壳客户端把窗口显示出来。调用方必须持有 s.mu。
//
// 唤回走本地 Unix socket：薄壳客户端启动时监听它，路径由 showSocketRel 钉死、两端互为
// 镜像。拨号失败只把错误往上报，绝不回退成启动新客户端——那正是「点一下显示就叠出第二个
// harness」的老毛病，两个进程会抢同一个 dsh 端口与 ~/.dsh 状态。
//
// 没有可唤回的子进程时为空操作而不是报错：runningLocked 与这里读的是同一个字段，中间
// 不存在另一条改动路径，真走到那个分支说明状态刚被复位，此时报错只会给用户一条没有
// 可操作内容的通知。
// @returns 拨号或写入失败的原因；没有子进程时为空操作。
func (s *Switchboard) showShellLocked() error {
	if s.child == nil || s.child.Process == nil {
		return nil
	}
	path := filepath.Join(s.home, showSocketRel)
	if err := dialShow(path); err != nil {
		return fmt.Errorf("showing the %s client through %s: %w", ModeShell, path, err)
	}
	return nil
}

// runningLocked 报告当前是否真有一个客户端在跑。调用方必须持有 s.mu。
//
// 不能只看 s.child：进程结束后 reap 先关 exited、再抢锁复位字段，两者之间 s.child 仍非
// nil，而那个进程已经是僵尸。此时若走「唤回」分支，官方版会真的再起一个实例，却不进入
// 状态跟踪（用户关掉切换器后它还在跑）。exited 关闭才是「进程确实已结束」的精确信号。
// @returns 有客户端在运行为真。
func (s *Switchboard) runningLocked() bool {
	if s.child == nil || s.exited == nil {
		return false
	}
	select {
	case <-s.exited:
		return false
	default:
		return true
	}
}

// Switch 切换到另一个客户端形态。互斥语义由「先停后起」保证：只有当前客户端的
// 进程组确认退出后才拉起新的，否则两者会同时占用 dsh 端口与 ~/.dsh 状态。
// @returns 切换失败的原因；失败时保留错误供托盘展示，并尽量回到可用状态。
func (s *Switchboard) Switch() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.switching {
		return nil
	}
	s.switching = true
	s.lastError = ""
	defer func() { s.switching = false }()

	next := ModeOfficial
	if s.current == ModeOfficial {
		next = ModeShell
	}
	s.stopLocked()
	if err := s.launchLocked(next); err != nil {
		s.lastError = err.Error()
		return err
	}
	// 切换成功后把新形态记为默认：用户最后一次的选择就是下次启动的形态。
	s.cfg.DefaultMode = next
	if err := s.saveLocked(); err != nil {
		// 配置写不进去不影响本次切换，只留痕。
		s.lastError = fmt.Sprintf("default mode not saved: %v", err)
	}
	return nil
}

// SetDefault 只修改默认形态，不立即切换。托盘菜单用它。
// @param mode - 目标客户端形态。
// @returns 参数非法或写入失败的原因。
func (s *Switchboard) SetDefault(mode Mode) error {
	if mode != ModeShell && mode != ModeOfficial {
		return fmt.Errorf("switchboard: unknown client mode %q", mode)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.DefaultMode = mode
	if err := s.saveLocked(); err != nil {
		s.lastError = err.Error()
		return err
	}
	s.lastError = ""
	return nil
}

// Status 返回当前快照，供托盘按固定间隔刷新。
// @returns 客户端形态、默认形态、切换中标志、最近的错误与包内是否可切换。
func (s *Switchboard) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Mode:        s.current,
		DefaultMode: s.cfg.DefaultMode,
		Switching:   s.switching,
		Error:       s.lastError,
		Switchable:  len(s.available) > 1,
	}
}

// Shutdown 停掉客户端并退出。切换器退出后客户端不应继续留在后台：
// 用户看不到托盘图标时就无法再管理它。
func (s *Switchboard) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

// launchLocked 启动一个客户端进程并记录形态。调用方必须持有 s.mu。
// @param mode - 要拉起的客户端形态。
// @returns 路径解析或启动失败的原因。
func (s *Switchboard) launchLocked(mode Mode) error {
	exe, err := switcherExecutable()
	if err != nil {
		return err
	}
	path, args, err := clientCommand(exe, mode)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Env = clientEnv(mode)
	// 独立进程组：客户端自己还会拉起 harness 与 Electron 的辅助进程，
	// 只有整组终止才能保证切换后不残留占着 dsh 端口的孤儿。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the %s client: %w", mode, err)
	}
	exited := make(chan struct{})
	s.child = cmd
	s.current = mode
	s.exited = exited
	// 回收子进程，避免僵尸；客户端进程结束时这里也会醒来，把状态复位成「没有客户端
	// 在跑」，托盘随之反映未运行态。注意薄壳客户端的关窗不再必然走到这里：由本切换器
	// 拉起时它只隐藏窗口、进程继续运行（见 hideOnCloseEnv 与 showShellLocked）。
	go s.reap(cmd, exited)
	return nil
}

// focusOfficialLocked 让已在运行的官方客户端把窗口显示出来。调用方必须持有 s.mu。
//
// 官方客户端自带 Electron 单实例锁：再启动一次它的二进制，新进程会把激活请求转交给已
// 运行的实例（focusPrimaryWindow 做 restore/show/focus）后立刻退出。这里因此既不能走
// launchLocked（它会覆盖正在跟踪的子进程，让状态与实际运行的两个进程脱节），也不等待
// 结果——唤回是否成功由已运行实例决定，切换器无从得知。
// @returns 解析或启动转发进程失败的原因。
func (s *Switchboard) focusOfficialLocked() error {
	exe, err := switcherExecutable()
	if err != nil {
		return err
	}
	path, args, err := clientCommand(exe, ModeOfficial)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("focusing the %s client: %w", ModeOfficial, err)
	}
	// 转发进程会在交出激活请求后立刻退出，这里只负责回收，避免僵尸。
	go func() { _ = cmd.Wait() }()
	return nil
}

// switcherExecutable 返回解析过符号链接的切换器自身路径。
//
// 客户端可执行文件的位置由切换器的安装位置推出，因此这里读 os.Executable 而不是接受
// 调用方传入：只有真正要启动进程的路径才需要它，纯解析规则由 clientCommand 单独承载
// 并被单测覆盖。
// @returns 可执行文件绝对路径。
func switcherExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locating the switcher executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// reap 等待子进程结束并复位状态。
// @param cmd - 由 launchLocked 启动的进程。
// @param exited - 本进程对应的退出信号通道，关闭后 stopLocked 即可返回。
func (s *Switchboard) reap(cmd *exec.Cmd, exited chan struct{}) {
	_ = cmd.Wait()
	// 先发退出信号再抢锁：stopLocked 等在 channel 上，不该被这里的加锁顺序拖住。
	close(exited)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.child == cmd {
		s.child = nil
		s.current = ""
		s.exited = nil
	}
}

// stopLocked 终止当前客户端进程组。调用方必须持有 s.mu。
func (s *Switchboard) stopLocked() {
	if s.child == nil || s.child.Process == nil {
		return
	}
	cmd, exited := s.child, s.exited
	// 先摘掉引用：紧接着的 launchLocked 要挂上新进程，而 reap 只在 s.child 仍是自己时
	// 才复位状态，所以这里清空不会与旧进程的回收互相干扰。
	s.child = nil
	s.current = ""
	s.exited = nil
	if exited == nil {
		return
	}
	pid := cmd.Process.Pid
	// 负号表示整个进程组：客户端会派生 harness 与 Electron 辅助进程。
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(stopGrace):
		// 优雅退出超时，整组强杀后再等回收收尾，避免留下僵尸。
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-exited
	}
}

// saveLocked 落盘配置。调用方必须持有 s.mu。
// @returns 创建目录或写入失败的原因。
func (s *Switchboard) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath, append(raw, '\n'), 0o644)
}

// clientEnv 返回拉起某种客户端形态时附加的环境。
//
// 薄壳客户端只有在由本切换器拉起时才关窗隐藏：托盘是隐藏之后唯一能把它叫回来的入口，
// 而这条通道只有本进程提供。官方客户端自带 Electron 的关窗隐藏语义，不需要这个变量，
// 因此返回 nil 让它继承本进程环境，而不是带上一份用不上的变量。
// @param mode - 客户端形态。
// @returns 子进程环境；nil 表示继承。
func clientEnv(mode Mode) []string {
	if mode != ModeShell {
		return nil
	}
	return append(os.Environ(), hideOnCloseEnv+"=1")
}

// clientCommand 解析一种客户端形态的可执行文件与参数。
//
// 薄壳客户端与切换器同处 ${PREFIX}/bin，按切换器自身路径的兄弟节点解析；官方客户端
// 是随包的 Electron 主程序，位置由玲珑包布局决定（<PREFIX>/electron/<name>，而
// ${PREFIX}/bin 的上一级就是 <PREFIX>）。Electron 主程序需要 --no-sandbox：玲珑
// 容器禁止提权，Chromium 沙箱用不了。
//
// 切换器自身的可执行文件路径由调用方传入而不是在这里读 os.Executable()：解析规则
// 因此可以脱离真实安装布局被测试，而不必在测试里伪造一份 ${PREFIX} 目录树。
// @param switcherExe - 切换器自身的可执行文件路径（符号链接已解析）。
// @param mode - 客户端形态。
// @returns 可执行文件绝对路径、启动参数，或解析失败的原因。
func clientCommand(switcherExe string, mode Mode) (string, []string, error) {
	switch mode {
	case ModeShell:
		shell := filepath.Join(filepath.Dir(switcherExe), shellExecutable)
		if _, err := os.Stat(shell); err != nil {
			return "", nil, fmt.Errorf("shell client executable unavailable (%s): %w", shell, err)
		}
		return shell, nil, nil
	case ModeOfficial:
		prefix := filepath.Dir(filepath.Dir(switcherExe))
		official := filepath.Join(prefix, "electron", officialExecutable)
		if _, err := os.Stat(official); err != nil {
			return "", nil, fmt.Errorf("official client executable unavailable (%s): %w", official, err)
		}
		return official, []string{"--no-sandbox"}, nil
	default:
		return "", nil, fmt.Errorf("switchboard: unknown client mode %q", mode)
	}
}

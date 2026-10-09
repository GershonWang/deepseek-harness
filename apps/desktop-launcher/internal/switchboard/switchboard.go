// Package switchboard 实现双客户端切换器：在同一个玲珑包内的两种客户端形态
// ——随包的 Go + Wails 薄壳客户端与官方 Electron 客户端——之间互斥切换，
// 并记录默认形态与悬浮球位置。
//
// 为什么必须分成进程：Wails v2 只暴露单窗口 API（runtime.Window* 全部作用于
// 当前窗口），常驻的悬浮球窗口与客户端主窗口无法共存于一个进程。因此切换器
// （悬浮球）与客户端各自成进程，本包只负责客户端的启动、整组终止与状态持久化。
//
// 互斥语义：任一时刻至多一个客户端在运行。切换即「停掉当前客户端的整棵进程组，
// 再拉起另一个」，因此正在进行的会话会被中断——这是产品选择，不是缺陷。
package switchboard

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// stopGrace 是终止客户端进程组时等待优雅退出的时长；超时后整组强杀。
// 客户端自身已有较长的优雅退出路径（保存窗口状态、停 harness），过短会留下残留。
const stopGrace = 5 * time.Second

// Config 是切换器的持久化配置。字段缺失时取零值，由 Default 补全。
type Config struct {
	// DefaultMode 是切换器启动时自动拉起的客户端形态。
	DefaultMode Mode `json:"defaultMode"`
	// BubbleX/BubbleY 是悬浮球左上角在屏幕上的位置。
	BubbleX int `json:"bubbleX"`
	BubbleY int `json:"bubbleY"`
	// BubbleSet 区分「从未记录位置」与「记录在原点」：只看坐标是否为零的话，用户真的
	// 把悬浮球拖到左上角这件事会被当成没记录过，下次启动又跳回默认位置。
	BubbleSet bool `json:"bubbleSet"`
}

// BubblePosition 是悬浮球在屏幕上的位置快照，直接交给前端。
//
// 用结构体而不是 (int, int)：Wails 对多返回值的映射不如单值可预期，而这里要额外带
// 一个「有没有记录过」的布尔。
type BubblePosition struct {
	// X/Y 是悬浮球左上角的屏幕坐标。
	X int `json:"x"`
	Y int `json:"y"`
	// Set 表示这两个坐标来自一次真实拖拽。
	Set bool `json:"set"`
}

// Status 是暴露给悬浮球前端的快照。
type Status struct {
	// Mode 是当前正在运行的客户端形态；没有客户端在跑时为空串。
	Mode Mode `json:"mode"`
	// DefaultMode 是下次启动时使用的形态。
	DefaultMode Mode `json:"defaultMode"`
	// Marker 是悬浮球下方那行形态标记（web / desktop）。它是技术标识而不是需要翻译的
	// 文案——两种语言下写法相同，进字典反而会被「中英逐字相同」判成漏翻；由 Go 侧给出，
	// 前端也不硬编码。
	Marker string `json:"marker"`
	// Switching 表示一次切换正在进行，前端据此禁用重复点击。
	Switching bool `json:"switching"`
	// Error 保留最近一次失败的原因，成功后清空。
	Error string `json:"error"`
}

// Switchboard 管理客户端进程与切换状态。所有导出方法都可由 Wails 前端并发调用，
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
	// 切换都要卡满 stopGrace（5 秒），表现为「点了没反应」。
	exited    chan struct{}
	switching bool
	lastError string
}

// New 构造切换器并读取持久化配置。配置文件缺失或损坏时回退默认值，
// 不拦启动：切换器不可用比配置损坏严重得多。
// @param home - 用户主目录，容器内与宿主是同一目录，因此配置可跨启动保留。
// @returns 就绪的切换器。
func New(home string) *Switchboard {
	s := &Switchboard{
		home:       home,
		configPath: filepath.Join(home, ".config", "dsh-desktop", "switch.json"),
		cfg:        Config{DefaultMode: ModeShell},
	}
	if raw, err := os.ReadFile(s.configPath); err == nil {
		var loaded Config
		if json.Unmarshal(raw, &loaded) == nil {
			s.cfg = normalize(loaded)
		}
	}
	return s
}

// normalize 把缺失或越界的配置收敛到可用值。
// @param cfg - 从磁盘读到的配置。
// @returns 可直接使用的配置。
func normalize(cfg Config) Config {
	if cfg.DefaultMode != ModeShell && cfg.DefaultMode != ModeOfficial {
		cfg.DefaultMode = ModeShell
	}
	return cfg
}

// ConfigPath 返回配置文件路径，供测试与诊断使用。
// @returns 切换器配置的绝对路径。
func (s *Switchboard) ConfigPath() string { return s.configPath }

// Start 拉起默认形态的客户端。已经有一个客户端在运行时不做任何事，避免切换器
// 重启后叠出第二个客户端。
// @returns 启动失败的原因；失败时切换器仍在运行，用户可再次点击切换重试。
func (s *Switchboard) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.child != nil {
		return nil
	}
	return s.launchLocked(s.cfg.DefaultMode)
}

// Switch 切换到另一个客户端形态。互斥语义由「先停后起」保证：只有当前客户端
// 的进程组确认退出后才拉起新的，否则两者会同时占用 dsh 端口与 ~/.dsh 状态。
// @returns 切换失败的原因；失败时保留错误供前端展示，并尽量回到可用状态。
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

// SetDefault 只修改默认形态，不立即切换。悬浮球右键菜单用它。
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

// Status 返回当前快照。前端按固定间隔轮询它，因此这里不做事件推送。
// @returns 客户端形态、默认形态、切换中标志与最近的错误。
func (s *Switchboard) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Mode:        s.current,
		Marker:      modeMarker(s.current),
		DefaultMode: s.cfg.DefaultMode,
		Switching:   s.switching,
		Error:       s.lastError,
	}
}

// BubblePosition 返回上次记录的悬浮球位置，供悬浮球启动时还原。
// @returns 屏幕坐标与「是否记录过」标志。
func (s *Switchboard) BubblePosition() BubblePosition {
	s.mu.Lock()
	defer s.mu.Unlock()
	return BubblePosition{X: s.cfg.BubbleX, Y: s.cfg.BubbleY, Set: s.cfg.BubbleSet}
}

// SaveBubblePosition 记录悬浮球位置，供下次启动还原。写失败只留痕：
// 位置丢失不该影响切换器本身。
// @param x - 屏幕横坐标。
// @param y - 屏幕纵坐标。
func (s *Switchboard) SaveBubblePosition(x, y int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.BubbleX, s.cfg.BubbleY, s.cfg.BubbleSet = x, y, true
	if err := s.saveLocked(); err != nil {
		s.lastError = err.Error()
	}
}

// Shutdown 停掉客户端并退出。切换器退出后客户端不应继续留在后台：
// 用户看不到悬浮球时就无法再管理它。
func (s *Switchboard) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

// modeMarker 给出一种客户端形态在悬浮球上显示的标记文字。
// @param mode - 客户端形态；空串表示当前没有客户端在运行。
// @returns 标记文字，未运行时为空串。
func modeMarker(mode Mode) string {
	switch mode {
	case ModeShell:
		return "web"
	case ModeOfficial:
		return "desktop"
	default:
		return ""
	}
}

// launchLocked 启动一个客户端进程并记录形态。调用方必须持有 s.mu。
// @param mode - 要拉起的客户端形态。
// @returns 路径解析或启动失败的原因。
func (s *Switchboard) launchLocked(mode Mode) error {
	path, args, err := clientCommand(mode)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
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
	// 回收子进程，避免僵尸；客户端被用户直接关窗时这里也会醒来，
	// 把状态复位成「没有客户端在跑」，悬浮球随之等待下一次点击。
	go s.reap(cmd, exited)
	return nil
}

// reap 等待子进程结束并复位状态。
// @param cmd - 由 launchLocked 启动的进程。
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

// clientCommand 解析一种客户端形态的可执行文件与参数。
//
// 薄壳客户端就是本二进制自身，以 --mode=shell 运行；官方客户端是随包的 Electron
// 主程序，位置由玲珑包布局决定（<PREFIX>/electron/<name>，而本二进制在
// <PREFIX>/bin/ 下）。Electron 主程序需要 --no-sandbox：玲珑容器禁止提权，
// Chromium 沙箱用不了。
// @param mode - 客户端形态。
// @returns 可执行文件绝对路径、启动参数，或解析失败的原因。
func clientCommand(mode Mode) (string, []string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("locating the launcher executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	switch mode {
	case ModeShell:
		return exe, []string{"--mode=shell"}, nil
	case ModeOfficial:
		prefix := filepath.Dir(filepath.Dir(exe))
		official := filepath.Join(prefix, "electron", officialExecutable)
		if _, err := os.Stat(official); err != nil {
			return "", nil, fmt.Errorf("official client executable unavailable (%s): %w", official, err)
		}
		return official, []string{"--no-sandbox"}, nil
	default:
		return "", nil, fmt.Errorf("switchboard: unknown client mode %q", mode)
	}
}

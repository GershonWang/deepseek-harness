// 客户端切换器的入口：一个常驻的托盘进程，负责在同包内随带的客户端形态之间互斥切换。
//
// 随带几个形态由包内容决定，启动时探测一次（见 internal/switchboard）：两个客户端都在时
// 托盘给出切换与默认形态，只随带薄壳客户端时菜单收敛为显示/关于/退出。切换器因此对
// 「含 Electron」与「不含 Electron」两个包是同一份实现，不存在会各自漂移的第二份。
//
// 为什么入口是托盘而不是窗口：切换器必须在客户端被关掉之后仍然可达（否则用户再无入口
// 管理后台客户端），而常驻窗口会遮挡内容、与全屏应用冲突，位置还可能被拖出屏幕导致入口
// 永久丢失。托盘图标由桌面面板托管，不存在这些失效模式。
//
// 进程职责边界：切换器自己不做客户端的事——它只启动/整组终止客户端，并在托盘上暴露
// 切换、设默认与退出三个动作（见 internal/switchboard）。
package main

import (
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/about"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/i18n"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/tray"
)

// mutexName 是单实例名。同一时刻只允许一个切换器：两个实例会各拉起一个客户端，
// 同时占用 dsh 端口与 ~/.dsh 状态。
//
// 取值刻意保留统一身份之前的那一个（DesktopDualSwitcher），不跟着包 ID 改名：旧包
// com.deepseek.dsh-desktop-dual 装的也是同一个切换器，两者可能在同一台机器上共存
// （本地装过测试包、商店升级尚未覆盖等）。同名让它们仍然互斥，最多是托盘入口只有一个；
// 一旦改名，两个切换器会各自拉起一个客户端去抢同一个 dsh 端口与 ~/.dsh 状态。旧包退场
// 后这个名字才可以一起改。
const mutexName = "org.deepseek.dsh.DesktopDualSwitcher"

// refreshInterval 是托盘刷新间隔。客户端可能在切换器之外结束（用户直接关掉客户端
// 窗口），轮询是发现这件事最省事的方式，而状态量极小。
const refreshInterval = time.Second

// retryInterval 是托盘状态的核对间隔：桌面外壳可能比切换器晚启动，也可能中途重启，
// 入口应当自己回来而不是要求用户重开应用。核对同时覆盖「还没注册上」与「注册到的宿主
// 已经换人」两种情况（见 tray.EnsureRegistered）。
const retryInterval = 5 * time.Second

// version 是玲珑包版本，打包时由 prepare-offline.sh 通过 -ldflags 注入。
// 常驻进程必须能自证版本：托盘里跑着的那个进程与用户装的包对不上时，先看这一行日志。
var version = "dev"

func main() {
	log.Printf("switcher %s starting", version)
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("switcher: locating the user home directory: %v", err)
	}
	board := switchboard.New(home)

	// 退出只能被触发一次：菜单可能被连点，关闭已关闭的 channel 会 panic。
	var quitOnce sync.Once
	quit := make(chan struct{})
	requestQuit := func() { quitOnce.Do(func() { close(quit) }) }

	// 安装前缀只解析一次：托盘图标与「关于」助手都按它定位，两处必须看到同一个值。
	prefix := installPrefix()
	client, err := tray.Connect(mutexName, tray.Options{
		Board:      board,
		Locale:     i18n.FromEnv(),
		IconPrefix: prefix,
		Version:    version,
		About:      about.New(prefix, version).Show,
		Quit:       requestQuit,
		Logf:       log.Printf,
	})
	if errors.Is(err, tray.ErrAlreadyRunning) {
		log.Print("switcher: another instance is already running; exiting")
		return
	}
	if err != nil {
		log.Fatalf("switcher: %v", err)
	}
	defer client.Close()

	// 托盘注册失败不阻止启动客户端：用户至少能进客户端。切换器继续驻留并周期性核对，
	// 桌面外壳回来时入口自动恢复。
	if err := client.EnsureRegistered(); err != nil {
		log.Printf("switcher: tray registration failed (%v); starting the default client and retrying", err)
	}
	if err := board.Start(); err != nil {
		log.Printf("switcher: starting the default client failed: %v", err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	refresh := time.NewTicker(refreshInterval)
	defer refresh.Stop()
	retry := time.NewTicker(retryInterval)
	defer retry.Stop()

	// 两个定时动作都要读切换器状态，而切换期间那把锁会被占住（最长一个 stopGrace）。在事件
	// 循环里同步做，会把同一循环里的退出与信号分支一起堵住——用户点了「退出」，托盘要等切换
	// 跑完才有反应。交给合并执行器：循环永不阻塞，也不会按 1s 的节拍堆出并发调用。
	refreshTray := tray.NewCoalescer(client.Refresh)
	reregisterTray := tray.NewCoalescer(func() {
		// 每次都用当前宿主核对一遍：宿主不在是预期状态（外壳可能比我们晚启动），
		// 不必刷日志；其余失败说明连接或导出出了问题，要留下记录。
		if err := client.EnsureRegistered(); err != nil && !errors.Is(err, tray.ErrWatcherUnavailable) {
			log.Printf("switcher: tray registration failed: %v", err)
		}
	})

	for {
		select {
		case <-quit:
			// 切换器退出后客户端不应继续留在后台：用户看不到托盘图标时就无法再管理它。
			board.Shutdown()
			return
		case <-signals:
			board.Shutdown()
			return
		case <-refresh.C:
			refreshTray.Trigger()
		case <-retry.C:
			reregisterTray.Trigger()
		}
	}
}

// installPrefix 从自身可执行文件推出安装前缀：<PREFIX>/bin/<exe> → <PREFIX>。
// 随包图标与两个客户端都按这个前缀定位，因此不依赖 PATH。
// @returns 安装前缀；无法解析自身路径时返回空串（调用方回退到内置图形）。
func installPrefix() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(filepath.Dir(exe))
}

// 双客户端切换器的入口：一个常驻的托盘进程，负责在两种客户端形态之间互斥切换。
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

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/i18n"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/tray"
)

// mutexName 是单实例名。同一时刻只允许一个切换器：两个实例会各拉起一个客户端，
// 同时占用 dsh 端口与 ~/.dsh 状态。
const mutexName = "org.deepseek.dsh.DesktopDualSwitcher"

// refreshInterval 是托盘刷新间隔。客户端可能在切换器之外结束（用户直接关掉客户端
// 窗口），轮询是发现这件事最省事的方式，而状态量极小。
const refreshInterval = time.Second

// retryInterval 是托盘注册的重试间隔：桌面外壳可能比切换器晚启动，也可能中途重启，
// 入口应当自己回来而不是要求用户重开应用。
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

	client, err := tray.Connect(mutexName, tray.Options{
		Board:      board,
		Locale:     i18n.FromEnv(),
		IconPrefix: installPrefix(),
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

	// 托盘注册失败不阻止启动客户端：用户至少能进客户端。切换器继续驻留并周期性重试，
	// 桌面外壳回来时入口自动恢复。
	if err := client.Register(); err != nil {
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
			client.Refresh()
		case <-retry.C:
			if !client.Registered() {
				if err := client.Register(); err == nil {
					log.Print("switcher: tray registered; the entry point is available again")
				}
			}
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

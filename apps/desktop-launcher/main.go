// 主入口：组装 Wails 应用（内嵌前端 + 绑定 App 控制器），并在进程被外部
// 信号终止时停掉 harness 子进程。
package main

import (
	"embed"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/app"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/appenv"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	home, _ := os.UserHomeDir()
	appenv.ConfigureChildEnv(home)
	packaging.ConfigureWebKitHelperPath()

	resolved := appenv.Resolve()
	controller := app.New(resolved.Config, home, app.ExternalConfigFilePath())

	// 外部终止（SIGTERM/SIGINT，如桌面管理器退出）时停 harness，避免子进程残留。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		controller.Shutdown()
		os.Exit(0)
	}()

	err := wails.Run(&options.App{
		Title:     "DeepSeek Harness",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 30, G: 30, B: 30, A: 255},
		OnStartup:        controller.OnStartup,
		OnShutdown:       controller.OnShutdown,
		Bind:             []interface{}{controller},
	})
	if err != nil {
		log.Fatalf("dsh-desktop: %v", err)
	}
}

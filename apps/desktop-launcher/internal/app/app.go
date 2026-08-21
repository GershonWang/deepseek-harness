// Package app 是 Wails 绑定层：向 Web 壳暴露控制方法，并定时推送状态事件。
// 它编排下层各领域包（supervisor/connector/toolchain/gitcred/packaging），
// 不含任何业务实现，只做"翻译"：领域状态 → 前端可渲染的快照。
package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/connector"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/gitcred"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/supervisor"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/toolchain"
)

// 事件名（前端监听）。
const (
	StatusEvent    = "harness:status"
	ToolchainEvent = "toolchain:status"
)

// FrontendStatus 是 Web 壳每次状态刷新收到的快照。
type FrontendStatus struct {
	Mode          string // "container" | "external"
	State         string // "starting" | "running" | "stopped"
	URL           string
	PID           int
	LastExit      string
	ExternalURL   string
	ConnectError  string
	Target        string // 前端 iframe 应加载的地址；空串表示显示引导页
	Busy          bool
	CanStart      bool
	CanStop       bool
	CanConnect    bool
	CanDisconnect bool
}

// ToolRow 是工具链表格的一行。
type ToolRow struct {
	Name    string
	Version string
	State   string // "installed" | "missing"
}

// ToolStatus 是工具自检与凭据分区的一次刷新结果。
type ToolStatus struct {
	Rows        []ToolRow
	Installed   string
	Installable string
	CredSaved   bool
	CredUser    string
	CredPath    string
}

// AboutInfo 是"关于"弹框的内容。
type AboutInfo struct {
	Program        string
	HarnessVersion string
	PackageVersion string
	Repo           string
}

// App 是绑定给 Web 壳的应用控制器。
type App struct {
	sup        *supervisor.Supervisor
	conn       *connector.Connector
	configPath string
	home       string
	ctx        context.Context

	mu           sync.Mutex
	externalBusy bool
}

// New 创建应用控制器并启动 harness 监护。
func New(cfg supervisor.Config, home, configPath string) *App {
	return &App{
		sup:        supervisor.NewSupervisor(cfg, supervisor.DefaultOptions()),
		conn:       connector.New(),
		configPath: configPath,
		home:       home,
	}
}

// ExternalConfigFilePath 返回外部 URL 配置文件路径。
func ExternalConfigFilePath() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "dsh-desktop", "config.json")
	}
	return filepath.Join(".cache", "dsh-desktop", "config.json")
}

// Shutdown 停止 harness 子进程（窗口关闭与外置信号两路共用；幂等）。
func (a *App) Shutdown() {
	a.sup.Stop()
}

// OnStartup 在窗口启动后保存上下文并开启 1s 状态轮询。
func (a *App) OnStartup(ctx context.Context) {
	a.ctx = ctx
	go a.tick(ctx)
}

// OnShutdown 在窗口关闭时停止 harness，避免子进程残留。
func (a *App) OnShutdown(_ context.Context) {
	a.sup.Stop()
}

func (a *App) tick(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.emitStatus()
		}
	}
}

// emitStatus 推送一次状态快照；ctx 尚未就绪时静默跳过。
func (a *App) emitStatus() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, StatusEvent, a.snapshot())
}

// emitToolchain 推送一次工具/凭据刷新结果。
func (a *App) emitToolchain(s ToolStatus) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, ToolchainEvent, s)
}

func (a *App) setBusy(b bool) {
	a.mu.Lock()
	a.externalBusy = b
	a.mu.Unlock()
}

func (a *App) isBusy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.externalBusy
}

// snapshot 组装当前完整状态。
func (a *App) snapshot() FrontendStatus {
	st := a.sup.Status()
	mode := a.conn.Mode()
	extURL := a.conn.ExternalURL()
	busy := a.isBusy()

	var target string
	switch mode {
	case domain.ModeExternal:
		target = extURL
	default:
		if st.State == domain.StateRunning {
			target = st.URL
		}
	}
	s := FrontendStatus{
		Mode:          modeName(mode),
		State:         stateName(st.State),
		URL:           st.URL,
		PID:           st.PID,
		LastExit:      st.LastExit,
		ExternalURL:   extURL,
		ConnectError:  a.conn.LastError(),
		Target:        target,
		Busy:          busy,
		CanStart:      st.State == domain.StateStopped && !busy,
		CanStop:       (st.State == domain.StateStarting || st.State == domain.StateRunning) && !busy,
		CanConnect:    mode == domain.ModeContainer && !busy,
		CanDisconnect: mode == domain.ModeExternal && !busy,
	}
	// 连接失败错误只在容器模式展示，成功后清除。
	if mode != domain.ModeExternal {
		s.ConnectError = a.conn.LastError()
	}
	return s
}

// resolveTarget 决定 Web 壳 iframe 应加载的目标：外部已连接优先于容器；
// 容器仅在运行中接管，其余返回空串（前端显示引导页）。纯函数便于单测。
func resolveTarget(mode domain.Mode, externalURL, containerURL string, running bool) string {
	if mode == domain.ModeExternal {
		return externalURL
	}
	if running {
		return containerURL
	}
	return ""
}

// Status 返回当前状态快照（前端首次加载调用）。
func (a *App) Status() FrontendStatus {
	return a.snapshot()
}

// StartServer 启动容器内 harness。
func (a *App) StartServer() FrontendStatus {
	a.sup.Start()
	a.emitStatus()
	return a.snapshot()
}

// StopServer 手动停止容器内 harness 并暂停自动重启。
func (a *App) StopServer() FrontendStatus {
	a.sup.StopHarness()
	a.emitStatus()
	return a.snapshot()
}

// ConnectExternal 校验并连接外部服务。确认与探测不阻塞前端：
// 校验失败立即返回错误文本；成功则后台探测，结果随状态事件推送。
func (a *App) ConnectExternal(raw string) string {
	if a.ctx == nil {
		return "应用尚未就绪"
	}
	u, err := a.conn.ValidateURL(raw)
	if err != nil {
		return "地址无效: " + err.Error()
	}
	if a.conn.NeedConfirmation(u) {
		selected, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
			Type:          runtime.QuestionDialog,
			Title:         "确认连接",
			Message:       "将连接远端 harness 服务 " + u + "，其命令在远端机器上执行，API key 等配置将发往该机器。确认连接？",
			Buttons:       []string{"连接", "取消"},
			DefaultButton: "取消",
		})
		if err != nil || selected != "连接" {
			return ""
		}
		a.conn.ConfirmHost(u)
	}

	// 连接前先停容器 harness（释放端口、暂停自动重启），避免端口冲突。
	a.sup.StopHarness()
	a.setBusy(true)
	a.emitStatus()
	go func() {
		err := a.conn.BeginExternal(u)
		if err == nil {
			_ = connector.SaveExternalURL(a.configPath, u)
		} else {
			a.sup.Restart() // 探测失败：恢复容器模式并重启 harness
		}
		a.setBusy(false)
		a.emitStatus()
	}()
	return ""
}

// DisconnectExternal 断开外部服务并重启容器 harness。
func (a *App) DisconnectExternal() {
	a.conn.EndExternal()
	a.setBusy(true)
	a.emitStatus()
	go func() {
		a.sup.Restart()
		select {
		case <-a.sup.Ready():
		case <-time.After(30 * time.Second):
		}
		a.setBusy(false)
		a.emitStatus()
	}()
}

// RefreshTools 后台刷新工具链自检与凭据状态，结果经事件推送。
func (a *App) RefreshTools() {
	go func() {
		toolStatus := a.collectTools()
		a.emitToolchain(toolStatus)
	}()
}

// collectTools 采集工具自检 + 已安装列表 + 凭据状态。
func (a *App) collectTools() ToolStatus {
	checks := toolchain.Check(toolchain.DefaultSpecs())
	installed := toolchain.ListInstalled(toolchain.InstallDir(a.home))
	user, _, found := gitcred.Read(a.home)

	rows := make([]ToolRow, 0, len(checks))
	for _, c := range checks {
		row := ToolRow{Name: c.Name, State: "missing"}
		if c.OK {
			row.State = "installed"
			row.Version = toolchain.VersionNumber(c.Version)
		}
		rows = append(rows, row)
	}
	return ToolStatus{
		Rows:        rows,
		Installed:   joinOrNone(installed),
		Installable: "go,ripgrep",
		CredSaved:   found,
		CredUser:    user,
		CredPath:    gitcred.Path(a.home),
	}
}

// SaveCredentials 保存 GitHub 凭据并刷新。
func (a *App) SaveCredentials(user, token string) string {
	if err := gitcred.Write(a.home, user, token); err != nil {
		return err.Error()
	}
	a.RefreshTools()
	return ""
}

// ClearCredentials 清除 GitHub 凭据并刷新。
func (a *App) ClearCredentials() {
	_ = gitcred.Clear(a.home)
	a.RefreshTools()
}

// About 返回关于弹框内容。
func (a *App) About() AboutInfo {
	return AboutInfo{
		Program:        "DeepSeek Harness",
		HarnessVersion: packaging.ResolveHarnessVersion(),
		PackageVersion: packaging.Version,
		Repo:           packaging.GithubRepo,
	}
}

func modeName(m domain.Mode) string {
	if m == domain.ModeExternal {
		return "external"
	}
	return "container"
}

func stateName(s domain.HarnessState) string {
	switch s {
	case domain.StateRunning:
		return "running"
	case domain.StateStarting:
		return "starting"
	default:
		return "stopped"
	}
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "无"
	}
	out := ""
	for i, it := range items {
		if i > 0 {
			out += ","
		}
		out += it
	}
	return out
}

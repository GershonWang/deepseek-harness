// Package app 是 Wails 绑定层：向 Web 壳暴露控制方法，并定时推送状态事件。
// 它编排下层各领域包（supervisor/connector/toolchain/hosttools/packaging），
// 不含任何业务实现，只做"翻译"：领域状态 → 前端可渲染的快照。
package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/appenv"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/clipboard"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/connector"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/hosttools"
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
	Mode               string // "container" | "external"
	State              string // "starting" | "running" | "stopped"
	URL                string
	PID                int
	LastExit           string
	ExternalURL        string
	ConnectError       string
	Target             string // 前端 iframe 应加载的地址；空串表示显示引导页
	Busy               bool
	StartupDiagnosing  bool // 是否正在进行启动失败自动诊断
	StartupDoctorReady bool // 自动诊断结果已就绪（本次失败周期内）
	CanStart           bool
	CanStop            bool
	CanConnect         bool
	CanDisconnect      bool
	SafeMode           string // "" | "plugins" | "config" | "full"
}

// ToolRow 是工具链表格的一行。
type ToolRow struct {
	Name    string
	Version string
	State   string // "installed" | "missing"
}

// ToolStatus 是工具自检的一次刷新结果。
type ToolStatus struct {
	Rows        []ToolRow
	Installed   string
	Installable string
	Catalog     []toolchain.CatalogStatus // 内置一键安装清单状态
	HostTools   []HostToolEntry           // 宿主命令挂载列表（仅沙箱环境）
	Sandboxed   bool                      // 是否玲珑打包（沙箱）环境
	Notice      string                    // 一次性提示（安装结果等）
	Installing  string                    // 正在安装的工具链名称（空串=无）
}

// HostToolEntry 是宿主命令挂载的渲染数据。
type HostToolEntry struct {
	Name    string
	Source  string
	Target  string
	Mounted bool // 本次实例启动时挂载是否生效（需重启应用才能更新）
}

// HostToolResult 是 AddHostTool 的返回。
type HostToolResult struct {
	Warning string
	Error   string
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
	dshCmd     string // dsh executable / node binary
	dshScript  string // path to dsh bin script (empty when dshCmd is itself the dsh bin)

	mu           sync.Mutex
	externalBusy bool
	safeMode     string // "plugins" | "config" | "full" | ""

	// 启动失败自动诊断状态（受 mu 保护；语义按失败周期计）。
	startupDoctorRunning  bool   // 自动诊断是否正在后台运行
	startupDoctorReady    bool   // 自动诊断结果是否已就绪（本失败周期内）
	startupDoctorDoneOnce bool   // 本次失败周期是否已触发过诊断（防重复触发）
	startupDoctorError    string // 自动诊断的 doctor 命令错误（非空表示诊断本身失败）
}

// New 创建应用控制器并启动 harness 监护。
func New(cfg supervisor.Config, home, configPath string) *App {
	// 推导 doctor 命令：Args 形如 ["web", "--port", "N"] 或 ["/path/to/bin.js", "web", "--port", "N"]
	// 后者表示 Command 是 node，第一个 arg 是 dsh 脚本路径。
	dshCmd := cfg.Command
	dshScript := ""
	if len(cfg.Args) >= 1 && strings.HasSuffix(cfg.Args[0], ".js") {
		dshScript = cfg.Args[0]
	}
	return &App{
		sup:        supervisor.NewSupervisor(cfg, supervisor.DefaultOptions()),
		conn:       connector.New(),
		configPath: configPath,
		home:       home,
		dshCmd:     dshCmd,
		dshScript:  dshScript,
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
	// 上一次状态，用于启动失败边沿检测（tick 是常驻 goroutine，局部变量即可）。
	prevState := a.sup.Status().State
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			curState := a.sup.Status().State
			a.trackStartupDoctor(prevState, curState)
			prevState = curState
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

// trackStartupDoctor 维护启动失败自动诊断状态：进入失败态触发一次后台诊断，
// 退出失败态（用户手动 Restart/StartSafeMode）重置本周期标志。
// 边沿检测幂等：仅状态真正变化时动作，每秒轮询不会重复触发。
func (a *App) trackStartupDoctor(prev, cur domain.HarnessState) {
	if maybeStartStartupDoctor(prev, cur, a.conn.Mode()) {
		a.startStartupDoctor()
		return
	}
	if exitFailedEdge(prev, cur) {
		a.resetStartupDoctor()
	}
}

// maybeStartStartupDoctor 判断状态边沿是否应自动触发启动失败诊断：
// 从非 failed 变为 failed，且处于容器模式（外置模式由用户显式触达，无需诊断）。
// 纯函数便于单测；同周期防重由调用方 startupDoctorDoneOnce 保证。
func maybeStartStartupDoctor(prev, cur domain.HarnessState, mode domain.Mode) bool {
	return prev != domain.StateFailed && cur == domain.StateFailed && mode == domain.ModeContainer
}

// exitFailedEdge 判断是否刚退出失败态（用户手动 Restart/StartSafeMode），
// 用于重置本失败周期的自动诊断状态。
func exitFailedEdge(prev, cur domain.HarnessState) bool {
	return prev == domain.StateFailed && cur != domain.StateFailed
}

// startStartupDoctor 触发一次后台自动诊断；本失败周期内只触发一次。
// RunDoctor 可能耗时数秒（执行 dsh doctor），放独立 goroutine 运行，不阻塞状态轮询。
func (a *App) startStartupDoctor() {
	a.mu.Lock()
	if a.startupDoctorDoneOnce {
		a.mu.Unlock()
		return
	}
	a.startupDoctorRunning = true
	a.startupDoctorDoneOnce = true
	a.mu.Unlock()

	go func() {
		report := a.RunDoctor()
		a.mu.Lock()
		if !a.startupDoctorRunning {
			// 周期已被退出失败态重置，丢弃过期结果。
			a.mu.Unlock()
			return
		}
		a.startupDoctorRunning = false
		a.startupDoctorReady = true
		a.startupDoctorError = report.Error
		a.mu.Unlock()
		a.emitStatus()
	}()
}

// resetStartupDoctor 清除本失败周期的自动诊断状态，等待下一次失败边沿重新触发。
func (a *App) resetStartupDoctor() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.startupDoctorRunning = false
	a.startupDoctorDoneOnce = false
	a.startupDoctorReady = false
	a.startupDoctorError = ""
}

// startupDoctorStatus 返回自动诊断的两个前端可见状态位。
func (a *App) startupDoctorStatus() (diagnosing, ready bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startupDoctorRunning, a.startupDoctorReady
}

// snapshot 组装当前完整状态。
func (a *App) snapshot() FrontendStatus {
	st := a.sup.Status()
	mode := a.conn.Mode()
	extURL := a.conn.ExternalURL()
	busy := a.isBusy()
	diagnosing, doctorReady := a.startupDoctorStatus()

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
		Mode:               modeName(mode),
		State:              stateName(st.State),
		URL:                st.URL,
		PID:                st.PID,
		LastExit:           st.LastExit,
		ExternalURL:        extURL,
		ConnectError:       a.conn.LastError(),
		Target:             target,
		Busy:               busy,
		StartupDiagnosing:  diagnosing,
		StartupDoctorReady: doctorReady,
		CanStart:           (st.State == domain.StateStopped || st.State == domain.StateFailed) && !busy,
		CanStop:            (st.State == domain.StateStarting || st.State == domain.StateRunning) && !busy,
		CanConnect:         mode == domain.ModeContainer && !busy,
		CanDisconnect:      mode == domain.ModeExternal && !busy,
		SafeMode:           a.safeMode,
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

// StartSafeMode 以插件安全模式启动 harness（跳过第三方 bundle，保留官方插件和用户数据）。
// 适用于升级后第三方插件不兼容导致启动失败的场景。
func (a *App) StartSafeMode() FrontendStatus {
	os.Setenv("DSH_SAFE_MODE", "plugins")
	a.safeMode = "plugins"
	a.sup.Restart()
	a.emitStatus()
	return a.snapshot()
}

// ExitSafeMode 退出安全模式，恢复正常启动。
func (a *App) ExitSafeMode() FrontendStatus {
	os.Unsetenv("DSH_SAFE_MODE")
	a.safeMode = ""
	a.sup.Restart()
	a.emitStatus()
	return a.snapshot()
}

// DoctorCheck 是诊断结果中的单条检查（与 doctor 包 DoctorReportCheckEntry 对齐的前端视图）。
type DoctorCheck struct {
	ID             string
	Name           string
	Category       string // "env" | "config" | "plugin" | "data"
	Severity       string // "info" | "warning" | "error" | "fatal"
	OK             bool
	Message        string
	Detail         string
	Fixable        bool
	SuggestedLevel int
}

// DoctorReport 是诊断结果的前端视图。
type DoctorReport struct {
	DshHome     string
	GeneratedAt string
	Checks      []DoctorCheck
	Total       int
	OK          int
	Failed      int
	Fatal       int
	Fixable     int
	Error       string // 非空表示 doctor 命令本身执行失败
}

// doctorEnv 构造 doctor 子进程环境：继承当前环境但剥离 DSH_SAFE_MODE，
// 再覆盖 DSH_HOME。安全模式会令 loadProfile 跳过第三方 bundle，若让
// doctor 继承它，诊断永远看不到真实安装中的第三方插件问题；
// 诊断必须反映完整安装状态，安全模式只是修复手段。
func (a *App) doctorEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "DSH_SAFE_MODE=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "DSH_HOME="+a.home)
}

// RunDoctor 运行 dsh doctor 并返回诊断结果。失败时 Error 字段包含错误信息。
func (a *App) RunDoctor() DoctorReport {
	args := []string{}
	if a.dshScript != "" {
		args = append(args, a.dshScript)
	}
	args = append(args, "doctor", "--json")

	cmd := exec.Command(a.dshCmd, args...)
	cmd.Env = a.doctorEnv()
	out, err := cmd.Output()
	if err != nil {
		return DoctorReport{Error: err.Error()}
	}

	// 用 json.RawMessage 先解一层结构
	var raw struct {
		DshHome     string `json:"dshHome"`
		GeneratedAt string `json:"generatedAt"`
		Checks      []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Category string `json:"category"`
			Severity string `json:"severity"`
			Result   struct {
				OK             bool   `json:"ok"`
				Message        string `json:"message"`
				Detail         string `json:"detail"`
				Fixable        bool   `json:"fixable"`
				SuggestedLevel int    `json:"suggestedLevel"`
			} `json:"result"`
		} `json:"checks"`
		Summary struct {
			Total   int `json:"total"`
			OK      int `json:"ok"`
			Failed  int `json:"failed"`
			Fatal   int `json:"fatal"`
			Fixable int `json:"fixable"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return DoctorReport{Error: "doctor output parse error: " + err.Error()}
	}

	checks := make([]DoctorCheck, 0, len(raw.Checks))
	for _, c := range raw.Checks {
		checks = append(checks, DoctorCheck{
			ID:             c.ID,
			Name:           c.Name,
			Category:       c.Category,
			Severity:       c.Severity,
			OK:             c.Result.OK,
			Message:        c.Result.Message,
			Detail:         c.Result.Detail,
			Fixable:        c.Result.Fixable,
			SuggestedLevel: c.Result.SuggestedLevel,
		})
	}

	return DoctorReport{
		DshHome:     raw.DshHome,
		GeneratedAt: raw.GeneratedAt,
		Checks:      checks,
		Total:       raw.Summary.Total,
		OK:          raw.Summary.OK,
		Failed:      raw.Summary.Failed,
		Fatal:       raw.Summary.Fatal,
		Fixable:     raw.Summary.Fixable,
	}
}

// RunDoctorRepair 运行指定级别的修复，返回修复结果摘要。
func (a *App) RunDoctorRepair(level int) string {
	args := []string{}
	if a.dshScript != "" {
		args = append(args, a.dshScript)
	}
	args = append(args, "doctor", "--repair", "1")
	if level >= 2 {
		args[len(args)-1] = "2"
	}
	if level >= 3 {
		args[len(args)-1] = "3"
	}

	cmd := exec.Command(a.dshCmd, args...)
	cmd.Env = a.doctorEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "修复失败: " + err.Error() + "\n" + string(out)
	}
	return string(out)
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

// collectTools 采集工具自检 + 已安装列表 + 清单状态 + 宿主挂载。
func (a *App) collectTools() ToolStatus {
	checks := toolchain.Check(toolchain.DefaultSpecs())
	dir := toolchain.InstallDir(a.home)
	installed := toolchain.ListInstalled(dir)

	rows := make([]ToolRow, 0, len(checks))
	for _, c := range checks {
		row := ToolRow{Name: c.Name, State: "missing"}
		if c.OK {
			row.State = "installed"
			row.Version = toolchain.VersionNumber(c.Version)
		}
		rows = append(rows, row)
	}

	hostTools := []HostToolEntry{}
	for _, e := range hosttools.List(a.home) {
		hostTools = append(hostTools, HostToolEntry{Name: e.Name, Source: e.Source, Target: e.Target, Mounted: e.Mounted})
	}

	return ToolStatus{
		Rows:        rows,
		Installed:   joinOrNone(installed),
		Installable: catalogInstallable(),
		Catalog:     toolchain.CatalogStatuses(dir),
		HostTools:   hostTools,
		Sandboxed:   a.sandboxed(),
	}
}

// sandboxed 判断是否玲珑打包（沙箱）环境：打包态可执行文件在 $PREFIX/bin，
// HarnessPrefix() 非空；开发态在 /tmp/go-build 下返回空。
func (a *App) sandboxed() bool {
	return packaging.HarnessPrefix() != ""
}

func catalogInstallable() string {
	names := []string{}
	for _, it := range toolchain.Catalog() {
		names = append(names, it.Name)
	}
	return strings.Join(names, ",")
}

// InstallToolchain 一键安装内置工具链。异步执行，结果经 toolchain 事件推送。
func (a *App) InstallToolchain(name string) string {
	item, ok := toolchain.Lookup(name)
	if !ok {
		return "未知工具链: " + name
	}
	// 立即推送"正在安装"状态，让前端实时显示。
	st := a.collectTools()
	st.Installing = name
	a.emitToolchain(st)
	go func() {
		dir := toolchain.InstallDir(a.home)
		err := toolchain.InstallFromCatalog(dir, item)
		notice := "工具链 " + name + " 安装成功"
		if err != nil {
			notice = "工具链 " + name + " 安装失败: " + err.Error()
		} else {
			// 安装后刷新环境注入（bin 软链已进 ~/.dsh-tools/bin）。
			appenv.ConfigureChildEnv(a.home)
		}
		st := a.collectTools()
		st.Notice = notice
		st.Installing = ""
		a.emitToolchain(st)
	}()
	return ""
}

// AddHostTool 把宿主命令路径挂载进沙箱（写 linglong config.d），返回冲突提示。
func (a *App) AddHostTool(source, name string) HostToolResult {
	if !a.sandboxed() {
		return HostToolResult{Error: "宿主挂载仅在玲珑打包环境生效（开发态宿主命令本就在 PATH）"}
	}
	if name == "" {
		name = hosttools.SuggestName(source)
	}
	e, warn, err := hosttools.Add(a.home, name, source)
	if err != nil {
		return HostToolResult{Error: err.Error()}
	}
	if conflicts := hostToolConflicts(e.Source, a.home); len(conflicts) > 0 {
		if warn != "" {
			warn += "；"
		}
		warn += "与按需安装同名的命令，宿主挂载优先生效: " + strings.Join(conflicts, ", ")
	}
	a.RefreshTools()
	return HostToolResult{Warning: warn}
}

// hostToolConflicts 返回宿主挂载源 bin 与按需安装 bin 同名的命令。
func hostToolConflicts(source, home string) []string {
	return toolchain.Conflicts(hosttools.EffectiveBin(source), filepath.Join(toolchain.InstallDir(home), "bin"))
}

// ListHostTools 返回已配置的宿主命令挂载。
func (a *App) ListHostTools() []HostToolEntry {
	out := []HostToolEntry{}
	for _, e := range hosttools.List(a.home) {
		out = append(out, HostToolEntry{Name: e.Name, Source: e.Source, Target: e.Target, Mounted: e.Mounted})
	}
	return out
}

// RemoveHostTool 移除宿主命令挂载配置。
func (a *App) RemoveHostTool(name string) string {
	if err := hosttools.Remove(a.home, name); err != nil {
		return err.Error()
	}
	a.RefreshTools()
	return ""
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

// ReadClipboardImage 读取当前 X11 CLIPBOARD 的 image/png 数据并返回
// base64 编码结果。剪贴板空、无图片或读取超时时返回空串（前端据此
// 判断“当前没有可粘贴的图片”），不视为错误。
//
// 背景：内嵌 WebKitGTK 的 paste 事件不暴露剪贴板位图，壳进程直接读
// X selection 反而完整可行；浏览器模式无此限制，该绑定只在 iframe
// 内嵌模式下被前端调用。
func (a *App) ReadClipboardImage() string {
	img, err := clipboard.ReadImage()
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(img)
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
	case domain.StateFailed:
		return "failed"
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

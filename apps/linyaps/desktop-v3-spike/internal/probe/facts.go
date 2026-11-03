// 本文件定义 spike 采集的事实模型与运行参数解析。
//
// 为什么把「采集什么」单独成文件：spike 的价值不在窗口能不能开起来，而在它产出的证据
// 能不能逐条对应升级前必须回答的问题（页面 origin、runtime 就绪时序、GPU 策略、真实链接的
// GTK/WebKit 版本）。事实字段一旦散落在 main 与 service 里，报告就会变成随手拼的日志，
// 后续评审无法确认某条结论到底有没有被采过。
package probe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ModulePath 是本 spike 模块的 import path。
//
// 单独写成常量而不是各处手抄：绑定全限定名（package.Type.Method）由它拼出，
// 前端与 Go 侧各算一次才有对照意义。
const ModulePath = "github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-v3-spike"

// 与服务绑定相关的常量。Service 类型定义在 package probe，因此全限定名里的包路径
// 是「模块路径 + /internal/probe」，而不是模块路径。
const (
	servicePackagePath = ModulePath + "/internal/probe"
	serviceTypeName    = "Service"
	serviceMethodName  = "Ping"
)

// BindingFQN 返回前端 Call.ByName 使用的全限定方法名。
//
// 为什么要 Go 侧也保留一份：报告同时记录「Go 认为的名字」与「前端实际调用的名字」，
// 两者不一致时能一眼看出是拼写/包路径问题，而不是先去怀疑绑定机制。
func BindingFQN() string {
	return servicePackagePath + "." + serviceTypeName + "." + serviceMethodName
}

// GoFacts 是只有 Go 进程才能知道的事实。
type GoFacts struct {
	// Module 是 spike 模块路径，便于报告与仓库文件对应。
	Module string `json:"module"`
	// Backend 是编译期图形栈（gtk4/gtk3/非 Linux），来自构建标签。
	Backend string `json:"backend"`
	// StackVersion 是运行时真实链接的 GTK 与 WebKitGTK 版本。
	StackVersion string `json:"stackVersion"`
	// GoVersion、GOOS、GOARCH 用于排除「在别的架构/工具链上复现不了」这类干扰。
	GoVersion string `json:"goVersion"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	// SessionType/Display/WaylandDisplay 决定 GTK 走 X11 还是 Wayland，
	// 无边框窗口拖拽与 resize 行为在两个后端上不同，报告必须带上。
	SessionType    string `json:"sessionType"`
	Display        string `json:"display"`
	WaylandDisplay string `json:"waylandDisplay"`
	// GPUPolicy 是本次实际传给窗口的硬件加速策略。
	GPUPolicy string `json:"gpuPolicy"`
	// PID、ReportPath、StartedAt 便于把多份报告与进程对应起来。
	PID        int    `json:"pid"`
	ReportPath string `json:"reportPath"`
	StartedAt  string `json:"startedAt"`
	// ExpectedBinding 是 Go 侧算出的绑定全限定名。
	ExpectedBinding string `json:"expectedBinding"`
}

// FrontendFacts 是壳页面回传的事实。
//
// 字段都用 JSON 标签固定命名：报告是给人和脚本同时看的，键名漂移会让历史报告失去可比性。
type FrontendFacts struct {
	// Href/Origin 回答最关键的一个问题：v3 下壳页面的 origin 是否仍是 wails://wails。
	// 客户端的 locale、字体、终端字号都存在该 origin 的 localStorage 里，origin 一变即全部丢失。
	Href   string `json:"href"`
	Origin string `json:"origin"`
	// UserAgent 用于识别 WebKitGTK 的真实版本（与 Go 侧 webkit_get_* 交叉验证）。
	UserAgent string `json:"userAgent"`
	// LocalStorageOK 证明该 origin 下可读可写；Detail 记录失败原因。
	LocalStorageOK     bool   `json:"localStorageOK"`
	LocalStorageDetail string `json:"localStorageDetail"`
	// RuntimeGlobal 表示 window.wails 是否存在。
	// 客户端现有静态壳用的是 window.go / window.runtime 全局；v3 不再注入这两个名字，
	// 只能靠 ES module 或运行时的全局导出做适配层，这条事实决定适配层怎么写。
	RuntimeGlobal bool `json:"runtimeGlobal"`
	// ClassicGlobals 表示旧的 window.go / window.runtime 是否仍存在（预期为 false）。
	ClassicGlobals bool `json:"classicGlobals"`
	// EarlyEventSeen 表示是否收到了服务启动期（窗口就绪前）发出的事件。
	// 客户端现在用早期事件驱动启动进度，v3 下这类事件会不会丢，必须实测。
	EarlyEventSeen bool `json:"earlyEventSeen"`
	// AnnounceSeen 表示是否收到 WindowRuntimeReady 之后的事件。
	AnnounceSeen bool `json:"announceSeen"`
	// DraggableValue 读取标题栏上的 --wails-draggable 计算值，
	// 用来确认 v3 仍认这个 CSS 属性（无边框窗口的拖拽完全依赖它）。
	DraggableValue string `json:"draggableValue"`
	// Screen 是 window.screen 的尺寸摘要，配合 DPI 缩放问题排查。
	Screen string `json:"screen"`
}

// BindingResult 记录一次 Call.ByName 的结果。
//
// 为什么绑定结果单独成字段而不用 Go 侧自查：绑定是前端 → Go 的单向调用，
// 成功与否只有前端知道；把它回传落盘才能区分「绑定路径写错」与「绑定机制不可用」。
type BindingResult struct {
	OK     bool   `json:"ok"`
	Reply  string `json:"reply"`
	Error  string `json:"error"`
	Called string `json:"called"`
}

// Report 是落盘报告的顶层结构。
type Report struct {
	// SchemaVersion 固定为 1：报告会被 spike 的后续迭代与升级评审反复比对。
	SchemaVersion int                `json:"schemaVersion"`
	GeneratedAt   string             `json:"generatedAt"`
	FlushReason   string             `json:"flushReason"`
	Go            GoFacts            `json:"go"`
	Frontend      *FrontendFacts     `json:"frontend,omitempty"`
	Binding       *BindingResult     `json:"binding,omitempty"`
	EarlyEmit     string             `json:"earlyEmit"`
	Marks         map[string]float64 `json:"marksMs"`
}

// marks 记录关键时点的相对耗时。
//
// 用包级变量是 spike 的有意取舍：时点由 main 里的窗口钩子写入、由 service 读取落盘，
// 两者之间隔着框架回调，注入一条链路只会让验证代码本身变成被验证对象。
var marks = struct {
	sync.Mutex
	values map[string]float64
}{values: map[string]float64{}}

// Mark 记录一个相对启动的时点（毫秒）。
//
// 同名时点保留第一次：spike 关心的是「第一次就绪」的时刻，重复触发不该覆盖它。
func Mark(name string, since time.Duration) {
	marks.Lock()
	defer marks.Unlock()
	if _, exists := marks.values[name]; exists {
		return
	}
	marks.values[name] = float64(since.Microseconds()) / 1000
}

// Marks 返回时点快照。
func Marks() map[string]float64 {
	marks.Lock()
	defer marks.Unlock()
	out := make(map[string]float64, len(marks.values))
	for k, v := range marks.values {
		out[k] = v
	}
	return out
}

// ReportPath 返回报告落盘路径。
//
// 默认放 XDG 状态目录而不是当前工作目录：玲珑容器里的 $PWD 可能是只读的，
// 而「报告写不出去」会伪装成 spike 挂掉，白白浪费一次真机验证。
func ReportPath() string {
	if p := os.Getenv("DSH_V3_SPIKE_REPORT"); p != "" {
		return p
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "dsh-v3-spike", "report.json")
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "dsh-v3-spike", "report.json")
}

// AutoClose 报告写完后是否自动退出。
//
// 供 verify.sh 使用：无人值守验证需要在拿到报告后自行结束进程，
// 否则脚本只能靠超时杀进程，把「程序卡住」和「验证通过」混为一谈。
func AutoClose() bool {
	return os.Getenv("DSH_V3_SPIKE_AUTOCLOSE") == "1"
}

// GPUPolicy 解析 DSH_V3_SPIKE_GPU 并返回窗口硬件加速策略。
//
// 取值只有 always 与 never：GTK4 用的 WebKitGTK 6.0 已删除 ON_DEMAND 策略，
// 传其它值一律落回 ALWAYS（见 wails v3.1.0 pkg/application/linux_cgo.go）。
func GPUPolicy() application.WebviewGpuPolicy {
	if os.Getenv("DSH_V3_SPIKE_GPU") == "never" {
		return application.WebviewGpuPolicyNever
	}
	return application.WebviewGpuPolicyAlways
}

// GPUPolicyName 返回 GPUPolicy 的可读名字，写进报告。
func GPUPolicyName() string {
	if os.Getenv("DSH_V3_SPIKE_GPU") == "never" {
		return "never"
	}
	return "always"
}

// GoFactsSnapshot 返回当前进程的 Go 侧事实。
func GoFactsSnapshot(started time.Time) GoFacts {
	return GoFacts{
		Module:          ModulePath,
		Backend:         BuildBackend,
		StackVersion:    RuntimeStackVersion(),
		GoVersion:       runtime.Version(),
		GOOS:            runtime.GOOS,
		GOARCH:          runtime.GOARCH,
		SessionType:     os.Getenv("XDG_SESSION_TYPE"),
		Display:         os.Getenv("DISPLAY"),
		WaylandDisplay:  os.Getenv("WAYLAND_DISPLAY"),
		GPUPolicy:       GPUPolicyName(),
		PID:             os.Getpid(),
		ReportPath:      ReportPath(),
		StartedAt:       started.Format(time.RFC3339),
		ExpectedBinding: BindingFQN(),
	}
}

// Preflight 在创建应用之前检查会污染结论的环境前提。
//
// 只拦「确定会失败且报错会被误读」的情况：没有显示服务时 GTK 会在半初始化状态退出，
// 现象与驱动问题、v3 兼容性问题都很像。其余一律放行，让真实失败暴露出来。
func Preflight() error {
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errors.New("没有 DISPLAY/WAYLAND_DISPLAY：请在桌面会话内运行（GTK 需要显示服务）")
	}
	path := ReportPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("报告目录不可写（%s）：%w", filepath.Dir(path), err)
	}
	return nil
}

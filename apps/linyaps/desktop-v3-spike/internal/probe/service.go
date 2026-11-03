// 本文件是 spike 的前端可见服务：一个是给绑定的最小方法，其余职责是接收前端事实并落盘。
//
// 为什么用事件而不是绑定回传事实：绑定（Call.ByName）本身正是待验证对象，一旦名字或
// 注册方式有误，事实就永远传不出来，报告会变成「什么都没发生」。事件通道不依赖绑定，
// 用它承载报告，失败也能落盘；绑定是否可用则由前端单独回传一条结果记录。
package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Service 是 spike 的服务实现。
//
// 依赖直接取自 application 实例而不是注入几个函数值：退出、发事件、订阅事件本来就是
// App 的能力，拆成可空回调只会多出「漏传一个就是空指针」的失败面——这正是第一版在真机上
// 踩到的坑。构造时校验一次 app 非空，其余全部由框架保证。
//
// 除 Ping 之外的字段与方法全部不导出或位于导出方法之外，避免误入绑定：
// 绑定只需要一个稳定的、签名极简的方法来验证调用链。
type Service struct {
	app        *application.App
	reportPath string
	autoClose  bool
	started    time.Time

	mu        sync.Mutex
	frontend  *FrontendFacts
	binding   *BindingResult
	earlyNote string
	written   int
}

// New 构造服务。
//
// app 必须非 nil：ServiceStartup 里要发早期事件、订阅前端事件，这些能力全部来自它。
// 与其在启动序列里以空指针崩溃，不如在装配处立刻停下并说清楚原因。
func New(app *application.App, reportPath string, autoClose bool, started time.Time) *Service {
	if app == nil {
		panic("probe.New: app 不能为 nil")
	}
	return &Service{
		app:        app,
		reportPath: reportPath,
		autoClose:  autoClose,
		started:    started,
	}
}

// ServiceStartup 在应用启动序列中执行，早于窗口就绪。
//
// 这里发出早期事件正是为了验证「v3 下窗口就绪前的事件会不会丢」：
// desktop-launcher 现在依赖这类事件推进启动进度条，v3 若会丢就必须改成
// 「就绪后重放一次」。
func (s *Service) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	delivered := s.app.Event.Emit("spike:early", map[string]any{
		"sinceStartMs": time.Since(s.started).Milliseconds(),
		"note":         "service startup, before window runtime ready",
	})
	s.mu.Lock()
	s.earlyNote = fmt.Sprintf("emitted at service startup, delivered=%v", delivered)
	s.mu.Unlock()
	Mark("service-startup", time.Since(s.started))

	// 订阅放在启动期：前端页面加载后先握手，再决定报告内容。
	s.app.Event.On("spike:hello", func(*application.CustomEvent) {
		// 重放 Go 侧事实：前端可能在 announce 之前就绪，握手时补发一次。
		s.app.Event.Emit("spike:go", GoFactsSnapshot(s.started))
	})
	s.app.Event.On("spike:frontend", func(event *application.CustomEvent) {
		facts := FrontendFacts{}
		if err := decodeEventData(event.Data, &facts); err != nil {
			slog.Warn("前端事实解析失败", "err", err)
			return
		}
		s.mu.Lock()
		s.frontend = &facts
		s.mu.Unlock()
		s.write("收到前端事实")
	})
	s.app.Event.On("spike:binding", func(event *application.CustomEvent) {
		result := BindingResult{}
		if err := decodeEventData(event.Data, &result); err != nil {
			slog.Warn("绑定结果解析失败", "err", err)
			return
		}
		s.mu.Lock()
		s.binding = &result
		s.mu.Unlock()
		s.write("收到绑定结果")
	})
	return nil
}

// Ping 是给绑定的最小可调用方法。
//
// 方法名与签名刻意保持稳定且简单（string → string）：绑定路径若失败，
// 原因就只可能出在名字解析或注册上，不会被参数序列化干扰。
func (s *Service) Ping(who string) string {
	return "pong:" + who
}

// Flush 用当前已知事实写一份报告。
//
// 关窗钩子里也要调一次：无人值守验证可能只跑到关窗，报告不能因为少了某个事件就为空。
func (s *Service) Flush(reason string) {
	s.write(reason)
}

// write 生成并落盘报告。
//
// 先写临时文件再 rename：报告被外部脚本并发读取时不会读到半截 JSON。
func (s *Service) write(reason string) {
	s.mu.Lock()
	report := Report{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		FlushReason:   reason,
		Go:            GoFactsSnapshot(s.started),
		Frontend:      s.frontend,
		Binding:       s.binding,
		EarlyEmit:     s.earlyNote,
		Marks:         Marks(),
	}
	shouldQuit := s.autoClose && s.frontend != nil
	s.written++
	s.mu.Unlock()

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		slog.Error("报告序列化失败", "err", err)
		return
	}
	data = append(data, '\n')
	path := s.reportPath
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		slog.Error("报告目录创建失败", "path", path, "err", err)
		return
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Error("报告写入失败", "path", tmp, "err", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Error("报告落盘失败", "path", path, "err", err)
		return
	}
	slog.Info("spike 报告已写入", "path", path, "reason", reason)

	if shouldQuit {
		// 留一点时间让日志刷出去，再退出；否则自动退出会把「刚写完」变成「没写完」。
		go func() {
			time.Sleep(200 * time.Millisecond)
			s.app.Quit()
		}()
	}
}

// decodeEventData 把事件载荷解码成目标结构。
//
// 前端发的对象经 JSON 传输后是 map[string]any，直接断言会 panic；
// 统一走一次序列化/反序列化，既稳妥也不用为每种载荷写断言分支。
func decodeEventData(data any, out any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("载荷序列化: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("载荷反序列化: %w", err)
	}
	return nil
}

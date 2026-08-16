# 桌面启动器:窗口居中、底部状态栏、服务器状态/关于弹框 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 `apps/desktop-launcher` 增加窗口居中、底部状态栏(左侧运行状态、右侧两个按钮)、服务器状态弹框(启动/重启/停止)和关于弹框(作者/仓库/版本)。

**Architecture:** 全部改动在 `apps/desktop-launcher`(独立 Go module,不入 pnpm workspace)。Supervisor 增加四态状态机与手动生命周期 API;GTK 层(cgo)在窗口底部挂状态栏、弹框用 GtkDialog/GtkAboutDialog;窗口用 `gtk_window_move` 居中。可测逻辑(状态机、文本格式化、版本解析)全部为纯 Go。

**Tech Stack:** Go + cgo + GTK3(webkit2gtk-4.1)、mock shell 脚本测试。

## Global Constraints

- 不修改 webview_go 依赖与上游源码;改动全部在 `apps/desktop-launcher/`。
- 保留系统标题栏;状态栏在窗口内容区**最底部**一行,右下角两个按钮。
- 可测逻辑必须纯 Go(状态机、格式化、版本解析);GTK 渲染层不做单测(headless 无显示),以 `go build`/`go vet` + 手动验证为门槛。
- 注释用中文;提交信息遵循仓库 conventional commits 英文风格(`feat(desktop-launcher): ...`)。
- 运行检查:`cd apps/desktop-launcher && go vet ./... && go build -o /dev/null . && go test ./... -count=1`。
- 每个任务结束提交一次。

---

### Task 1: Supervisor 状态机与手动生命周期 API

**Files:**
- Modify: `apps/desktop-launcher/supervisor.go`
- Modify: `apps/desktop-launcher/testdata/mock-dsh-web.sh`(已有,不动)
- Create: `apps/desktop-launcher/testdata/mock-exit-3.sh`
- Test: `apps/desktop-launcher/supervisor_control_test.go`

**Interfaces:**
- Produces(后续任务依赖的签名):
  - `type HarnessState int`;常量 `StateStarting`/`StateRunning`/`StateStopped`
  - `type HarnessStatus struct { State HarnessState; URL string; PID int; LastExit string }`
  - `func (s *Supervisor) Status() HarnessStatus`
  - `func (s *Supervisor) Start()`
  - `func (s *Supervisor) Restart()`
  - `func (s *Supervisor) StopHarness()`
  - Supervisor 新增字段:`state HarnessState`、`url string`、`pid int`、`lastExit string`、`manuallyStopped bool`、`startCh chan struct{}`

- [ ] **Step 1: 写失败测试**

新建 `apps/desktop-launcher/supervisor_control_test.go`:

```go
package main

import (
	"strings"
	"testing"
	"time"
)

// waitState 轮询直到 Status 达到指定状态,超时失败。
func waitState(t *testing.T, sup *Supervisor, want HarnessState) HarnessStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := sup.Status()
		if st.State == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待状态 %v 超时,当前 %v (%+v)", want, st.State, st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSupervisor_StatusRunningThenStopped(t *testing.T) {
	env := DesktopEnv{Command: "sh", Args: []string{"testdata/mock-dsh-web.sh"}, LogDir: t.TempDir(), Port: "0"}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()
	st := waitState(t, sup, StateRunning)
	if !strings.Contains(st.URL, "127.0.0.1:18080") {
		t.Fatalf("URL 应为 mock 端口,got %q", st.URL)
	}
	sup.StopHarness()
	st = waitState(t, sup, StateStopped)
	if !strings.Contains(st.LastExit, "signal=terminated") {
		t.Fatalf("LastExit 应为 SIGTERM 信号,got %q", st.LastExit)
	}
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_LastExitRecordsExitCode(t *testing.T) {
	env := DesktopEnv{Command: "sh", Args: []string{"testdata/mock-exit-3.sh"}, LogDir: t.TempDir(), Port: "0"}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()
	st := waitState(t, sup, StateStopped)
	if !strings.Contains(st.LastExit, "exited code=3") {
		t.Fatalf("LastExit 应为 exited code=3,got %q", st.LastExit)
	}
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_ManualStopPausesAutoRestart(t *testing.T) {
	env := DesktopEnv{Command: "sh", Args: []string{"testdata/mock-dsh-web.sh"}, LogDir: t.TempDir(), Port: "0"}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()
	waitState(t, sup, StateRunning)
	sup.StopHarness()
	waitState(t, sup, StateStopped)
	// 超过首个退避(500ms)后仍应保持 stopped,不被自动重启
	time.Sleep(1500 * time.Millisecond)
	if st := sup.Status(); st.State != StateStopped {
		t.Fatalf("手动停止后不应自动重启,state=%v", st.State)
	}
	// Start() 恢复运行
	sup.Start()
	waitState(t, sup, StateRunning)
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_RestartRespawns(t *testing.T) {
	env := DesktopEnv{Command: "sh", Args: []string{"testdata/mock-dsh-web.sh"}, LogDir: t.TempDir(), Port: "0"}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()
	old := waitState(t, sup, StateRunning)
	sup.Restart()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := sup.Status()
		if st.State == StateRunning && st.PID != old.PID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Restart 后 PID 未变化,old=%d current=%d", old.PID, sup.Status().PID)
		}
		time.Sleep(20 * time.Millisecond)
	}
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_StartWhileRunningIsNoop(t *testing.T) {
	env := DesktopEnv{Command: "sh", Args: []string{"testdata/mock-dsh-web.sh"}, LogDir: t.TempDir(), Port: "0"}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()
	old := waitState(t, sup, StateRunning)
	sup.Start()
	time.Sleep(200 * time.Millisecond)
	if st := sup.Status(); st.PID != old.PID {
		t.Fatalf("运行中 Start() 不应换进程,old=%d current=%d", old.PID, st.PID)
	}
	sup.Stop()
	sup.Wait()
}
```

- [ ] **Step 2: 建 mock-exit-3.sh 并跑测试确认失败**

创建 `apps/desktop-launcher/testdata/mock-exit-3.sh`(加执行位):

```sh
#!/bin/sh
# 诊断/测试 mock:打印就绪行后立即以退出码 3 退出。
echo "dsh web: http://127.0.0.1:18085"
exit 3
```

Run: `cd apps/desktop-launcher && chmod +x testdata/mock-exit-3.sh && go test -run "TestSupervisor_StatusRunningThenStopped|TestSupervisor_LastExitRecordsExitCode|TestSupervisor_ManualStopPausesAutoRestart|TestSupervisor_RestartRespawns|TestSupervisor_StartWhileRunningIsNoop" -v -count=1 -timeout 60s`

Expected: 编译失败(`Supervisor 无 Status/Start/Restart/StopHarness 字段或方法`)。

- [ ] **Step 3: 实现状态机(修改 supervisor.go)**

结构体与构造函数:

```go
// HarnessState 描述 harness 生命周期状态。
type HarnessState int

const (
	StateStarting HarnessState = iota // 已 spawn、未就绪
	StateRunning                      // 就绪行已匹配,存活
	StateStopped                      // 未运行(记录上次退出原因)
)

// HarnessStatus 是 UI 轮询用的 harness 快照。
type HarnessStatus struct {
	State    HarnessState
	URL      string // StateRunning 时的就绪地址
	PID      int    // 当前子进程 PID;停止时为 0
	LastExit string // 上次退出原因,如 "exited code=3" / "killed by signal=terminated";从未退出时为空
}
```

`Supervisor` 结构体新增字段(在 `exited` 字段后):

```go
	state          HarnessState
	url            string
	pid            int
	lastExit       string
	manuallyStopped bool   // 手动停止后暂停自动重启,直到 Start()/Restart()
	startCh        chan struct{} // 唤醒 run():Start/Restart/Stop 解除阻塞
```

`NewSupervisor` 初始化 `startCh`:

```go
	return &Supervisor{
		env:     env,
		options: options,
		ready:   make(chan string, 1),
		startCh: make(chan struct{}, 1),
	}
```

新增三个控制方法(放在 `Stop()` 之后):

```go
// Start 手动启动:仅停止态生效,恢复崩溃自动重启。
func (s *Supervisor) Start() {
	s.mu.Lock()
	if s.stopping || s.state != StateStopped {
		s.mu.Unlock()
		return
	}
	s.manuallyStopped = false
	s.mu.Unlock()
	select {
	case s.startCh <- struct{}{}:
	default:
	}
}

// Restart 手动重启:停止态直接唤醒 spawn,运行态先 SIGTERM 再唤醒。
func (s *Supervisor) Restart() {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	s.manuallyStopped = false
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		killProcessGroup(cmd, syscall.SIGTERM)
	}
	select {
	case s.startCh <- struct{}{}:
	default:
	}
}

// StopHarness 手动停止:杀当前 harness 并暂停自动重启,直到 Start()。
func (s *Supervisor) StopHarness() {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	s.manuallyStopped = true
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		killProcessGroup(cmd, syscall.SIGTERM)
	}
}

// Status 返回当前 harness 状态快照。
func (s *Supervisor) Status() HarnessStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return HarnessStatus{State: s.state, URL: s.url, PID: s.pid, LastExit: s.lastExit}
}
```

`Stop()` 在置 `stopping = true` 后、SIGTERM 前,加唤醒(解除 run() 在 `<-s.startCh` 的阻塞):

```go
	s.mu.Lock()
	s.stopping = true
	cmd := s.cmd
	cancel := s.cancel
	exited := s.exited
	s.mu.Unlock()

	// 唤醒 run()(可能阻塞在手动停止等待),让其检查 stopping 后退出
	select {
	case s.startCh <- struct{}{}:
	default:
	}
```

`run()` 循环改为(整体替换现有函数体):

```go
func (s *Supervisor) run() {
	attempt := 0
	for {
		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		manuallyStopped := s.manuallyStopped
		s.mu.Unlock()

		// 手动停止态:暂停自动重启,等 Start()/Restart() 唤醒
		if manuallyStopped {
			<-s.startCh
			s.mu.Lock()
			s.manuallyStopped = false
			attempt = 0
			s.mu.Unlock()
		}

		s.spawn()

		// 注意:不要在这里消费 s.ready——main() 的 <-sup.Ready() 负责
		// 接收就绪 URL 并打开窗口。本循环只负责监护:直接等子进程退出。
		s.mu.Lock()
		exited := s.exited
		s.mu.Unlock()
		if exited != nil {
			<-exited
		}

		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		manuallyStopped = s.manuallyStopped
		s.mu.Unlock()
		if manuallyStopped {
			continue // 回到顶部,进入手动停止等待
		}

		// 退避重启;startCh 可打断(Start/Restart 手动唤醒)
		attempt++
		delay := s.options.RestartDelayMs * (1 << (attempt - 1))
		if delay > s.options.MaxRestartDelayMs {
			delay = s.options.MaxRestartDelayMs
		}
		s.logf("[supervisor] restarting harness in %dms (attempt %d)", delay, attempt)
		select {
		case <-time.After(time.Duration(delay) * time.Millisecond):
		case <-s.startCh:
			attempt = 0
		}
	}
}
```

`spawn()` 状态重置与 PID 记录(在既有 `s.exited = exited` 的锁块中追加):

```go
	s.mu.Lock()
	s.logFile = logFile
	s.cancel = cancel
	s.cmd = cmd
	s.exited = exited
	s.state = StateStarting
	s.url = ""
	s.pid = 0
	s.lastExit = ""
	s.mu.Unlock()

	if err := cmd.Start(); err != nil {
		// 启动失败(如 node 二进制缺失):记录并立即放行等待方,由 run() 退避重启。
		s.logf("[supervisor] harness start failed: %v", err)
		close(exited)
		return
	}

	s.mu.Lock()
	s.pid = cmd.Process.Pid
	s.mu.Unlock()

	// 唯一调用 cmd.Wait() 的 goroutine:记录退出原因并关闭 exited。
	// 其余等待方(run/Stop/Wait)只等通道,避免 exec.CommandContext 的
	// ctxResult 只发一次、并发 Wait 的第二个调用者永久阻塞。
	go func() {
		err := cmd.Wait()
		reason := exitReason(cmd, err)
		if reason != "" {
			s.logf("[supervisor] harness %s", reason)
		}
		if err != nil {
			s.logf("[supervisor] harness wait error: %v", err)
		}
		s.mu.Lock()
		s.state = StateStopped
		s.pid = 0
		s.lastExit = reason
		s.mu.Unlock()
		close(exited)
	}()
}

// exitReason 把进程退出状态转成诊断字符串;空表示无退出状态。
func exitReason(cmd *exec.Cmd, err error) string {
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() && cmd.ProcessState.ExitCode() != -1 {
		return fmt.Sprintf("exited code=%d", cmd.ProcessState.ExitCode())
	}
	if cmd.ProcessState != nil {
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return fmt.Sprintf("killed by signal=%s", ws.Signal())
		}
		return "ended (no exit code)"
	}
	return ""
}
```

`readyScanner.Write` 匹配到就绪行时更新状态(在 `r.sup.ready <- match[1]` 前):

```go
		if match := readyPattern.FindStringSubmatch(line); match != nil {
			r.sup.markReady(match[1])
			select {
			case r.sup.ready <- match[1]:
			default:
			}
		}
```

新增 `markReady`(放在 `Status()` 后):

```go
// markReady 记录就绪地址并进入运行态。
func (s *Supervisor) markReady(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateRunning
	s.url = url
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd apps/desktop-launcher && go test -run "TestSupervisor_StatusRunningThenStopped|TestSupervisor_LastExitRecordsExitCode|TestSupervisor_ManualStopPausesAutoRestart|TestSupervisor_RestartRespawns|TestSupervisor_StartWhileRunningIsNoop" -v -count=1 -timeout 60s`

Expected: 全部 PASS。

- [ ] **Step 5: 回归既有测试**

Run: `cd apps/desktop-launcher && go vet ./... && go build -o /dev/null . && go test ./... -count=1 -timeout 60s`

Expected: 全部 PASS(含既有 supervisor_stop_test.go / supervisor_test.go)。

- [ ] **Step 6: 提交**

```bash
git add apps/desktop-launcher/supervisor.go apps/desktop-launcher/supervisor_control_test.go apps/desktop-launcher/testdata/mock-exit-3.sh
git commit -m "feat(desktop-launcher): add harness status API and manual lifecycle control"
```

---

### Task 2: 状态文本格式化与版本解析(纯函数)

**Files:**
- Create: `apps/desktop-launcher/ui_state.go`
- Create: `apps/desktop-launcher/version.go`
- Test: `apps/desktop-launcher/ui_state_test.go`、`apps/desktop-launcher/version_test.go`

**Interfaces:**
- Consumes: Task 1 的 `HarnessStatus`/`HarnessState`/`StateStarting`/`StateRunning`/`StateStopped`
- Produces:
  - `func statusBarText(st HarnessStatus) string`
  - `type ServerDialogState struct { State, Detail string; CanStart, CanRestart, CanStop bool }`
  - `func serverDialogState(st HarnessStatus) ServerDialogState`
  - `var packageVersion = "dev"`(由 prepare-offline.sh 用 `-ldflags "-X main.packageVersion=..."` 注入)
  - `func resolveHarnessVersion() string`
  - `func readVersion(path string) string`

- [ ] **Step 1: 写失败测试**

`apps/desktop-launcher/ui_state_test.go`:

```go
package main

import (
	"testing"
)

func TestStatusBarText(t *testing.T) {
	cases := []struct {
		name string
		st   HarnessStatus
		want string
	}{
		{"running", HarnessStatus{State: StateRunning, URL: "http://127.0.0.1:40275"}, "● 运行中 http://127.0.0.1:40275"},
		{"starting", HarnessStatus{State: StateStarting}, "● 启动中"},
		{"stopped with exit", HarnessStatus{State: StateStopped, LastExit: "exited code=3"}, "● 已停止 (exited code=3)"},
		{"stopped clean", HarnessStatus{State: StateStopped}, "● 已停止"},
	}
	for _, c := range cases {
		if got := statusBarText(c.st); got != c.want {
			t.Errorf("%s: statusBarText = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestServerDialogState(t *testing.T) {
	running := serverDialogState(HarnessStatus{State: StateRunning, URL: "http://127.0.0.1:40275", PID: 123})
	if running.State != "运行中" || !running.CanRestart || !running.CanStop || running.CanStart {
		t.Errorf("running 态错误:%+v", running)
	}
	if running.Detail != "地址: http://127.0.0.1:40275\nPID: 123" {
		t.Errorf("running Detail 错误:%q", running.Detail)
	}
	stopped := serverDialogState(HarnessStatus{State: StateStopped, LastExit: "killed by signal=terminated"})
	if stopped.State != "已停止" || !stopped.CanStart || stopped.CanRestart || stopped.CanStop {
		t.Errorf("stopped 态错误:%+v", stopped)
	}
	if stopped.Detail != "上次退出: killed by signal=terminated" {
		t.Errorf("stopped Detail 错误:%q", stopped.Detail)
	}
}
```

`apps/desktop-launcher/version_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadVersion(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkg, []byte(`{"name":"x","version":"1.2.3"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := readVersion(pkg); v != "1.2.3" {
		t.Fatalf("readVersion = %q, want 1.2.3", v)
	}
	if v := readVersion(filepath.Join(dir, "missing.json")); v != "" {
		t.Fatalf("readVersion(missing) = %q, want empty", v)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := readVersion(bad); v != "" {
		t.Fatalf("readVersion(bad json) = %q, want empty", v)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd apps/desktop-launcher && go test -run "TestStatusBarText|TestServerDialogState|TestReadVersion" -count=1`

Expected: 编译失败(`undefined: statusBarText`)。

- [ ] **Step 3: 实现纯函数**

`apps/desktop-launcher/ui_state.go`:

```go
package main

import "fmt"

// statusBarText 生成状态栏左侧指示文本(状态 + 端口/退出原因)。
func statusBarText(st HarnessStatus) string {
	switch st.State {
	case StateRunning:
		return "● 运行中 " + st.URL
	case StateStarting:
		return "● 启动中"
	default:
		if st.LastExit != "" {
			return "● 已停止 (" + st.LastExit + ")"
		}
		return "● 已停止"
	}
}

// ServerDialogState 是服务器状态弹框的一次刷新内容。
type ServerDialogState struct {
	State              string
	Detail             string
	CanStart, CanRestart, CanStop bool
}

// serverDialogState 由当前状态推导弹框文本与按钮可用性。
func serverDialogState(st HarnessStatus) ServerDialogState {
	switch st.State {
	case StateRunning:
		return ServerDialogState{
			State:      "运行中",
			Detail:     fmt.Sprintf("地址: %s\nPID: %d", st.URL, st.PID),
			CanStart:   false,
			CanRestart: true,
			CanStop:    true,
		}
	case StateStarting:
		return ServerDialogState{
			State:      "启动中",
			Detail:     "harness 正在启动…",
			CanStart:   false,
			CanRestart: true,
			CanStop:    true,
		}
	default:
		return ServerDialogState{
			State:      "已停止",
			Detail:     "上次退出: " + st.LastExit,
			CanStart:   true,
			CanRestart: false,
			CanStop:    false,
		}
	}
}
```

`apps/desktop-launcher/version.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// packageVersion 是玲珑包版本,由 prepare-offline.sh 通过
// -ldflags "-X main.packageVersion=..." 注入;本地 go build 未注入时为 "dev"。
var packageVersion = "dev"

// githubRepo 是项目仓库地址,用于关于弹框。
const githubRepo = "https://github.com/GershonWang/deepseek-harness"

type packageManifest struct {
	Version string `json:"version"`
}

// resolveHarnessVersion 读取打包态 $PREFIX/harness/package.json 或开发态
// ../cli/package.json 的版本号;都读不到时返回 "unknown"。
func resolveHarnessVersion() string {
	if p := harnessPrefix(); p != "" {
		if v := readVersion(filepath.Join(p, "harness", "package.json")); v != "" {
			return v
		}
	}
	cwd, _ := os.Getwd()
	if v := readVersion(filepath.Join(cwd, "..", "cli", "package.json")); v != "" {
		return v
	}
	return "unknown"
}

// harnessPrefix 返回打包态 prefix(files 目录);开发态可执行文件在
// /tmp/go-build... 时返回空。
func harnessPrefix() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(exe))
}

// readVersion 读取 package.json 的 version 字段;文件缺失或解析失败返回空串。
func readVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m packageManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m.Version
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd apps/desktop-launcher && go test -run "TestStatusBarText|TestServerDialogState|TestReadVersion" -v -count=1`

Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add apps/desktop-launcher/ui_state.go apps/desktop-launcher/version.go apps/desktop-launcher/ui_state_test.go apps/desktop-launcher/version_test.go
git commit -m "feat(desktop-launcher): add status text formatting and version resolution"
```

---

### Task 3: GTK 状态栏、弹框与窗口居中

**Files:**
- Create: `apps/desktop-launcher/ui.go`
- Modify: `apps/desktop-launcher/window.go`
- Modify: `apps/desktop-launcher/linglong/prepare-offline.sh`
- Test: 无单测(GTK 需显示);以 `go build`/`go vet` + 手动运行验证

**Interfaces:**
- Consumes: Task 1 的 `*Supervisor`(`Status`/`Start`/`Restart`/`StopHarness`),Task 2 的 `statusBarText`/`serverDialogState`/`resolveHarnessVersion`/`packageVersion`/`githubRepo`
- Produces:
  - `func installDesktopUI(win unsafe.Pointer, sup *Supervisor)`(window.go 调用)
  - 包级全局:`activeSupervisor *Supervisor`、`mainWindow *C.GtkWindow`、`statusLabel *C.GtkWidget`、`serverDialog *C.GtkWidget`

- [ ] **Step 1: 写 ui.go(GTK cgo 实现)**

`apps/desktop-launcher/ui.go`(完整文件):

```go
package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
#include <stdint.h>
#include <stdlib.h>

extern void dshOnServerStatusClicked(void);
extern void dshOnAboutClicked(void);
extern void dshRefreshStatus(void);
extern void dshOnServerStart(void);
extern void dshOnServerRestart(void);
extern void dshOnServerStop(void);
extern void dshOnServerDialogDestroyed(void);

// ---- 窗口居中:按屏幕尺寸移动窗口到中心 ----
static void dsh_center_window(GtkWindow *win, gint ww, gint wh) {
  GdkScreen *screen = gtk_window_get_screen(win);
  if (screen == NULL) {
    return;
  }
  gint sw = gdk_screen_get_width(screen);
  gint sh = gdk_screen_get_height(screen);
  gint x = (sw - ww) / 2;
  gint y = (sh - wh) / 2;
  gtk_window_move(win, x > 0 ? x : 0, y > 0 ? y : 0);
}

// ---- 状态栏按钮回调 ----
static void dsh_server_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerStatusClicked(); }
static void dsh_about_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnAboutClicked(); }

// ---- 把 webview 摘进 vbox,底部插状态栏;返回状态指示 label ----
static GtkWidget *dsh_install_status_bar(GtkWindow *win) {
  GtkWidget *webview = gtk_bin_get_child(GTK_BIN(win));
  GtkWidget *vbox = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);
  GtkWidget *bar = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 4);
  GtkWidget *label = gtk_label_new("● 启动中");
  gtk_widget_set_halign(label, GTK_ALIGN_START);
  GtkWidget *btn_server = gtk_button_new_with_label("服务器状态");
  GtkWidget *btn_about = gtk_button_new_with_label("关于");
  g_signal_connect(btn_server, "clicked", G_CALLBACK(dsh_server_clicked), NULL);
  g_signal_connect(btn_about, "clicked", G_CALLBACK(dsh_about_clicked), NULL);
  gtk_box_pack_start(GTK_BOX(bar), label, TRUE, TRUE, 4);
  gtk_box_pack_end(GTK_BOX(bar), btn_about, FALSE, FALSE, 4);
  gtk_box_pack_end(GTK_BOX(bar), btn_server, FALSE, FALSE, 4);
  gtk_container_remove(GTK_CONTAINER(win), webview);
  gtk_box_pack_start(GTK_BOX(vbox), webview, TRUE, TRUE, 0);
  gtk_box_pack_start(GTK_BOX(vbox), bar, FALSE, FALSE, 0);
  gtk_container_add(GTK_CONTAINER(win), vbox);
  gtk_widget_show_all(vbox);
  return label;
}

// ---- 状态轮询(1s,GTK 主循环回调) ----
static gboolean dsh_status_tick(gpointer d) {
  (void)d;
  dshRefreshStatus();
  return G_SOURCE_CONTINUE;
}

// ---- 服务器状态弹框 ----
static void dsh_server_start_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerStart(); }
static void dsh_server_restart_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerRestart(); }
static void dsh_server_stop_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerStop(); }
static void dsh_server_dialog_destroyed(GtkWidget *w, gpointer d) { (void)w; (void)d; dshOnServerDialogDestroyed(); }
static void dsh_dialog_response(GtkDialog *dlg, gint resp, gpointer d) {
  (void)d;
  if (resp == GTK_RESPONSE_CLOSE || resp == GTK_RESPONSE_DELETE_EVENT) {
    gtk_widget_destroy(GTK_WIDGET(dlg));
  }
}

static GtkWidget *dsh_make_server_dialog(GtkWindow *parent) {
  GtkWidget *dlg = gtk_dialog_new_with_buttons(
      "服务器状态", parent, GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
      "_关闭", GTK_RESPONSE_CLOSE, NULL);
  g_signal_connect(dlg, "response", G_CALLBACK(dsh_dialog_response), NULL);
  g_signal_connect(dlg, "destroy", G_CALLBACK(dsh_server_dialog_destroyed), NULL);
  GtkWidget *content = gtk_dialog_get_content_area(GTK_DIALOG(dlg));
  GtkWidget *grid = gtk_grid_new();
  gtk_grid_set_row_spacing(GTK_GRID(grid), 8);
  gtk_grid_set_column_spacing(GTK_GRID(grid), 8);
  GtkWidget *l_state = gtk_label_new("状态: …");
  GtkWidget *l_detail = gtk_label_new("");
  gtk_widget_set_halign(l_state, GTK_ALIGN_START);
  gtk_widget_set_halign(l_detail, GTK_ALIGN_START);
  GtkWidget *btn_start = gtk_button_new_with_label("启动");
  GtkWidget *btn_restart = gtk_button_new_with_label("重启");
  GtkWidget *btn_stop = gtk_button_new_with_label("停止");
  g_signal_connect(btn_start, "clicked", G_CALLBACK(dsh_server_start_clicked), NULL);
  g_signal_connect(btn_restart, "clicked", G_CALLBACK(dsh_server_restart_clicked), NULL);
  g_signal_connect(btn_stop, "clicked", G_CALLBACK(dsh_server_stop_clicked), NULL);
  gtk_grid_attach(GTK_GRID(grid), l_state, 0, 0, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), l_detail, 0, 1, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), btn_start, 0, 2, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), btn_restart, 1, 2, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), btn_stop, 2, 2, 1, 1);
  gtk_container_add(GTK_CONTAINER(content), grid);
  gtk_widget_show_all(dlg);
  return dlg;
}

// ---- 刷新服务器弹框内容与按钮可用性(tick 调用) ----
static void dsh_update_server_dialog(GtkWidget *dlg, const char *state, const char *detail,
                                     gboolean can_start, gboolean can_restart, gboolean can_stop) {
  GtkWidget *content = gtk_dialog_get_content_area(GTK_DIALOG(dlg));
  GList *content_kids = gtk_container_get_children(GTK_CONTAINER(content));
  GtkWidget *grid = GTK_WIDGET(g_list_nth_data(content_kids, 0));
  g_list_free(content_kids);
  GList *kids = gtk_container_get_children(GTK_CONTAINER(grid));
  GtkWidget *l_state = GTK_WIDGET(g_list_nth_data(kids, 0));
  GtkWidget *l_detail = GTK_WIDGET(g_list_nth_data(kids, 1));
  GtkWidget *btn_start = GTK_WIDGET(g_list_nth_data(kids, 2));
  GtkWidget *btn_restart = GTK_WIDGET(g_list_nth_data(kids, 3));
  GtkWidget *btn_stop = GTK_WIDGET(g_list_nth_data(kids, 4));
  gtk_label_set_text(GTK_LABEL(l_state), state);
  gtk_label_set_text(GTK_LABEL(l_detail), detail);
  gtk_widget_set_sensitive(btn_start, can_start);
  gtk_widget_set_sensitive(btn_restart, can_restart);
  gtk_widget_set_sensitive(btn_stop, can_stop);
  g_list_free(kids);
}

// ---- 关于弹框(GtkAboutDialog,run 阻塞式) ----
static void dsh_show_about_dialog(GtkWindow *parent, const char *program,
                                  const char *version, const char *comments,
                                  const char *website, const char *author) {
  const char *authors[] = { author, NULL };
  GtkWidget *dlg = gtk_about_dialog_new();
  gtk_about_dialog_set_program_name(GTK_ABOUT_DIALOG(dlg), program);
  gtk_about_dialog_set_version(GTK_ABOUT_DIALOG(dlg), version);
  gtk_about_dialog_set_comments(GTK_ABOUT_DIALOG(dlg), comments);
  gtk_about_dialog_set_website(GTK_ABOUT_DIALOG(dlg), website);
  gtk_about_dialog_set_website_label(GTK_ABOUT_DIALOG(dlg), website);
  gtk_about_dialog_set_authors(GTK_ABOUT_DIALOG(dlg), authors);
  gtk_window_set_transient_for(GTK_WINDOW(dlg), parent);
  gtk_dialog_run(GTK_DIALOG(dlg));
  gtk_widget_destroy(dlg);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// 包级 UI 状态:单一窗口实例,由 installDesktopUI 初始化,GTK 回调使用。
var (
	activeSupervisor *Supervisor
	mainWindow       *C.GtkWindow
	statusLabel      *C.GtkWidget
	serverDialog     *C.GtkWidget
)

// installDesktopUI 挂载底部状态栏、注册 1s 状态轮询并居中窗口。
// 必须在 w.Run() 之前调用;win 来自 webview.WebView.Window()。
func installDesktopUI(win unsafe.Pointer, sup *Supervisor) {
	if win == nil {
		return
	}
	activeSupervisor = sup
	mainWindow = (*C.GtkWindow)(win)
	statusLabel = C.dsh_install_status_bar(mainWindow)
	C.dsh_center_window(mainWindow, 1280, 800)
	C.g_timeout_add(1000, C.GSourceFunc(C.dsh_status_tick), nil)
	dshRefreshStatus()
}

//export dshRefreshStatus
func dshRefreshStatus() {
	sup := activeSupervisor
	if sup == nil {
		return
	}
	st := sup.Status()

	bar := C.CString(statusBarText(st))
	C.gtk_label_set_text((*C.GtkLabel)(unsafe.Pointer(statusLabel)), bar)
	C.free(unsafe.Pointer(bar))

	if serverDialog != nil {
		d := serverDialogState(st)
		state := C.CString(d.State)
		detail := C.CString(d.Detail)
		C.dsh_update_server_dialog(serverDialog, state, detail,
			boolToGboolean(d.CanStart), boolToGboolean(d.CanRestart), boolToGboolean(d.CanStop))
		C.free(unsafe.Pointer(state))
		C.free(unsafe.Pointer(detail))
	}
}

//export dshOnServerStatusClicked
func dshOnServerStatusClicked() {
	if activeSupervisor == nil {
		return
	}
	if serverDialog != nil {
		C.gtk_window_present((*C.GtkWindow)(unsafe.Pointer(serverDialog)))
		return
	}
	serverDialog = C.dsh_make_server_dialog(mainWindow)
	dshRefreshStatus()
}

//export dshOnServerDialogDestroyed
func dshOnServerDialogDestroyed() {
	serverDialog = nil
}

//export dshOnServerStart
func dshOnServerStart() {
	if activeSupervisor != nil {
		activeSupervisor.Start()
	}
}

//export dshOnServerRestart
func dshOnServerRestart() {
	if activeSupervisor != nil {
		activeSupervisor.Restart()
	}
}

//export dshOnServerStop
func dshOnServerStop() {
	if activeSupervisor != nil {
		activeSupervisor.StopHarness()
	}
}

//export dshOnAboutClicked
func dshOnAboutClicked() {
	version := fmt.Sprintf("harness %s\n玲珑包 %s", resolveHarnessVersion(), packageVersion)
	prog := C.CString("DeepSeek Harness")
	ver := C.CString(version)
	comments := C.CString("DeepSeek Harness 桌面客户端,以受监护子进程运行 harness 并加载其 Web GUI。")
	website := C.CString(githubRepo)
	author := C.CString("GershonWang")
	C.dsh_show_about_dialog(mainWindow, prog, ver, comments, website, author)
	C.free(unsafe.Pointer(prog))
	C.free(unsafe.Pointer(ver))
	C.free(unsafe.Pointer(comments))
	C.free(unsafe.Pointer(website))
	C.free(unsafe.Pointer(author))
}

func boolToGboolean(b bool) C.gboolean {
	if b {
		return 1
	}
	return 0
}
```

- [ ] **Step 2: 接线 window.go**

`apps/desktop-launcher/window.go` 的 `openWindow` 中,在 `w.Navigate(url)` 之前插入(需 import `"unsafe"`):

```go
	// 底部状态栏、服务器/关于按钮、状态轮询与窗口居中
	installDesktopUI(w.Window(), sup)
```

- [ ] **Step 3: 注入玲珑包版本(prepare-offline.sh)**

`apps/desktop-launcher/linglong/prepare-offline.sh` 第 3 步 go build 前加版本提取,并给 go build 加 `-ldflags`:

```sh
# 3. Go 启动器(webkit2gtk-4.0 pkg-config shim 指向 4.1)
#    必须在 module 目录内构建:仓库根没有 go.mod,从根 go build 会报
#    "cannot find main module"
sh apps/desktop-launcher/linglong/prepare-pkgconfig.sh /tmp/dsh-pkgconfig
# 注入玲珑包版本到关于弹框(从 linglong.yaml 的 package.version 提取)
LL_VERSION=$(grep -oP '^\s+version: \K[0-9.]+' apps/desktop-launcher/linglong/linglong.yaml | head -1)
( cd apps/desktop-launcher && PKG_CONFIG_PATH=/tmp/dsh-pkgconfig CGO_ENABLED=1 \
  go build -ldflags "-X main.packageVersion=$LL_VERSION" -o "$ROOT/$STAGE/bin/dsh-desktop-launcher" . )
```

- [ ] **Step 4: 编译与静态检查**

Run: `cd apps/desktop-launcher && go vet ./... && go build -o /dev/null .`

Expected: 无输出、退出码 0。若报 `g_list_nth_data`/GTK 符号未定义,确认 `pkg-config gtk+-3.0` 环境(参考 `prepare-pkgconfig.sh` 生成的 `PKG_CONFIG_PATH`)。

- [ ] **Step 5: 手动运行验证**

Run: `cd apps/desktop-launcher && PKG_CONFIG_PATH=/tmp/dsh-pkgconfig go build -o dsh-desktop-launcher . && DSH_DESKTOP_DSH_BIN="$(pwd)/testdata/mock-dsh-web.sh" ./dsh-desktop-launcher`

Expected(有显示环境):
1. 窗口出现在屏幕中央。
2. 窗口底部出现状态栏:左侧先"● 启动中",mock 就绪后变"● 运行中 http://127.0.0.1:18080"。
3. 点"服务器状态"弹框显示"运行中"与地址/PID,重启/停止可点;点"停止"后状态变"已停止"且不自动重启,再点"启动"恢复。
4. 点"关于"弹框显示程序名、作者 GershonWang、仓库链接、harness 版本与"玲珑包 dev"。
5. 关闭窗口,程序正常退出(不残留进程)。

完成后 `kill %1` 清理 mock 进程。

- [ ] **Step 6: 回归测试 + 提交**

Run: `cd apps/desktop-launcher && go test ./... -count=1 -timeout 60s`

```bash
git add apps/desktop-launcher/ui.go apps/desktop-launcher/window.go apps/desktop-launcher/linglong/prepare-offline.sh
git commit -m "feat(desktop-launcher): add status bar, server/about dialogs, centered window"
```

---

### Task 4: 文档与收尾验证

**Files:**
- Modify: `apps/desktop-launcher/README.md`
- Modify: `apps/desktop-launcher/linglong/linglong.yaml`(如需更新版本号 0.1.0.9 → 0.1.0.10 以重新出包)

**Interfaces:**
- Consumes: 前三个任务的产物
- Produces: 更新后的 README

- [ ] **Step 1: 更新 README**

`apps/desktop-launcher/README.md` 的文件结构表加两行:

```markdown
| `ui.go` | 底部状态栏、服务器状态/关于弹框、窗口居中(GTK cgo) |
| `version.go` | harness/玲珑版本解析(`packageVersion` 由 prepare-offline 注入) |
```

在"玲珑打包"一节补一行:

```markdown
- 玲珑包版本由 prepare-offline 从 linglong.yaml 提取并注入 launcher(`-ldflags -X main.packageVersion=...`),关于弹框展示
```

- [ ] **Step 2: 全量检查**

Run: `cd apps/desktop-launcher && go vet ./... && go build -o /dev/null . && go test ./... -count=1 -timeout 60s`

Expected: 全部通过。

- [ ] **Step 3: 重新打包验证(可选但推荐)**

```sh
sh apps/desktop-launcher/linglong/prepare-offline.sh
ll-builder build -f apps/desktop-launcher/linglong/linglong.yaml
ll-builder export --ref main:org.deepseek.dsh-desktop/0.1.0.9/x86_64
ll-cli run org.deepseek.dsh-desktop
```

Expected: 沙箱内窗口居中、底部状态栏显示"运行中"+ 端口,两个弹框正常。

- [ ] **Step 4: 提交**

```bash
git add apps/desktop-launcher/README.md
git commit -m "docs(desktop-launcher): document status bar, dialogs, and version injection"
```

---

## Self-Review

**规格覆盖:** 窗口居中(Task 3 Step 1 `dsh_center_window`)、底部状态栏 + 左侧状态指示 + 右侧两按钮(Task 3)、服务器状态弹框(状态/地址/退出原因 + 启动/重启/停止,Tasks 1+2+3)、关于弹框(作者/仓库链接/harness 版本/玲珑版本,Tasks 2+3)、版本注入(Task 3 Step 3)、测试(状态机 Task 1、纯函数 Task 2)。规格全项落地。

**占位符扫描:** 无 TBD/TODO;每个步骤含完整代码与预期输出。

**类型一致性:** `HarnessStatus`(State/URL/PID/LastExit)在 Task 1 定义、Task 2 消费;`stateBarText`/`serverDialogState` 在 Task 2 定义、Task 3 消费;`packageVersion`/`resolveHarnessVersion`/`githubRepo` 在 Task 2 定义、Task 3 消费;`installDesktopUI` 在 Task 3 定义、window.go 调用。名称全链路一致。

**已知取舍:** GTK 层无单测(需显示环境),以 `go build`/`go vet` + Task 3 Step 5 手动验证兜底;`gtk_window_move` 在 Wayland 下可能被合成器忽略(kwin_x11 不受影响),README 已注明于规格,实现时若需可补充说明。

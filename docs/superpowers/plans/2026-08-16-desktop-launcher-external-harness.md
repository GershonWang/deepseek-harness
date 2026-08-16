# 桌面启动器:外部 harness 连接 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 服务器状态弹框新增"本机/远端服务"连接模式:用户可在容器内 harness 与外部 harness(本机 `npx @deepseek-ai/dsh web` 或网络可达的其他机器)之间切换,切外部先停容器 harness,断开自动回容器模式。

**Architecture:** 全部改动在 `apps/desktop-launcher/`。新增纯 Go 的 `connection.go`(探测/URL 校验/持久化/连接状态,可单测);`ui.go` 弹框重设计(模式切换 + URL 输入 + 连接/断开,视觉交 designer);`window.go` 注入 webview `Navigate` 闭包;`supervisor.go` 不动(复用 Start/StopHarness/Restart)。

**Tech Stack:** Go + cgo + GTK3;httptest 测 HTTP 探测;mock shell 脚本测连接状态机。

## Global Constraints

- 不修改 webview_go 依赖与上游源码;改动全部在 `apps/desktop-launcher/`。
- 切外部模式必须先停容器 harness(`StopHarness`,暂停自动重启);断开自动回容器(`Restart()` + 等就绪 + 导航回)。
- 外部 URL 记忆 + 自动填充,不自动重连;非 loopback 地址连接前弹确认(会话内同 host 只弹一次)。
- 所有 `Navigate` 与 GTK 调用在 GTK 主线程;探测在 goroutine,结果经 `g_idle_add` 回主线程。
- 可测逻辑纯 Go(探测/loopback 判断/持久化/连接器状态);GTK 层构建 + 手动验证。
- 注释用中文;提交信息英文 conventional commits(`feat(desktop-launcher): ...`)。
- 运行检查:`cd apps/desktop-launcher && go vet ./... && go build -o /dev/null . && go test ./... -count=1 -timeout 60s`。
- 每个任务结束提交一次。

---

### Task 1: connection.go 纯 Go 核心 + 测试

**Files:**
- Create: `apps/desktop-launcher/connection.go`
- Test: `apps/desktop-launcher/connection_test.go`

**Interfaces:**
- Produces(后续任务依赖的签名):
  - `type Mode int`;常量 `ModeContainer`/`ModeExternal`
  - `func isLoopbackHost(host string) bool`
  - `func probe(rawURL string, timeout time.Duration) error`
  - `func loadExternalURL(path string) string` / `func saveExternalURL(path string, rawURL string) error`
  - `type Connector struct`(字段私有);`func NewConnector() *Connector`
  - `(*Connector) Mode() Mode` / `ExternalURL() string` / `LastError() string`
  - `(*Connector) ValidateURL(raw string) (string, error)`
  - `(*Connector) NeedConfirmation(rawURL string) bool` / `ConfirmHost(rawURL string)`
  - `(*Connector) BeginExternal(rawURL string) error` / `EndExternal()`

- [ ] **Step 1: 写失败测试**

创建 `apps/desktop-launcher/connection_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":   true,
		"localhost":   true,
		"::1":         true,
		"[::1]":       true,
		"192.168.1.50": false,
		"8.8.8.8":     false,
		"example.com": false,
	}
	for host, want := range cases {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestProbe(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()
	if err := probe(ok.URL, time.Second); err != nil {
		t.Fatalf("probe 200 应成功,got %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	if err := probe(bad.URL, time.Second); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("probe 500 应失败并含状态码,got %v", err)
	}

	// 未监听端口:连接拒绝
	if err := probe("http://127.0.0.1:1", time.Second); err == nil {
		t.Fatal("probe 未监听端口应失败")
	}
}

func TestExternalURLPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if v := loadExternalURL(path); v != "" {
		t.Fatalf("缺失文件应返回空串,got %q", v)
	}
	if err := saveExternalURL(path, "http://127.0.0.1:3456"); err != nil {
		t.Fatal(err)
	}
	if v := loadExternalURL(path); v != "http://127.0.0.1:3456" {
		t.Fatalf("roundtrip 失败,got %q", v)
	}
	if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := loadExternalURL(path); v != "" {
		t.Fatalf("损坏 JSON 应返回空串,got %q", v)
	}
}

func TestConnector_ValidateURL(t *testing.T) {
	c := NewConnector()
	for _, bad := range []string{"", "ftp://x", "not a url", "http://"} {
		if _, err := c.ValidateURL(bad); err == nil {
			t.Errorf("ValidateURL(%q) 应失败", bad)
		}
	}
	if u, err := c.ValidateURL("  http://127.0.0.1:3456  "); err != nil || u != "http://127.0.0.1:3456" {
		t.Errorf("ValidateURL 规范化失败,got %q, %v", u, err)
	}
}

func TestConnector_Confirmation(t *testing.T) {
	c := NewConnector()
	if c.NeedConfirmation("http://127.0.0.1:3456") {
		t.Fatal("loopback 不应需要确认")
	}
	if c.NeedConfirmation("http://localhost:3456") {
		t.Fatal("localhost 不应需要确认")
	}
	if !c.NeedConfirmation("http://192.168.1.50:3456") {
		t.Fatal("非 loopback 应需要确认")
	}
	c.ConfirmHost("http://192.168.1.50:3456")
	if c.NeedConfirmation("http://192.168.1.50:3456") {
		t.Fatal("确认后同 host 不应再弹")
	}
	if !c.NeedConfirmation("http://192.168.1.51:3456") {
		t.Fatal("不同 host 仍应确认")
	}
}

func TestConnector_BeginExternal(t *testing.T) {
	c := NewConnector()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()

	if err := c.BeginExternal(ok.URL); err != nil {
		t.Fatalf("BeginExternal 应成功,got %v", err)
	}
	if c.Mode() != ModeExternal || c.ExternalURL() != ok.URL {
		t.Fatalf("状态错误:mode=%v url=%q", c.Mode(), c.ExternalURL())
	}
	c.EndExternal()
	if c.Mode() != ModeContainer {
		t.Fatal("EndExternal 后应回容器模式")
	}

	// 探测失败:保持当前模式并记录错误
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	if err := c.BeginExternal(bad.URL); err == nil {
		t.Fatal("BeginExternal 500 应失败")
	}
	if c.Mode() != ModeContainer {
		t.Fatalf("失败后模式不应变,got %v", c.Mode())
	}
	if c.LastError() == "" {
		t.Fatal("LastError 不应为空")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd apps/desktop-launcher && go test -run "TestIsLoopbackHost|TestProbe|TestExternalURLPersistence|TestConnector_" -count=1`

Expected: 编译失败(`undefined: isLoopbackHost`)。

- [ ] **Step 3: 实现 connection.go**

创建 `apps/desktop-launcher/connection.go`:

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Mode 表示 webview 当前加载的服务来源。
type Mode int

const (
	ModeContainer Mode = iota // 容器内 harness(默认)
	ModeExternal              // 外部 URL(本机/远端 harness)
)

// probeTimeout 外部服务探测超时。
const probeTimeout = 3 * time.Second

// externalConfig 是外部连接配置文件的 JSON 结构。
type externalConfig struct {
	ExternalURL string `json:"externalUrl"`
}

// isLoopbackHost 判断 host 是否为回环地址(127.0.0.1/localhost/::1)。
func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// probe 探测 rawURL 是否存活:HTTP GET,2xx/3xx 视为成功。
func probe(rawURL string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}

// loadExternalURL 读取配置中的外部 URL;文件缺失或损坏返回空串。
func loadExternalURL(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg externalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return cfg.ExternalURL
}

// saveExternalURL 写入外部 URL 配置。
func saveExternalURL(path string, rawURL string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(externalConfig{ExternalURL: rawURL}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Connector 管理外部连接状态与安全确认记忆。方法由 GTK 主线程调用;
// 不触碰 GTK/supervisor,便于单测。
type Connector struct {
	mu             sync.Mutex
	mode           Mode
	externalURL    string
	lastError      string
	confirmedHosts map[string]bool
	probe          func(rawURL string, timeout time.Duration) error
}

// NewConnector 创建连接器;probe 用默认 HTTP 探测。
func NewConnector() *Connector {
	return &Connector{
		mode:           ModeContainer,
		confirmedHosts: make(map[string]bool),
		probe:          probe,
	}
}

// Mode 返回当前模式。
func (c *Connector) Mode() Mode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mode
}

// ExternalURL 返回已连接的外部 URL。
func (c *Connector) ExternalURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.externalURL
}

// LastError 返回最近一次连接失败原因。
func (c *Connector) LastError() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastError
}

// ValidateURL 解析并规范化用户输入的 URL;仅允许 http/https 协议。
func (c *Connector) ValidateURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("仅支持 http/https 地址")
	}
	if u.Host == "" {
		return "", errors.New("缺少主机名")
	}
	return u.String(), nil
}

// NeedConfirmation 判断连接该 URL 前是否需要安全确认
// (非回环地址且本会话未确认过)。
func (c *Connector) NeedConfirmation(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if isLoopbackHost(u.Hostname()) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.confirmedHosts[u.Hostname()]
}

// ConfirmHost 记录本会话已确认的 host。
func (c *Connector) ConfirmHost(rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.confirmedHosts[u.Hostname()] = true
}

// BeginExternal 探测 rawURL 并切到外部模式;失败返回错误并保持当前模式。
func (c *Connector) BeginExternal(rawURL string) error {
	if err := c.probe(rawURL, probeTimeout); err != nil {
		c.mu.Lock()
		c.lastError = err.Error()
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.mode = ModeExternal
	c.externalURL = rawURL
	c.lastError = ""
	c.mu.Unlock()
	return nil
}

// EndExternal 回到容器模式。
func (c *Connector) EndExternal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = ModeContainer
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd apps/desktop-launcher && go test -run "TestIsLoopbackHost|TestProbe|TestExternalURLPersistence|TestConnector_" -v -count=1`

Expected: 全部 PASS。

- [ ] **Step 5: 全量回归 + 提交**

Run: `cd apps/desktop-launcher && go vet ./... && go build -o /dev/null . && go test ./... -count=1 -timeout 60s`

```bash
git add apps/desktop-launcher/connection.go apps/desktop-launcher/connection_test.go
git commit -m "feat(desktop-launcher): add external connection probe and state core"
```

---

### Task 2: 弹框重设计 + 外部连接交互(ui.go/window.go/ui_state.go)

**Files:**
- Modify: `apps/desktop-launcher/window.go`(注入 Navigate 闭包)
- Modify: `apps/desktop-launcher/ui.go`(弹框重设计 + 连接交互;视觉细节交 designer 定稿)
- Modify: `apps/desktop-launcher/ui_state.go`(外部模式状态文本)
- Test: 无单测(GTK 需显示);以 go build/go vet + 手动验证为门槛

**Interfaces:**
- Consumes: Task 1 的 `Connector`/`Mode`/`probe`/`loadExternalURL`/`saveExternalURL`;既有 `(*Supervisor).StopHarness/Restart/Status`;`statusBarText`/`serverDialogState`
- Produces:
  - `func installDesktopUI(win unsafe.Pointer, sup *Supervisor, navigate func(string))`(签名扩展)
  - 包级全局:`connector *Connector`、`navigateFn func(string)`、`configPath string`(外部 URL 配置文件路径)
  - C 弹框新增:`dsh_dlg_mode_container`/`dsh_dlg_mode_external`(模式切换)、`dsh_dlg_url_entry`(URL 输入)、`dsh_dlg_btn_connect`/`dsh_dlg_btn_disconnect`、`dsh_dlg_error_label`、`dsh_dlg_ext_state`(外部状态区)
  - `//export dshOnModeChanged` / `dshOnExternalConnect` / `dshOnExternalDisconnect` / `dshOnProbeResult` / `dshOnNavIdle`
  - `func externalConfigFilePath() string`(`~/.config/dsh-desktop/config.json`;HOME 不可用时回退 LogDir)

- [ ] **Step 1: 扩展 window.go 注入 Navigate 闭包**

`apps/desktop-launcher/window.go` 的 `openWindow` 中,把 `installDesktopUI(w.Window(), sup)` 改为:

```go
	// 底部状态栏、服务器/关于按钮、状态轮询、窗口居中与外部连接导航
	installDesktopUI(w.Window(), sup, func(u string) {
		w.Navigate(u)
	})
```

`w.Navigate` 只在 GTK 主线程的调用点被触发(弹框回调与 idle 回调),线程安全。

- [ ] **Step 2: ui_state.go 增加外部模式文本(纯函数)**

在 `apps/desktop-launcher/ui_state.go` 末尾追加:

```go
// externalStatusBarText 生成外部模式的状态栏文本。
func externalStatusBarText(connector *Connector) string {
	if u := connector.ExternalURL(); u != "" {
		return "● 外部服务 " + u
	}
	return "● 外部模式"
}

// ExternalDialogState 是外部模式弹框状态区的文本与按钮可用性。
type ExternalDialogState struct {
	State, Detail     string
	CanConnect        bool
	CanDisconnect     bool
}

// externalDialogState 由连接器状态推导弹框文本与按钮可用性。
func externalDialogState(connector *Connector, busy bool) ExternalDialogState {
	connected := connector.Mode() == ModeExternal
	return ExternalDialogState{
		State:        map[bool]string{true: "已连接", false: "未连接"}[connected],
		Detail:       "外部地址: " + connector.ExternalURL(),
		CanConnect:   !connected && !busy,
		CanDisconnect: connected && !busy,
	}
}
```

在 `apps/desktop-launcher/ui_state_test.go` 末尾追加测试:

```go
func TestExternalStatusBarText(t *testing.T) {
	c := NewConnector()
	if got := externalStatusBarText(c); got != "● 外部模式" {
		t.Errorf("未连接文本错误:%q", got)
	}
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ok.Close()
	if err := c.BeginExternal(ok.URL); err != nil {
		t.Fatal(err)
	}
	if got := externalStatusBarText(c); got != "● 外部服务 "+ok.URL {
		t.Errorf("已连接文本错误:%q", got)
	}
}

func TestExternalDialogState(t *testing.T) {
	c := NewConnector()
	s := externalDialogState(c, false)
	if s.State != "未连接" || !s.CanConnect || s.CanDisconnect {
		t.Errorf("未连接态错误:%+v", s)
	}
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ok.Close()
	_ = c.BeginExternal(ok.URL)
	s = externalDialogState(c, true)
	if s.State != "已连接" || s.CanConnect || s.CanDisconnect {
		t.Errorf("连接中 busy 态错误:%+v", s)
	}
}
```

(文件顶部需补 `"net/http"`、`"net/http/httptest"` import。)

- [ ] **Step 3: ui.go 弹框重设计(结构 + 交互)**

`apps/desktop-launcher/ui.go` 的 cgo 前奏新增(在现有 statics 旁):

```c
// ---- 外部连接:模式切换、URL 输入、连接/断开 ----
static GtkWidget *dsh_dlg_mode_container = NULL;
static GtkWidget *dsh_dlg_mode_external = NULL;
static GtkWidget *dsh_dlg_url_entry = NULL;
static GtkWidget *dsh_dlg_btn_connect = NULL;
static GtkWidget *dsh_dlg_btn_disconnect = NULL;
static GtkWidget *dsh_dlg_error_label = NULL;
static GtkWidget *dsh_dlg_ext_state = NULL;
static GtkWidget *dsh_dlg_container_buttons = NULL; // 启动/重启/停止 行

extern void dshOnModeChanged(void);
extern void dshOnExternalConnect(void);
extern void dshOnExternalDisconnect(void);
extern void dshOnProbeResult(void);
extern void dshOnNavIdle(void);

static void dsh_mode_toggled(GtkToggleButton *b, gpointer d) { (void)b; (void)d; dshOnModeChanged(); }
static void dsh_external_connect_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnExternalConnect(); }
static void dsh_external_disconnect_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnExternalDisconnect(); }
static gboolean dsh_probe_idle(gpointer d) { (void)d; dshOnProbeResult(); return G_SOURCE_REMOVE; }
static gboolean dsh_nav_idle(gpointer d) { (void)d; dshOnNavIdle(); return G_SOURCE_REMOVE; }
```

`dsh_make_server_dialog` 改为(在原内容区 vbox 顶部插模式行、URL 行,并把按钮按模式分组):

```c
static GtkWidget *dsh_make_server_dialog(GtkWindow *parent) {
  GtkWidget *dlg = gtk_dialog_new_with_buttons(
      "服务器状态", parent, GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
      "_关闭", GTK_RESPONSE_CLOSE, NULL);
  g_signal_connect(dlg, "response", G_CALLBACK(dsh_dialog_response), NULL);
  g_signal_connect(dlg, "destroy", G_CALLBACK(dsh_server_dialog_destroyed), NULL);
  gtk_widget_set_size_request(dlg, 440, -1);

  GtkWidget *content = gtk_dialog_get_content_area(GTK_DIALOG(dlg));
  GtkWidget *vbox = gtk_box_new(GTK_ORIENTATION_VERTICAL, 10);
  gtk_widget_set_margin_start(vbox, 18);
  gtk_widget_set_margin_end(vbox, 18);
  gtk_widget_set_margin_top(vbox, 18);

  // 模式选择行:容器内 / 本机或远端服务
  GtkWidget *mode_row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
  GtkWidget *mode_label = gtk_label_new("连接模式");
  gtk_widget_set_halign(mode_label, GTK_ALIGN_START);
  dsh_dlg_mode_container = gtk_toggle_button_new_with_label("容器内");
  dsh_dlg_mode_external = gtk_toggle_button_new_with_label("本机/远端服务");
  gtk_toggle_button_set_active(GTK_TOGGLE_BUTTON(dsh_dlg_mode_container), TRUE);
  g_signal_connect(dsh_dlg_mode_container, "toggled", G_CALLBACK(dsh_mode_toggled), NULL);
  g_signal_connect(dsh_dlg_mode_external, "toggled", G_CALLBACK(dsh_mode_toggled), NULL);
  gtk_box_pack_start(GTK_BOX(mode_row), mode_label, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(mode_row), dsh_dlg_mode_container, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(mode_row), dsh_dlg_mode_external, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(vbox), mode_row, FALSE, FALSE, 0);
  gtk_widget_set_halign(mode_row, GTK_ALIGN_START);

  // 状态区(两种模式共用一行状态)
  GtkWidget *grid = gtk_grid_new();
  gtk_grid_set_row_spacing(GTK_GRID(grid), 8);
  gtk_grid_set_column_spacing(GTK_GRID(grid), 14);
  GtkWidget *key_state = gtk_label_new("状态");
  gtk_widget_set_halign(key_state, GTK_ALIGN_END);
  gtk_style_context_add_class(gtk_widget_get_style_context(key_state), "dsh-dialog-key");
  dsh_dlg_dot = gtk_label_new("●");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_dot), "dsh-state-dot");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_dot), "dsh-state-stopped");
  dsh_dlg_state = gtk_label_new("…");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_state), "dsh-dialog-state");
  gtk_widget_set_halign(dsh_dlg_state, GTK_ALIGN_START);
  GtkWidget *state_row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 6);
  gtk_box_pack_start(GTK_BOX(state_row), dsh_dlg_dot, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(state_row), dsh_dlg_state, FALSE, FALSE, 0);
  dsh_dlg_key1 = gtk_label_new("");
  dsh_dlg_val1 = gtk_label_new("");
  dsh_dlg_key2 = gtk_label_new("");
  dsh_dlg_val2 = gtk_label_new("");
  GtkWidget *detail_keys[] = {dsh_dlg_key1, dsh_dlg_key2};
  GtkWidget *detail_vals[] = {dsh_dlg_val1, dsh_dlg_val2};
  for (int i = 0; i < 2; i++) {
    gtk_style_context_add_class(gtk_widget_get_style_context(detail_keys[i]), "dsh-dialog-key");
    gtk_widget_set_halign(detail_keys[i], GTK_ALIGN_END);
    gtk_widget_set_halign(detail_vals[i], GTK_ALIGN_START);
    gtk_label_set_selectable(GTK_LABEL(detail_vals[i]), TRUE);
  }
  gtk_grid_attach(GTK_GRID(grid), key_state, 0, 0, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), state_row, 1, 0, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_key1, 0, 1, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_val1, 1, 1, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_key2, 0, 2, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_val2, 1, 2, 1, 1);
  gtk_box_pack_start(GTK_BOX(vbox), grid, FALSE, FALSE, 0);

  // 外部模式:URL 输入 + 连接/断开 + 错误标签
  GtkWidget *ext_row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
  GtkWidget *ext_label = gtk_label_new("服务地址");
  gtk_widget_set_halign(ext_label, GTK_ALIGN_START);
  dsh_dlg_url_entry = gtk_entry_new();
  gtk_entry_set_placeholder_text(GTK_ENTRY(dsh_dlg_url_entry), "http://127.0.0.1:3456");
  gtk_widget_set_hexpand(dsh_dlg_url_entry, TRUE);
  dsh_dlg_btn_connect = gtk_button_new_with_label("连接");
  dsh_dlg_btn_disconnect = gtk_button_new_with_label("断开");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_btn_connect), "suggested-action");
  g_signal_connect(dsh_dlg_btn_connect, "clicked", G_CALLBACK(dsh_external_connect_clicked), NULL);
  g_signal_connect(dsh_dlg_btn_disconnect, "clicked", G_CALLBACK(dsh_external_disconnect_clicked), NULL);
  gtk_box_pack_start(GTK_BOX(ext_row), ext_label, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(ext_row), dsh_dlg_url_entry, TRUE, TRUE, 0);
  gtk_box_pack_start(GTK_BOX(ext_row), dsh_dlg_btn_connect, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(ext_row), dsh_dlg_btn_disconnect, FALSE, FALSE, 0);
  dsh_dlg_ext_state = gtk_label_new("");
  gtk_widget_set_halign(dsh_dlg_ext_state, GTK_ALIGN_START);
  dsh_dlg_error_label = gtk_label_new("");
  gtk_widget_set_halign(dsh_dlg_error_label, GTK_ALIGN_START);
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_error_label), "dsh-dialog-error");
  gtk_box_pack_start(GTK_BOX(vbox), ext_row, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(vbox), dsh_dlg_ext_state, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(vbox), dsh_dlg_error_label, FALSE, FALSE, 0);

  // 容器模式按钮行
  dsh_dlg_container_buttons = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
  gtk_widget_set_halign(dsh_dlg_container_buttons, GTK_ALIGN_END);
  dsh_dlg_btn_start = gtk_button_new_with_label("启动");
  dsh_dlg_btn_restart = gtk_button_new_with_label("重启");
  dsh_dlg_btn_stop = gtk_button_new_with_label("停止");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_btn_start), "suggested-action");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_btn_stop), "destructive-action");
  g_signal_connect(dsh_dlg_btn_start, "clicked", G_CALLBACK(dsh_server_start_clicked), NULL);
  g_signal_connect(dsh_dlg_btn_restart, "clicked", G_CALLBACK(dsh_server_restart_clicked), NULL);
  g_signal_connect(dsh_dlg_btn_stop, "clicked", G_CALLBACK(dsh_server_stop_clicked), NULL);
  gtk_box_pack_start(GTK_BOX(dsh_dlg_container_buttons), dsh_dlg_btn_start, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(dsh_dlg_container_buttons), dsh_dlg_btn_restart, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(dsh_dlg_container_buttons), dsh_dlg_btn_stop, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(vbox), dsh_dlg_container_buttons, FALSE, FALSE, 0);

  gtk_container_add(GTK_CONTAINER(content), vbox);
  gtk_widget_show_all(dlg);
  return dlg;
}
```

`dsh_update_server_dialog` 改为按模式更新(容器模式显示状态+按钮;外部模式显示外部状态区):

```c
// ---- 刷新服务器弹框(按模式分支) ----
static void dsh_update_server_dialog(GtkWidget *dlg, const char *state_text, int state,
                                     const char *detail,
                                     gboolean can_start, gboolean can_restart, gboolean can_stop) {
  (void)dlg;
  gboolean external = gtk_toggle_button_get_active(GTK_TOGGLE_BUTTON(dsh_dlg_mode_external));
  gtk_widget_set_visible(dsh_dlg_container_buttons, !external);
  if (!external) {
    gtk_label_set_text(GTK_LABEL(dsh_dlg_state), state_text);
    dsh_set_state_class(dsh_dlg_dot, state);
    dsh_update_detail(detail);
    gtk_widget_set_sensitive(dsh_dlg_btn_start, can_start);
    gtk_widget_set_sensitive(dsh_dlg_btn_restart, can_restart);
    gtk_widget_set_sensitive(dsh_dlg_btn_stop, can_stop);
  }
}
```

CSS 追加错误标签与外部状态样式(在 `dsh_css` 字符串末尾):

```c
    ".dsh-dialog-error {\n"
    "  color: #e5534b;\n"
    "  font-size: 12px;\n"
    "}\n"
    ".dsh-dialog-ext-state {\n"
    "  color: alpha(@theme_fg_color, 0.8);\n"
    "  font-size: 12px;\n"
    "}\n";
```

Go 侧(ui.go)新增全局与导出回调:

```go
// 外部连接状态与导航
var (
	connector  *Connector
	navigateFn func(string)
	configPath string
	// 异步探测结果与待导航 URL(经 g_idle_add 回主线程)
	probeResultErr  error
	probeResultURL  string
	pendingNavURL   string
	externalBusy    bool
)

// externalConfigFilePath 返回外部 URL 配置文件路径;HOME 不可用时回退 LogDir。
func externalConfigFilePath() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "dsh-desktop", "config.json")
	}
	return filepath.Join(logDirPath(), "config.json")
}
```

`installDesktopUI` 签名与初始化改为:

```go
func installDesktopUI(win unsafe.Pointer, sup *Supervisor, navigate func(string)) {
	if win == nil {
		return
	}
	activeSupervisor = sup
	navigateFn = navigate
	connector = NewConnector()
	configPath = externalConfigFilePath()
	mainWindow = (*C.GtkWindow)(win)
	C.dsh_apply_style(mainWindow)
	statusLabel = C.dsh_install_status_bar(mainWindow)
	C.dsh_center_window(mainWindow, 1280, 800)
	C.dsh_start_status_tick()
	dshRefreshStatus()
}
```

`dshOnModeChanged`(模式切换,GTK 主线程):

```go
//export dshOnModeChanged
func dshOnModeChanged() {
	external := C.gtk_toggle_button_get_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_external))) != 0
	if external {
		C.gtk_toggle_button_set_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_container)), 0)
	} else {
		C.gtk_toggle_button_set_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_external)), 0)
	}
	if serverDialog != nil {
		dshRefreshStatus()
	}
}
```

`dshOnExternalConnect`(连接按钮,GTK 主线程):

```go
//export dshOnExternalConnect
func dshOnExternalConnect() {
	if connector == nil || navigateFn == nil {
		return
	}
	raw := C.GoString(C.gtk_entry_get_text((*C.GtkEntry)(unsafe.Pointer(dsh_dlg_url_entry))))
	u, err := connector.ValidateURL(raw)
	if err != nil {
		setDialogError("地址无效: " + err.Error())
		return
	}
	if connector.NeedConfirmation(u) {
		if !confirmExternal(u) { // GtkMessageDialog 是/否
			return
		}
		connector.ConfirmHost(u)
	}
	// 异步探测:不阻塞 GTK 主线程
	externalBusy = true
	dshRefreshStatus()
	go func() {
		err := probe(u, probeTimeout)
		probeResultURL = u
		probeResultErr = err
		C.g_idle_add(C.GSourceFunc(C.dsh_probe_idle), nil)
	}()
}

//export dshOnProbeResult
func dshOnProbeResult() {
	u := probeResultURL
	err := probeResultErr
	externalBusy = false
	if err != nil {
		setDialogError("连接失败: " + err.Error())
		dshRefreshStatus()
		return
	}
	// 停容器 harness(释放端口、暂停自动重启),再切外部并导航
	activeSupervisor.StopHarness()
	if cerr := connector.BeginExternal(u); cerr != nil {
		setDialogError("连接失败: " + cerr.Error())
		dshRefreshStatus()
		return
	}
	_ = saveExternalURL(configPath, u)
	setDialogError("")
	cu := C.CString(u)
	C.gtk_entry_set_text((*C.GtkEntry)(unsafe.Pointer(dsh_dlg_url_entry)), cu)
	C.free(unsafe.Pointer(cu))
	navigateFn(u)
	dshRefreshStatus()
}
```

`dshOnExternalDisconnect`(断开,自动回容器):

```go
//export dshOnExternalDisconnect
func dshOnExternalDisconnect() {
	if connector == nil || activeSupervisor == nil || navigateFn == nil {
		return
	}
	connector.EndExternal()
	externalBusy = true
	dshRefreshStatus()
	// 重启容器 harness,等就绪后导航回(异步,有界 30s)
	go func() {
		activeSupervisor.Restart()
		select {
		case u := <-activeSupervisor.Ready():
			pendingNavURL = u
		case <-time.After(30 * time.Second):
			pendingNavURL = ""
		}
		C.g_idle_add(C.GSourceFunc(C.dsh_nav_idle), nil)
	}()
}

//export dshOnNavIdle
func dshOnNavIdle() {
	externalBusy = false
	u := pendingNavURL
	pendingNavURL = ""
	if u != "" {
		navigateFn(u)
	}
	dshRefreshStatus()
}
```

`confirmExternal`(安全确认框,GTK 主线程):

```go
// confirmExternal 弹确认框;返回用户是否确认。
// 用 NULL 格式串 + format_secondary_text 设置正文,避免 cgo 变参与 C 字符串泄漏。
func confirmExternal(u string) bool {
	cmsg := C.CString("将连接远端 harness 服务,其命令在远端机器上执行,API key 等配置将发往该机器。确认连接?")
	defer C.free(unsafe.Pointer(cmsg))
	cbtnYes := C.CString("_连接")
	cbtnNo := C.CString("_取消")
	defer C.free(unsafe.Pointer(cbtnYes))
	defer C.free(unsafe.Pointer(cbtnNo))
	dlg := C.gtk_message_dialog_new(mainWindow, C.GTK_DIALOG_MODAL|C.GTK_DIALOG_DESTROY_WITH_PARENT,
		C.GTK_MESSAGE_QUESTION, C.GTK_BUTTONS_NONE, nil)
	C.gtk_message_dialog_format_secondary_text((*C.GtkMessageDialog)(unsafe.Pointer(dlg)), C.CString("%s"), cmsg)
	C.gtk_dialog_add_buttons((*C.GtkDialog)(unsafe.Pointer(dlg)), cbtnYes, C.GTK_RESPONSE_YES, cbtnNo, C.GTK_RESPONSE_NO, nil)
	resp := int(C.gtk_dialog_run((*C.GtkDialog)(unsafe.Pointer(dlg))))
	C.gtk_widget_destroy(dlg)
	return resp == int(C.GTK_RESPONSE_YES)
}
```

`setDialogError` / `dshRefreshStatus` 外部模式分支:

```go
// setDialogError 设置弹框错误标签文本。
func setDialogError(text string) {
	if serverDialog == nil {
		return
	}
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	C.gtk_label_set_text((*C.GtkLabel)(unsafe.Pointer(dsh_dlg_error_label)), c)
}
```

`dshRefreshStatus` 中,状态栏文本按模式分支:

```go
	st := sup.Status()
	var barText string
	if connector != nil && connector.Mode() == ModeExternal {
		barText = externalStatusBarText(connector)
	} else {
		barText = statusBarText(st)
	}
	bar := C.CString(barText)
	C.dsh_set_status_label((*C.GtkLabel)(unsafe.Pointer(statusLabel)), bar, C.int(st.State))
	C.free(unsafe.Pointer(bar))
```

`dshOnServerStatusClicked` 打开弹框时填充 URL 输入并刷新:

```go
	if serverDialog != nil {
		C.gtk_window_present((*C.GtkWindow)(unsafe.Pointer(serverDialog)))
		return
	}
	serverDialog = C.dsh_make_server_dialog(mainWindow)
	if connector != nil {
		if u := connector.ExternalURL(); u == "" {
			u = loadExternalURL(configPath)
		}
		if u != "" {
			c := C.CString(u)
			C.gtk_entry_set_text((*C.GtkEntry)(unsafe.Pointer(dsh_dlg_url_entry)), c)
			C.free(unsafe.Pointer(c))
		}
	}
	dshRefreshStatus()
```

`dsh_server_dialog_destroyed` 需追加清空新增 statics(在现有清空后):

```go
	dsh_dlg_mode_container = nil; ...
```

(对应 C:destroy 回调里把新 statics 置 NULL。)

- [ ] **Step 4: 编译与静态检查**

Run: `cd apps/desktop-launcher && PKG_CONFIG_PATH=/tmp/dsh-pkgconfig go vet ./... && PKG_CONFIG_PATH=/tmp/dsh-pkgconfig go build -o /dev/null .`

Expected: 无 error(允许已知 GTK deprecation 警告)。若 `C.gtk_message_dialog_new` 的格式串报错,改用 `C.gtk_message_dialog_new(mainWindow, flags, type, buttons, nil)` 再 `gtk_message_dialog_format_secondary_text`。

- [ ] **Step 5: 手动验证(有显示环境)**

Run: `cd apps/desktop-launcher && PKG_CONFIG_PATH=/tmp/dsh-pkgconfig go build -o dsh-desktop-launcher . && DSH_DESKTOP_DSH_BIN="$(pwd)/testdata/mock-dsh-web.sh" ./dsh-desktop-launcher`

验证清单:
1. 弹框出现"连接模式"两枚单选按钮,默认"容器内",启动/重启/停止可见。
2. 切"本机/远端服务":启动/重启/停止隐藏,显示服务地址输入行 + 连接/断开。
3. 填一个未监听的地址 → 连接 → 约 3s 后红色错误提示,模式不变。
4. 起一个真实外部服务(如 `node -e "require('http').createServer((q,s)=>s.end('ok')).listen(3456)"`)→ 连接 http://127.0.0.1:3456 → 状态栏变"● 外部服务 http://127.0.0.1:3456",窗口加载该页面;容器 mock 已被停止(日志出现 terminated)。
5. 点断开 → 自动回到容器模式:mock 重启,窗口回到 http://127.0.0.1:18080,状态栏恢复"● 运行中"。
6. 填局域网地址 → 弹确认框;取消不连接,确认后连接成功。
7. 关闭弹框重开:URL 输入框已自动填充上次地址。
8. 关闭窗口,程序正常退出,无残留进程。

完成后 `kill %1` 清理 mock。

- [ ] **Step 6: 回归测试 + 提交**

Run: `cd apps/desktop-launcher && go test ./... -count=1 -timeout 60s`

```bash
git add apps/desktop-launcher/ui.go apps/desktop-launcher/window.go apps/desktop-launcher/ui_state.go apps/desktop-launcher/ui_state_test.go
git commit -m "feat(desktop-launcher): add external harness connection to server dialog"
```

---

### Task 3: 文档与收尾验证

**Files:**
- Modify: `apps/desktop-launcher/README.md`

- [ ] **Step 1: 更新 README**

`apps/desktop-launcher/README.md` 文件结构表加一行:

```markdown
| `connection.go` | 外部服务连接:探测、URL 校验、持久化、连接状态(纯 Go) |
```

"环境变量"表后新增一节"连接外部服务"(简述):

```markdown
## 连接外部服务

服务器状态弹框支持两种连接模式:

- **容器内**(默认):启动并监护玲珑容器内捆绑的 harness。
- **本机/远端服务**:连接外部 harness 服务(本机 `npx @deepseek-ai/dsh web` 或网络可达的其他机器)。切到外部模式会先停止容器内 harness;断开后自动重启容器 harness 并导航回。

连接外部服务的前提:目标 harness 需绑定可访问的接口(`dsh web --host <LAN-IP>`;`--host 0.0.0.0` 被上游有意拒绝,原因见其 CLI 提示),局域网可达或经端口转发/隧道。外部地址(非 127.0.0.1/localhost)首次连接会弹安全确认;上次地址记忆在 `~/.config/dsh-desktop/config.json`,打开弹框自动填充、不自动重连。
```

- [ ] **Step 2: 全量检查**

Run: `cd apps/desktop-launcher && go vet ./... && go build -o /dev/null . && go test ./... -count=1 -timeout 60s`

Expected: 全部通过。

- [ ] **Step 3: 提交**

```bash
git add apps/desktop-launcher/README.md
git commit -m "docs(desktop-launcher): document external harness connection"
```

---

## Self-Review

**规格覆盖:** 切外部先停容器(Task 2 Step 3 `StopHarness`)、断开自动回容器(Task 2 `dshOnExternalDisconnect` → `Restart` + Ready + 导航)、URL 记忆自动填充不重连(Task 1 持久化 + Task 2 打开弹框填充)、非 loopback 确认(Connector.NeedConfirmation/ConfirmHost + Task 2 confirmExternal)、探测失败留当前模式(Task 2 probe 失败分支)、弹框布局重设计(Task 2,visual 交 designer)、状态栏外部模式文本(Task 2 Step 2)。规格全项落地。

**占位符扫描:** 无 TBD/TODO;每个步骤含完整代码与验证命令。

**类型一致性:** `Connector`/`Mode`/`probe`/`loadExternalURL`/`saveExternalURL`(Task 1)→ Task 2 消费;`externalStatusBarText`/`ExternalDialogState`(Task 2 Step 2)→ Task 2 Step 3 消费;`installDesktopUI(win, sup, navigate func(string))` 在 Task 2 定义,window.go 调用;`externalConfigFilePath`/`configPath` 在 Task 2 定义并使用。名称全链路一致。

**已知取舍:** GTK 层无单测(需显示环境),以 go build/vet + Task 2 Step 5 手动验证兜底;`g_idle_add` 异步回主线程用包级全局暂存结果(单实例应用,无并发窗口);`w.Navigate` 仅在主线程调用点触发。

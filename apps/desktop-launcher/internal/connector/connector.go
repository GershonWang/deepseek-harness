// Package connector 管理外部连接状态与安全确认记忆。纯 Go、无 GUI 依赖，
// 方法由上层（app 绑定层）在主线程或受控 goroutine 调用。
package connector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

// ProbeTimeout 外部服务探测超时。
const ProbeTimeout = 3 * time.Second

// externalConfig 是外部连接配置文件的 JSON 结构。
type externalConfig struct {
	ExternalURL string `json:"externalUrl"`
}

// IsLoopbackHost 判断 host 是否为回环地址（127.0.0.1/localhost/::1）。
func IsLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// harnessBootMarker 是 harness Web 服务渲染首页时注入的启动标记。
//
// `dsh web` 把 `globalThis["__DSH_BOOT__"] = {...}` 写进首页 HTML（见
// packages/host/webserver 的 injections），浏览器客户端也从同一标记取启动图。
// 它同时是「这个地址后面确实是一个 harness」的可判定证据：仅凭 2xx 无法区分静态文件
// 服务器、反向代理默认页或任意 HTTP 服务——那些地址会被判成「已连接」，随后连不上。
const harnessBootMarker = "__DSH_BOOT__"

// probeBodyLimit 是探测时最多读取的响应体字节数。首页只有几 KB，读到上限仍没有标记
// 即可判定不是 harness；上限同时挡住超大响应（不能为了找标记无界读取）。
const probeBodyLimit = 256 << 10

// Probe 探测 rawURL 是否是一个可用的 harness：HTTP GET 返回 2xx/3xx，且响应体前
// probeBodyLimit 字节内含 harness 启动标记。
//
// 错误文案保持英文技术细节（与既有的 `HTTP %d`、url.Parse 的错误一致）：领域包只报
// 事实，界面文案由 app 层渲染，而本函数的错误经 Connector.LastError 原样送到错误弹框。
func Probe(rawURL string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, probeBodyLimit))
	if err != nil {
		return err
	}
	if !bytes.Contains(body, []byte(harnessBootMarker)) {
		return fmt.Errorf("not a harness web UI: response body has no %s marker", harnessBootMarker)
	}
	return nil
}

// LoadExternalURL 读取配置中的外部 URL；文件缺失或损坏返回空串。
func LoadExternalURL(path string) string {
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

// SaveExternalURL 写入外部 URL 配置。
func SaveExternalURL(path string, rawURL string) error {
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

// Connector 维护连接模式、外部地址与安全确认记忆。
type Connector struct {
	mu             sync.Mutex
	mode           domain.Mode
	externalURL    string
	lastError      string
	confirmedHosts map[string]bool
	probe          func(rawURL string, timeout time.Duration) error
}

// New 创建连接器，probe 用默认 HTTP 探测。
func New() *Connector {
	return &Connector{
		mode:           domain.ModeContainer,
		confirmedHosts: make(map[string]bool),
		probe:          Probe,
	}
}

// Mode 返回当前模式。
func (c *Connector) Mode() domain.Mode {
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

// ValidationKind 是 URL 校验失败的归类。
//
// 领域包只报事实：措辞属于界面语言，由 app 层按 kind 查字典渲染（见 docs/i18n.md
// 第六节）。url.Parse 自身的错误不归类——那是英文技术细节，原样透传给排障。
type ValidationKind string

const (
	// ValidationScheme 协议不是 http/https。
	ValidationScheme ValidationKind = "scheme"
	// ValidationHost 缺少主机名。
	ValidationHost ValidationKind = "host"
)

// ValidationError 是归类后的 URL 校验失败。
type ValidationError struct {
	// Kind 失败归类。
	Kind ValidationKind
}

// Error 实现 error；取值即分类 id，便于日志与断言。
func (e *ValidationError) Error() string { return string(e.Kind) }

// ValidateURL 解析并规范化用户输入的 URL；仅允许 http/https 协议。
func (c *Connector) ValidateURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", &ValidationError{Kind: ValidationScheme}
	}
	if u.Host == "" {
		return "", &ValidationError{Kind: ValidationHost}
	}
	return u.String(), nil
}

// NeedConfirmation 判断连接该 URL 前是否需要安全确认（非回环地址且本会话未确认过）。
func (c *Connector) NeedConfirmation(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if IsLoopbackHost(u.Hostname()) {
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

// BeginExternal 探测 rawURL 并切到外部模式；失败返回错误并保持当前模式。
func (c *Connector) BeginExternal(rawURL string) error {
	if err := c.probe(rawURL, ProbeTimeout); err != nil {
		c.mu.Lock()
		c.lastError = err.Error()
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.mode = domain.ModeExternal
	c.externalURL = rawURL
	c.lastError = ""
	c.mu.Unlock()
	return nil
}

// EndExternal 回到容器模式。
func (c *Connector) EndExternal() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = domain.ModeContainer
}

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

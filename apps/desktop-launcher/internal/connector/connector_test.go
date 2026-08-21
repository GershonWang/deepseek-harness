package connector

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":    true,
		"localhost":    true,
		"::1":          true,
		"[::1]":        true,
		"192.168.1.50": false,
		"8.8.8.8":      false,
		"example.com":  false,
	}
	for host, want := range cases {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestProbe(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()
	if err := Probe(ok.URL, time.Second); err != nil {
		t.Fatalf("probe 200 应成功,got %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	if err := Probe(bad.URL, time.Second); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("probe 500 应失败并含状态码,got %v", err)
	}

	if err := Probe("http://127.0.0.1:1", time.Second); err == nil {
		t.Fatal("probe 未监听端口应失败")
	}
}

func TestExternalURLPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if v := LoadExternalURL(path); v != "" {
		t.Fatalf("缺失文件应返回空串,got %q", v)
	}
	if err := SaveExternalURL(path, "http://127.0.0.1:3456"); err != nil {
		t.Fatal(err)
	}
	if v := LoadExternalURL(path); v != "http://127.0.0.1:3456" {
		t.Fatalf("roundtrip 失败,got %q", v)
	}
	if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := LoadExternalURL(path); v != "" {
		t.Fatalf("损坏 JSON 应返回空串,got %q", v)
	}
}

func TestConnector_ValidateURL(t *testing.T) {
	c := New()
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
	c := New()
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
	c := New()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()

	if err := c.BeginExternal(ok.URL); err != nil {
		t.Fatalf("BeginExternal 应成功,got %v", err)
	}
	if c.Mode() != domain.ModeExternal || c.ExternalURL() != ok.URL {
		t.Fatalf("状态错误:mode=%v url=%q", c.Mode(), c.ExternalURL())
	}
	c.EndExternal()
	if c.Mode() != domain.ModeContainer {
		t.Fatal("EndExternal 后应回容器模式")
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	if err := c.BeginExternal(bad.URL); err == nil {
		t.Fatal("BeginExternal 500 应失败")
	}
	if c.Mode() != domain.ModeContainer {
		t.Fatalf("失败后模式不应变,got %v", c.Mode())
	}
	if c.LastError() == "" {
		t.Fatal("LastError 不应为空")
	}
}

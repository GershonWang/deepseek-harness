package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
	if running.State != "运行中" || !running.CanStop || running.CanStart {
		t.Errorf("running 态错误:%+v", running)
	}
	if running.Detail != "地址: http://127.0.0.1:40275\nPID: 123" {
		t.Errorf("running Detail 错误:%q", running.Detail)
	}
	stopped := serverDialogState(HarnessStatus{State: StateStopped, LastExit: "killed by signal=terminated"})
	if stopped.State != "已停止" || !stopped.CanStart || stopped.CanStop {
		t.Errorf("stopped 态错误:%+v", stopped)
	}
	if stopped.Detail != "上次退出: killed by signal=terminated" {
		t.Errorf("stopped Detail 错误:%q", stopped.Detail)
	}
}

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

func TestToolPanelState_Lines(t *testing.T) {
	checks := []ToolCheck{
		{Name: "git", OK: true, Version: "git version 2.40.0"},
		{Name: "python3", OK: false, Err: "exec: not found"},
	}
	state := toolPanelState(checks, []string{"go"})
	joined := strings.Join(state.Installable, "\n")
	if !strings.Contains(joined, "go") {
		t.Fatalf("installable missing go: %q", joined)
	}
	if len(state.Installed) != 1 || state.Installed[0] != "go" {
		t.Fatalf("installed: %v", state.Installed)
	}
}

func TestToolPanelText_RendersCheckOkAndMissing(t *testing.T) {
	state := toolPanelState([]ToolCheck{
		{Name: "git", OK: true, Version: "git version 2.40.0"},
		{Name: "jq", OK: false, Err: "exec: not found"},
	}, nil)
	text := toolPanelText(state)
	want := "git\t1\t2.40.0\njq\t0\texec: not found\nINSTALL\t无\tgo,ripgrep"
	if text != want {
		t.Fatalf("toolPanelText = %q, want %q", text, want)
	}
}

func TestVersionNumber(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"git", "git version 2.34.1", "2.34.1"},
		{"python", "Python 3.10.12", "3.10.12"},
		{"leading v", "v18.19.0", "18.19.0"},
		{"dash prefix", "jq-1.6", "1.6"},
		{"no digit", "unknown", "unknown"},
	}
	for _, c := range cases {
		if got := versionNumber(c.in); got != c.want {
			t.Errorf("versionNumber(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCredentialPanelState_HasToken(t *testing.T) {
	home := t.TempDir()
	_ = WriteGitCredentials(home, "u", "tok")
	state := credentialPanelState(home, gitCredentialsPath(home))
	if !state.HasToken || state.User != "u" || state.StoragePath == "" {
		t.Fatalf("state: %+v", state)
	}
	if text := credentialStatusText(state); !strings.Contains(text, "u") || strings.Contains(text, "tok") {
		t.Fatalf("状态行不应泄露明文令牌且应含用户名: %q", text)
	}
}

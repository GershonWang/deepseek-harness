package main

import (
	"strings"
	"testing"
)

func TestResolveTarget(t *testing.T) {
	const guid = "data:text/html,guidance"
	cases := []struct {
		name                      string
		mode                      Mode
		externalURL, containerURL string
		running                   bool
		want                      string
	}{
		{"external connected wins", ModeExternal, "http://10.0.0.5:3456", "http://127.0.0.1:1", false, "http://10.0.0.5:3456"},
		{"external connected, container running still external", ModeExternal, "http://10.0.0.5:3456", "http://127.0.0.1:1", true, "http://10.0.0.5:3456"},
		{"container running", ModeContainer, "", "http://127.0.0.1:3456", true, "http://127.0.0.1:3456"},
		{"container stopped", ModeContainer, "", "http://127.0.0.1:3456", false, guid},
		{"container starting", ModeContainer, "", "", false, guid},
	}
	for _, c := range cases {
		if got := resolveTarget(c.mode, c.externalURL, c.containerURL, c.running, guid); got != c.want {
			t.Errorf("%s: resolveTarget = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestGuidanceURL(t *testing.T) {
	u := guidanceURL()
	if !strings.HasPrefix(u, "data:text/html;charset=utf-8,") {
		t.Fatalf("guidanceURL 前缀错误:%q", u)
	}
	if strings.Contains(u, " ") || strings.Contains(u, "\n") {
		t.Fatalf("guidanceURL 含未编码空白:长度 %d", len(u))
	}
	// 二次调用返回同一缓存值,不重复 PathEscape
	if guidanceURL() != u {
		t.Error("guidanceURL 未缓存")
	}
	if !strings.Contains(u, "DeepSeek%20Harness") {
		t.Error("guidanceURL 未包含标题")
	}
	if !strings.Contains(u, "npx%20@deepseek-ai%2Fdsh%20web") {
		t.Error("guidanceURL 未包含 npx 启动命令")
	}
}

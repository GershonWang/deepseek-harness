package app

import (
	"testing"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

func TestResolveTarget(t *testing.T) {
	cases := []struct {
		name                      string
		mode                      domain.Mode
		externalURL, containerURL string
		running                   bool
		want                      string
	}{
		{"external connected wins", domain.ModeExternal, "http://10.0.0.5:3456", "http://127.0.0.1:1", false, "http://10.0.0.5:3456"},
		{"external connected, container running still external", domain.ModeExternal, "http://10.0.0.5:3456", "http://127.0.0.1:1", true, "http://10.0.0.5:3456"},
		{"container running", domain.ModeContainer, "", "http://127.0.0.1:3456", true, "http://127.0.0.1:3456"},
		{"container stopped", domain.ModeContainer, "", "http://127.0.0.1:3456", false, ""},
		{"container starting", domain.ModeContainer, "", "", false, ""},
	}
	for _, c := range cases {
		if got := resolveTarget(c.mode, c.externalURL, c.containerURL, c.running); got != c.want {
			t.Errorf("%s: resolveTarget = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestStateName(t *testing.T) {
	if got := stateName(domain.StateRunning); got != "running" {
		t.Errorf("stateName(running) = %q", got)
	}
	if got := stateName(domain.StateStarting); got != "starting" {
		t.Errorf("stateName(starting) = %q", got)
	}
	if got := stateName(domain.StateFailed); got != "failed" {
		t.Errorf("stateName(failed) = %q", got)
	}
	if got := stateName(domain.StateStopped); got != "stopped" {
		t.Errorf("stateName(stopped) = %q", got)
	}
}

func TestJoinOrNone(t *testing.T) {
	if got := joinOrNone(nil); got != "无" {
		t.Errorf("joinOrNone(nil) = %q", got)
	}
	if got := joinOrNone([]string{"go", "rg"}); got != "go,rg" {
		t.Errorf("joinOrNone = %q", got)
	}
}

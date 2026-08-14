package main

import (
	"os"
	"testing"
)

func TestResolveDesktopEnv_OverrideBin(t *testing.T) {
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	t.Setenv("DSH_DESKTOP_PORT", "8080")
	env := resolveDesktopEnv()
	if env.Command != "/custom/dsh" {
		t.Errorf("expected /custom/dsh, got %s", env.Command)
	}
	if env.Args[0] != "web" || env.Args[1] != "--port" || env.Args[2] != "8080" {
		t.Errorf("unexpected args: %v", env.Args)
	}
}

func TestResolveDesktopEnv_DefaultPort(t *testing.T) {
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	os.Unsetenv("DSH_DESKTOP_PORT")
	env := resolveDesktopEnv()
	if env.Port != "0" {
		t.Errorf("expected default port 0, got %s", env.Port)
	}
}

func TestResolveDesktopEnv_LogDir(t *testing.T) {
	t.Setenv("DSH_DESKTOP_DSH_BIN", "/custom/dsh")
	t.Setenv("DSH_DESKTOP_LOG_DIR", "/tmp/test-logs")
	env := resolveDesktopEnv()
	if env.LogDir != "/tmp/test-logs" {
		t.Errorf("expected /tmp/test-logs, got %s", env.LogDir)
	}
}

func TestResolveNode_Override(t *testing.T) {
	t.Setenv("DSH_DESKTOP_NODE", "/usr/local/bin/node22")
	n := resolveNode()
	if n != "/usr/local/bin/node22" {
		t.Errorf("expected override, got %s", n)
	}
}

func TestResolveNode_Default(t *testing.T) {
	os.Unsetenv("DSH_DESKTOP_NODE")
	n := resolveNode()
	if n != "node" && n != "node.exe" {
		t.Errorf("expected node or node.exe, got %s", n)
	}
}

package toolchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

func TestCheck_StubPath(t *testing.T) {
	dir := t.TempDir()
	stubs := map[string]string{
		"git":     "git version 2.40.0",
		"python3": "Python 3.11.2",
		"node":    "v24.9.0",
		"curl":    "curl 8.5.0",
		"jq":      "jq-1.7.1",
	}
	for name, out := range stubs {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+out+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	defer func() { _ = os.Setenv("PATH", old) }()

	checks := Check(DefaultSpecs())
	byName := map[string]domain.ToolCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if !byName["git"].OK || !strings.Contains(byName["git"].Version, "2.40.0") {
		t.Fatalf("git check: %+v", byName["git"])
	}
	if !byName["python3"].OK {
		t.Fatalf("python3 check: %+v", byName["python3"])
	}
}

func TestCheck_Missing(t *testing.T) {
	dir := t.TempDir()
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir)
	defer func() { _ = os.Setenv("PATH", old) }()
	checks := Check(DefaultSpecs())
	for _, c := range checks {
		if c.OK {
			t.Fatalf("expected %s missing, got OK", c.Name)
		}
	}
}

func TestVersionNumber(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"git version 2.34.1", "2.34.1"},
		{"Python 3.10.12", "3.10.12"},
		{"v18.19.0", "18.19.0"},
		{"jq-1.6", "1.6"},
		{"unknown", "unknown"},
	}
	for _, c := range cases {
		if got := VersionNumber(c.in); got != c.want {
			t.Errorf("VersionNumber(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

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
	// PATH 前置 stub 目录后，git 应解析到 stub 路径，供来源分类使用。
	if want := filepath.Join(dir, "git"); byName["git"].Path != want {
		t.Fatalf("git Path = %q, want %q", byName["git"].Path, want)
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
		// 未命中时 LookPath 失败，Path 必须为空（调用方靠它做来源分类）。
		if c.Path != "" {
			t.Fatalf("expected %s Path empty, got %q", c.Name, c.Path)
		}
	}
}

func TestProbeCommands(t *testing.T) {
	dir := t.TempDir()
	// 正常命中：--version 输出可解析版本。
	okCmd := filepath.Join(dir, "fake-probe-cmd")
	if err := os.WriteFile(okCmd, []byte("#!/bin/sh\necho fake-probe 1.2.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 命中但 --version 非零退出：可用性以 LookPath 为准，版本留空。
	noverCmd := filepath.Join(dir, "fake-probe-nover")
	if err := os.WriteFile(noverCmd, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir)
	defer func() { _ = os.Setenv("PATH", old) }()

	got := ProbeCommands([]string{"fake-probe-cmd", "fake-probe-nover", "no-such-cmd-x"})
	byName := map[string]domain.ToolCheck{}
	for _, c := range got {
		byName[c.Name] = c
	}
	if len(got) != 2 {
		t.Fatalf("未命中的命令不应出现在结果里: %+v", got)
	}
	c1 := byName["fake-probe-cmd"]
	if !c1.OK || c1.Path != okCmd || c1.Version != "1.2.3" {
		t.Fatalf("fake-probe-cmd: got %+v, want OK path=%s version=1.2.3", c1, okCmd)
	}
	c2 := byName["fake-probe-nover"]
	if !c2.OK || c2.Path != noverCmd || c2.Version != "" {
		t.Fatalf("fake-probe-nover: got %+v, want OK path=%s version 空", c2, noverCmd)
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

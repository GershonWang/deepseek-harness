package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// ToolCheck 记录一次工具探测结果。
type ToolCheck struct {
	Name    string
	OK      bool
	Version string // OK 时的版本字符串(若有)
	Err     string // 失败原因
}

// ToolSpec 声明一个可探测工具。
type ToolSpec struct {
	Name    string
	Command []string // 探测命令,如 {"git","--version"}
}

// DefaultToolSpecs 返回自检面板覆盖的关键工具。
func DefaultToolSpecs() []ToolSpec {
	return []ToolSpec{
		{Name: "git", Command: []string{"git", "--version"}},
		{Name: "python3", Command: []string{"python3", "--version"}},
		{Name: "node", Command: []string{"node", "--version"}},
		{Name: "curl", Command: []string{"curl", "--version"}},
		{Name: "jq", Command: []string{"jq", "--version"}},
		{Name: "pnpm", Command: []string{"pnpm", "--version"}},
	}
}

// CheckTools 依序探测,单工具失败不影响其余。
func CheckTools(specs []ToolSpec) []ToolCheck {
	out := make([]ToolCheck, 0, len(specs))
	for _, s := range specs {
		c := ToolCheck{Name: s.Name}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var buf bytes.Buffer
		cmd := exec.CommandContext(ctx, s.Command[0], s.Command[1:]...)
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		cancel()
		if err != nil {
			c.Err = err.Error()
		} else {
			c.OK = true
			c.Version = firstLine(buf.String())
		}
		out = append(out, c)
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Package toolchain 提供工具链自检（探测版本/缺失）与按需安装。
package toolchain

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

// Spec 声明一个可探测工具。
type Spec struct {
	Name      string
	Command   []string // 探测命令，如 {"git","--version"}
	NoVersion bool     // true=仅存在性探测（如 xdg-open），成功即视为可用，不提取版本号
}

// DefaultSpecs 返回随包内置的关键工具清单（与 linglong/tools.yaml 的 tools
// 段保持同步），供自检面板与工具市场的"内置工具"区展示。xdg-open 没有
// 安全的版本探测命令（见 tools.yaml 注释），用存在性探测。
func DefaultSpecs() []Spec {
	return []Spec{
		{Name: "git", Command: []string{"git", "--version"}},
		{Name: "git-lfs", Command: []string{"git", "lfs", "version"}},
		{Name: "python3", Command: []string{"python3", "--version"}},
		{Name: "curl", Command: []string{"curl", "--version"}},
		{Name: "wget", Command: []string{"wget", "--version"}},
		{Name: "jq", Command: []string{"jq", "--version"}},
		{Name: "unzip", Command: []string{"unzip", "-v"}},
		{Name: "xxd", Command: []string{"xxd", "--version"}},
		{Name: "node", Command: []string{"node", "--version"}},
		{Name: "pnpm", Command: []string{"pnpm", "--version"}},
		{Name: "dsh", Command: []string{"dsh", "--version"}},
		{Name: "xdg-open", Command: []string{"command", "-v", "xdg-open"}, NoVersion: true},
	}
}

// Check 依序探测，单工具失败不影响其余。每次探测最多 5 秒。
func Check(specs []Spec) []domain.ToolCheck {
	out := make([]domain.ToolCheck, 0, len(specs))
	for _, s := range specs {
		c := domain.ToolCheck{Name: s.Name}
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
			if s.NoVersion {
				// 仅存在性探测（如 xdg-open）：版本号无意义，标记为"内置"。
				c.Version = "内置"
			} else {
				c.Version = firstLine(buf.String())
			}
		}
		out = append(out, c)
	}
	return out
}

// VersionNumber 从探测输出里提取首个数字段并去掉前导非数字字符，把
// "git version 2.34.1" / "Python 3.10.12" / "v18.19.0" / "jq-1.6"
// 归一为 "2.34.1" / "3.10.12" / "18.19.0" / "1.6"。
func VersionNumber(v string) string {
	v = strings.TrimSpace(v)
	for i := 0; i < len(v); i++ {
		if v[i] >= '0' && v[i] <= '9' {
			return v[i:]
		}
	}
	return v
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

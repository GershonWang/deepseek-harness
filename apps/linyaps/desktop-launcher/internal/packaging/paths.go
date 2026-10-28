// Package packaging 处理打包态与开发态的路径，以及 webkit2gtk 的平台适配
// （辅助进程路径与合成后端）。
// Linux 专属逻辑放 webkit_linux.go；本文件跨平台。
package packaging

import (
	"os"
	"path/filepath"
)

// HarnessPrefix 返回打包态 prefix（files 目录）；开发态可执行文件在临时目录
// 时返回空。
func HarnessPrefix() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(exe))
}

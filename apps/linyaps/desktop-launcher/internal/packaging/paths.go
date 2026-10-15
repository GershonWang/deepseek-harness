// Package packaging 处理打包态与开发态的路径、版本，以及 webkit2gtk 的平台适配
// （辅助进程路径与合成后端）。
// Linux 专属逻辑放 webkit_linux.go；本文件跨平台。
package packaging

import (
	"os"
	"path/filepath"
)

// Version 是玲珑包版本，由 prepare-offline.sh 通过 -ldflags
// "-X main.packageVersion=..." 注入；本地构建未注入时为 "dev"。
// 标题栏在标题后面显示它；完整信息（DSH 版本、两个仓库、封装作者）由双客户端包的
// 托盘「关于」给出，因此这里不再有对应的仓库与署名常量。
var Version = "dev"

// HarnessPrefix 返回打包态 prefix（files 目录）；开发态可执行文件在临时目录
// 时返回空。
func HarnessPrefix() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(exe))
}

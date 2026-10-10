// 启动进度上报插件的注入侧：把内嵌的 JS 写进 launcher 运行时目录，并生成
// overlay 里插入该插件的行。
//
// 为什么内嵌而不是随包发布：它只服务桌面壳的加载页，落在 launcher 自己的运行时
// 目录（与 supervisor-overlay.yml 同处）即可，既不必进玲珑打包闭包，也不改
// harness 安装树；写失败时省略这一行，加载页退回粗粒度阶段，启动不受影响。
package appenv

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// startupProgressPluginSource 是内嵌的上报插件源码，与本文件同目录。
//
//go:embed startup_progress.mjs
var startupProgressPluginSource string

const (
	// startupProgressFileName 是写入运行时目录后的文件名（与 overlay 同目录）。
	startupProgressFileName = "startup-progress.mjs"
	// startupProgressRowID 是 overlay 插入行的 id：固定值便于在 harness 的插件树
	// 与诊断输出里认出这是桌面壳注入的条目。
	startupProgressRowID = "dsh-desktop-startup-progress"
)

// writeStartupProgressPlugin 把上报插件写入 launcher 运行时目录，返回其绝对路径。
// 绝对路径是 overlay 行能加载它的前提：harness 从 profile 目录解析裸包名，
// 拿不到 launcher 运行时目录下的文件。
func writeStartupProgressPlugin() (string, error) {
	path := filepath.Join(resolveLogDir(), startupProgressFileName)
	if err := os.WriteFile(path, []byte(startupProgressPluginSource), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// startupProgressOverlayRow 生成 overlay 里插入上报插件的 YAML 片段；pluginPath
// 为空表示插件没写成功，返回空串让调用方省略该行。
//
// 路径用 YAML 双引号标量转义：运行时目录可能含空格或非 ASCII 字符（HOME 由用户
// 决定），裸标量会让 overlay 解析失败，进而让整个 --patch 层失效。
func startupProgressOverlayRow(pluginPath string) string {
	if pluginPath == "" {
		return ""
	}
	return fmt.Sprintf("- insert:\n    - id: %s\n      name: %s\n",
		startupProgressRowID, strconv.Quote(pluginPath))
}

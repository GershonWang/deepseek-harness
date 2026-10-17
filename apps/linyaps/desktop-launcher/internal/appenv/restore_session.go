// 会话恢复插件的注入侧：把内嵌的 JS 写进 launcher 运行时目录，并生成 overlay 里
// 插入该插件、携带「上次打开的会话」的行。
//
// 为什么内嵌而不是随包发布：它只服务桌面壳的这一条还原路径，落在 launcher 自己的
// 运行时目录（与 supervisor-overlay.yml 同处）即可，既不必进玲珑打包闭包，也不改
// harness 安装树；写失败时省略这一行，客户端退回自己的回退策略，启动不受影响。
package appenv

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// restoreSessionPluginSource 是内嵌的恢复插件源码，与本文件同目录。
//
//go:embed restore_session.mjs
var restoreSessionPluginSource string

const (
	// restoreSessionFileName 是写入运行时目录后的文件名（与 overlay 同目录）。
	restoreSessionFileName = "restore-session.mjs"
	// restoreSessionRowID 是 overlay 插入行的 id：固定值便于在 harness 的插件树与
	// 诊断输出里认出这是桌面壳注入的条目。
	restoreSessionRowID = "dsh-desktop-restore-session"
)

// writeRestoreSessionPlugin 把恢复插件写入 launcher 运行时目录，返回其绝对路径。
// 绝对路径是 overlay 行能加载它的前提：harness 从 profile 目录解析裸包名，拿不到
// launcher 运行时目录下的文件。
func writeRestoreSessionPlugin() (string, error) {
	path := filepath.Join(resolveLogDir(), restoreSessionFileName)
	if err := os.WriteFile(path, []byte(restoreSessionPluginSource), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// restoreSessionOverlayRow 生成 overlay 里插入恢复插件的 YAML 片段。
//
// 会话 id 走行 config，而不是生成一份带 id 的插件源码：插件因此能保持静态、不必
// import 任何模块（见 restore_session.mjs 的约束），id 也不需要任何转义处理。
//
// pluginPath 为空（插件没写成功）或 sessionID 为空（壳还没记住任何会话）时返回
// 空串，让调用方省略该行——那时客户端按自己的回退策略启动，与注入失败同一结果。
//
// 两个字符串都用 YAML 双引号标量转义：运行时目录可能含空格或非 ASCII 字符
// （HOME 由用户决定），裸标量会让 overlay 解析失败，进而让整个 --patch 层失效。
func restoreSessionOverlayRow(pluginPath, sessionID string) string {
	if pluginPath == "" || sessionID == "" {
		return ""
	}
	return fmt.Sprintf("- insert:\n    - id: %s\n      name: %s\n      config:\n        sessionId: %s\n",
		restoreSessionRowID, strconv.Quote(pluginPath), strconv.Quote(sessionID))
}

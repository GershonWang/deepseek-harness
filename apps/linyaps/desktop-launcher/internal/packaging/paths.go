// Package packaging 处理打包态与开发态的路径、版本，以及 webkit2gtk 的平台适配
// （辅助进程路径与合成后端）。
// Linux 专属逻辑放 webkit_linux.go；本文件跨平台。
package packaging

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Version 是玲珑包版本，由 prepare-offline.sh 通过 -ldflags
// "-X main.packageVersion=..." 注入；本地构建未注入时为 "dev"。
var Version = "dev"

// GithubRepo 是玲珑封装（fork）仓库地址，用于关于弹框：玲珑包特有的改动都
// 落在这里，而不是上游。
const GithubRepo = "https://github.com/GershonWang/deepseek-harness"

// UpstreamRepo 是上游 DSH 官方仓库地址。关于弹框必须同时给出两者：本包是在
// 上游之上做的封装，只留 fork 地址会让用户把上游的问题报到这里。
const UpstreamRepo = "https://github.com/deepseek-ai/deepseek-harness"

// Packager 是玲珑封装作者署名。它标识的是「谁把这个 fork 打成玲珑包」，
// 与上游作者无关，故不随上游同步变动。
const Packager = "Jokul"

type packageManifest struct {
	Version string `json:"version"`
}

// HarnessPrefix 返回打包态 prefix（files 目录）；开发态可执行文件在临时目录
// 时返回空。
func HarnessPrefix() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(exe))
}

// ResolveHarnessVersion 读取打包态 $PREFIX/harness/package.json 或开发态
// ../cli/package.json 的版本号；都读不到时返回 "unknown"。
func ResolveHarnessVersion() string {
	if p := HarnessPrefix(); p != "" {
		if v := readVersion(filepath.Join(p, "harness", "package.json")); v != "" {
			return v
		}
	}
	cwd, _ := os.Getwd()
	if v := readVersion(filepath.Join(cwd, "..", "cli", "package.json")); v != "" {
		return v
	}
	return "unknown"
}

// AboutIconPath 返回关于弹框图标路径：打包态取 $PREFIX/share/icons 下的
// hicolor 图标；开发态取可执行文件目录或当前目录的 icons/dsh-desktop.png。
// 都没有时返回空串。
func AboutIconPath() string {
	if p := HarnessPrefix(); p != "" {
		cand := filepath.Join(p, "share", "icons", "hicolor", "256x256", "apps", "dsh-desktop.png")
		if fileExists(cand) {
			return cand
		}
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "icons", "dsh-desktop.png")
		if fileExists(cand) {
			return cand
		}
	}
	cand := filepath.Join("icons", "dsh-desktop.png")
	if fileExists(cand) {
		return cand
	}
	return ""
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// readVersion 读取 package.json 的 version 字段；文件缺失或解析失败返回空串。
func readVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m packageManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m.Version
}

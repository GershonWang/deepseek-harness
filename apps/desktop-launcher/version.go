package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// packageVersion 是玲珑包版本,由 prepare-offline.sh 通过
// -ldflags "-X main.packageVersion=..." 注入;本地 go build 未注入时为 "dev"。
var packageVersion = "dev"

// githubRepo 是项目仓库地址,用于关于弹框。
const githubRepo = "https://github.com/GershonWang/deepseek-harness"

type packageManifest struct {
	Version string `json:"version"`
}

// resolveHarnessVersion 读取打包态 $PREFIX/harness/package.json 或开发态
// ../cli/package.json 的版本号;都读不到时返回 "unknown"。
func resolveHarnessVersion() string {
	if p := harnessPrefix(); p != "" {
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

// harnessPrefix 返回打包态 prefix(files 目录);开发态可执行文件在
// /tmp/go-build... 时返回空。
func harnessPrefix() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(exe))
}

// readVersion 读取 package.json 的 version 字段;文件缺失或解析失败返回空串。
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

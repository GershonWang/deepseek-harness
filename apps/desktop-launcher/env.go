package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// DesktopEnv 描述子进程启动参数。
type DesktopEnv struct {
	Command string
	Args    []string
	LogDir  string
	Port    string
}

// resolveDesktopEnv 按优先级解析子进程环境：
// 1. DSH_DESKTOP_DSH_BIN 环境变量（开发调试）
// 2. $PREFIX/harness/lib/bin.js 存在（玲珑打包态）
// 3. repo 内 apps/cli/lib/bin.js 存在（开发态）
func resolveDesktopEnv() DesktopEnv {
	port := os.Getenv("DSH_DESKTOP_PORT")
	if port == "" {
		port = "0"
	}

	logDir := os.Getenv("DSH_DESKTOP_LOG_DIR")
	if logDir == "" {
		home, _ := os.UserHomeDir()
		logDir = filepath.Join(home, ".cache", "dsh-desktop")
	}

	// 优先级 1：显式指定 dsh bin
	if bin := os.Getenv("DSH_DESKTOP_DSH_BIN"); bin != "" {
		return DesktopEnv{
			Command: bin,
			Args:    []string{"web", "--port", port},
			LogDir:  logDir,
			Port:    port,
		}
	}

	// 优先级 2：打包态 —— $PREFIX/harness/lib/bin.js
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	prefix := filepath.Dir(exeDir) // .../files/bin -> .../files
	packagedBin := filepath.Join(prefix, "harness", "lib", "bin.js")
	if _, err := os.Stat(packagedBin); err == nil {
		node := resolveNode()
		return DesktopEnv{
			Command: node,
			Args:    []string{packagedBin, "web", "--port", port},
			LogDir:  logDir,
			Port:    port,
		}
	}

	// 优先级 3：开发态 —— repo 内 apps/cli/lib/bin.js
	// go run . 时可执行文件在 /tmp/go-build...，用 CWD 推算 repo 根
	cwd, _ := os.Getwd()
	devBin := filepath.Join(cwd, "..", "cli", "lib", "bin.js")
	if _, err := os.Stat(devBin); err == nil {
		node := resolveNode()
		return DesktopEnv{
			Command: node,
			Args:    []string{devBin, "web", "--port", port},
			LogDir:  logDir,
			Port:    port,
		}
	}

	// 回退：直接用 node + 当前目录的 bin.js
	node := resolveNode()
	return DesktopEnv{
		Command: node,
		Args:    []string{"bin.js", "web", "--port", port},
		LogDir:  logDir,
		Port:    port,
	}
}

// resolveNode 返回 node 可执行文件路径。
// DSH_DESKTOP_NODE 优先，否则用 PATH 中的 node。
func resolveNode() string {
	if n := os.Getenv("DSH_DESKTOP_NODE"); n != "" {
		return n
	}
	if runtime.GOOS == "windows" {
		return "node.exe"
	}
	return "node"
}

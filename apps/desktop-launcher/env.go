package main

import (
	"fmt"
	"net"
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
	if port == "" || port == "0" {
		// 稳定端口：harness 重启时复用同一端口，GUI 的 WebSocket/HTTP
		// 才能重连。若每次随机（--port 0），重启后 GUI 指着死端口，
		// 表现为永久 load failed。DSH_DESKTOP_PORT 显式指定时尊重之。
		port = reservePort()
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
		// 打包态优先用捆绑的 Node 24（harness 运行时需要 Node >=24：
		// node:zlib.createZstdDecompress、Promise.withResolvers 等），
		// 避免依赖宿主/容器的 PATH node（beige 只有 20.15.1，跑不起来）。
		node := filepath.Join(prefix, "node", "bin", "node")
		if _, statErr := os.Stat(node); statErr != nil {
			node = resolveNode()
		}
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

// configurePackagedEnv 为打包态设置子进程（harness 及其 spawn 的 zenity 等）
// 需要的环境变量。玲珑 layer 只含 $PREFIX，usr/share 的 GSettings schema
// 不会随 apt depends 进容器，zenity (GTK4) 会因缺
// org.gtk.gtk4.Settings.FileChooser schema 而崩溃；构建期已把编译好的
// schema 放在 $PREFIX/share/glib-2.0/schemas，这里设 GSETTINGS_SCHEMA_DIR
// 让 GSettings 找到。GTK_A11Y=none 规避沙箱内无 a11y 总线的警告。
func configurePackagedEnv() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	prefix := filepath.Dir(filepath.Dir(exe)) // .../files/bin -> .../files
	schemaDir := filepath.Join(prefix, "share", "glib-2.0", "schemas")
	if _, statErr := os.Stat(filepath.Join(schemaDir, "gschemas.compiled")); statErr == nil {
		_ = os.Setenv("GSETTINGS_SCHEMA_DIR", schemaDir)
	}
	_ = os.Setenv("GTK_A11Y", "none")
}

// reservePort 选一个空闲的 loopback 端口并返回其字符串。
// 用 net.Listen(":0") 拿到系统分配的端口后立即关闭监听，端口随即释放；
// 之后子进程用该端口 bind 存在极小竞态（他人抢用），但对本地 harness 足够稳。
func reservePort() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "0"
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return fmt.Sprintf("%d", port)
}

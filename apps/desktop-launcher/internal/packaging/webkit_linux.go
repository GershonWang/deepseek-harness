//go:build linux

package packaging

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigureWebKitHelperPath 让 webkit2gtk 找到其辅助进程
// （WebKitNetworkProcess / WebKitWebProcess / WebKitGPUProcess）。
//
// 背景：Debian 正式构建的 webkit2gtk 把 helper 目录硬编码在
// libwebkit2gtk-4.1.so.0 里（/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1），
// 运行时 /usr 只读、玲珑 layer 不导出 /usr 写入。打包时已用
// patch-webkit-exec-path.sh 把该字符串字节替换为 /tmp/dsh-webkit-4.1，
// 这里在启动时把 /tmp/dsh-webkit-4.1 建为指向 ${PREFIX} 下真实 helper
// 目录的符号链接。注入 bundle 用 WEBKIT_INJECTED_BUNDLE_PATH 覆盖。
// 开发态（未打包）不动：helper 在系统标准路径，默认即正确。
func ConfigureWebKitHelperPath() {
	prefix := HarnessPrefix()
	if prefix == "" {
		return
	}
	helperDir := filepath.Join(prefix, "lib", "x86_64-linux-gnu", "webkit2gtk-4.1")
	network := filepath.Join(helperDir, "WebKitNetworkProcess")
	if _, statErr := os.Stat(network); statErr != nil {
		return // 开发态：helper 在系统路径，无需处理
	}
	// /tmp 可写，建符号链接 /tmp/dsh-webkit-4.1 -> 真实 helper 目录。链接可
	// 残留自旧包运行（包 id 变更或卸载后成悬空），复用前必须验证目标仍指向
	// 当前包目录。
	const shortPath = "/tmp/dsh-webkit-4.1"
	if !webkitHelperLinkUsable(shortPath, helperDir) {
		_ = os.Remove(shortPath)
		if err := os.Symlink(helperDir, shortPath); err != nil {
			fmt.Fprintf(os.Stderr, "dsh-desktop: 创建 webkit helper 符号链接失败: %v\n", err)
			return
		}
	}
	_ = os.Setenv("WEBKIT_INJECTED_BUNDLE_PATH", filepath.Join(helperDir, "injected-bundle"))
}

// webkitHelperLinkUsable 判断短路径符号链接能否直接复用：存在、指向
// expected 目录、且该目录里的 WebKitNetworkProcess 可访问（未卸载）。
func webkitHelperLinkUsable(shortPath, expected string) bool {
	target, err := os.Readlink(shortPath)
	if err != nil {
		return false
	}
	if target != expected {
		return false
	}
	_, err = os.Stat(filepath.Join(expected, "WebKitNetworkProcess"))
	return err == nil
}

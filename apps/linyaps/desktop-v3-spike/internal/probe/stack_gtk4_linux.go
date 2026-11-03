//go:build linux && cgo && !gtk3 && !purego

// 本文件是 GTK4 + WebKitGTK 6.0 构建变体的运行时事实采集。
//
// 为什么单独按构建标签分文件：GTK3 与 GTK4 是两个包名、两个 API 集，版本查询函数也不同；
// 用标签把差异关在文件边界内，主流程就永远只面对 BuildBackend/RuntimeStackVersion 两个符号。
package probe

/*
#cgo linux pkg-config: gtk4 webkitgtk-6.0

#include <gtk/gtk.h>
#include <webkit/webkit.h>
*/
import "C"

import "fmt"

// BuildBackend 是编译期图形栈标识，写进报告用于区分构建变体。
const BuildBackend = "gtk4 (WebKitGTK 6.0)"

// RuntimeStackVersion 返回运行时真实链接的 GTK 与 WebKitGTK 版本。
//
// 为什么不只看构建标签：GTK4/WebKitGTK 6.0 的 ABI 与行为在小版本间会变（例如 6.0 删掉了
// ON_DEMAND 硬件加速策略），报告里没有真实版本，同一结论就没法在另一台机器上复现或推翻。
func RuntimeStackVersion() string {
	return fmt.Sprintf("GTK %d.%d.%d · WebKitGTK %d.%d.%d",
		int(C.GTK_MAJOR_VERSION), int(C.GTK_MINOR_VERSION), int(C.GTK_MICRO_VERSION),
		int(C.webkit_get_major_version()), int(C.webkit_get_minor_version()), int(C.webkit_get_micro_version()))
}

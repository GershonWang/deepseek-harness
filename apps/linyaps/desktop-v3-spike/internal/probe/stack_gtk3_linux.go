//go:build linux && cgo && gtk3 && !purego

// 本文件是 GTK3 + WebKit2GTK 4.1 构建变体（-tags gtk3）的运行时事实采集。
//
// 为什么要保留这个变体：wails v3.1.0 的源码里 gtk3 后端仍然存在，而 deepin 25 的 base 只带
// GTK3。若 GTK4 打包遇到短期阻塞，这条路径是唯一的过渡选项；保留它才能把「GTK4 做不出来」
// 与「v3 本身跑不通」区分开。
package probe

/*
#cgo linux pkg-config: gtk+-3.0 webkit2gtk-4.1

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
*/
import "C"

import "fmt"

// BuildBackend 是编译期图形栈标识，写进报告用于区分构建变体。
const BuildBackend = "gtk3 (WebKit2GTK 4.1)"

// RuntimeStackVersion 返回运行时真实链接的 GTK 与 WebKit2GTK 版本。
func RuntimeStackVersion() string {
	return fmt.Sprintf("GTK %d.%d.%d · WebKit2GTK %d.%d.%d",
		int(C.GTK_MAJOR_VERSION), int(C.GTK_MINOR_VERSION), int(C.GTK_MICRO_VERSION),
		int(C.webkit_get_major_version()), int(C.webkit_get_minor_version()), int(C.webkit_get_micro_version()))
}

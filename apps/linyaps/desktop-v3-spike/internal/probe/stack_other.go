//go:build !linux || !cgo || purego

// 本文件是非 Linux、或未启用 cgo、或 purego 构建的图形栈占位实现。
//
// 为什么需要它：spike 的类型检查与 API 校对是在没有 GTK 开发包的机器上做的
// （GOOS=windows 交叉编译）。没有这层占位，那条同样有价值的检查路径就建不起来。
package probe

// BuildBackend 表示当前构建不链接 GTK/WebKit。
const BuildBackend = "非 Linux / 未启用 cgo 构建"

// RuntimeStackVersion 在这些构建下没有可探测的图形栈。
func RuntimeStackVersion() string {
	return "n/a"
}

//go:build !linux || !cgo || purego

// 本文件是非 GTK4/WebKitGTK 6.0 构建的占位实现。
//
// 为什么需要它：spike 的类型检查用 GOOS=windows 交叉编译完成，没有这层占位，
// 那条检查路径会因为缺少 Install 而建不起来。

package webviewperm

func init() {
	state.Lock()
	state.status.Unsupported = true
	state.Unlock()
}

// Install 在不受支持的构建里是空操作。
func Install() {}

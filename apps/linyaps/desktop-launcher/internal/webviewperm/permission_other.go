//go:build !linux

package webviewperm

// Install 在非 Linux 平台是空实现。
//
// 该缺陷来自 WebKitGTK 对 permission-request 的默认拒绝；其它平台的 Wails 后端
// 使用各自的 WebView（Windows 走 WebView2），不经过这个信号。保留空实现是为了让
// 启动器在非 Linux 上仍能编译，而不是把平台判断散落到 main。
func Install() {}

//go:build windows

package toolchain

import "os"

// openPartFile 打开断点续传文件。
//
// Windows 没有 O_NOFOLLOW；创建符号链接需要特权或开发者模式，且 .downloads
// 已是用户私有目录，此处退化为普通打开（审计 N9 的纵深防御在 unix 侧生效）。
func openPartFile(path string, flag int) (*os.File, error) {
	return os.OpenFile(path, flag, 0o600)
}

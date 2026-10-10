//go:build unix

package toolchain

import (
	"os"
	"syscall"
)

// openPartFile 打开断点续传文件，拒绝跟随末段符号链接。
//
// 文件已位于用户私有的 <tools>/.downloads（0700）下，目录可信性由
// ensurePrivateDir 保证；这里的 O_NOFOLLOW 覆盖「末段被人换成链接」的情况，
// 直接报错而不是把下载内容写进链接指向的任意文件（审计 N9）。
func openPartFile(path string, flag int) (*os.File, error) {
	return os.OpenFile(path, flag|syscall.O_NOFOLLOW, 0o600)
}

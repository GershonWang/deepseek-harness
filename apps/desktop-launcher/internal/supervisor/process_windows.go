//go:build windows

package supervisor

import (
	"fmt"
	"os/exec"
)

// setProcessGroupAttr Windows 无进程组概念，exec.Cmd 无需额外属性。
func setProcessGroupAttr(cmd *exec.Cmd) {}

// signalExitReason Windows 无信号语义，恒返回 false（退到"ended"分支）。
func signalExitReason(cmd *exec.Cmd) (string, bool) {
	return "", false
}

// terminateTree 通过 taskkill（不带 /F）请求优雅关闭整个进程树。
func terminateTree(cmd *exec.Cmd) {
	runTaskkill(cmd, "")
}

// killTree 通过 taskkill /F /T 强制终止整个进程树。
func killTree(cmd *exec.Cmd) {
	runTaskkill(cmd, "/F")
}

func runTaskkill(cmd *exec.Cmd, force string) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	args := []string{"/PID", fmt.Sprint(cmd.Process.Pid), "/T"}
	if force != "" {
		args = append(args, force)
	}
	_ = exec.Command("taskkill", args...).Run()
}

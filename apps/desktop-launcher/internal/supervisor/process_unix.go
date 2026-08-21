//go:build unix

package supervisor

import (
	"fmt"
	"os/exec"
	"syscall"
)

// setProcessGroupAttr 让子进程拥有独立进程组，便于向整棵树发信号。
func setProcessGroupAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalExitReason 返回"被信号终止"的诊断字符串；非信号退出时 ok=false。
func signalExitReason(cmd *exec.Cmd) (s string, ok bool) {
	ws, found := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !found || !ws.Signaled() {
		return "", false
	}
	return fmt.Sprintf("killed by signal=%s", ws.Signal()), true
}

// terminateTree 向进程组发 SIGTERM（优雅终止）。
func terminateTree(cmd *exec.Cmd) {
	killGroup(cmd, syscall.SIGTERM)
}

// killTree 向进程组发 SIGKILL（强制终止）。
func killTree(cmd *exec.Cmd) {
	killGroup(cmd, syscall.SIGKILL)
}

func killGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// 负号 PID 将信号广播到整个进程组；失败属竞态，由后续 Kill 兜底。
	_ = syscall.Kill(-cmd.Process.Pid, sig)
}

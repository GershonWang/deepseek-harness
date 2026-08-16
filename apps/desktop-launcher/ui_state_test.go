package main

import (
	"testing"
)

func TestStatusBarText(t *testing.T) {
	cases := []struct {
		name string
		st   HarnessStatus
		want string
	}{
		{"running", HarnessStatus{State: StateRunning, URL: "http://127.0.0.1:40275"}, "● 运行中 http://127.0.0.1:40275"},
		{"starting", HarnessStatus{State: StateStarting}, "● 启动中"},
		{"stopped with exit", HarnessStatus{State: StateStopped, LastExit: "exited code=3"}, "● 已停止 (exited code=3)"},
		{"stopped clean", HarnessStatus{State: StateStopped}, "● 已停止"},
	}
	for _, c := range cases {
		if got := statusBarText(c.st); got != c.want {
			t.Errorf("%s: statusBarText = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestServerDialogState(t *testing.T) {
	running := serverDialogState(HarnessStatus{State: StateRunning, URL: "http://127.0.0.1:40275", PID: 123})
	if running.State != "运行中" || !running.CanRestart || !running.CanStop || running.CanStart {
		t.Errorf("running 态错误:%+v", running)
	}
	if running.Detail != "地址: http://127.0.0.1:40275\nPID: 123" {
		t.Errorf("running Detail 错误:%q", running.Detail)
	}
	stopped := serverDialogState(HarnessStatus{State: StateStopped, LastExit: "killed by signal=terminated"})
	if stopped.State != "已停止" || !stopped.CanStart || stopped.CanRestart || stopped.CanStop {
		t.Errorf("stopped 态错误:%+v", stopped)
	}
	if stopped.Detail != "上次退出: killed by signal=terminated" {
		t.Errorf("stopped Detail 错误:%q", stopped.Detail)
	}
}

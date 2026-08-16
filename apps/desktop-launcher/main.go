package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	env := resolveDesktopEnv()
	configurePackagedEnv()
	sup := NewSupervisor(env, DefaultSupervisorOptions())

	// 启动子进程监护
	sup.Start()

	// 等待就绪
	select {
	case url := <-sup.Ready():
		fmt.Fprintf(os.Stderr, "dsh-desktop: harness ready at %s\n", url)
		// 打开窗口（阻塞主线程）
		openWindow(url, sup)
	case <-time.After(30 * time.Second):
		fmt.Fprintln(os.Stderr, "dsh-desktop: harness startup timeout")
		sup.Stop()
		os.Exit(1)
	}

	// 窗口关闭，确保子进程已停止
	sup.Wait()
}

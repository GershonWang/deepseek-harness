#!/bin/sh
# 复现 mock：打印就绪行后保持存活，并留下一个逃逸到独立进程组（setsid）
# 的孙进程，继承 stdout 管道 write end。Stop() 的进程组 SIGTERM/SIGKILL
# 打不到该孙进程，管道永不 EOF，cmd.Wait() 可能因此永不返回。
echo "dsh web: http://127.0.0.1:18084"
setsid sh -c 'sleep 300' &
sleep 300

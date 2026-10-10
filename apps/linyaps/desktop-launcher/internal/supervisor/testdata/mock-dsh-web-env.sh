#!/bin/sh
# 门控/环境注入测试的 mock 子进程：打印就绪行，token 段带出
# DSH_TEST_CANARY 环境变量值（未注入时为空），随后驻留供父进程控制。
echo "dsh web: http://127.0.0.1:18080/?token=${DSH_TEST_CANARY}"
sleep 300

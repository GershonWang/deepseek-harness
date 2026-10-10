#!/bin/sh
# 诊断/测试 mock:不打印就绪行,立即以退出码 1 退出 —— 模拟启动失败(harness 起不来)。
echo "mock: startup failure (no ready line)"
exit 1
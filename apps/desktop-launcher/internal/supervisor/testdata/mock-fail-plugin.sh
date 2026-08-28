#!/bin/sh
# 诊断/测试 mock:模拟插件树加载失败 —— stderr 打印确定性错误特征后退出 1。
# 与 mock-fail-start.sh(无特征,走熔断)对照,验证 supervisor 快速失败路径。
echo "dsh: plugin tree failed to load: Cannot find package '@deepseek-ai/dsh-host-apiproxy'" >&2
exit 1
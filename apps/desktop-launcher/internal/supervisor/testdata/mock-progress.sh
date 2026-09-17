#!/bin/sh
# 启动进度上报 mock:模拟 launcher 注入的上报插件 —— 先在 stderr 打两行进度,
# 再在 stdout 打就绪行,最后挂住不退(与真实 harness 一样由 supervisor 终止)。
# 与 mock-dsh-web.sh 对照,用于验证 supervisor 的进度解析与首发输出时刻。
echo "dsh-desktop: startup 3/7" >&2
sleep 0.2
echo "dsh-desktop: startup 7/7" >&2
echo "dsh web: http://127.0.0.1:18081"
sleep 300

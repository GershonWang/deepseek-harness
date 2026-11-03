#!/bin/sh
# 本脚本在真机（Deepin 25 桌面会话）上跑一遍 spike 并收集证据。
#
# 为什么要有脚本而不是一段说明：v3 的可行性结论必须能被别人用同一条命令复现。
# 脚本固定报告路径与自动退出行为，避免「跑起来了但证据没落盘」「报告和进程对不上」。
#
# 用法：sh verify.sh [报告路径]
# 退出码：0 = 报告已生成；1 = 构建失败；2 = 运行失败/超时；3 = 报告缺少关键事实。
set -eu

HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$HERE"

REPORT=${1:-"$HERE/bin/report.json"}
BIN="$HERE/bin/dsh-v3-spike"
mkdir -p "$(dirname -- "$REPORT")"
rm -f "$REPORT"

echo "==> 检查构建前提"
if ! command -v pkg-config >/dev/null 2>&1; then
  echo "缺少 pkg-config：请先安装 libgtk-4-dev 与 libwebkitgtk-6.0-dev" >&2
  exit 1
fi
if ! pkg-config --exists gtk4; then
  echo "缺少 gtk4 开发包（libgtk-4-dev）" >&2
  exit 1
fi
if ! pkg-config --exists webkitgtk-6.0; then
  echo "缺少 webkitgtk-6.0 开发包（libwebkitgtk-6.0-dev）" >&2
  exit 1
fi
pkg-config --modversion gtk4 webkitgtk-6.0

echo "==> 构建（GTK4 + WebKitGTK 6.0）"
if ! CGO_ENABLED=1 go build -tags "production" -o "$BIN" .; then
  echo "构建失败：先看上一行的 cgo/链接错误，再判断是环境问题还是 v3 不兼容" >&2
  exit 1
fi

echo "==> 运行 spike（最多 90s，拿到报告后自动退出）"
# 自动退出让脚本不必区分「跑完退出」与「卡死被杀」：超时即可判失败。
# 这两种失败要分开报：超时通常意味着窗口没起来（会话/显示服务问题），
# 非零退出码则多半是程序自身 panic——混成一句话会把人引到错误的排查方向。
set +e
DSH_V3_SPIKE_REPORT="$REPORT" DSH_V3_SPIKE_AUTOCLOSE=1 timeout 90 "$BIN"
status=$?
set -e
case $status in
  0) ;;
  124)
    echo "运行超时：90s 内没有生成报告。先确认当前会话有 DISPLAY/WAYLAND_DISPLAY，再看窗口是否出现" >&2
    exit 2
    ;;
  *)
    echo "运行失败（退出码 $status）：报告未生成。若是 panic，堆栈就在上方——请按第一处本项目代码定位，不要直接归因于 v3 兼容性" >&2
    exit 2
    ;;
esac

if [ ! -s "$REPORT" ]; then
  echo "报告缺失：$REPORT" >&2
  exit 2
fi

echo "==> 报告：$REPORT"
cat "$REPORT"

# 关键事实缺失时明确失败，否则「报告存在」会被误读成「验证通过」。
for key in '"frontend"' '"binding"' '"origin"'; do
  if ! grep -q "$key" "$REPORT"; then
    echo "报告缺少关键字段：$key" >&2
    exit 3
  fi
done

echo "==> 完成：请按 README.zh.md 的判定表逐条核对（origin、localStorage、绑定、早期事件、拖拽）"

#!/bin/sh
# 容器内启动入口：准备 WebKit helper 路径与 GTK4 数据目录，运行 spike，并把报告
# 打到标准输出。
#
# 为什么先放在 shell 里而不是写进 Go：第一轮打包验证要在**不重新编译二进制**的前提下
# 完成，包装脚本能覆盖同一组环境变量。验证通过后这些设置会移进 Go
# （internal/packaging），与 desktop-launcher 的组织方式保持一致。
#
# 注意：/tmp/dsh-webkit-6.0 必须与 internal/packaging/webkit-exec-path.txt 一致——
# .so 里被字节替换成那个路径，两边写岔就是「装得上、窗口起不来」。
set -eu

PREFIX=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
HELPERS="$PREFIX/lib/x86_64-linux-gnu/webkitgtk-6.0"
if [ -d "$HELPERS" ]; then
  rm -f /tmp/dsh-webkit-6.0
  ln -s "$HELPERS" /tmp/dsh-webkit-6.0
  export WEBKIT_INJECTED_BUNDLE_PATH="$HELPERS/injected-bundle"
fi

if [ -d "$PREFIX/share" ]; then
  export XDG_DATA_DIRS="$PREFIX/share:${XDG_DATA_DIRS:-/usr/share}"
fi
if [ -d "$PREFIX/share/glib-2.0/schemas" ]; then
  export GSETTINGS_SCHEMA_DIR="$PREFIX/share/glib-2.0/schemas"
fi
if [ -d "$PREFIX/lib/x86_64-linux-gnu/gstreamer-1.0" ]; then
  export GST_PLUGIN_PATH="$PREFIX/lib/x86_64-linux-gnu/gstreamer-1.0"
fi

# 报告默认落 /tmp：容器内 $HOME 只读，写用户目录会直接失败。ll-builder run 结束后
# 容器即销毁，所以下面再把报告打到标准输出，作为唯一能带出容器的证据。
export DSH_V3_SPIKE_REPORT="${DSH_V3_SPIKE_REPORT:-/tmp/dsh-v3-spike-report.json}"

# 默认自动退出：ll-builder run 不会把调用者的环境变量带进容器（实测 DSH_V3_SPIKE_AUTOCLOSE
# 传不进来，进程就一直挂着，报告写完了但 ll-builder 永不返回）。验证包的正确默认值是
# 「拿到报告就退出」，再由下面的 timeout 兜底——否则一次超时就留下一个占着容器的进程。
export DSH_V3_SPIKE_AUTOCLOSE="${DSH_V3_SPIKE_AUTOCLOSE:-1}"

# WebKitGTK 6.0 默认启用 UI 进程沙箱，它按编译期路径 /usr/bin/bwrap 启动
# xdg-dbus-proxy；容器里 /usr 只读且没有该路径，于是启动直接失败：
#   Failed to start dbus proxy: Failed to spawn child process "/usr/bin/bwrap"
# 第一轮验证先用上游提供的开关关掉沙箱，把后续链路（GTK4 数据、页面加载、绑定）
# 先跑通；正式方案要么随包提供 bwrap 并做路径字节补丁（与 helper 同一手法），
# 要么明确接受「玲珑容器内不再嵌套 WebKit 沙箱」——这是需要单独决策的安全取舍。
export WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1

# 无 a11y bus 时 GTK 会反复尝试连接并刷警告；验证不需要它。
export GTK_A11Y=none

# 兜底超时：即使自动退出失效，也让包装脚本拿到控制权、打印报告后返回，
# 避免又一次「进程挂住、ll-builder 永不返回、容器状态残留」。
timeout -k 5 45 "$PREFIX/bin/dsh-v3-spike" "$@"
status=$?

if [ -f "$DSH_V3_SPIKE_REPORT" ]; then
  echo "===== SPIKE REPORT BEGIN ====="
  cat "$DSH_V3_SPIKE_REPORT"
  echo "===== SPIKE REPORT END ====="
fi
exit $status

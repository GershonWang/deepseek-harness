#!/bin/sh
# 容器内启动入口：按单一路径清单建立短路径链接，运行 spike，并把报告打到标准输出。
#
# 为什么是 shell 而不是写进 Go：本模块验证的是「容器里能不能跑」，包装脚本不需要重新
# 编译二进制就能换环境；结论成立后这些设置再移进 Go（internal/packaging），
# 与 desktop-launcher 的组织方式一致。
set -eu

PREFIX=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
PATHS="$PREFIX/share/dsh-v3-spike/short-paths.txt"
[ -f "$PATHS" ] || { echo "run-spike: 缺少 $PATHS（打包脚本未安装短路径清单）" >&2; exit 1; }
# shellcheck disable=SC1090
. "$PATHS"

link() {
  target=$1
  short=$2
  [ -e "$target" ] || return 0
  rm -f "$short"
  ln -s "$target" "$short"
}

# helper 目录与沙箱依赖的 bwrap / xdg-dbus-proxy 都放在 /tmp 短路径：
# .so 里被替换成这些路径，运行时只需它们存在且可执行。
link "$PREFIX/lib/x86_64-linux-gnu/webkitgtk-6.0" "$WEBKIT_EXEC"
link "$PREFIX/bin/bwrap" "$BWARP"
link "$PREFIX/bin/xdg-dbus-proxy" "$DBUS_PROXY"

if [ -d "$PREFIX/share" ]; then
  export XDG_DATA_DIRS="$PREFIX/share:${XDG_DATA_DIRS:-/usr/share}"
fi
if [ -d "$PREFIX/share/glib-2.0/schemas" ]; then
  export GSETTINGS_SCHEMA_DIR="$PREFIX/share/glib-2.0/schemas"
fi
if [ -d "$PREFIX/lib/x86_64-linux-gnu/gstreamer-1.0" ]; then
  export GST_PLUGIN_PATH="$PREFIX/lib/x86_64-linux-gnu/gstreamer-1.0"
fi

# 显式给出库搜索路径：沙箱打开后，helper 进程跑在 bwrap 的挂载命名空间里，
# 实测它读不到容器级的 ld.so.cache，于是 WebKitWebProcess 一启动就报
# "libwebkitgtk-6.0.so.4: cannot open shared object file"。
# LD_LIBRARY_PATH 会被 helper 继承，且不依赖沙箱内 /etc 是否可见。
export LD_LIBRARY_PATH="$PREFIX/lib/x86_64-linux-gnu:$PREFIX/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

# 报告默认落 /tmp：容器内 $HOME 只读，写用户目录会直接失败。ll-builder run 结束后
# 容器即销毁，所以下面再把报告打到标准输出，作为唯一能带出容器的证据。
export DSH_V3_SPIKE_REPORT="${DSH_V3_SPIKE_REPORT:-/tmp/dsh-v3-spike-report.json}"

# 默认自动退出：ll-builder run 不会把调用者的环境变量带进容器（实测传不进来，
# 进程会一直挂着），验证包的正确默认值是「拿到报告就退出」。
export DSH_V3_SPIKE_AUTOCLOSE="${DSH_V3_SPIKE_AUTOCLOSE:-1}"

# 无法连接 a11y bus 时 GTK 会反复告警；验证不需要辅助功能。
export GTK_A11Y=none

# 逃生舱：若某台机器/某个发行版的容器里 bwrap 不可用，可显式关掉 WebKit 的 UI 进程
# 沙箱再排查。默认保持沙箱开启（与宿主行为一致）。
if [ "${DSH_SPIKE_NO_SANDBOX:-0}" = "1" ]; then
  export WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS=1
fi

# 兜底超时：即使自动退出失效，也让包装脚本拿到控制权、打印报告后返回，
# 避免「进程挂住、ll-builder 永不返回、容器状态残留」。
timeout -k 5 45 "$PREFIX/bin/dsh-v3-spike" "$@"
status=$?

if [ -f "$DSH_V3_SPIKE_REPORT" ]; then
  echo "===== SPIKE REPORT BEGIN ====="
  cat "$DSH_V3_SPIKE_REPORT"
  echo "===== SPIKE REPORT END ====="
fi
exit $status

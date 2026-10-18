#!/bin/sh
# linyaps 四个玲珑版本的聚合打包入口。
#
# 为什么需要这个转发层：构建与导出逻辑住在各版本模块自己的 build-linglong.sh 里，
# 那是单一事实来源。本脚本只把版本名映射成模块路径再转发，不复制任何构建步骤——
# 复制出来的第二份迟早与模块脚本漂移，而这类漂移要打到真机上组装时才显形。
#
# 用法: 在仓库根运行
#   sh apps/linyaps/build-linglong.sh <版本> [模块脚本参数]
# 版本:
#   web       玲珑 web 版（Go + Wails 薄壳客户端）
#   official  玲珑官方客户端版（Electron）
#   dual      玲珑切换器整合版（两个客户端 + 切换器）
#   tray      玲珑托盘薄壳版（仅薄壳客户端 + 切换器，不含官方 Electron）
# 也接受模块目录名 desktop-launcher / desktop-linglong / desktop-dual。
# 参数原样透传给模块脚本（如 --no-prepare）。
set -eu
cd "$(dirname "$0")/../.." # 仓库根

TRAY=0
case "${1:-}" in
  web | desktop-launcher) MODULE=desktop-launcher ;;
  official | desktop-linglong) MODULE=desktop-linglong ;;
  dual | desktop-dual) MODULE=desktop-dual ;;
  # tray 没有独立的模块目录：它是 dual 模块「不要官方客户端」的那份变体，模块脚本自己
  # 支持 --without-official。这里只多补一个参数，避免把整套构建逻辑再抄一份。
  tray) MODULE=desktop-dual; TRAY=1 ;;
  *)
    echo "用法: sh apps/linyaps/build-linglong.sh <web|official|dual|tray> [模块脚本参数]" >&2
    exit 2
    ;;
esac
shift

# 用 set -- 在原有参数前面插入开关，而不是先把开关拼进一个变量再用未加引号的展开：
# 后者要靠词拆分才成参数，开关里一旦出现空格或通配符就会变形，静态检查也判不了。
if [ "$TRAY" = "1" ]; then
  set -- --without-official "$@"
fi

exec sh "apps/linyaps/$MODULE/build-linglong.sh" "$@"

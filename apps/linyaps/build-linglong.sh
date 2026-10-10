#!/bin/sh
# linyaps 三个玲珑版本的聚合打包入口。
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
# 也接受模块目录名 desktop-launcher / desktop-linglong / desktop-dual。
# 参数原样透传给模块脚本（如 --no-prepare）。
set -eu
cd "$(dirname "$0")/../.." # 仓库根

case "${1:-}" in
  web | desktop-launcher) MODULE=desktop-launcher ;;
  official | desktop-linglong) MODULE=desktop-linglong ;;
  dual | desktop-dual) MODULE=desktop-dual ;;
  *)
    echo "用法: sh apps/linyaps/build-linglong.sh <web|official|dual> [模块脚本参数]" >&2
    exit 2
    ;;
esac
shift

exec sh "apps/linyaps/$MODULE/build-linglong.sh" "$@"

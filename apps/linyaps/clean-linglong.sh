#!/bin/sh
# linyaps 三个玲珑版本的聚合清理入口。
#
# 与 build-linglong.sh 同理：清理逻辑住在各版本模块自己的 clean-linglong.sh 里
# （含 --deep/--nuclear 分级），本脚本只做版本名到模块路径的映射与转发。
# 三个模块共用仓库根的 ll-builder 工作区，逐个清理彼此独立，不会互相干扰。
#
# 用法: 在仓库根运行
#   sh apps/linyaps/clean-linglong.sh <版本> [模块脚本参数]
# 版本:
#   web | official | dual   单个版本（也接受模块目录名）
#   all                     依次清理三个版本，遇到第一个失败即停止
# 参数原样透传给模块脚本（如 --deep / --nuclear）。
set -eu
cd "$(dirname "$0")/../.." # 仓库根

case "${1:-}" in
  web | desktop-launcher) MODULE=desktop-launcher ;;
  official | desktop-linglong) MODULE=desktop-linglong ;;
  dual | desktop-dual) MODULE=desktop-dual ;;
  all)
    # 逐个执行且不吞失败：清理是破坏性动作，静默跳过某个版本会让「清理过了」这句话不成立。
    shift
    for module in desktop-launcher desktop-linglong desktop-dual; do
      echo "==> 清理 $module"
      sh "apps/linyaps/$module/clean-linglong.sh" "$@"
    done
    exit 0
    ;;
  *)
    echo "用法: sh apps/linyaps/clean-linglong.sh <web|official|dual|all> [模块脚本参数]" >&2
    exit 2
    ;;
esac
shift

exec sh "apps/linyaps/$MODULE/clean-linglong.sh" "$@"

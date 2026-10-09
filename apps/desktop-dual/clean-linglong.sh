#!/bin/sh
# 清除 desktop-dual 的构建缓存与中间产物，确保下一次构建从干净状态开始。
#
# 为什么复用 apps/desktop-launcher/clean-linglong.sh：三个客户端模块共用同一个
# ll-builder 工作区（仓库根 linglong/）、同一批 *.uab 导出产物，以及同一套仓库级编译
# 产物（lib/、types/、node_modules、pnpm store）。那份脚本已经把这些共享项连同三级
# 清理档位一起覆盖；本模块特有的只有宿主暂存 stage/。若三份各写一遍，共享项的清理
# 规则迟早会各自漂移。
#
# 注意：本模块的 stage/ 是两份客户端产物的合并结果，清掉它不会动
# apps/desktop-launcher 与 apps/desktop-linglong 各自的 stage/——它们由各自的
# clean-linglong.sh 负责。
#
# 用法: 在仓库根运行
#   sh apps/desktop-dual/clean-linglong.sh            # 普通清理（默认）
#   sh apps/desktop-dual/clean-linglong.sh --deep     # 深度清理（依赖 + 构建）
#   sh apps/desktop-dual/clean-linglong.sh --nuclear  # 彻底清理（最极端）
set -eu
cd "$(dirname "$0")/../.." # 仓库根

# 本模块特有：prepare-offline.sh 的合并暂存（两个客户端，约 2.1 GB）。
STAGE=apps/desktop-dual/stage
if [ -d "$STAGE" ]; then
  echo "  - $STAGE/"
  rm -rf "$STAGE"
fi

sh apps/desktop-launcher/clean-linglong.sh "$@"

#!/bin/sh
# 组装双客户端包的宿主机产物：分别调用两个既有模块的 prepare-offline.sh，
# 再把两份 stage 合并到本模块的 stage/，供玲珑容器复制组装。
#
# 为什么不在这里重新实现构建：薄壳版与官方版各自的产出链路（离线暂存、依赖搬运、
# 主程序探测）已由各自模块维护，这里再写一遍就是第二份会漂移的实现。
#
# 用法：在仓库根运行
#   sh apps/desktop-dual/prepare-offline.sh
set -eu

LL_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$LL_DIR/../.." # 仓库根

echo "==> 1/4 产出薄壳版客户端产物"
sh apps/desktop-launcher/linglong/prepare-offline.sh

echo "==> 2/4 产出官方 Electron 客户端产物"
sh apps/desktop-linglong/prepare-offline.sh

echo "==> 3/4 合并两份产物"
STAGE=apps/desktop-dual/stage
rm -rf "$STAGE"
mkdir -p "$STAGE" "$STAGE/electron"
cp -a apps/desktop-launcher/linglong/stage/. "$STAGE/"
cp -a apps/desktop-linglong/stage/. "$STAGE/electron/"

echo "==> 4/4 收敛官方客户端主程序名"
# electron-builder 依 productName 生成名字（实测形如 @deepseek-aidsh-desktop），
# 而切换器按固定名 dsh-desktop-official 查找。名字里带 @ 也会让 .desktop 与 shell
# 引用更难写，这里统一改掉，让「上游怎么命名」不再泄漏到运行期。
MAIN_EXE=
for candidate in "$STAGE"/electron/*; do
  [ -f "$candidate" ] || continue
  [ -x "$candidate" ] || continue
  case "$candidate" in
    */chrome-sandbox|*.so|*.so.*) continue ;;
  esac
  MAIN_EXE=$candidate
  break
done
if [ -z "$MAIN_EXE" ]; then
  echo "未在 Electron 产物里找到主程序" >&2
  exit 1
fi
mv "$MAIN_EXE" "$STAGE/electron/dsh-desktop-official"

echo "==> 暂存完成: $STAGE"
du -sh "$STAGE"

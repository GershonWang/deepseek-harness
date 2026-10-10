#!/bin/sh
# 组装双客户端包的宿主机产物：分别调用两个既有模块的 prepare-offline.sh，合并两份
# stage，再构建本模块的切换器与「关于」对话框助手，供玲珑容器复制组装。
#
# 为什么不在合并里重新实现客户端构建：薄壳版与官方版各自的产出链路（离线暂存、依赖
# 搬运、主程序探测）已由各自模块维护，这里再写一遍就是第二份会漂移的实现。切换器与
# 对话框助手反过来属于本模块，因此在这里构建。
#
# 用法：在仓库根运行
#   sh apps/linyaps/desktop-dual/prepare-offline.sh
set -eu

LL_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$LL_DIR/../../.." # 仓库根

echo "==> 1/6 产出薄壳版客户端产物"
sh apps/linyaps/desktop-launcher/linglong/prepare-offline.sh

echo "==> 2/6 产出官方 Electron 客户端产物"
sh apps/linyaps/desktop-linglong/prepare-offline.sh

echo "==> 3/6 合并两份产物"
STAGE=apps/linyaps/desktop-dual/stage
rm -rf "$STAGE"
mkdir -p "$STAGE" "$STAGE/electron"
cp -a apps/linyaps/desktop-launcher/linglong/stage/. "$STAGE/"
cp -a apps/linyaps/desktop-linglong/stage/. "$STAGE/electron/"

echo "==> 4/6 构建切换器（纯 Go，无 cgo）"
# 必须在 module 目录内构建：仓库根没有 go.mod。CGO_ENABLED=0 让切换器不链接
# GTK/WebKit——它只是托盘进程，不需要图形库，容器内也就不必带图形开发文件。
# 版本号注入二进制并在启动时打印，便于把托盘里跑着的进程与安装的包版本对上。
mkdir -p "$STAGE/bin"
LL_VERSION=$(grep -oP '^\s+version: \K[0-9.]+' apps/linyaps/desktop-dual/linglong.yaml | head -1)
(cd apps/linyaps/desktop-dual && CGO_ENABLED=0 go build -trimpath \
  -ldflags "-X main.version=$LL_VERSION" \
  -o stage/bin/dsh-desktop-switcher .)
if [ ! -x "$STAGE/bin/dsh-desktop-switcher" ]; then
  echo "切换器构建失败：$STAGE/bin/dsh-desktop-switcher 未生成" >&2
  exit 1
fi

echo "==> 5/6 编译「关于」对话框助手（GTK3）"
# 切换器不带窗口工具链，而「关于」需要一个能显示中文的窗口；容器里 GTK3 运行时本来
# 就在（薄壳客户端依赖它），因此这里用宿主机的 GTK3 开发文件编一个几十行的助手，而不是
# 往容器里塞 zenity 之类的新依赖。缺开发文件必须明确失败：否则包能装但「关于」点了没反应。
if ! pkg-config --exists gtk+-3.0; then
  echo "缺少 gtk+-3.0 开发文件，无法编译「关于」对话框助手" >&2
  exit 1
fi
# shellcheck disable=SC2046 # 词拆分正是 pkg-config 的用法
cc -O2 -Wall -o "$STAGE/bin/dsh-desktop-about" \
  apps/linyaps/desktop-dual/tools/dsh-about.c $(pkg-config --cflags --libs gtk+-3.0)
if [ ! -x "$STAGE/bin/dsh-desktop-about" ]; then
  echo "对话框助手构建失败：$STAGE/bin/dsh-desktop-about 未生成" >&2
  exit 1
fi

echo "==> 6/6 收敛官方客户端主程序名"
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

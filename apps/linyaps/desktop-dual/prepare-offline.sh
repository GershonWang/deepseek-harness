#!/bin/sh
# 组装双客户端包与 tray 变体的宿主机产物：调用既有模块的 prepare-offline.sh，合并
# stage（tray 变体只合并薄壳版那份），再构建本模块的切换器与「关于」对话框助手，
# 供玲珑容器复制组装。
#
# 为什么不在合并里重新实现客户端构建：薄壳版与官方版各自的产出链路（离线暂存、依赖
# 搬运、主程序探测）已由各自模块维护，这里再写一遍就是第二份会漂移的实现。切换器与
# 对话框助手反过来属于本模块，因此在这里构建。
#
# 为什么 tray 变体由本脚本加开关产出：它要的正是本脚本的其余步骤（同一份薄壳产物、
# 同一个切换器、同一个对话框助手），另起一份脚本只会让这三处慢慢漂移。
#
# 用法：在仓库根运行
#   sh apps/linyaps/desktop-dual/prepare-offline.sh [--without-official]
#   --without-official: 不产出官方 Electron 全链，stage 里也就没有 electron/ 目录，
#                       供 linglong-tray.yaml 组装「薄壳版 + 托盘切换器」
set -eu

# 循环解析而不是只看 $1：开关要能任意组合，只看 $1 会把后面的静默丢掉。未知参数直接
# 失败——拼错开关却照常去跑整套 Electron 暂存，是这里最贵的失败方式。
WITHOUT_OFFICIAL=0
while [ $# -gt 0 ]; do
  case "$1" in
    --without-official) WITHOUT_OFFICIAL=1 ;;
    *)
      echo "未知参数: $1" >&2
      echo "用法: sh apps/linyaps/desktop-dual/prepare-offline.sh [--without-official]" >&2
      exit 2
      ;;
  esac
  shift
done

LL_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$LL_DIR/../../.." # 仓库根

echo "==> 1/6 产出薄壳版客户端产物"
sh apps/linyaps/desktop-launcher/linglong/prepare-offline.sh

STAGE=apps/linyaps/desktop-dual/stage

if [ "$WITHOUT_OFFICIAL" = "0" ]; then
  echo "==> 2/6 产出官方 Electron 客户端产物"
  sh apps/linyaps/desktop-linglong/prepare-offline.sh
else
  # 第 2 步是本脚本里最贵的一步（整套 Electron 客户端的离线暂存），tray 变体整体跳过。
  # 这里不给切换器传任何开关：它按包内是否存在官方客户端自行决定菜单，而包内没有
  # electron/ 目录就是「只有薄壳版」这一事实的完整表达。
  echo "==> 2/6 跳过官方 Electron 客户端产物（--without-official）"
fi

echo "==> 3/6 合并产物"
rm -rf "$STAGE"
mkdir -p "$STAGE"
cp -a apps/linyaps/desktop-launcher/linglong/stage/. "$STAGE/"
if [ "$WITHOUT_OFFICIAL" = "0" ]; then
  mkdir -p "$STAGE/electron"
  cp -a apps/linyaps/desktop-linglong/stage/. "$STAGE/electron/"
fi

echo "==> 4/6 构建切换器（纯 Go，无 cgo）"
# 必须在 module 目录内构建：仓库根没有 go.mod。CGO_ENABLED=0 让切换器不链接
# GTK/WebKit——它只是托盘进程，不需要图形库，容器内也就不必带图形开发文件。
# 版本号注入二进制并在启动时打印，便于把托盘里跑着的进程与安装的包版本对上。
mkdir -p "$STAGE/bin"
# 版本号取自本次要打的包清单：tray 变体的包版本写在 linglong-tray.yaml 里，与双客户端
# 包不是同一条版本线。固定读 linglong.yaml 会把双客户端的版本印在托盘进程上，上面那条
# 「进程版本对得上安装的包」的用途随即失效。
if [ "$WITHOUT_OFFICIAL" = "1" ]; then
  LL_YAML=apps/linyaps/desktop-dual/linglong-tray.yaml
else
  LL_YAML=apps/linyaps/desktop-dual/linglong.yaml
fi
LL_VERSION=$(grep -oP '^\s+version: \K[0-9.]+' "$LL_YAML" | head -1)
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

if [ "$WITHOUT_OFFICIAL" = "0" ]; then
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
else
  echo "==> 6/6 跳过官方客户端主程序改名（--without-official）"
fi

echo "==> 暂存完成: $STAGE"
# 结尾按 stage 里实际的产物列出客户端，而不是按开关推断：开关只说明跳过了哪一步，
# 复用旧 stage 或上游布局变化都可能让两者不一致，人工核对要看的是实际内容。
if [ -x "$STAGE/bin/dsh-desktop-launcher" ]; then
  echo "    客户端: 薄壳版 bin/dsh-desktop-launcher"
else
  echo "    ✗ 薄壳客户端缺失: bin/dsh-desktop-launcher" >&2
fi
if [ -x "$STAGE/electron/dsh-desktop-official" ]; then
  echo "    客户端: 官方版 electron/dsh-desktop-official"
elif [ "$WITHOUT_OFFICIAL" = "0" ]; then
  echo "    ✗ 官方客户端缺失: electron/dsh-desktop-official" >&2
fi
du -sh "$STAGE"

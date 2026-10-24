#!/bin/sh
# 一键构建玲珑包（双客户端：官方 Electron + Go/Wails 薄壳；tray 变体只含薄壳）:
#   1. 宿主机产出客户端并合并暂存(prepare-offline.sh)
#   2. ll-builder build 在容器内组装
#   3. ll-builder export 导出 .uab 安装包到仓库根
#
# 两个变体共用本脚本：默认打双客户端包（linglong.yaml），--without-official 打
# 「薄壳版 + 托盘切换器」的 tray 变体（linglong-tray.yaml）。变体只决定选哪份包清单、
# 要不要跑 Electron 全链，以及导出树的整棵体积上限。
#
# 用法: 在仓库根运行
#   sh apps/linyaps/desktop-dual/build-linglong.sh [--without-official] [--no-prepare]
#   --without-official: 打 tray 变体（跳过官方 Electron 全链）
#   --no-prepare: 跳过 prepare-offline(复用现有 stage/,仅重打包)
set -eu
cd "$(dirname "$0")/../../.." # 仓库根

# 循环解析而不是只看 $1：两个开关要能组合（如 --no-prepare --without-official 复用
# 上一次 tray 的 stage 只重打包），只看 $1 时后一个开关会被静默丢掉、照常去跑 Electron。
WITHOUT_OFFICIAL=0
NO_PREPARE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --without-official) WITHOUT_OFFICIAL=1 ;;
    --no-prepare) NO_PREPARE=1 ;;
    *)
      echo "未知参数: $1" >&2
      echo "用法: sh apps/linyaps/desktop-dual/build-linglong.sh [--without-official] [--no-prepare]" >&2
      exit 2
      ;;
  esac
  shift
done

# 选包清单：tray 变体的清单里没有 Electron 段，混用会让容器组装阶段去拷一个不存在的
# stage/electron，失败点从「选错清单」挪到几百行之后的构建容器里。
if [ "$WITHOUT_OFFICIAL" = "1" ]; then
  YAML=apps/linyaps/desktop-dual/linglong-tray.yaml
else
  YAML=apps/linyaps/desktop-dual/linglong.yaml
fi
LL_ID=$(grep -oP '^\s+id: \K[0-9a-zA-Z.-]+' "$YAML" | head -1)
LL_VERSION=$(grep -oP '^\s+version: \K[0-9.]+' "$YAML" | head -1)
echo "==> 目标玲珑包: $LL_ID $LL_VERSION"

# 导出树整棵上限，按变体分档。薄壳版那份断言默认 1 GiB；双客户端包在同一棵树里多带
# 一整套官方 Electron 运行时，必然超过它，因此整棵这一项单独给值（阈值口径同薄壳版：
# 实测值 + 约 15% 余量）。tray 变体不含 Electron，沿用默认值即可，不另外维护一个近似数。
# 其余内容断言（该在的、不该在的、lib 单项上限）两变体共用同一份脚本。
MAX_TREE_UNIFIED=$((2300 * 1024 * 1024))

if [ "$NO_PREPARE" = "1" ]; then
  echo "==> 跳过 prepare-offline(复用现有 stage/)"
elif [ "$WITHOUT_OFFICIAL" = "1" ]; then
  sh apps/linyaps/desktop-dual/prepare-offline.sh --without-official
else
  sh apps/linyaps/desktop-dual/prepare-offline.sh
fi

# 容器工具清单 overlay 由上游 standard 预设派生（与薄壳版的 build-linglong.sh 同一行
# 调用、同一份脚本，不是第二份实现）。为什么在 prepare 之后再跑一次，而不是只依赖
# prepare-offline 的产物：--no-prepare 复用的是旧 stage，overlay 可能从未生成过（早于
# 该步骤的 stage），也可能是上游 preset 改动前的过期副本，而容器内的
# install-shell-runtime.sh 只对源文件缺失做硬断言、认不出过期内容。放在变体分支之外，
# 是因为 dual 与 tray 两个变体都要这一步。
echo "==> 由上游 standard 预设派生容器工具清单 overlay"
node apps/linyaps/desktop-launcher/linglong/gen-preset-overlay.mjs

# ll-builder 用 setuid newuidmap 映射用户命名空间；集成终端的 NoNewPrivs 会让它失败，
# 预检在必要时改走宿主透传包装。
. apps/linyaps/desktop-launcher/linglong/ll-builder-preflight.sh

# 构建器把「拷不进去」降级成警告：之后照常 [Install Files]/[Commit Contents] 并导出
# 产物，而包内那份文件停留在基础层的旧版本，两个环节都不报错（AUDIT N17）。双客户端包
# 同时含 Electron 与薄壳两套运行时，静默丢文件的后果比单客户端包更大，因此保留完整日志
# 并在导出前把该告警升级为硬失败；tray 变体沿用同一条路径，不另设分支。
BUILD_LOG=linglong/build.log
# 日志落在 ll-builder 的工作区内（.gitignore 的 /linglong/）。该目录由构建器在构建
# 过程中自行创建，脚本不能假设它在此之前存在：工作区被清空后 tee 因父目录缺失写不进
# 日志，却仍照常把构建输出转发到终端，外围看起来是一次正常构建，而导出已被跳过。
mkdir -p linglong
STATUS_FILE=$(mktemp)
trap 'rm -f "$STATUS_FILE"' EXIT
# POSIX sh 没有 pipefail，pipeline 的退出码只反映 tee。这里临时关掉 errexit，把 tee 与
# 构建器两侧的退出码都读回来，并先判日志是否真的落盘；否则 tee 失败会被 errexit 静默
# 吞掉，后续校验与导出全部跳过而终端上没有任何失败提示。
set +e
( set +e; ll-builder build -f "$YAML"; echo $? > "$STATUS_FILE" ) 2>&1 | tee "$BUILD_LOG"
TEE_STATUS=$?
set -e
if [ "$TEE_STATUS" -ne 0 ] || [ ! -s "$BUILD_LOG" ]; then
  echo "构建日志未完整落盘（$BUILD_LOG，tee 退出码 $TEE_STATUS），无法校验构建输出，中止导出" >&2
  exit 1
fi
BUILD_STATUS=$(cat "$STATUS_FILE")
if [ "$BUILD_STATUS" -ne 0 ]; then
  echo "ll-builder build 失败（退出码 $BUILD_STATUS），完整日志: $BUILD_LOG" >&2
  exit 1
fi

# 复用薄壳版的日志校验：它把未豁免的 'failed to copy' 判为失败。双客户端包内含薄壳版，
# webkit 那条豁免规则正是为它准备的；Electron 侧的共享库告警不受豁免，同样会被拦下。
sh apps/linyaps/desktop-launcher/linglong/verify-builder-log.sh "$BUILD_LOG"

# 导出树验收与薄壳版共用同一份脚本（依赖实体、工具清单、体积与内容断言），只有整棵上限
# 按变体分档：tray 变体不含 Electron，不传第二个参数即用薄壳版的默认值。这里只断言、
# 不改产物：导出树是提交层的物化副本，改它不会进包——本包早先把修复写在这一步，于是包内
# libblas/liblapack 断链一直存在，而断言看的是被改过的树。
echo "==> 验收导出树（依赖实体、工具清单、体积与内容）"
if [ "$WITHOUT_OFFICIAL" = "1" ]; then
  sh apps/linyaps/desktop-launcher/linglong/verify-export-tree.sh linglong/output/binary/files
else
  sh apps/linyaps/desktop-launcher/linglong/verify-export-tree.sh linglong/output/binary/files "$MAX_TREE_UNIFIED"
fi

ll-builder export --ref "main:$LL_ID/$LL_VERSION/x86_64"

ART="${LL_ID}_${LL_VERSION}_x86_64_main.uab"
[ -f "$ART" ] || { echo "导出失败: 未找到 $ART" >&2; exit 1; }
echo "==> 完成: $ART ($(du -h "$ART" | cut -f1))"
echo "==> 安装: ll-cli install ./$ART"

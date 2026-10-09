#!/bin/sh
# 一键构建玲珑包（官方 Electron 客户端）:
#   1. 宿主机产出 Electron 的 Linux 可运行目录并暂存(prepare-offline.sh)
#   2. ll-builder build 在容器内组装
#   3. ll-builder export 导出 .uab 安装包到仓库根
#
# 用法: 在仓库根运行
#   sh apps/desktop-linglong/build-linglong.sh [--no-prepare]
#   --no-prepare: 跳过 prepare-offline(复用现有 stage/,仅重打包)
set -eu
cd "$(dirname "$0")/../.." # 仓库根

YAML=apps/desktop-linglong/linglong.yaml
LL_ID=$(grep -oP '^\s+id: \K[0-9a-zA-Z.-]+' "$YAML" | head -1)
LL_VERSION=$(grep -oP '^\s+version: \K[0-9.]+' "$YAML" | head -1)
echo "==> 目标玲珑包: $LL_ID $LL_VERSION"

if [ "${1:-}" = "--no-prepare" ]; then
  echo "==> 跳过 prepare-offline(复用现有 stage/)"
else
  sh apps/desktop-linglong/prepare-offline.sh
fi

# 构建器把「拷不进去」降级成警告：之后照常 [Install Files]/[Commit Contents] 并导出
# 产物，而包内那份文件停留在基础层的旧版本，两个环节都不报错（AUDIT N17）。因此保留
# 完整日志，并在导出前把该告警升级为硬失败：能出包不等于出的是这次构建的东西。
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

# 复用薄壳版的日志校验：它把未豁免的 'failed to copy' 一律判失败。本包产物里没有
# webkit（那条豁免规则在此不会命中），所以同一条规则对 Electron 的共享库同样生效，
# 不必再写一份只差豁免清单的实现。
sh apps/desktop-launcher/linglong/verify-builder-log.sh "$BUILD_LOG"

ll-builder export --ref "main:$LL_ID/$LL_VERSION/x86_64"

ART="${LL_ID}_${LL_VERSION}_x86_64_main.uab"
[ -f "$ART" ] || { echo "导出失败: 未找到 $ART" >&2; exit 1; }
echo "==> 完成: $ART ($(du -h "$ART" | cut -f1))"
echo "==> 安装: ll-cli install ./$ART"

#!/bin/sh
# 一键构建玲珑(Linglong)程序包:
#   1. 宿主机构建全部产物并暂存(linglong/prepare-offline.sh)
#   2. ll-builder build 在容器内组装
#   3. ll-builder export 导出 .uab 安装包到仓库根
# 用法: 在仓库根运行
#   sh apps/desktop-launcher/build-linglong.sh [--no-prepare]
#   --no-prepare: 跳过 prepare-offline(复用现有 stage/,仅重打包)
set -eu
cd "$(dirname "$0")/../.." # 仓库根

YAML=apps/desktop-launcher/linglong/linglong.yaml
LL_ID=$(grep -oP '^\s+id: \K[0-9a-zA-Z.-]+' "$YAML" | head -1)
LL_VERSION=$(grep -oP '^\s+version: \K[0-9.]+' "$YAML" | head -1)
echo "==> 目标玲珑包: $LL_ID $LL_VERSION"

if [ "${1:-}" = "--no-prepare" ]; then
  echo "==> 跳过 prepare-offline(复用现有 stage/)"
else
  sh apps/desktop-launcher/linglong/prepare-offline.sh
fi

ll-builder build -f "$YAML"
echo "==> 清理 gcc 编译工具链（保留运行时库，减约 140 MB）"
sh apps/desktop-launcher/linglong/prune-gcc-toolchain.sh linglong/output/binary/files
echo "==> 校验合并产物树工具清单（含 git-core helper，launcher 以 GIT_EXEC_PATH 指回它）"
sh apps/desktop-launcher/linglong/verify-tools.sh linglong/output/binary/files
ll-builder export --ref "main:$LL_ID/$LL_VERSION/x86_64"

ART="${LL_ID}_${LL_VERSION}_x86_64_main.uab"
[ -f "$ART" ] || { echo "导出失败: 未找到 $ART" >&2; exit 1; }
echo "==> 完成: $ART ($(du -h "$ART" | cut -f1))"
echo "==> 安装: ll-cli install ./$ART"
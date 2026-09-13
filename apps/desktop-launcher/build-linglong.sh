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

# 容器工具清单 overlay 是上游 standard 预设的整文件副本,落后就会让打包会话静默
# 少挂插件。这里在容器组装之前拦截漂移,顺带确认 persona 配置能被随包
# dsh-persona 解析(否则挂载时才会抛 ValidationError,那已经在用户机器上了)。
echo "==> 校验预设 overlay 与上游 standard 的一致性"
node apps/desktop-launcher/linglong/verify-preset-overlay.mjs

# 构建器把「拷不进去」降级成警告：实测 libwebkit2gtk-4.1.so.0 复制失败（无效的参数）
# 之后仍照常 [Install Files]/[Commit Contents] 并导出 345 MB 产物，而包内沿用基础层
# 的旧库——比第 33 条更隐蔽，连报错都没有（AUDIT N17）。因此保留完整日志，并在导出
# 前把该告警升级为硬失败：能出包不等于出的是这次构建的东西。
BUILD_LOG=linglong/build.log
STATUS_FILE=$(mktemp)
trap 'rm -f "$STATUS_FILE"' EXIT
( set +e; ll-builder build -f "$YAML"; echo $? > "$STATUS_FILE" ) 2>&1 | tee "$BUILD_LOG"
BUILD_STATUS=$(cat "$STATUS_FILE")
if [ "$BUILD_STATUS" -ne 0 ]; then
  echo "ll-builder build 失败（退出码 $BUILD_STATUS），完整日志: $BUILD_LOG" >&2
  exit 1
fi
sh apps/desktop-launcher/linglong/verify-builder-log.sh "$BUILD_LOG"
echo "==> 清理 gcc 编译工具链（保留运行时库，减约 140 MB）"
sh apps/desktop-launcher/linglong/prune-gcc-toolchain.sh linglong/output/binary/files
echo "==> 校验合并产物树工具清单（含 git-core helper，launcher 以 GIT_EXEC_PATH 指回它）"
if sh apps/desktop-launcher/linglong/verify-tools.sh linglong/output/binary/files; then
  echo "==> 工具清单校验通过"
else
  echo "==> ⚠ 工具清单校验有失败项，继续导出（不影响包功能）" >&2
fi
ll-builder export --ref "main:$LL_ID/$LL_VERSION/x86_64"

ART="${LL_ID}_${LL_VERSION}_x86_64_main.uab"
[ -f "$ART" ] || { echo "导出失败: 未找到 $ART" >&2; exit 1; }
echo "==> 完成: $ART ($(du -h "$ART" | cut -f1))"
echo "==> 安装: ll-cli install ./$ART"
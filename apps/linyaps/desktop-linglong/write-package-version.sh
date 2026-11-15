#!/bin/sh
# 把玲珑包版本写进暂存产物，供应用「关于」窗口显示分发包版本。
#
# 玲珑包版本定义在 linglong.yaml 的 package.version，而该清单不随包安装，Electron
# 应用运行时读不到它。因此构建把版本写到 stage/resources/linglong-version.json，
# 应用启动后从自己的资源目录读回（见 apps/desktop/src/package-version.ts）。
#
# 手动流程（prepare-offline.sh 后直接 ll-builder build）与一键流程
# （build-linglong.sh --no-prepare 复用旧 stage）都会调用本脚本，缺了这一步「关于」
# 窗口会回退显示上游的桌面客户端版本，而不是玲珑包版本。
set -eu

LL_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$LL_DIR/../../.." # 仓库根

YAML=apps/linyaps/desktop-linglong/linglong.yaml
STAGE=apps/linyaps/desktop-linglong/stage

VERSION=$(grep -oP '^\s+version: \K[0-9][0-9A-Za-z.+_-]*' "$YAML" | head -1)
if [ -z "$VERSION" ]; then
  echo "未从 $YAML 解析到 package.version" >&2
  exit 1
fi

if [ ! -d "$STAGE/resources" ]; then
  echo "暂存产物缺少 resources/ 目录: $STAGE（先运行 prepare-offline.sh）" >&2
  exit 1
fi

printf '{\n  "version": "%s"\n}\n' "$VERSION" > "$STAGE/resources/linglong-version.json"
echo "==> 已写入分发包版本: $VERSION"

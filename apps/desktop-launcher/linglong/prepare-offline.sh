#!/bin/sh
# 在宿主机构建全部产物并暂存到 stage/，玲珑构建只做组装。
#
# 背景：玲珑构建容器环境问题多（Debian npm 代理 bug、无 HOME、beige 无
# Node 22、tsdown 在 Node 22 下加载配置失败），重工具链全部在宿主机跑，
# 容器内只复制组装。源码改动后需重新运行本脚本再打包。
#
# 用法：在仓库根运行
#   sh apps/desktop-launcher/linglong/prepare-offline.sh
set -eu
cd "$(dirname "$0")/../../.."   # 仓库根

STAGE=apps/desktop-launcher/linglong/stage
rm -rf "$STAGE"
mkdir -p "$STAGE/bin"

# 1. harness 全量构建（lib + web 前端）
pnpm install --frozen-lockfile
pnpm run build

# 2. deploy dsh 闭包并修复（peer deps、符号链接实体化、legacy hoists）
pnpm --filter @deepseek-ai/dsh deploy --legacy --prod \
  --config.auto-install-peers=false --config.node-linker=hoisted \
  "$STAGE/harness"
node scripts/fix-deploy-closure.mjs "$STAGE/harness"

# 3. Go 启动器（webkit2gtk-4.0 pkg-config shim 指向 4.1）
sh apps/desktop-launcher/linglong/prepare-pkgconfig.sh /tmp/dsh-pkgconfig
PKG_CONFIG_PATH=/tmp/dsh-pkgconfig CGO_ENABLED=1 \
  go build -o "$STAGE/bin/dsh-desktop-launcher" ./apps/desktop-launcher

echo "prepare-offline: 产物已暂存到 $STAGE"
echo "  下一步：ll-builder build -f apps/desktop-launcher/linglong/linglong.yaml"

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
ROOT=$(pwd)
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
#    必须在 module 目录内构建：仓库根没有 go.mod，从根 go build 会报
#    "cannot find main module"
sh apps/desktop-launcher/linglong/prepare-pkgconfig.sh /tmp/dsh-pkgconfig
( cd apps/desktop-launcher && PKG_CONFIG_PATH=/tmp/dsh-pkgconfig CGO_ENABLED=1 \
  go build -o "$ROOT/$STAGE/bin/dsh-desktop-launcher" . )

# 4. 捆绑 Node 24（harness 运行时需要 >=24：node:zlib.createZstdDecompress、
#    Promise.withResolvers、node:module.stripTypeScriptTypes；beige 只有 20 跑不起来）
#    linglong.yaml 组装时直接复用 stage/node，容器内不再下载。
if [ ! -x "$STAGE/node/bin/node" ]; then
  echo "prepare-offline: 下载 Node 24.9.0..."
  unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
  wget -q -O /tmp/node24.tar.gz https://registry.npmmirror.com/-/binary/node/v24.9.0/node-v24.9.0-linux-x64.tar.gz
  mkdir -p "$STAGE/node"
  tar -xzf /tmp/node24.tar.gz -C "$STAGE/node" --strip-components=1
fi

echo "prepare-offline: 产物已暂存到 $STAGE"
echo "  下一步：ll-builder build -f apps/desktop-launcher/linglong/linglong.yaml"

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

# 2.5 注入外部链接桥：桌面壳 GUI 里的 target=_blank 外链在 Wails WebKitGTK
#     中开不了新窗口，需在打包的 GUI dist 里注入脚本，把点击 URL 经
#     postMessage 交给启动器（BrowserOpenURL → 随包 xdg-open → 宿主 portal）。
sh apps/desktop-launcher/linglong/inject-link-bridge.sh \
  "$STAGE/harness/node_modules/@deepseek-ai/dsh-web-frontend/dist"

# 3. Go 启动器（wails，webkit2gtk-4.1；用 -tags webkit2_41 显式选 4.1）
#    必须在 module 目录内构建：仓库根没有 go.mod，从根 go build 会报
#    "cannot find main module"
# 注入玲珑包版本到关于弹框(从 linglong.yaml 的 package.version 提取；
# Version 变量在 internal/packaging 包，ldflags 需带完整 import 路径)
LL_VERSION=$(grep -oP '^\s+version: \K[0-9.]+' apps/desktop-launcher/linglong/linglong.yaml | head -1)
( cd apps/desktop-launcher && CGO_ENABLED=1 \
  go build -tags "production webkit2_41" \
  -ldflags "-X github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging.Version=$LL_VERSION" \
  -o "$ROOT/$STAGE/bin/dsh-desktop-launcher" . )

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

# 4.5 捆绑 pnpm（随包离线可用）：corepack 首次调用需联网下载 pnpm，且缓存
#     落 $HOME/.cache（容器内可能只读）；改为出厂直连捆绑 CLI。版本取
#     package.json 的 packageManager 字段，保证与仓库锁定的 pnpm 一致。
PNPM_V=$(node -e "console.log(require('./package.json').packageManager.split('@')[1])")
if [ ! -f "$STAGE/node/lib/node_modules/pnpm/bin/pnpm.cjs" ]; then
  echo "prepare-offline: 下载 pnpm $PNPM_V..."
  unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
  wget -q -O /tmp/pnpm.tgz "https://registry.npmmirror.com/pnpm/-/pnpm-$PNPM_V.tgz"
  mkdir -p "$STAGE/node/lib/node_modules/pnpm"
  tar -xzf /tmp/pnpm.tgz -C "$STAGE/node/lib/node_modules/pnpm" --strip-components=1
fi
# node/bin/pnpm 薄包装（路径由脚本位置推导，任意机器一致）。
# 注意：$0 可能经 $PREFIX/bin/pnpm 的软链调用（dirname 只拿到软链目录），
# 先用 readlink -f 解析真实位置（<node>/bin）；pnpm 装在
# <node>/lib/node_modules/pnpm，入口用 bin/pnpm.mjs（pnpm 11 的 bin/pnpm.cjs
# 只是 import('./pnpm.mjs') 兼容存根，真正 CLI 由 pnpm.mjs 加载 ../dist/pnpm.mjs）。
printf '%s\n' '#!/bin/sh' 'SELF=$(readlink -f "$0" 2>/dev/null || echo "$0")' \
  'DIR=$(dirname "$SELF")' \
  'exec "$DIR/node" "$DIR/../lib/node_modules/pnpm/bin/pnpm.mjs" "$@"' \
  > "$STAGE/node/bin/pnpm"
chmod +x "$STAGE/node/bin/pnpm"

# 5. 用捆绑 Node 24 在宿主机预编译 node-pty（运行时沙箱无 gcc/make，
#    一旦触发 node-gyp 源码编译终端就不可用；必须在此编译好 pty.node 打进包）。
#    宿主需有 make/gcc/python3。--nodedir 用捆绑 node 自带头文件，避免联网下载。
if [ -d "$STAGE/harness/node_modules/node-pty" ]; then
  echo "prepare-offline: 编译 node-pty (bundled node $($STAGE/node/bin/node --version))..."
  NODE_GYP="$ROOT/$STAGE/node/lib/node_modules/npm/node_modules/node-gyp/bin/node-gyp.js"
  ( cd "$ROOT/$STAGE/harness" && "$ROOT/$STAGE/node/bin/node" "$NODE_GYP" rebuild \
      --nodedir="$ROOT/$STAGE/node" --directory=node_modules/node-pty )
fi

echo "prepare-offline: 产物已暂存到 $STAGE"
echo "  下一步：ll-builder build -f apps/desktop-launcher/linglong/linglong.yaml"

#!/bin/sh
# 捆绑 Node 的 pnpm 薄包装。
#
# 单一来源：宿主侧 prepare-offline.sh 与容器内 linglong.yaml 的 fallback 都复制本文件，
# 两处各写一遍时改一处就会漂移（AUDIT N-extra3）。
#
# 为什么用 readlink -f 解析 $0：可能经 $PREFIX/bin/pnpm 软链调用，dirname 只拿得到
# 软链所在目录；解析出真实位置才能找到同目录的 node 与 ../lib/node_modules/pnpm。
#
# 入口用 bin/pnpm.mjs：pnpm 11 的 bin/pnpm.cjs 只是 import('./pnpm.mjs') 兼容存根，
# 真正 CLI 由 pnpm.mjs 加载 ../dist/pnpm.mjs。
SELF=$(readlink -f "$0" 2>/dev/null || echo "$0")
DIR=$(dirname "$SELF")
exec "$DIR/node" "$DIR/../lib/node_modules/pnpm/bin/pnpm.mjs" "$@"

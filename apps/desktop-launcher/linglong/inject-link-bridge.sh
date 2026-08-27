#!/bin/sh
# 把桌面壳外部链接桥注入到打包 harness 的 Web 前端 dist：
#   1) 复制 dsh-link-bridge.js 到 dist/assets/（与页面同源，frontend-static 直接服务）
#   2) 在 dist/index.html 的 </body> 前追加脚本标签（幂等：已含标记则跳过）
# 宿主侧在 prepare-offline.sh 的 dsh deploy 之后调用；容器内只做组装，不再碰 dist。
# 用法: inject-link-bridge.sh <dist-dir>
#   例: sh apps/desktop-launcher/linglong/inject-link-bridge.sh \
#       apps/desktop-launcher/linglong/stage/harness/node_modules/@deepseek-ai/dsh-web-frontend/dist
set -eu
DIST=${1:?usage: inject-link-bridge.sh <dist-dir>}
BRIDGE=$(dirname "$0")/dsh-link-bridge.js
INDEX="$DIST/index.html"
MARKER='dsh-link-bridge.js'

if [ ! -f "$INDEX" ]; then
  echo "inject-link-bridge: $INDEX 不存在（dist 结构变化？），中止构建" >&2
  exit 1
fi

mkdir -p "$DIST/assets"
cp "$BRIDGE" "$DIST/assets/dsh-link-bridge.js"
chmod 644 "$DIST/assets/dsh-link-bridge.js"

if grep -q "$MARKER" "$INDEX"; then
  echo "inject-link-bridge: $INDEX 已含桥接标签，跳过"
else
  sed -i "s#</body>#  <script src=\"/assets/dsh-link-bridge.js\"></script>\n</body>#" "$INDEX"
  echo "inject-link-bridge: 已注入到 $INDEX"
fi
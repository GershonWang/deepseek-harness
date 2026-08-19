#!/bin/sh
set -eu
# test-verify-tools.sh:verify-tools.sh 的通过/失败路径
# 用法: sh apps/desktop-launcher/linglong/test-verify-tools.sh
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
VERIFY="$ROOT/apps/desktop-launcher/linglong/verify-tools.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

mkbin() { mkdir -p "$(dirname "$TMP/$1")"; : > "$TMP/$1"; chmod +x "$TMP/$1"; }

# 通过路径:齐全的产物树
mkbin bin/git; mkbin bin/python3; mkbin bin/curl; mkbin bin/wget
mkbin bin/jq; mkbin bin/unzip; mkbin bin/xxd
mkdir -p "$TMP/node/bin"; : > "$TMP/node/bin/corepack"; chmod +x "$TMP/node/bin/corepack"
if "$VERIFY" "$TMP" >/dev/null 2>&1; then
  echo "PASS: 齐全产物树应通过"
else
  echo "FAIL: 齐全产物树未通过" >&2; exit 1
fi

# 失败路径:删 git
rm "$TMP/bin/git"
if "$VERIFY" "$TMP" >/dev/null 2>&1; then
  echo "FAIL: git 缺失应失败" >&2; exit 1
fi
echo "PASS: git 缺失时退出非零"

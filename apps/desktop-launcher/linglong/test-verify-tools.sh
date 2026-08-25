#!/bin/sh
set -eu
# test-verify-tools.sh:verify-tools.sh 的通过/失败路径
# 用法: sh apps/desktop-launcher/linglong/test-verify-tools.sh
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
VERIFY="$ROOT/apps/desktop-launcher/linglong/verify-tools.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

mkbin() { mkdir -p "$(dirname "$TMP/$1")"; : > "$TMP/$1"; chmod +x "$TMP/$1"; }

# 通过路径:齐全的产物树（git-core helper 随包，launcher 以 GIT_EXEC_PATH 指回）
mkbin bin/git; mkbin bin/git-lfs; mkbin bin/python3; mkbin bin/curl; mkbin bin/wget
mkbin bin/jq; mkbin bin/unzip; mkbin bin/xxd; mkbin bin/node
mkdir -p "$TMP/lib/git-core"; : > "$TMP/lib/git-core/git-remote-https"; chmod +x "$TMP/lib/git-core/git-remote-https"
mkdir -p "$TMP/node/bin"; : > "$TMP/node/bin/corepack"; chmod +x "$TMP/node/bin/corepack"
if "$VERIFY" "$TMP" >/dev/null 2>&1; then
  echo "PASS: 齐全产物树应通过"
else
  echo "FAIL: 齐全产物树未通过" >&2; exit 1
fi

# base 已标记的工具缺位也应通过(由基础运行时 org.deepin.base 提供,不进 $PREFIX)
rm "$TMP/bin/python3" "$TMP/bin/curl" "$TMP/bin/unzip"
if "$VERIFY" "$TMP" >/dev/null 2>&1; then
  echo "PASS: base 工具缺位应通过(基础运行时提供)"
else
  echo "FAIL: base 工具缺位未通过" >&2; exit 1
fi
# 恢复齐全树,供后续失败路径使用
mkbin bin/python3; mkbin bin/curl; mkbin bin/unzip

# 失败路径:git-core helper 缺失(launcher 的 GIT_EXEC_PATH 指向它,缺失即远程操作必挂)
rm "$TMP/lib/git-core/git-remote-https"
if "$VERIFY" "$TMP" >/dev/null 2>&1; then
  echo "FAIL: git-core helper 缺失应失败" >&2; exit 1
fi
echo "PASS: git-core helper 缺失时退出非零"
: > "$TMP/lib/git-core/git-remote-https"; chmod +x "$TMP/lib/git-core/git-remote-https"

# 失败路径:删 git
rm "$TMP/bin/git"
if "$VERIFY" "$TMP" >/dev/null 2>&1; then
  echo "FAIL: git 缺失应失败" >&2; exit 1
fi
echo "PASS: git 缺失时退出非零"

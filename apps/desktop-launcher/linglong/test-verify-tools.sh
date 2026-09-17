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
mkbin bin/jq; mkbin bin/unzip; mkbin bin/xxd; mkbin bin/node; mkbin bin/dsh
mkbin bin/xdg-open; mkbin bin/wl-paste
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

# installable/index 一致性校验的参照物缺失路径。
# 影子树把脚本与 tools.yaml 放到别处，让 $(dirname $0)/../internal/.../index.json
# 解析到不存在的路径：旧版这一段没有 else，校验与失败判定会一起静默消失。
SHADOW="$TMP/shadow/bin"
SH="$(command -v sh)"
mkdir -p "$SHADOW"
cp "$VERIFY" "$SHADOW/verify-tools.sh"
cp "$ROOT/apps/desktop-launcher/linglong/tools.yaml" "$SHADOW/tools.yaml"
: > "$TMP/bin/git"; chmod +x "$TMP/bin/git"   # 先恢复齐全树，隔离出单一失败源

out=$(sh "$SHADOW/verify-tools.sh" "$TMP" 2>&1) && {
  echo "FAIL: index.json 缺失应失败（旧版会静默跳过一致性校验）" >&2; exit 1; }
case "$out" in
  *index.json*) ;;
  *) echo "FAIL: index.json 缺失时的报错未点明参照物: $out" >&2; exit 1 ;;
esac
echo "PASS: index.json 缺失时退出非零并点明原因"

# 失败路径：PATH 上取不到 python3。旧版只打印 SKIP 就继续，校验等于不存在。
# 影子树里补一份真实 index.json，把"参照物缺失"这条失败源消掉，剩下的只可能是
# python3；断言同时要求 FAIL 与 python3，避免把清单里那行 `OK python3` 当成命中。
SHADOW_INDEX_DIR="$TMP/shadow/internal/toolchain/tools"
mkdir -p "$SHADOW_INDEX_DIR"
cp "$ROOT/apps/desktop-launcher/internal/toolchain/tools/index.json" "$SHADOW_INDEX_DIR/index.json"
NOPY="$TMP/nopy"
mkdir -p "$NOPY"
for c in awk mktemp rm dirname diff sort cat head sed grep sh; do
  p=$(command -v "$c" 2>/dev/null || true)
  [ -n "$p" ] && ln -sf "$p" "$NOPY/$c"
done
out=$(PATH="$NOPY" "$SH" "$SHADOW/verify-tools.sh" "$TMP" 2>&1) && {
  echo "FAIL: 缺 python3 应失败（旧版只打印 SKIP）" >&2; exit 1; }
case "$out" in
  *"FAIL installable/index"*python3*) ;;
  *) echo "FAIL: 缺 python3 时的报错未点明原因: $out" >&2; exit 1 ;;
esac
echo "PASS: 缺 python3 时退出非零并点明原因"

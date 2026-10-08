#!/bin/sh
# verify-artifact-size.sh 的通过/失败路径。
# 用法: sh apps/desktop-launcher/linglong/test-verify-artifact-size.sh
#
# 产物树用 fixture 目录模拟，不需要真实 ll-builder：被断言的只有路径的存在性与字节数。
set -eu

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
SCRIPT="$ROOT/apps/desktop-launcher/linglong/verify-artifact-size.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { echo "PASS: $1"; pass=$((pass + 1)); }
bad() { echo "FAIL: $1" >&2; fail=$((fail + 1)); }
skip() { echo "SKIP: $1"; }

# expect_fail <说明> <输出文件> <期望出现的子串...>
expect_fail() {
  desc=$1; out=$2; shift 2
  for needle in "$@"; do
    if ! grep -qF -- "$needle" "$out"; then
      bad "$desc（输出缺少「$needle」）"
      sed 's/^/       /' "$out" >&2
      return
    fi
  done
  ok "$desc"
}

# healthy_prefix <prefix>：造出一棵满足全部断言的导出树。
healthy_prefix() {
  p=$1
  mkdir -p "$p/bin" "$p/harness/lib" "$p/lib/x86_64-linux-gnu"
  : > "$p/bin/node"
  : > "$p/harness/lib/bin.js"
  : > "$p/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0"
}

new_case() {
  CASE="$TMP/$1"
  mkdir -p "$CASE"
  PREFIX="$CASE/prefix"
  healthy_prefix "$PREFIX"
}

run_case() {
  set +e
  sh "$SCRIPT" "$PREFIX" > "$1" 2>&1
  status=$?
  set -e
  return $status
}

# --- 场景 1：干净导出树 → 通过 ---
new_case healthy
if run_case "$TMP/out-healthy"; then
  ok "干净导出树通过"
else
  bad "干净导出树未通过"; sed 's/^/       /' "$TMP/out-healthy" >&2
fi

# --- 场景 2：lib/gcc 残留（裁剪失效）→ 失败 ---
new_case gcc
mkdir -p "$PREFIX/lib/gcc/x86_64-linux-gnu/12"
: > "$PREFIX/lib/gcc/x86_64-linux-gnu/12/cc1"
if run_case "$TMP/out-gcc"; then
  bad "lib/gcc 残留时应失败"
else
  expect_fail "lib/gcc 残留非零退出并指名" "$TMP/out-gcc" "FAIL 体积断言" "lib/gcc"
fi

# --- 场景 3：node/include 残留（裁剪失效）→ 失败 ---
new_case nodeinc
mkdir -p "$PREFIX/node/include/node"
: > "$PREFIX/node/include/node/node.h"
if run_case "$TMP/out-nodeinc"; then
  bad "node/include 残留时应失败"
else
  expect_fail "node/include 残留非零退出并指名" "$TMP/out-nodeinc" "FAIL 体积断言" "node/include"
fi

# --- 场景 4：bin/node 缺失 → 失败 ---
new_case nonode
rm "$PREFIX/bin/node"
if run_case "$TMP/out-nonode"; then
  bad "bin/node 缺失时应失败"
else
  expect_fail "bin/node 缺失非零退出并指名" "$TMP/out-nonode" "FAIL 体积断言" "bin/node"
fi

# --- 场景 5：harness 入口缺失 → 失败 ---
new_case noentry
rm "$PREFIX/harness/lib/bin.js"
if run_case "$TMP/out-noentry"; then
  bad "harness 入口缺失时应失败"
else
  expect_fail "harness 入口缺失非零退出并指名" "$TMP/out-noentry" "FAIL 体积断言" "harness/lib/bin.js"
fi

# --- 场景 5b：构建中间态（*.tsbuildinfo）进产物 → 失败 ---
new_case tsbuild
mkdir -p "$PREFIX/harness/node_modules/@deepseek-ai/dsh-x/lib"
: > "$PREFIX/harness/node_modules/@deepseek-ai/dsh-x/lib/tsconfig.tsbuildinfo"
if run_case "$TMP/out-tsbuild"; then
  bad "产物含 *.tsbuildinfo 时应失败"
else
  expect_fail "构建中间态进产物非零退出并指名" "$TMP/out-tsbuild" "FAIL 体积断言" "tsbuildinfo"
fi

# --- 场景 6/7：体积超限 → 失败（用稀疏文件造出表观体积，不占磁盘） ---
if command -v truncate >/dev/null 2>&1; then
  # 单项超限：lib/x86_64-linux-gnu 造到 400 MiB（上限 340 MiB）
  new_case biglib
  truncate -s 400M "$PREFIX/lib/x86_64-linux-gnu/libfiller.so"
  if run_case "$TMP/out-biglib"; then
    bad "lib/x86_64-linux-gnu 超限时应失败"
  else
    expect_fail "lib 单项超限非零退出并指名" "$TMP/out-biglib" "FAIL 体积断言" "lib/x86_64-linux-gnu" "超过上限"
  fi

  # 整棵超限：在断言范围之外放一个大文件（上限 1024 MiB）
  new_case bigtree
  mkdir -p "$PREFIX/share/dsh-fonts"
  truncate -s 1100M "$PREFIX/share/dsh-fonts/filler.ttf"
  if run_case "$TMP/out-bigtree"; then
    bad "整棵导出树超限时应失败"
  else
    expect_fail "整棵超限非零退出并指名" "$TMP/out-bigtree" "FAIL 体积断言" "超过上限"
  fi
else
  skip "无 truncate，跳过体积超限场景"
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "test-verify-artifact-size: $fail 项失败（通过 $pass 项）" >&2
  exit 1
fi
echo "test-verify-artifact-size: 全部 $pass 项通过"

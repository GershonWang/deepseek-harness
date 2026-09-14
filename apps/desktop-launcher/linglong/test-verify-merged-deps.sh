#!/bin/sh
# verify-merged-deps.sh 的通过/失败路径。
# 用法: sh apps/desktop-launcher/linglong/test-verify-merged-deps.sh
#
# 产物树用 fixture 目录模拟：脚本按 dirname $0 找 linglong.yaml / tools.yaml，
# 因此每个用例把脚本与清单一起复制到独立目录，不需要真实 ll-builder。
set -eu

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
SCRIPT="$ROOT/apps/desktop-launcher/linglong/verify-merged-deps.sh"
REAL_YAML="$ROOT/apps/desktop-launcher/linglong/linglong.yaml"
REAL_TOOLS="$ROOT/apps/desktop-launcher/linglong/tools.yaml"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# 脚本用 dirname $0 定位单一来源文件（../internal/packaging/webkit-exec-path.txt）；
# 用例都在 $TMP/<name>/ 下，把它按同样的相对位置放一份。
mkdir -p "$TMP/internal/packaging"
cp "$ROOT/apps/desktop-launcher/internal/packaging/webkit-exec-path.txt" "$TMP/internal/packaging/"

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

# mkentity <prefix> <相对路径>：造出一个普通文件实体。
mkentity() {
  mkdir -p "$(dirname "$1/$2")"
  : > "$1/$2"
}

# healthy_prefix <prefix>：按规则表造出全部"期望出现在产物里"的实体，
# 路径取自真实产物层的实测落点（见 verify-merged-deps.sh 的规则表注释）。
# webkit 那份带 exec-path 补丁标记——产物断言要求进包的是打过补丁的那一份。
WEBKIT_SHORT_PATH=$(tr -d '[:space:]' < "$ROOT/apps/desktop-launcher/internal/packaging/webkit-exec-path.txt")
healthy_prefix() {
  p=$1
  mkentity "$p" lib/x86_64-linux-gnu/libjavascriptcoregtk-4.1.so.0
  mkentity "$p" bin/zip
  mkentity "$p" bin/git
  mkentity "$p" bin/git-lfs
  mkentity "$p" bin/wget
  mkentity "$p" bin/jq
  mkentity "$p" bin/xxd
  mkentity "$p" bin/xdg-open
  # webkit：普通文件 + 补丁短路径字符串（模拟 build 段解出并打过补丁的那一份）
  mkdir -p "$p/lib/x86_64-linux-gnu"
  printf 'ELF...%s.../injected-bundle/' "$WEBKIT_SHORT_PATH" \
    > "$p/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0"
}

new_case() {
  CASE="$TMP/$1"
  mkdir -p "$CASE"
  cp "$SCRIPT" "$CASE/verify-merged-deps.sh"
  cp "$REAL_TOOLS" "$CASE/tools.yaml"
  # 脚本按 dirname $0 的布局找 ../internal/packaging/webkit-exec-path.txt；用例都放在
  # $TMP/<name>/ 下，因此单一来源文件在 $TMP/internal/ 下备一份即可（与仓库布局一致）。
  mkdir -p "$CASE/prefix"
  PREFIX="$CASE/prefix"
}

run_case() {
  set +e
  sh "$CASE/verify-merged-deps.sh" "$PREFIX" > "$1" 2>&1
  status=$?
  set -e
  return $status
}

# --- 场景 1：真实清单 + 齐全产物树 → 通过 ---
# 这一项同时证明覆盖完整：真实 linglong.yaml 里的每个包都被规则表认领。
new_case healthy
cp "$REAL_YAML" "$CASE/linglong.yaml"
healthy_prefix "$PREFIX"
if run_case "$TMP/out-healthy"; then
  ok "真实清单 + 齐全产物树通过（每个依赖都被规则表认领）"
else
  bad "齐全产物树未通过"; sed 's/^/       /' "$TMP/out-healthy" >&2
fi

# --- 场景 2：tools.yaml 工具实体缺失 → 失败 ---
new_case notool
cp "$REAL_YAML" "$CASE/linglong.yaml"
healthy_prefix "$PREFIX"
rm "$PREFIX/bin/git"
if run_case "$TMP/out-notool"; then
  bad "工具实体缺失时应失败"
else
  expect_fail "工具实体缺失非零退出并指名" "$TMP/out-notool" "FAIL git" "bin/git"
fi

# --- 场景 3：webkit 库缺失 → 失败 ---
new_case nolib
cp "$REAL_YAML" "$CASE/linglong.yaml"
healthy_prefix "$PREFIX"
rm "$PREFIX/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0"
if run_case "$TMP/out-nolib"; then
  bad "webkit 库缺失时应失败"
else
  expect_fail "webkit 库缺失非零退出并指名" "$TMP/out-nolib" "FAIL libwebkit2gtk-4.1-0"
fi

# --- 场景 3b：webkit 在，但没有 exec-path 补丁标记（进包的是未打补丁的旧库）→ 失败 ---
new_case unpatched
cp "$REAL_YAML" "$CASE/linglong.yaml"
healthy_prefix "$PREFIX"
printf 'ELF.../usr/lib/x86_64-linux-gnu/webkit2gtk-4.1...' \
  > "$PREFIX/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0"
if run_case "$TMP/out-unpatched"; then
  bad "webkit 未打补丁时应失败"
else
  expect_fail "webkit 缺补丁标记非零退出并指名" "$TMP/out-unpatched" "FAIL libwebkit2gtk-4.1-0" "exec-path 补丁标记"
fi

# --- 场景 4：zip 实体缺失（只在 depends 里、tools.yaml 未收录的包）→ 失败 ---
new_case nozip
cp "$REAL_YAML" "$CASE/linglong.yaml"
healthy_prefix "$PREFIX"
rm "$PREFIX/bin/zip"
if run_case "$TMP/out-nozip"; then
  bad "zip 缺失时应失败"
else
  expect_fail "zip 缺失非零退出并指名" "$TMP/out-nozip" "FAIL zip" "bin/zip"
fi

# --- 场景 5：产物里出现字符设备（N19 产物级特征）→ 失败 ---
new_case chardev
cp "$REAL_YAML" "$CASE/linglong.yaml"
healthy_prefix "$PREFIX"
DEV="$PREFIX/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0.19.7"
if mknod "$DEV" c 0 0 2>/dev/null; then
  if run_case "$TMP/out-chardev"; then
    bad "存在字符设备时应失败"
  else
    expect_fail "字符设备非零退出并指名" "$TMP/out-chardev" "字符设备" "libwebkit2gtk-4.1.so.0.19.7"
  fi
else
  # 非 root 且无 CAP_MKNOD 时无法造出真实字符设备；此时退回"跳过硬失败"，只提示。
  skip "无 mknod 权限，跳过字符设备场景"
fi

# --- 场景 6：产物里残留 *.dpkg-new → 失败 ---
new_case residue
cp "$REAL_YAML" "$CASE/linglong.yaml"
healthy_prefix "$PREFIX"
mkentity "$PREFIX" usr/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0.19.7.dpkg-new
if run_case "$TMP/out-residue"; then
  bad "存在 *.dpkg-new 残留时应失败"
else
  expect_fail "dpkg 中间态残留非零退出并指名" "$TMP/out-residue" "dpkg 中间态" "dpkg-new"
fi

# --- 场景 7：新增依赖未在规则表认领 → 失败（不允许静默无人校验） ---
new_case unclaimed
cp "$REAL_YAML" "$CASE/linglong.yaml"
sed 's/^      - xdg-utils$/      - xdg-utils\n      - libnewdep-9.9/' "$CASE/linglong.yaml" > "$CASE/linglong.yaml.new"
mv "$CASE/linglong.yaml.new" "$CASE/linglong.yaml"
healthy_prefix "$PREFIX"
if run_case "$TMP/out-unclaimed"; then
  bad "未认领的依赖应失败"
else
  expect_fail "未认领依赖非零退出并指名" "$TMP/out-unclaimed" "FAIL libnewdep-9.9" "规则表里认领"
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "test-verify-merged-deps: $fail 项失败（通过 $pass 项）" >&2
  exit 1
fi
echo "test-verify-merged-deps: 全部 $pass 项通过"

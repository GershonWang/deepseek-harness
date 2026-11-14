#!/bin/sh
# verify-merged-deps.sh 的通过/失败路径。
# 用法: sh apps/linyaps/desktop-linglong/test-verify-merged-deps.sh
#
# 产物树用 fixture 目录模拟：脚本按 dirname $0 找 linglong.yaml，因此每个用例把
# 脚本与清单一起复制到独立目录，不需要真实 ll-builder。
set -eu

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
SCRIPT="$ROOT/apps/linyaps/desktop-linglong/verify-merged-deps.sh"
REAL_YAML="$ROOT/apps/linyaps/desktop-linglong/linglong.yaml"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { echo "PASS: $1"; pass=$((pass + 1)); }
bad() { echo "FAIL: $1" >&2; fail=$((fail + 1)); }

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

# mkcase <name>：在 $TMP/<name>/ 下放一份脚本与清单。
mkcase() {
  dir="$TMP/$1"
  mkdir -p "$dir"
  cp "$SCRIPT" "$dir/"
  cp "$REAL_YAML" "$dir/"
  printf '%s\n' "$dir"
}

# entity <prefix> <相对路径>：造出一个普通文件实体。
entity() {
  mkdir -p "$1/$(dirname "$2")"
  : > "$1/$2"
}

# 全部依赖都齐备的产物树，作为"应当通过"的基线。
seed_complete() {
  prefix=$1
  for path in bin/git bin/git-lfs bin/wget bin/jq bin/xxd bin/zip bin/xdg-open bin/wl-paste; do
    entity "$prefix" "$path"
  done
  mkdir -p "$prefix/lib/x86_64-linux-gnu"
}

# --- 1. 依赖齐备时通过 ---
dir=$(mkcase complete)
prefix="$dir/prefix"
seed_complete "$prefix"
if sh "$dir/verify-merged-deps.sh" "$prefix" > "$dir/out" 2>&1; then
  ok "全部依赖齐备时通过"
else
  bad "全部依赖齐备时应通过"
  sed 's/^/       /' "$dir/out" >&2
fi

# --- 2. 缺一个工具实体时失败，并点名该包 ---
dir=$(mkcase missing)
prefix="$dir/prefix"
seed_complete "$prefix"
rm -f "$prefix/bin/jq"
if sh "$dir/verify-merged-deps.sh" "$prefix" > "$dir/out" 2>&1; then
  bad "缺少 jq 时应失败"
else
  expect_fail "缺少 jq 时点名该包" "$dir/out" "FAIL jq:" "bin/jq"
fi

# --- 3. 声明了但规则表没认领时失败 ---
dir=$(mkcase unclaimed)
prefix="$dir/prefix"
seed_complete "$prefix"
# 往清单里加一个规则表里没有的包。
printf '      - libunheard-of-package\n' >> "$dir/linglong.yaml"
if sh "$dir/verify-merged-deps.sh" "$prefix" > "$dir/out" 2>&1; then
  bad "未认领的依赖应失败"
else
  expect_fail "未认领的依赖被点名" "$dir/out" "未在 verify-merged-deps.sh 的规则表里认领"
fi

# --- 4. 产物树里的字符设备被判定为损坏 ---
dir=$(mkcase chardev)
prefix="$dir/prefix"
seed_complete "$prefix"
# 字符设备带执行位时 -x 为真，会骗过只查存在性的断言。
if mknod "$prefix/bin/ghost" c 1 3 2>/dev/null; then
  if sh "$dir/verify-merged-deps.sh" "$prefix" > "$dir/out" 2>&1; then
    bad "字符设备应被判为损坏"
  else
    expect_fail "字符设备被判定为损坏" "$dir/out" "字符设备"
  fi
else
  echo "SKIP: 无权限创建字符设备，跳过该用例"
fi

# --- 5. dpkg 中间态残留被判定为损坏 ---
dir=$(mkcase dpkgnew)
prefix="$dir/prefix"
seed_complete "$prefix"
entity "$prefix" "bin/jq.dpkg-new"
if sh "$dir/verify-merged-deps.sh" "$prefix" > "$dir/out" 2>&1; then
  bad "dpkg 残留应被判为损坏"
else
  expect_fail "dpkg 残留被判定为损坏" "$dir/out" "dpkg 中间态"
fi

# --- 6. 库搜索路径上的断链被判为损坏 ---
dir=$(mkcase dangling)
prefix="$dir/prefix"
seed_complete "$prefix"
ln -s /nonexistent/libfoo.so.1 "$prefix/lib/x86_64-linux-gnu/libfoo.so.1"
if sh "$dir/verify-merged-deps.sh" "$prefix" > "$dir/out" 2>&1; then
  bad "断链符号链接应被判为损坏"
else
  expect_fail "断链符号链接被判定为损坏" "$dir/out" "断链符号链接"
fi

# --- 6b. git 模板缺失时报 FAIL（有 share/git-core 但缺 description）---
dir=$(mkcase templates-broken)
prefix="$dir/prefix"
seed_complete "$prefix"
mkdir -p "$prefix/share/git-core/templates"
if sh "$dir/verify-merged-deps.sh" "$prefix" > "$dir/out" 2>&1; then
  bad "模板目录缺 description 时应失败"
else
  expect_fail "模板目录缺 description 时点名" "$dir/out" "FAIL git templates"
fi

# --- 6c. git 模板齐备时 OK ---
dir=$(mkcase templates-ok)
prefix="$dir/prefix"
seed_complete "$prefix"
mkdir -p "$prefix/share/git-core/templates"
: > "$prefix/share/git-core/templates/description"
if sh "$dir/verify-merged-deps.sh" "$prefix" > "$dir/out" 2>&1; then
  if grep -qF "OK   git templates" "$dir/out"; then
    ok "模板齐备时报告 OK"
  else
    bad "模板齐备时应报告 OK"
  fi
else
  bad "模板齐备时应通过"
  sed 's/^/       /' "$dir/out" >&2
fi

# --- 7. 清单解析不出依赖时失败（段结构被改坏）---
dir=$(mkcase nodeps)
prefix="$dir/prefix"
seed_complete "$prefix"
printf 'version: "1"\npackage:\n  id: x\n' > "$dir/linglong.yaml"
if sh "$dir/verify-merged-deps.sh" "$prefix" > "$dir/out" 2>&1; then
  bad "解析不出依赖时应失败"
else
  expect_fail "解析不出依赖时报错" "$dir/out" "未能从"
fi

echo
echo "test-verify-merged-deps: $pass passed, $fail failed"
[ "$fail" -eq 0 ] || exit 1

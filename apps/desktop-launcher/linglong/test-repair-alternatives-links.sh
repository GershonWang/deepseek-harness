#!/bin/sh
# repair-alternatives-links.sh 的用例：断链被修好、正常链接不被误动、无实体时硬失败。
set -eu

SCRIPT=$(dirname "$0")/repair-alternatives-links.sh
[ -f "$SCRIPT" ] || { echo "缺少 $SCRIPT" >&2; exit 1; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
fail=0

check() {
  desc=$1; shift
  if "$@"; then echo "OK   $desc"; else echo "FAIL $desc" >&2; fail=1; fi
}

# --- 用例 1：alternatives 断链 + 包内实体 → 必须修成相对链接且可解析 ---
root=$TMP/case1
mkdir -p "$root/lib/x86_64-linux-gnu/blas"
: > "$root/lib/x86_64-linux-gnu/blas/libblas.so.3.11.0"
ln -s /etc/alternatives/libblas.so.3-x86_64-linux-gnu "$root/lib/x86_64-linux-gnu/libblas.so.3"
sh "$SCRIPT" "$root" > "$TMP/case1.log" 2>&1 || { echo "FAIL 用例1 脚本非零退出" >&2; cat "$TMP/case1.log" >&2; fail=1; }
check "用例1 断链已可解析" test -e "$root/lib/x86_64-linux-gnu/libblas.so.3"
check "用例1 指向包内实体" test "$(readlink "$root/lib/x86_64-linux-gnu/libblas.so.3")" = "blas/libblas.so.3.11.0"

# --- 用例 2：正常链接不得被动 ---
root=$TMP/case2
mkdir -p "$root/lib/x86_64-linux-gnu"
: > "$root/lib/x86_64-linux-gnu/libfoo.so.1.2.3"
ln -s libfoo.so.1.2.3 "$root/lib/x86_64-linux-gnu/libfoo.so.1"
before=$(readlink "$root/lib/x86_64-linux-gnu/libfoo.so.1")
sh "$SCRIPT" "$root" > /dev/null 2>&1 || { echo "FAIL 用例2 脚本非零退出" >&2; fail=1; }
check "用例2 正常链接未被改动" test "$(readlink "$root/lib/x86_64-linux-gnu/libfoo.so.1")" = "$before"

# --- 用例 3：断链但包内无实体 → 必须硬失败（不能静默放过）---
root=$TMP/case3
mkdir -p "$root/lib/x86_64-linux-gnu"
ln -s /etc/alternatives/libmissing.so.3-x86_64-linux-gnu "$root/lib/x86_64-linux-gnu/libmissing.so.3"
if sh "$SCRIPT" "$root" > "$TMP/case3.log" 2>&1; then
  echo "FAIL 用例3 无实体时应当非零退出" >&2; fail=1
else
  echo "OK   用例3 无实体时硬失败"
fi

# --- 用例 4：键入 PREFIX 缺失 → 非零退出 ---
if sh "$SCRIPT" "$TMP/nonexistent" > /dev/null 2>&1; then
  echo "FAIL 用例4 PREFIX 不存在时应当非零退出" >&2; fail=1
else
  echo "OK   用例4 PREFIX 不存在时硬失败"
fi

# --- 用例 5：真实产物（若存在）修完后 libgstlibav.so 的 blas 依赖可解析 ---
real=/opt/apps/com.deepseek.dsh-desktop/files
if [ -d "$real/lib/x86_64-linux-gnu" ]; then
  stage=$TMP/real
  mkdir -p "$stage"
  # 只复制本用例需要的部分，避免整树拷贝
  mkdir -p "$stage/lib/x86_64-linux-gnu"
  cp -a "$real/lib/x86_64-linux-gnu/blas" "$stage/lib/x86_64-linux-gnu/" 2>/dev/null || true
  cp -a "$real/lib/x86_64-linux-gnu/lapack" "$stage/lib/x86_64-linux-gnu/" 2>/dev/null || true
  cp -a "$real/lib/x86_64-linux-gnu/libblas.so.3" "$stage/lib/x86_64-linux-gnu/" 2>/dev/null || true
  cp -a "$real/lib/x86_64-linux-gnu/liblapack.so.3" "$stage/lib/x86_64-linux-gnu/" 2>/dev/null || true
  if sh "$SCRIPT" "$stage" > /dev/null 2>&1; then
    check "用例5 真实产物 libblas 修复后可解析" test -e "$stage/lib/x86_64-linux-gnu/libblas.so.3"
    check "用例5 真实产物 liblapack 修复后可解析" test -e "$stage/lib/x86_64-linux-gnu/liblapack.so.3"
  else
    echo "FAIL 用例5 真实产物上脚本非零退出" >&2; fail=1
  fi
else
  echo "SKIP 用例5 未找到真实产物树"
fi

[ "$fail" -eq 0 ] || { echo "test-repair-alternatives-links: 有用例失败" >&2; exit 1; }
echo "test-repair-alternatives-links: 全部通过"

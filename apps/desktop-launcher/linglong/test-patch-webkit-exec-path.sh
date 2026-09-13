#!/bin/sh
# patch-webkit-exec-path.sh 的通过/失败路径。
# 用法: sh apps/desktop-launcher/linglong/test-patch-webkit-exec-path.sh
set -eu

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
SCRIPT="$ROOT/apps/desktop-launcher/linglong/patch-webkit-exec-path.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { echo "PASS: $1"; pass=$((pass + 1)); }
bad() { echo "FAIL: $1" >&2; fail=$((fail + 1)); }

# 造一个含硬编码路径的"假的 .so"：够 patch 脚本做字节替换即可。
make_so() {
  {
    printf 'ELF-header-padding'
    printf '/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1'
    printf 'gap'
    printf '/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1/injected-bundle/'
    printf 'tail'
  } > "$1"
}

# --- 通过路径：替换成功，且旧路径消失、新路径出现 ---
SO="$TMP/ok.so"; make_so "$SO"
if out=$(sh "$SCRIPT" "$SO" 2>&1); then
  short=$(tr -d '[:space:]' < "$ROOT/apps/desktop-launcher/internal/packaging/webkit-exec-path.txt")
  if grep -qaF -- "$short" "$SO" && ! grep -qaF '/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1' "$SO"; then
    ok "替换成功：新短路径已写入，原路径已消失"
  else
    bad "替换后内容不符（新路径缺失或原路径残留）"; echo "$out" >&2
  fi
else
  bad "正常 .so 应当替换成功"; echo "$out" >&2
fi

# --- 失败路径：没有硬编码路径（版本或构建方式变了）→ 非零退出 ---
NO="$(printf 'nothing-to-patch-here')"
printf '%s' "$NO" > "$TMP/none.so"
if sh "$SCRIPT" "$TMP/none.so" >/dev/null 2>&1; then
  bad "无硬编码路径时应失败"
else
  ok "无硬编码路径时非零退出"
fi

# --- 失败路径：短路径比原路径长（等长替换不成立）---
# 复制脚本与单一来源文件到 fixture，改长短路径后运行。
FIX="$TMP/fix"
mkdir -p "$FIX/linglong" "$FIX/internal/packaging"
cp "$SCRIPT" "$FIX/linglong/patch-webkit-exec-path.sh"
printf '/tmp/%s\n' "$(printf 'x%.0s' $(seq 1 60))" > "$FIX/internal/packaging/webkit-exec-path.txt"
SO2="$TMP/long.so"; make_so "$SO2"
set +e
out=$(sh "$FIX/linglong/patch-webkit-exec-path.sh" "$SO2" 2>&1)
status=$?
set -e
if [ "$status" -eq 0 ]; then
  bad "短路径过长时应失败"
else
  case "$out" in
    *"replacement longer than original"*) ok "短路径过长时非零退出并说明原因" ;;
    *) bad "过长时的报错不明确"; echo "$out" >&2 ;;
  esac
fi

# --- 失败路径：单一来源文件缺失 ---
FIX2="$TMP/fix2"
mkdir -p "$FIX2/linglong"
cp "$SCRIPT" "$FIX2/linglong/patch-webkit-exec-path.sh"
set +e
sh "$FIX2/linglong/patch-webkit-exec-path.sh" "$TMP/ok.so" >/dev/null 2>&1
status=$?
set -e
if [ "$status" -eq 0 ]; then
  bad "单一来源文件缺失时应失败"
else
  ok "单一来源文件缺失时非零退出"
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "test-patch-webkit-exec-path: $fail 项失败（通过 $pass 项）" >&2
  exit 1
fi
echo "test-patch-webkit-exec-path: 全部 $pass 项通过"

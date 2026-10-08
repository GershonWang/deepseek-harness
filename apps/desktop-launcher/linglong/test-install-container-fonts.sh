#!/bin/sh
# install-container-fonts.sh 的通过/失败路径。
# 用法: sh apps/desktop-launcher/linglong/test-install-container-fonts.sh
#
# 只断言脚本自己的行为——落盘位置、权限、自包含配置、参数守卫。真实字体的族名由
# 构建段的 fc-query 结论背书（见 linglong.yaml 的 CJK 段），不在这里重测字形。
set -eu

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
SCRIPT="$ROOT/apps/desktop-launcher/linglong/install-container-fonts.sh"
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

# 造一份拉丁/等宽字体源目录：脚本只按 *.ttf 与 OFL.txt 取件，内容无关。
mkdir -p "$TMP/latin" "$TMP/mono" "$TMP/cjk"
printf 'latin-font' > "$TMP/latin/A.ttf"
printf 'mono-font' > "$TMP/mono/M.ttf"
printf 'license' > "$TMP/latin/OFL.txt"
printf 'cjk-font' > "$TMP/cjk/wqy-microhei.ttc"

run() { # run <输出文件> [中文字体文件]
  set +e
  sh "$SCRIPT" "$CASE/prefix" "$TMP/latin" "$TMP/mono" ${2:+"$2"} > "$1" 2>&1
  status=$?
  set -e
  return $status
}

# --- 场景 1：不给中文字体 → 照旧只装拉丁与等宽 ---
CASE="$TMP/no-cjk"
if run "$TMP/out-no-cjk"; then
  FONTDIR="$CASE/prefix/share/dsh-fonts"
  if [ -f "$FONTDIR/A.ttf" ] && [ -f "$FONTDIR/M.ttf" ] && [ -f "$FONTDIR/OFL-latin.txt" ]; then
    ok "不传中文字体时仍装好拉丁与等宽（含许可）"
  else
    bad "不传中文字体时拉丁/等宽/许可缺失"; ls -la "$FONTDIR" >&2
  fi
  if [ -e "$FONTDIR/wqy-microhei.ttc" ]; then
    bad "不传中文字体时不应出现 .ttc"
  else
    ok "不传中文字体时不落 .ttc"
  fi
else
  bad "不传中文字体时应成功"; sed 's/^/       /' "$TMP/out-no-cjk" >&2
fi

# --- 场景 2：给中文字体 → .ttc 落进同一个 fontconfig 目录，权限 644 ---
CASE="$TMP/with-cjk"
if run "$TMP/out-with-cjk" "$TMP/cjk/wqy-microhei.ttc"; then
  FONTDIR="$CASE/prefix/share/dsh-fonts"
  if [ -f "$FONTDIR/wqy-microhei.ttc" ] && cmp -s "$TMP/cjk/wqy-microhei.ttc" "$FONTDIR/wqy-microhei.ttc"; then
    ok "中文字体按字节落进 share/dsh-fonts"
  else
    bad "中文字体未落盘或内容不一致"; ls -la "$FONTDIR" >&2
  fi
  if [ "$(stat -c '%a' "$FONTDIR/wqy-microhei.ttc" 2>/dev/null)" = "644" ]; then
    ok "中文字体权限为 644"
  else
    bad "中文字体权限不是 644"
  fi
else
  bad "给中文字体时应成功"; sed 's/^/       /' "$TMP/out-with-cjk" >&2
fi

# --- 场景 3：自包含 fontconfig 配置——目录、可写缓存、以及中文族别名 ---
CASE="$TMP/with-cjk"
CONF="$CASE/prefix/etc/fonts/dsh-fonts.conf"
if [ -f "$CONF" ] \
   && grep -qF "<dir>$CASE/prefix/share/dsh-fonts</dir>" "$CONF" \
   && grep -qF "<cachedir>$CASE/prefix/var/cache/fontconfig</cachedir>" "$CONF" \
   && grep -qF "<family>WenQuanYi Micro Hei</family>" "$CONF"; then
  ok "配置含字体目录、可写缓存与中文族别名"
else
  bad "自包含配置缺少目录/缓存/中文族别名"; sed 's/^/       /' "$CONF" >&2 2>/dev/null || true
fi

# --- 场景 4：中文字体文件不存在 → 非零退出并指名 ---
CASE="$TMP/missing-cjk"
if run "$TMP/out-missing-cjk" "$TMP/cjk/nope.ttc"; then
  bad "中文字体缺失时应失败"
else
  expect_fail "中文字体缺失非零退出并指名" "$TMP/out-missing-cjk" "中文字体文件不存在" "nope.ttc"
fi

# --- 场景 5：拉丁目录不存在 → 旧的参数守卫仍然生效 ---
CASE="$TMP/missing-latin"
set +e
sh "$SCRIPT" "$CASE/prefix" "$TMP/nope" "$TMP/mono" > "$TMP/out-missing-latin" 2>&1
status=$?
set -e
if [ "$status" -eq 0 ]; then
  bad "拉丁目录缺失时应失败"
else
  expect_fail "拉丁目录缺失非零退出并指名" "$TMP/out-missing-latin" "目录不存在"
fi

# --- 场景 6：参数不全 → 用法提示 + 非零退出 ---
CASE="$TMP/usage"
set +e
sh "$SCRIPT" "$CASE/prefix" > "$TMP/out-usage" 2>&1
status=$?
set -e
if [ "$status" -eq 0 ]; then
  bad "缺参数时应失败"
else
  expect_fail "缺参数非零退出并给用法" "$TMP/out-usage" "用法: install-container-fonts.sh" "中文字体文件"
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "test-install-container-fonts: $fail 项失败（通过 $pass 项）" >&2
  exit 1
fi
echo "test-install-container-fonts: 全部 $pass 项通过"

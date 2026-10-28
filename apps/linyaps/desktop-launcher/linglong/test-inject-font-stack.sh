#!/bin/sh
# inject-font-stack.sh 的通过/失败路径，以及注入产物本身的判据。
# 用法: sh apps/linyaps/desktop-launcher/linglong/test-inject-font-stack.sh
#
# 断言的是脚本行为与产出物内容：注入位置、幂等、缺入口时的硬失败，以及 CSS 里
# 随包族名确实排在栈首。字体实体是否存在由 install-container-fonts.sh 的测试与
# 构建段负责，不在这里重测。
set -eu

ROOT=$(cd "$(dirname "$0")/../../../../" && pwd)   # 仓库根
SCRIPT="$ROOT/apps/linyaps/desktop-launcher/linglong/inject-font-stack.sh"
STYLE="$ROOT/apps/linyaps/desktop-launcher/linglong/dsh-font-stack.css"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { echo "PASS: $1"; pass=$((pass + 1)); }
bad() { echo "FAIL: $1" >&2; fail=$((fail + 1)); }

# 造一份最小 dist：结构取自真实 apps/web 产物（<head>…</head> + <body>）。
make_dist() {
  dir=$1
  mkdir -p "$dir/assets"
  cat > "$dir/index.html" <<'HTML'
<!doctype html>
<html lang="en">
  <head>
    <title>DSH</title>
  </head>
  <body>
    <div id="root"></div>
  </body>
</html>
HTML
}

# --- 场景 1：正常注入 ---
DIST="$TMP/dist"
make_dist "$DIST"
if sh "$SCRIPT" "$DIST" > "$TMP/out-ok" 2>&1; then
  if [ -f "$DIST/assets/dsh-font-stack.css" ] && cmp -s "$STYLE" "$DIST/assets/dsh-font-stack.css"; then
    ok "样式按字节落到 dist/assets/dsh-font-stack.css"
  else
    bad "样式未落盘或内容不一致"; ls -la "$DIST/assets" >&2
  fi
  if grep -q '<link rel="stylesheet" href="/assets/dsh-font-stack.css">' "$DIST/index.html"; then
    ok "index.html 含字体栈样式标签"
  else
    bad "index.html 缺少字体栈样式标签"; sed 's/^/       /' "$DIST/index.html" >&2
  fi
  link_line=$(grep -n 'dsh-font-stack.css' "$DIST/index.html" | head -1 | cut -d: -f1)
  head_line=$(grep -n '</head>' "$DIST/index.html" | head -1 | cut -d: -f1)
  if [ -n "$link_line" ] && [ -n "$head_line" ] && [ "$link_line" -lt "$head_line" ]; then
    ok "样式标签位于 </head> 之前（首屏绘制前就位）"
  else
    bad "样式标签必须在 </head> 之前（link@${link_line:-?} head@${head_line:-?}）"
  fi
else
  bad "正常 dist 应注入成功"; sed 's/^/       /' "$TMP/out-ok" >&2
fi

# --- 场景 2：重复运行幂等——标签只有一处，样式内容不变 ---
if sh "$SCRIPT" "$DIST" > "$TMP/out-again" 2>&1; then
  count=$(grep -c 'dsh-font-stack.css' "$DIST/index.html" || true)
  if [ "$count" -eq 1 ]; then
    ok "重复注入幂等（标签只有一处）"
  else
    bad "重复注入产生 $count 处标签"; sed 's/^/       /' "$DIST/index.html" >&2
  fi
else
  bad "重复运行应成功（幂等跳过）"; sed 's/^/       /' "$TMP/out-again" >&2
fi

# --- 场景 3：CSS 判据——随包族名排在栈首，用户设置仍可覆盖 ---
# 正文栈首个 family 必须是随包拉丁字体，第二个是随包中文字体：WebKit 按 family
# 顺序为缺字形字符回退：拉丁先命中随包 Noto Sans Display，中文再按
# 「宿主思源黑体 → 宿主 Noto CJK → 随包文泉驿」回退。
if grep -qF -- "--dsh-font-family-text-default: 'Noto Sans Display', 'Source Han Sans SC', 'Noto Sans CJK SC'," "$STYLE"; then
  ok "正文默认栈以随包拉丁与宿主 CJK 字体顺序开头"
else
  bad "正文默认栈首不是随包拉丁 + 宿主 CJK 顺序"; sed 's/^/       /' "$STYLE" >&2
fi
if grep -qF -- "'WenQuanYi Micro Hei', -apple-system" "$STYLE"; then
  ok "随包文泉驿仍留在栈中作为无宿主 CJK 机器的兜底"
else
  bad "随包文泉驿兜底被移出栈"; sed 's/^/       /' "$STYLE" >&2
fi
if grep -qF -- "--dsh-font-family-code-default: 'JetBrains Mono'" "$STYLE"; then
  ok "代码默认栈以随包 JetBrains Mono 开头"
else
  bad "代码默认栈首不是 JetBrains Mono"; sed 's/^/       /' "$STYLE" >&2
fi
if grep -q '!important' "$STYLE" && grep -q ':root:root' "$STYLE"; then
  ok "覆盖声明带 :root:root 与 !important（压过运行时插入的 base.css）"
else
  bad "覆盖声明缺少 :root:root 或 !important"
fi
if grep -q -- '--dsh-font-family-text:' "$STYLE" || grep -q -- '--dsh-font-family-code:' "$STYLE"; then
  bad "不应覆盖用户在设置里选的 --dsh-font-family-text/-code（只覆盖默认栈）"
else
  ok "只覆盖默认栈，用户自定义字体仍然优先"
fi

# --- 场景 4：dist 缺 index.html → 非零退出并指名 ---
DIST_MISSING="$TMP/no-index"
mkdir -p "$DIST_MISSING"
if sh "$SCRIPT" "$DIST_MISSING" > "$TMP/out-no-index" 2>&1; then
  bad "缺 index.html 时应失败"
else
  if grep -qF "index.html 不存在" "$TMP/out-no-index"; then
    ok "缺 index.html 非零退出并指名"
  else
    bad "缺 index.html 的报错未指名"; sed 's/^/       /' "$TMP/out-no-index" >&2
  fi
fi

# --- 场景 5：index.html 没有 </head> → 非零退出而不是静默跳过 ---
DIST_NOHEAD="$TMP/no-head"
mkdir -p "$DIST_NOHEAD/assets"
printf '<html><body>no head here</body></html>\n' > "$DIST_NOHEAD/index.html"
if sh "$SCRIPT" "$DIST_NOHEAD" > "$TMP/out-no-head" 2>&1; then
  bad "缺 </head> 时应失败"
else
  if grep -qF "缺少 </head>" "$TMP/out-no-head"; then
    ok "缺 </head> 非零退出并说明"
  else
    bad "缺 </head> 的报错未说明"; sed 's/^/       /' "$TMP/out-no-head" >&2
  fi
fi

# --- 场景 6：缺参数 → 用法提示 + 非零退出 ---
if sh "$SCRIPT" > "$TMP/out-usage" 2>&1; then
  bad "缺参数时应失败"
else
  if grep -qF "usage: inject-font-stack.sh" "$TMP/out-usage"; then
    ok "缺参数非零退出并给用法"
  else
    bad "缺参数的报错未给用法"; sed 's/^/       /' "$TMP/out-usage" >&2
  fi
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "test-inject-font-stack: $fail 项失败（通过 $pass 项）" >&2
  exit 1
fi
echo "test-inject-font-stack: 全部 $pass 项通过"

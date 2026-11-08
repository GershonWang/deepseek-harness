#!/bin/sh
# inject-webkit-perf.sh 的通过/失败路径，以及注入产物本身的判据。
# 用法: sh apps/linyaps/desktop-launcher/linglong/test-inject-webkit-perf.sh
#
# 断言的是脚本行为与产出物内容：注入位置、幂等、缺入口时的硬失败、对照实验开关，
# 以及 CSS 里确实只覆盖了预期的属性。样式是否真的降低 WebKit 渲染开销由真机手感与
# 采样决定，不在这里测。
set -eu

ROOT=$(cd "$(dirname "$0")/../../../../" && pwd)   # 仓库根
SCRIPT="$ROOT/apps/linyaps/desktop-launcher/linglong/inject-webkit-perf.sh"
STYLE="$ROOT/apps/linyaps/desktop-launcher/linglong/dsh-webkit-perf.css"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { echo "PASS: $1"; pass=$((pass + 1)); }
bad() { echo "FAIL: $1" >&2; fail=$((fail + 1)); }

# 造一份最小 dist：结构取自真实 web 前端产物（<head>…</head> + <body>）。
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
  if [ -f "$DIST/assets/dsh-webkit-perf.css" ] && cmp -s "$STYLE" "$DIST/assets/dsh-webkit-perf.css"; then
    ok "样式按字节落到 dist/assets/dsh-webkit-perf.css"
  else
    bad "样式未落盘或内容不一致"; ls -la "$DIST/assets" >&2
  fi
  if grep -q '<link rel="stylesheet" href="/assets/dsh-webkit-perf.css">' "$DIST/index.html"; then
    ok "index.html 含渲染降级样式标签"
  else
    bad "index.html 缺少渲染降级样式标签"; sed 's/^/       /' "$DIST/index.html" >&2
  fi
  link_line=$(grep -n 'dsh-webkit-perf.css' "$DIST/index.html" | head -1 | cut -d: -f1)
  head_line=$(grep -n '</head>' "$DIST/index.html" | head -1 | cut -d: -f1)
  if [ -n "$link_line" ] && [ -n "$head_line" ] && [ "$link_line" -lt "$head_line" ]; then
    ok "样式标签位于 </head> 之前（首屏绘制前就位）"
  else
    bad "样式标签必须在 </head> 之前（link@${link_line:-?} head@${head_line:-?}）"
  fi
else
  bad "正常 dist 应注入成功"; sed 's/^/       /' "$TMP/out-ok" >&2
fi

# --- 场景 2：重复运行幂等——标签只有一处 ---
if sh "$SCRIPT" "$DIST" > "$TMP/out-again" 2>&1; then
  count=$(grep -c 'dsh-webkit-perf.css' "$DIST/index.html" || true)
  if [ "$count" -eq 1 ]; then
    ok "重复注入幂等（标签只有一处）"
  else
    bad "重复注入产生 $count 处标签"; sed 's/^/       /' "$DIST/index.html" >&2
  fi
else
  bad "重复运行应成功（幂等跳过）"; sed 's/^/       /' "$TMP/out-again" >&2
fi

# --- 场景 3：CSS 判据——只覆盖布局过渡时长，且选择器不依赖哈希类名 ---
if grep -qF '[data-animating]' "$STYLE"; then
  ok "选择器挂在稳定的 data-animating 属性上（不依赖构建期哈希类名）"
else
  bad "选择器缺少 data-animating，会随上游类名变化失效"
fi
if grep -qF 'transition-duration: 0s' "$STYLE" && grep -q '!important' "$STYLE"; then
  ok "过渡时长被置零并带 !important"
else
  bad "缺少 transition-duration: 0s 或 !important"
fi
if grep -qE '^:root:root ' "$STYLE"; then
  ok "覆盖声明带 :root:root（压过运行时插入的 base.css）"
else
  bad "覆盖声明缺少 :root:root"
fi
# 判据只看声明行：注释里提到这些属性名是允许的，规则行出现才算问题。
if grep -qE '^[[:space:]]*transition-delay' "$STYLE"; then
  bad "不应覆盖 transition-delay：它承担一次性时序，覆盖只会让动效跳变"
else
  ok "未覆盖 transition-delay（避免误伤时序）"
fi
if grep -q 'grid-template-columns:' "$STYLE" || grep -qE '^[[:space:]]*width:' "$STYLE"; then
  bad "不应直接改写 grid/width 属性值，只关过渡时长即可"
else
  ok "未改写布局属性本身，只关闭其过渡"
fi

# --- 场景 4：DSH_WEBKIT_PERF=0 时整体跳过（对照实验），且不落盘 ---
DIST_OFF="$TMP/dist-off"
make_dist "$DIST_OFF"
if DSH_WEBKIT_PERF=0 sh "$SCRIPT" "$DIST_OFF" > "$TMP/out-off" 2>&1; then
  if [ -f "$DIST_OFF/assets/dsh-webkit-perf.css" ]; then
    bad "对照实验关闭时不应落盘样式"
  else
    ok "对照实验关闭时不落盘样式"
  fi
  if grep -q 'dsh-webkit-perf.css' "$DIST_OFF/index.html"; then
    bad "对照实验关闭时不应注入标签"
  else
    ok "对照实验关闭时不注入标签"
  fi
else
  bad "对照实验关闭时应成功退出"; sed 's/^/       /' "$TMP/out-off" >&2
fi

# --- 场景 5：dist 缺 index.html → 非零退出并指名 ---
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

# --- 场景 6：index.html 没有 </head> → 非零退出而不是静默跳过 ---
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

# --- 场景 7：缺参数 → 用法提示 + 非零退出 ---
if sh "$SCRIPT" > "$TMP/out-usage" 2>&1; then
  bad "缺参数时应失败"
else
  if grep -qF "usage: inject-webkit-perf.sh" "$TMP/out-usage"; then
    ok "缺参数非零退出并给用法"
  else
    bad "缺参数的报错未给用法"; sed 's/^/       /' "$TMP/out-usage" >&2
  fi
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "test-inject-webkit-perf: $fail 项失败（通过 $pass 项）" >&2
  exit 1
fi
echo "test-inject-webkit-perf: 全部 $pass 项通过"

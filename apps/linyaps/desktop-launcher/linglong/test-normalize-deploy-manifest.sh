#!/bin/sh
# normalize-deploy-manifest.mjs 的通过/失败路径。
# 用法: sh apps/linyaps/desktop-launcher/linglong/test-normalize-deploy-manifest.sh
#
# 用 fixture 目录模拟部署产物：脚本只读 <harness>/package.json 与
# <harness>/node_modules/<包名>/package.json，不需要真实 pnpm 部署，也不联网。
set -eu

ROOT=$(cd "$(dirname "$0")/../../../../" && pwd)   # 仓库根
SCRIPT="$ROOT/apps/linyaps/desktop-launcher/tools/normalize-deploy-manifest.mjs"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { echo "PASS: $1"; pass=$((pass + 1)); }
bad() { echo "FAIL: $1" >&2; fail=$((fail + 1)); }

# expect_contains <说明> <输出文件> <期望子串>
expect_contains() {
  desc=$1; out=$2; needle=$3
  if grep -qF -- "$needle" "$out"; then
    ok "$desc"
  else
    bad "$desc（输出缺少「$needle」）"; sed 's/^/       /' "$out" >&2
  fi
}

# new_case <名称>：建出一个空的 harness 目录并导出 HARNESS。
new_case() {
  CASE="$TMP/$1"
  HARNESS="$CASE/harness"
  mkdir -p "$HARNESS/node_modules"
}

# add_pkg <包名> <版本>：在 fixture 的 node_modules 里造出该包的实际清单。
add_pkg() {
  mkdir -p "$HARNESS/node_modules/$1"
  printf '{"name":"%s","version":"%s"}\n' "$1" "$2" > "$HARNESS/node_modules/$1/package.json"
}

run_case() {
  set +e
  node "$SCRIPT" "$HARNESS" > "$1" 2>&1
  status=$?
  set -e
  return $status
}

# --- 场景 1：dependencies 与 devDependencies 的协议值都被解析为部署目录内的实际版本 ---
new_case healthy
add_pkg '@deepseek-ai/dsh-hmr' '0.2.1-rc.2'
cat > "$HARNESS/package.json" <<'JSON'
{
  "name": "@deepseek-ai/dsh",
  "version": "0.2.1-rc.2",
  "dependencies": { "@deepseek-ai/dsh-hmr": "workspace:*", "zod": "^3.23.8" },
  "devDependencies": { "@deepseek-ai/dsh-hmr": "workspace:~" }
}
JSON
if run_case "$TMP/out-healthy"; then
  if node -e '
const m = require(process.argv[1])
const ok = m.dependencies["@deepseek-ai/dsh-hmr"] === "0.2.1-rc.2"
  && m.devDependencies["@deepseek-ai/dsh-hmr"] === "0.2.1-rc.2"
  && m.dependencies.zod === "^3.23.8"
process.exit(ok ? 0 : 1)
' "$HARNESS/package.json"; then
    ok "协议值被解析为实际版本，非协议值保持原样"
  else
    bad "协议值未按预期解析"; sed 's/^/       /' "$HARNESS/package.json" >&2
  fi
else
  bad "正常产物未通过"; sed 's/^/       /' "$TMP/out-healthy" >&2
fi

# --- 场景 2：协议值指向 node_modules 里不存在的包 → 失败并指名该包 ---
# 这是「deploy 闭包不完整」的形态。静默跳过会让产物继续带协议值，
# 问题被推迟到用户点安装按需 bundle 时才暴露，因此必须在这里失败。
new_case missing
cat > "$HARNESS/package.json" <<'JSON'
{ "name": "@deepseek-ai/dsh", "version": "0.2.1-rc.2",
  "dependencies": { "@deepseek-ai/dsh-absent": "workspace:*" } }
JSON
if run_case "$TMP/out-missing"; then
  bad "缺包时应失败"
else
  expect_contains "缺包非零退出并指名" "$TMP/out-missing" "dsh-absent"
fi

# --- 场景 3：清单已是发布形态 → 幂等通过，不改写文件 ---
new_case idempotent
add_pkg '@deepseek-ai/dsh-hmr' '0.2.1-rc.2'
cat > "$HARNESS/package.json" <<'JSON'
{ "name": "@deepseek-ai/dsh", "version": "0.2.1-rc.2",
  "dependencies": { "@deepseek-ai/dsh-hmr": "0.2.1-rc.2" } }
JSON
BEFORE=$(cat "$HARNESS/package.json")
if run_case "$TMP/out-idempotent"; then
  if [ "$BEFORE" = "$(cat "$HARNESS/package.json")" ]; then
    ok "已归一化的清单保持不变"
  else
    bad "已归一化的清单被改写"
  fi
else
  bad "已归一化的清单应通过"; sed 's/^/       /' "$TMP/out-idempotent" >&2
fi

# --- 场景 4：协议值落在依赖字段之外的字段 → 残留断言失败并给出路径 ---
# 覆盖「pnpm 或上游把依赖挪到别的字段」这一形态：替换逻辑不碰它，但断言必须点名，
# 否则安装器的源码判定仍会命中，问题同样被推迟。
new_case residual
cat > "$HARNESS/package.json" <<'JSON'
{ "name": "@deepseek-ai/dsh", "version": "0.2.1-rc.2",
  "dependencies": { "zod": "^3.23.8" },
  "pnpmOverrides": { "@deepseek-ai/dsh-x": "workspace:*" } }
JSON
if run_case "$TMP/out-residual"; then
  bad "残留协议值时应失败"
else
  expect_contains "残留协议值非零退出并给出路径" "$TMP/out-residual" "pnpmOverrides.@deepseek-ai/dsh-x"
fi

# --- 场景 5：非协议的其他依赖写法一律不动 ---
new_case others
add_pkg 'local-pkg' '1.0.0'
cat > "$HARNESS/package.json" <<'JSON'
{ "name": "@deepseek-ai/dsh", "version": "0.2.1-rc.2",
  "dependencies": { "local-pkg": "link:../local-pkg", "zod": "npm:zod@3.23.8", "exact": "1.2.3" } }
JSON
BEFORE=$(cat "$HARNESS/package.json")
if run_case "$TMP/out-others"; then
  if [ "$BEFORE" = "$(cat "$HARNESS/package.json")" ]; then
    ok "link:/npm:/具体版本均保持原样"
  else
    bad "非协议依赖被改写"; sed 's/^/       /' "$HARNESS/package.json" >&2
  fi
else
  bad "非协议依赖场景应通过"; sed 's/^/       /' "$TMP/out-others" >&2
fi

# --- 场景 6：部署根清单缺失 → 失败 ---
new_case nomanifest
if run_case "$TMP/out-nomanifest"; then
  bad "清单缺失时应失败"
else
  expect_contains "清单缺失非零退出并指名" "$TMP/out-nomanifest" "部署根清单不存在"
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "test-normalize-deploy-manifest: $fail 项失败（通过 $pass 项）" >&2
  exit 1
fi
echo "test-normalize-deploy-manifest: 全部 $pass 项通过"

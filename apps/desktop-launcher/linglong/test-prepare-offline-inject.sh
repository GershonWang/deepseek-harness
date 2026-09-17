#!/bin/sh
set -eu
# test-prepare-offline-inject.sh:prepare-offline.sh 的 inject_workspace_pkg 通过/失败路径
# 用法: sh apps/desktop-launcher/linglong/test-prepare-offline-inject.sh
#
# 函数从真实脚本里按定义边界提取，测的是构建实际使用的那一份，而不是副本。
# 断言集中在两点：注入判据取自包自己声明的入口（不再写死 lib/index.js），
# 以及目标 lib/ 已存在时不得把源目录嵌套复制成 lib/lib。
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
SCRIPT="$ROOT/apps/desktop-launcher/linglong/prepare-offline.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

sed -n '/^inject_workspace_pkg() {$/,/^}$/p' "$SCRIPT" > "$TMP/fn.sh"
if [ ! -s "$TMP/fn.sh" ]; then
  echo "FAIL: 未能从 $SCRIPT 提取 inject_workspace_pkg" >&2
  exit 1
fi

STAGE="$TMP/stage"
cd "$TMP"
# shellcheck disable=SC1090
. "$TMP/fn.sh"

fail() { echo "FAIL: $1" >&2; exit 1; }
pass() { echo "PASS: $1"; }
NM="$STAGE/harness/node_modules/@deepseek-ai"

# 用例 1：入口是 lib/index.cjs（schemastery 的形态），目标已是完整拷贝 → 不得注入。
# 修复前判据写死 lib/index.js，该守卫恒真，于是每次都重拷并嵌套出 lib/lib。
# 源侧多放一个文件：一旦真的注入了，它就会出现在目标里，这是"是否注入"的观测点。
mkdir -p pkgs/cjs/lib "$NM/cjs/lib"
printf '%s' '{"name":"@deepseek-ai/cjs","main":"lib/index.cjs","module":"lib/index.mjs"}' > pkgs/cjs/package.json
: > pkgs/cjs/lib/index.cjs
: > pkgs/cjs/lib/index.mjs
: > pkgs/cjs/lib/from-source.txt
: > "$NM/cjs/lib/index.cjs"
: > "$NM/cjs/lib/only-in-dest.txt"
inject_workspace_pkg pkgs/cjs
[ -f "$NM/cjs/lib/only-in-dest.txt" ] || fail "目标已完整时不应重新注入（目标侧文件被覆盖）"
[ ! -e "$NM/cjs/lib/from-source.txt" ] || fail "目标已完整时不应重新注入"
[ ! -e "$NM/cjs/lib/lib" ] || fail "目标 lib/ 已存在时不得嵌套复制出 lib/lib"
pass "入口为 lib/index.cjs 且目标完整时不注入"

# 用例 2：目标只有空壳 lib/（deploy 闭包的软链残留）→ 必须注入，且布局扁平。
mkdir -p pkgs/shell/lib "$NM/shell/lib"
printf '%s' '{"name":"@deepseek-ai/shell","main":"lib/index.cjs"}' > pkgs/shell/package.json
: > pkgs/shell/lib/index.cjs
inject_workspace_pkg pkgs/shell
[ -f "$NM/shell/lib/index.cjs" ] || fail "入口缺失时必须注入"
[ -f "$NM/shell/package.json" ] || fail "注入时应带上 package.json"
[ ! -e "$NM/shell/lib/lib" ] || fail "注入不得嵌套出 lib/lib"
pass "空壳目标目录会补入，且不嵌套"

# 用例 3：目标完全不存在 → 注入 lib/、bin/、package.json 与 README。
mkdir -p pkgs/full/lib pkgs/full/bin
printf '%s' '{"name":"@deepseek-ai/full","main":"lib/index.mjs"}' > pkgs/full/package.json
: > pkgs/full/lib/index.mjs
: > pkgs/full/bin/full-cli
: > pkgs/full/README.md
inject_workspace_pkg pkgs/full
[ -f "$NM/full/lib/index.mjs" ] || fail "目标不存在时应注入 lib/"
[ -f "$NM/full/bin/full-cli" ] || fail "目标不存在时应注入 bin/"
[ -f "$NM/full/README.md" ] || fail "目标不存在时应注入 README"
pass "目标不存在时完整注入"

# 用例 4：入口只由 exports 声明（无 main/module 字段），目标已有该入口 → 不注入。
mkdir -p pkgs/exports/lib "$NM/exports/lib"
printf '%s' '{"name":"@deepseek-ai/exports","exports":{".":{"import":"./lib/index.mjs"}}}' > pkgs/exports/package.json
: > pkgs/exports/lib/index.mjs
: > "$NM/exports/lib/index.mjs"
: > "$NM/exports/lib/sentinel.txt"
inject_workspace_pkg pkgs/exports
[ -f "$NM/exports/lib/sentinel.txt" ] || fail "exports 声明的入口已存在时不应重新注入"
pass "exports 声明的入口被认作已就位"

# 用例 5：源码包没有 lib/（例如纯静态资源包）→ 不进注入分支，也不报错。
mkdir -p pkgs/nolib
printf '%s' '{"name":"@deepseek-ai/nolib","main":"lib/index.js"}' > pkgs/nolib/package.json
inject_workspace_pkg pkgs/nolib
[ ! -e "$NM/nolib" ] || fail "没有 lib/ 的包不应被注入"
pass "没有 lib/ 的包静默跳过"

# 用例 6：非 @deepseek-ai 域与 experimental 包仍被跳过。
mkdir -p pkgs/other/lib
printf '%s' '{"name":"lodash"}' > pkgs/other/package.json
: > pkgs/other/lib/index.js
inject_workspace_pkg pkgs/other
[ ! -e "$NM/other" ] || fail "非 @deepseek-ai 域的包不应被注入"
pass "非 @deepseek-ai 域被跳过"

echo "全部通过"

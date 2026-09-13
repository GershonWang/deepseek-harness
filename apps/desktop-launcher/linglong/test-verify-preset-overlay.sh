#!/bin/sh
set -eu
# test-verify-preset-overlay.sh:verify-preset-overlay.mjs 的通过/失败路径。
#
# 为什么需要它:闸门本身是整文件副本唯一的漂移防线,它的失败路径若失效,漂移会
# 重新变成静默的。每个用例都断言闸门不仅非零退出,而且是因为预期的那条断言。
#
# 用法: sh apps/desktop-launcher/linglong/test-verify-preset-overlay.sh
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
GATE="$ROOT/apps/desktop-launcher/linglong/verify-preset-overlay.mjs"
SRC="$ROOT/packages/preset/agent-presets/presets/standard/agent.cordis.yml"
OVL="$ROOT/apps/desktop-launcher/linglong/harness-overlay/agent-presets/standard/agent.cordis.yml"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fail=0

# 断言闸门对给定 overlay 非零退出,且输出里出现预期关键字。
expect_reject() {
  label=$1; overlay=$2; keyword=$3
  if out=$(node "$GATE" "$SRC" "$overlay" 2>&1); then
    echo "FAIL: $label 应被拒绝,实际通过" >&2
    fail=1
  elif printf '%s' "$out" | grep -qF "$keyword"; then
    echo "PASS: $label 被拒绝($keyword)"
  else
    echo "FAIL: $label 被拒绝,但不是预期原因(期望含 '$keyword')" >&2
    printf '%s\n' "$out" >&2
    fail=1
  fi
}

# 通过路径:仓库内的规范副本必须一致且 persona 可被随包 schema 解析。
if node "$GATE" "$SRC" "$OVL" >/dev/null 2>&1; then
  echo "PASS: 规范 overlay 应通过"
else
  echo "FAIL: 规范 overlay 未通过" >&2
  node "$GATE" "$SRC" "$OVL" >&2 || true
  fail=1
fi

# 漂移:overlay 的某一行配置与上游不同(用 tool-web.fetch 模拟上游演进)。
sed 's/^    fetch: true$/    fetch: false/' "$OVL" > "$TMP/drift.yml"
expect_reject "roster 漂移" "$TMP/drift.yml" "roster 与上游漂移"

# 段落丢失:容器工具链说明被删掉,overlay 退化成上游原文 + 空行。
grep -v 'This deployment runs inside a Linglong sandbox container' "$OVL" > "$TMP/no-paragraph.yml"
expect_reject "容器说明缺失" "$TMP/no-paragraph.yml" "容器工具链说明丢失"

# 字段回退:退回已被 dsh-persona 移除的 text 字段。
sed 's/^    prefix: >-$/    text: >-/' "$OVL" > "$TMP/text-field.yml"
expect_reject "persona 用了 text 字段" "$TMP/text-field.yml" "prefix 与 suffix"

# schema 拒绝:结构检查都通过,但随包 dsh-persona 不接受该值。
sed "s/^    suffix: Your working directory is {{cwd}}\.$/    suffix: Your working directory is {{cwd}}.\n    includeRuntimeContext: 'yes'/" "$OVL" > "$TMP/bad-schema.yml"
expect_reject "schema 拒绝" "$TMP/bad-schema.yml" "随包 dsh-persona 拒绝"

exit "$fail"

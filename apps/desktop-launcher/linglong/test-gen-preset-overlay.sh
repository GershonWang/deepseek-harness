#!/bin/sh
set -eu
# test-gen-preset-overlay.sh:gen-preset-overlay.mjs 的通过/失败路径。
#
# 为什么需要它:闸门负责把上游 standard 预设派生成容器工具清单 overlay,它的失败
# 路径若失效,派生会重新变成静默的——段落丢失、工具与 tools.yaml 脱节、persona
# schema 不兼容都会到用户机器上才暴露。每个用例都断言闸门不仅非零退出,而且是
# 因为预期的那条断言。
#
# 用法: sh apps/desktop-launcher/linglong/test-gen-preset-overlay.sh
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
GATE="$ROOT/apps/desktop-launcher/linglong/gen-preset-overlay.mjs"
SRC="$ROOT/packages/bundle/web-app/presets/standard.patch.yml"
PARA="$ROOT/apps/desktop-launcher/linglong/harness-overlay/agent-presets/persona-container-toolchain.txt"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fail=0

# 断言闸门对给定输入非零退出,且输出里出现预期关键字。
expect_reject() {
  label=$1; keyword=$2; src=$3; out=$4; para=$5
  if stdout=$(node "$GATE" "$src" "$out" "$para" 2>&1); then
    echo "FAIL: $label 应被拒绝,实际通过" >&2
    fail=1
  elif printf '%s' "$stdout" | grep -qF "$keyword"; then
    echo "PASS: $label 被拒绝($keyword)"
  else
    echo "FAIL: $label 被拒绝,但不是预期原因(期望含 '$keyword')" >&2
    printf '%s\n' "$stdout" >&2
    fail=1
  fi
}

# 通过路径:规范源必须派生成功,且产物里带上容器说明。
if node "$GATE" "$SRC" "$TMP/ok.yml" "$PARA" >/dev/null 2>&1; then
  if grep -qF 'Linglong sandbox container' "$TMP/ok.yml"; then
    echo "PASS: 规范源派生成功且含容器说明"
  else
    echo "FAIL: 派生成功但产物里没有容器说明" >&2
    fail=1
  fi
else
  echo "FAIL: 规范源未通过" >&2
  node "$GATE" "$SRC" "$TMP/ok.yml" "$PARA" >&2 || true
  fail=1
fi

# 上游改名:注册行不在,派生没有对象(用改 id 模拟上游重命名)。
sed 's/^    - id: preset-standard$/    - id: preset-standard-renamed/' "$SRC" > "$TMP/renamed.yml"
expect_reject "上游注册行改名" "id=preset-standard 的 insert 行有 0 条" "$TMP/renamed.yml" "$TMP/renamed-out.yml" "$PARA"

# 字段回退:persona 退回已被 dsh-persona 移除的 text 字段。
sed 's/^              prefix: You are a coding agent/              text: You are a coding agent/' "$SRC" > "$TMP/text-field.yml"
expect_reject "persona 用了 text 字段" "schema 是 prefix/suffix" "$TMP/text-field.yml" "$TMP/text-out.yml" "$PARA"

# 段落丢失:段落文件缺少认领容器事实的那句。
printf 'This deployment runs in a container.\n' > "$TMP/paragraph-no-marker.txt"
expect_reject "容器说明缺失" "容器工具链说明丢失" "$SRC" "$TMP/nomarker-out.yml" "$TMP/paragraph-no-marker.txt"

# 工具脱节:段落点名了 tools.yaml 没声明的工具。
sed 's/dsh, curl, wget/dsh, curl, wget, nosuchtool/' "$PARA" > "$TMP/paragraph-stray-tool.txt"
expect_reject "点名未声明工具" "tools.yaml 的 tools: 没有它" "$SRC" "$TMP/stray-out.yml" "$TMP/paragraph-stray-tool.txt"

# 段落文件缺失:构建输入不全,没有可回退的来源。
expect_reject "段落文件缺失" "段落文件不存在或不可读" "$SRC" "$TMP/missing-out.yml" "$TMP/no-such-paragraph.txt"

# schema 拒绝:结构检查都通过,但随包 dsh-persona 不接受该值。
sed "s/^              suffix: Your working directory is {{cwd}}\.$/              suffix: Your working directory is {{cwd}}.\n              includeRuntimeContext: 'yes'/" "$SRC" > "$TMP/bad-schema.yml"
expect_reject "schema 拒绝" "随包 dsh-persona 拒绝" "$TMP/bad-schema.yml" "$TMP/bad-out.yml" "$PARA"

# 上游自带段落:再追加会变成重复段落,属于需人工确认的岔口。
sed 's/^              prefix: You are a coding agent powered by the {{model}} model\.$/              prefix: You are a coding agent powered by the {{model}} model. This deployment runs inside a Linglong sandbox container./' "$SRC" > "$TMP/already.yml"
expect_reject "上游已自带段落" "上游可能已自带该段落" "$TMP/already.yml" "$TMP/already-out.yml" "$PARA"

exit "$fail"

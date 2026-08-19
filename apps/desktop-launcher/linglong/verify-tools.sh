#!/bin/sh
# 校验玲珑合并产物树中 tools.yaml 清单工具的可用性。
# 宿主侧在 ll-builder build 之后运行(buildext depends 的合并发生在 preCommit,
# build: 容器阶段看不到合并结果)。
# 用法: verify-tools.sh <merged-prefix>   e.g. linglong/output/binary/files
set -eu
PREFIX=${1:?usage: verify-tools.sh <merged-prefix>}
YAML=$(dirname "$0")/tools.yaml
LIST=$(mktemp)
trap 'rm -f "$LIST"' EXIT

# 解析受约束的 YAML 子集,仅在 tools: 段内识别工具,输出 "name|binary|verify|shim"
# (installable:/excluded: 内的 2 空格 name 不算工具)
awk '
  /^[a-zA-Z0-9_-]+:$/ {
    if (name != "") emit();
    name = "";
    sec = $1; sub(/:$/, "", sec);
    in_tools = (sec == "tools") ? 1 : 0;
    next;
  }
  in_tools && /^  [a-zA-Z0-9_-]+:$/ {
    if (name != "") emit();
    name = $1; sub(/:$/, "", name);
    binary=""; verify=""; shim=0;
    next;
  }
  in_tools && /^    binary: / { binary=$2; next; }
  in_tools && /^    verify: / { sub(/^    verify: /, ""); verify=$0; next; }
  in_tools && /^    shim: true$/ { shim=1; next; }
  END { if (name != "") emit(); }
  function emit() {
    printf "%s|%s|%s|%d\n", name, binary, verify, shim;
    name="";
  }
' "$YAML" > "$LIST"

fail=0
while IFS='|' read -r name binary verify shim; do
  if [ "$shim" = "1" ]; then
    if [ -x "$PREFIX/node/bin/corepack" ] \
       && "$PREFIX/node/bin/corepack" pnpm --version >/dev/null 2>&1; then
      echo "OK   $name (corepack shim)"
    else
      echo "FAIL $name: corepack pnpm --version 失败" >&2; fail=1
    fi
    continue
  fi
  if [ -n "$binary" ] && [ -x "$PREFIX/$binary" ]; then
    if [ -n "$verify" ]; then
      if PATH="$PREFIX/bin:$PATH" sh -c "$verify" >/dev/null 2>&1; then
        echo "OK   $name ($verify)"
      else
        echo "FAIL $name: 执行 '$verify' 失败" >&2; fail=1
      fi
    else
      echo "OK   $name"
    fi
  else
    echo "FAIL $name: $PREFIX/$binary 缺失或不可执行" >&2; fail=1
  fi
done < "$LIST"

exit $fail

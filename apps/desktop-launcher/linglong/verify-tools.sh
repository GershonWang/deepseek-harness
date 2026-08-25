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

# 解析受约束的 YAML 子集,仅在 tools: 段内识别工具,输出 "name|binary|verify|shim|base"
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
    binary=""; verify=""; shim=0; base=0;
    next;
  }
  in_tools && /^    binary: / { binary=$2; next; }
  in_tools && /^    verify: / { sub(/^    verify: /, ""); verify=$0; next; }
  in_tools && /^    shim: true$/ { shim=1; next; }
  in_tools && /^    base: true$/ { base=1; next; }
  END { if (name != "") emit(); }
  function emit() {
    printf "%s|%s|%s|%d|%d\n", name, binary, verify, shim, base;
    name="";
  }
' "$YAML" > "$LIST"

fail=0
while IFS='|' read -r name binary verify shim base; do
  if [ "$shim" = "1" ]; then
    if [ -x "$PREFIX/node/bin/pnpm" ] \
       && "$PREFIX/node/bin/pnpm" --version >/dev/null 2>&1; then
      echo "OK   $name (bundled pnpm)"
    elif [ -x "$PREFIX/node/bin/corepack" ] \
       && "$PREFIX/node/bin/corepack" pnpm --version >/dev/null 2>&1; then
      echo "OK   $name (corepack shim)"
    else
      echo "FAIL $name: bundled pnpm / corepack 均不可用" >&2; fail=1
    fi
    continue
  fi
  if [ -z "$binary" ]; then
    echo "FAIL $name: 缺少 binary 且非 shim" >&2; fail=1
    continue
  fi
  if [ -x "$PREFIX/$binary" ]; then
    if [ -n "$verify" ]; then
      if PATH="$PREFIX/bin:$PATH" sh -c "$verify" >/dev/null 2>&1; then
        echo "OK   $name ($verify)"
      else
        echo "FAIL $name: 执行 '$verify' 失败" >&2; fail=1
      fi
    else
      echo "OK   $name"
    fi
  elif [ "$base" = "1" ]; then
    # 基础运行时提供(org.deepin.base /usr/bin),不进 $PREFIX;运行时以
    # ll-builder run --exec 逐项确认,此处只记录来源含义。
    echo "OK   $name (base-provided)"
  else
    echo "FAIL $name: $PREFIX/$binary 缺失或不可执行" >&2; fail=1
  fi
done < "$LIST"

# installable 段校验：sha256 必须填实（不含占位符 "<"）。运行时实际生效的
# 清单在 launcher 的 internal/toolchain/catalog.go，本段与其同步；占位即视为
# 白名单未就绪并中止导出，防止"界面可安装、实际必失败"的假承诺。
INST=$(mktemp)
awk '
  /^[a-zA-Z0-9_-]+:$/ {
    sec = $1; sub(/:$/, "", sec);
    in_inst = (sec == "installable") ? 1 : 0;
    next;
  }
  in_inst && /^  [a-zA-Z0-9_-]+:$/ {
    name = $1; sub(/:$/, "", name);
    next;
  }
  in_inst && /^    sha256: / {
    sub(/^    sha256: /, "");
    print name "|" $0
  }
' "$YAML" > "$INST"
while IFS='|' read -r name sha; do
  case "$sha" in
    *'<'*|'""'|'') echo "FAIL installable/$name: sha256 未填实（占位或为空）" >&2; fail=1 ;;
    *) echo "OK   installable/$name (sha256 已填实)" ;;
  esac
done < "$INST"
rm -f "$INST"

# git 功能探测（宿主侧静态）：launcher 启动时为整个 harness 进程树注入
# GIT_EXEC_PATH=<prefix>/lib/git-core（packagedGitExecPath 按可执行文件位置
# 推导，任意机器一致），因此 lib/git-core 里的远程 helper 必须随包存在；
# helper 缺失即 git push/fetch 全部不可用。缺失即视为失败，避免
# "git --version 通过但 git push 必挂"的虚假自检。
if [ -x "$PREFIX/bin/git" ] && [ ! -f "$PREFIX/lib/git-core/git-remote-https" ]; then
  echo "FAIL git: lib/git-core/git-remote-https 缺失（GIT_EXEC_PATH 指向它），容器内远程操作将不可用" >&2
  fail=1
fi

exit $fail

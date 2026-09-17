#!/bin/sh
# 校验 buildext.apt.depends 声明的依赖是否真的进了玲珑合并产物树。
#
# 为什么需要它：ll-builder 在 build: 阶段看不到 depends——生成命令只装 build_depends，
# depends 是在 build 段之后（preCommit 的合并阶段）才安装并合进 $PREFIX（同一事实见
# verify-tools.sh 的头部注释与 verify-container-deps.sh 的说明，后者因此只校验
# build_depends）。于是「依赖是否真的进了包」只有在这里、在合并产物树上才能回答。
#
# 与 verify-tools.sh 的分工：那个脚本按 linglong/tools.yaml 的 tools 段校验"工具能用"
# （存在、可执行、能打印版本）；本脚本按 linglong.yaml 的 buildext.apt 段校验"依赖有着落"，
# 并补上它覆盖不到的两类——只在 depends 里出现的包（库、字体、证书），以及产物里出现
# 字符设备这种结构性损坏（字符设备带可执行位时 `-x` 为真，会骗过 verify-tools.sh）。
#
# 校验项（任一不满足即非零退出）：
#   1. 产物树里不得出现字符设备，也不得残留 *.dpkg-new——AUDIT N19「写入未落盘」在产物
#      侧的判据；
#   2. buildext.apt 里声明的每个包都必须在下面的规则表里被认领，认领方式五选一：
#        tool:<name>     该工具由 linglong/tools.yaml 声明，断言 $PREFIX/<binary> 存在
#        path:<rel>      本脚本直接断言产物内该实体存在且是普通文件
#        patched:<rel>   同上，且实体里必须能找到 exec-path 补丁的短路径（说明交付的是
#                        build 段打过补丁的那一份，而不是基础层或容器里的原样旧库）
#        base:<理由>     由基础运行时 org.deepin.base 提供，按设计不进 $PREFIX
#        none:<理由>     声明了但当前不交付任何实体到产物（附实测理由，见 AUDIT）
#      未认领即失败：新增依赖不允许静默地无人校验。
#
# 用法: verify-merged-deps.sh <merged-prefix>   e.g. linglong/output/binary/files
set -eu

PREFIX=${1:?usage: verify-merged-deps.sh <merged-prefix>}
YAML=$(dirname "$0")/linglong.yaml
TOOLS_YAML=$(dirname "$0")/tools.yaml
# exec-path 补丁的短路径与 launcher、patch 脚本共用同一份单一来源文件；两侧各写一遍
# 就会在改一处时静默失配（patch-webkit-exec-path.sh 的头部注释记录了同样的理由）。
WEBKIT_SHORT_PATH_FILE=$(dirname "$0")/../internal/packaging/webkit-exec-path.txt
[ -f "$WEBKIT_SHORT_PATH_FILE" ] || { echo "verify-merged-deps: 缺少短路径单一来源文件 $WEBKIT_SHORT_PATH_FILE" >&2; exit 1; }
WEBKIT_SHORT_PATH=$(tr -d '[:space:]' < "$WEBKIT_SHORT_PATH_FILE")
[ -n "$WEBKIT_SHORT_PATH" ] || { echo "verify-merged-deps: $WEBKIT_SHORT_PATH_FILE 为空" >&2; exit 1; }
DEPS=$(mktemp)
RULES=$(mktemp)
trap 'rm -f "$DEPS" "$RULES"' EXIT

# 规则表：包名|认领方式。每一行的理由都来自对真实产物层的实测（见 AUDIT N19/N26），
# 不是按包名猜的：基础镜像已有的包不会进 $PREFIX（buildext 只合并"新装差异"）。
cat > "$RULES" <<'EOF'
libwebkit2gtk-4.1-0|patched:lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0
libjavascriptcoregtk-4.1-0|path:lib/x86_64-linux-gnu/libjavascriptcoregtk-4.1.so.0
libgtk-3-0|base:基础层已提供 libgtk-3（实测三版产物层均无该 .so）
libglib2.0-0|base:基础层已提供 libglib-2.0（实测同上）
ca-certificates|base:基础层已提供 /etc/ssl/certs/ca-certificates.crt（实测）
python3|base:基础层已提供（tools.yaml 标 base: true）
curl|base:基础层已提供（tools.yaml 标 base: true）
unzip|base:基础层已提供（tools.yaml 标 base: true）
fonts-wqy-microhei|none:实测三版产物层均无 wqy 字体，运行时 /usr/share/fonts 由宿主挂载覆盖（AUDIT N26）
git|tool:git
git-lfs|tool:git-lfs
wget|tool:wget
zip|path:bin/zip
jq|tool:jq
xxd|tool:xxd
xdg-utils|tool:xdg-open
wl-clipboard|tool:wl-paste
EOF

# 依赖清单：buildext.apt 的 build_depends 与 depends 两段一并读（去重、剥注释）。
awk '
  /^[a-zA-Z0-9_-]+:/ { in_bx = ($1 == "buildext:") ? 1 : 0; in_apt = 0; next }
  in_bx && /^  apt:/ { in_apt = 1; next }
  in_bx && in_apt && /^      - / {
    sub(/^      - /, ""); sub(/[[:space:]]*#.*$/, "");
    if ($0 != "" && !seen[$0]++) print
  }
' "$YAML" > "$DEPS"

if [ ! -s "$DEPS" ]; then
  echo "verify-merged-deps: 未能从 $YAML 解析出 buildext.apt 依赖（段结构变了？）" >&2
  exit 1
fi

# tool:<name> 的实体路径取自 tools.yaml（单一事实来源）。解析规则沿用该文件头部声明的
# 受限格式（tools: 段、2 空格工具名、4 空格 binary:），与本脚本的规则表一样不做 YAML 全解析。
tool_binary() {
  awk -v want="$1" '
    /^[a-zA-Z0-9_-]+:/ { in_tools = ($1 == "tools:") ? 1 : 0; name = ""; next }
    in_tools && /^  [a-zA-Z0-9_-]+:$/ { name = $1; sub(/:$/, "", name); next }
    in_tools && /^    binary: / { if (name == want) { print $2; exit } }
  ' "$TOOLS_YAML"
}

fail=0

# --- 1. 结构性损坏：字符设备与 dpkg 中间态残留 ---
chardev=$(find "$PREFIX" -type c 2>/dev/null | head -5 || true)
if [ -n "$chardev" ]; then
  echo "FAIL 产物树里存在字符设备（写入未落盘被复制进包）：" >&2
  printf '%s\n' "$chardev" | sed 's/^/       /' >&2
  fail=1
fi
residue=$(find "$PREFIX" -name '*.dpkg-new' 2>/dev/null | head -5 || true)
if [ -n "$residue" ]; then
  echo "FAIL 产物树里残留 dpkg 中间态文件：" >&2
  printf '%s\n' "$residue" | sed 's/^/       /' >&2
  fail=1
fi

# --- 2. 逐个 depends 包套用认领规则 ---
while IFS= read -r pkg; do
  rule=$(awk -F'|' -v p="$pkg" '$1 == p { print $2; exit }' "$RULES")
  if [ -z "$rule" ]; then
    echo "FAIL $pkg: 未在 verify-merged-deps.sh 的规则表里认领——新增依赖需要一条可核对的落点断言" >&2
    fail=1
    continue
  fi
  kind=${rule%%:*}
  arg=${rule#*:}
  case "$kind" in
    tool)
      binary=$(tool_binary "$arg")
      if [ -z "$binary" ]; then
        echo "FAIL $pkg: tools.yaml 里没有工具 $arg（规则表与清单漂移）" >&2
        fail=1
      elif [ -f "$PREFIX/$binary" ]; then
        echo "OK   $pkg (tools.yaml: $arg → $binary)"
      else
        echo "FAIL $pkg: 产物树缺少 $binary（tools.yaml 声明的工具未随包交付）" >&2
        fail=1
      fi
      ;;
    path)
      if [ -f "$PREFIX/$arg" ]; then
        echo "OK   $pkg ($arg)"
      else
        echo "FAIL $pkg: 产物树缺少 $arg" >&2
        fail=1
      fi
      ;;
    patched)
      if [ ! -f "$PREFIX/$arg" ]; then
        echo "FAIL $pkg: 产物树缺少 $arg" >&2
        fail=1
      elif ! grep -qaF -- "$WEBKIT_SHORT_PATH" "$PREFIX/$arg"; then
        # 交付的必须是 build 段从 apt 候选 .deb 解出、打过 exec-path 补丁的那一份。
        # 缺这个标记说明进包的还是基础层/容器里的原样旧库——补丁失效意味着 helper
        # 进程路径指向容器内不存在的 /usr/lib/...，产物会「装得上但 GUI 起不来」。
        echo "FAIL $pkg: $arg 里找不到 exec-path 补丁标记 $WEBKIT_SHORT_PATH（进包的不是打过补丁的那一份）" >&2
        fail=1
      else
        echo "OK   $pkg ($arg，已打 exec-path 补丁)"
      fi
      ;;
    base)
      echo "OK   $pkg (基础层提供：$arg)"
      ;;
    none)
      echo "OK   $pkg (按设计不进产物：$arg)"
      ;;
    *)
      echo "FAIL $pkg: 规则表里的认领方式「$kind」无法识别" >&2
      fail=1
      ;;
  esac
done < "$DEPS"

if [ "$fail" -ne 0 ]; then
  echo "verify-merged-deps: buildext.apt 依赖未按声明交付到产物树。" >&2
  exit 1
fi

echo "verify-merged-deps: buildext.apt 依赖在产物树中均有落点"
exit 0

#!/bin/sh
# 校验 buildext.apt.depends 声明的依赖是否真的进了玲珑合并产物树。
#
# 为什么需要它：ll-builder 在 build: 阶段看不到 depends——生成命令只装 build_depends，
# depends 是在 build 段之后（preCommit 的合并阶段）才安装并合进 $PREFIX。于是「依赖
# 是否真的进了包」只有在这里、在合并产物树上才能回答。构建日志那边的
# verify-builder-log.sh 只看拷贝告警，看不到合并阶段的落点。
#
# 与薄壳版的差异：薄壳版基于 linglong/tools.yaml 的 tools 段做 tool:<name> 认领，
# 那是它的工具链市场清单；Electron 版没有那份清单，因此这里一律用 path: 直接断言
# 产物内实体存在，理由写在规则表每一行的注释里。
#
# 校验项（任一不满足即非零退出）：
#   1. 产物树里不得出现字符设备，也不得残留 *.dpkg-new——「写入未落盘」在产物侧的判据；
#   2. buildext.apt 里声明的每个包都必须在下面的规则表里被认领，认领方式三选一：
#        path:<rel>      断言产物内该实体存在且是普通文件
#        present:<rel>   断言产物内该路径存在（文件、目录或符号链接均可）
#        base:<理由>     由基础运行时 org.deepin.base 提供，按设计不进 $PREFIX
#      未认领即失败：新增依赖不允许静默地无人校验。
#   3. 库搜索路径上不得有断链符号链接——动态链接器会静默跳过整块依赖它的库。
#
# 用法: verify-merged-deps.sh <merged-prefix>   e.g. linglong/output/binary/files
set -eu

PREFIX=${1:?usage: verify-merged-deps.sh <merged-prefix>}
YAML=$(dirname "$0")/linglong.yaml
DEPS=$(mktemp)
RULES=$(mktemp)
trap 'rm -f "$DEPS" "$RULES"' EXIT

# 规则表：包名|认领方式。每一行都对应产物里的一个真实落点，不是按包名猜的：
# 基础镜像已有的包不会进 $PREFIX（buildext 只合并"新装差异"），这类只能标 base。
cat > "$RULES" <<'EOF'
# --- Chromium 运行库：基础层已提供，合并不产生新文件 ---
libgtk-3-0|base:基础层已提供 libgtk-3.so.0
libnss3|base:基础层已提供 libnss3.so
libgbm1|base:基础层已提供 libgbm.so.1
libasound2|base:基础层已提供 libasound.so.2
libatk-bridge2.0-0|base:基础层已提供 libatk-bridge-2.0.so.0
libatspi2.0-0|base:基础层已提供 libatspi.so.0
libcups2|base:基础层已提供 libcups.so.2
libxkbcommon0|base:基础层已提供 libxkbcommon.so.0
libxcomposite1|base:基础层已提供 libXcomposite.so.1
libxdamage1|base:基础层已提供 libXdamage.so.1
libxrandr2|base:基础层已提供 libXrandr.so.2
libdrm2|base:基础层已提供 libdrm.so.2
libpango-1.0-0|base:基础层已提供 libpango-1.0.so.0
libcairo2|base:基础层已提供 libcairo.so.2
libxshmfence1|base:基础层已提供 libxshmfence.so.1
# --- 通用工具链：合并在 build 段之后写入 $PREFIX/bin ---
git|path:bin/git
git-lfs|path:bin/git-lfs
wget|path:bin/wget
jq|path:bin/jq
xxd|path:bin/xxd
zip|path:bin/zip
xdg-utils|path:bin/xdg-open
wl-clipboard|path:bin/wl-paste
# 基础层已带 python3 / curl / unzip（薄壳版的 tools.yaml 同样标 base: true）。
python3|base:基础层已提供（薄壳版 tools.yaml 标 base: true）
curl|base:基础层已提供（薄壳版 tools.yaml 标 base: true）
unzip|base:基础层已提供（薄壳版 tools.yaml 标 base: true）
# 证书落在 /etc 而非 $PREFIX；git/https/python 的校验链由基础层提供。
ca-certificates|base:基础层已提供 /etc/ssl/certs/ca-certificates.crt
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

# --- 2. 库搜索路径上的断链符号链接 ---
# depends 合并会原样保留符号链接，而 Debian 的 alternatives 顶层链接指向
# /etc/alternatives 这个绝对路径，该目录由基础层提供、不进 $PREFIX，链接即断。
# 后果不是链接报错，而是依赖它的动态库加载失败、使用方被整块跳过。
# 检查范围有意限定在 lib/x86_64-linux-gnu：只有这里的断链会被动态链接器遇到，
# 全树扫描必然误报「按设计就断」的链接（aspell 词典指向宿主挂载点等）。
dangling=$(find "$PREFIX/lib/x86_64-linux-gnu" -maxdepth 1 -type l ! -exec test -e {} \; -print 2>/dev/null | head -5 || true)
if [ -n "$dangling" ]; then
  echo "FAIL 库搜索路径上存在断链符号链接（dynamic linker 会静默跳过其依赖方）：" >&2
  printf '%s\n' "$dangling" | sed 's/^/       /' >&2
  fail=1
fi

# --- 2b. git 的 init 模板必须进了产物树 ---
# build 段从 .deb 解出并复制；模板缺失不会让任何命令失败，只会让每次 init 打一条
# 告警、并给新仓库少一套示例钩子——正是那种「装得上但静默降级」的缺陷。
if [ -d "$PREFIX/share/git-core" ]; then
  if [ ! -f "$PREFIX/share/git-core/templates/description" ]; then
    echo "FAIL git templates: $PREFIX/share/git-core/templates 缺少 description" >&2
    fail=1
  else
    echo "OK   git templates (share/git-core/templates)"
  fi
else
  echo "WARN git templates: 产物内没有 share/git-core（非玲珑打包场景？）"
fi

# --- 3. 逐个 depends 包套用认领规则 ---
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
    path)
      if [ -f "$PREFIX/$arg" ]; then
        echo "OK   $pkg ($arg)"
      else
        echo "FAIL $pkg: 产物树缺少 $arg" >&2
        fail=1
      fi
      ;;
    present)
      if [ -e "$PREFIX/$arg" ] || [ -L "$PREFIX/$arg" ]; then
        echo "OK   $pkg ($arg)"
      else
        echo "FAIL $pkg: 产物树缺少 $arg" >&2
        fail=1
      fi
      ;;
    base)
      echo "OK   $pkg (基础层提供：$arg)"
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

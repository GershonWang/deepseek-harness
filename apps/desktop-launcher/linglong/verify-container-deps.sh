#!/bin/sh
# 在 build: 容器阶段硬校验 buildext.apt.depends 声明的依赖是否真的装进了容器。
#
# 为什么需要这个脚本：ll-builder 由 buildext 声明生成的 apt 命令行以 `|| echo "$?"`
# 结尾，apt 失败只打印一个数字、不中止构建；而实测（AUDIT N19）还发现即便 apt 报告
# Unpacking/Setting up 成功，写入也可能静默不落盘——overlay upperdir 里只剩字符设备
# `*.dpkg-new`，实体停在基础层旧版本。两种情况都会让「依赖升级」被静默丢弃，产出
# 装得上但行为陈旧的包。因此在组装之前硬失败，把静默问题变成构建故障。
#
# 校验项（任一不满足即非零退出）：
#   1. 每个包 dpkg 状态为 install ok installed；
#   2. 已装版本与 apt 候选版本一致（否则说明本次安装没有生效）；
#   3. `dpkg --verify` 无输出（实体内容与包记录的 md5sums 一致）；
#   4. 包内每个存在的条目都是普通文件（overlay 写失败留下的是字符设备）；
#   5. /usr 下不残留 `*.dpkg-new` 中间态文件。
#
# 用法: verify-container-deps.sh [sysroot]
#   sysroot 默认 /；指向解包后的根时，同一套检查可在宿主侧复跑。
#   依赖 dpkg-query / dpkg / apt-cache —— 构建容器是 Debian 基座，三者都在。
set -eu

SYSROOT=${1:-/}
YAML=$(dirname "$0")/linglong.yaml
DEPS=$(mktemp)
trap 'rm -f "$DEPS"' EXIT

# 依赖清单取自 linglong.yaml 的 buildext.apt 段（build_depends 与 depends 一并校验），
# 保持单一事实来源：加包只改 yaml，这里自动跟着走。
awk '
  /^[a-zA-Z0-9_-]+:/ { in_bx = ($1 == "buildext:") ? 1 : 0; in_apt = 0; next }
  in_bx && /^  apt:/ { in_apt = 1; next }
  in_bx && in_apt && /^  [a-zA-Z]/ { in_apt = 0; next }
  in_bx && in_apt && /^      - / {
    sub(/^      - /, ""); sub(/[[:space:]]*#.*$/, "");
    if ($0 != "" && !seen[$0]++) print
  }
' "$YAML" > "$DEPS"

if [ ! -s "$DEPS" ]; then
  echo "verify-container-deps: 未能从 $YAML 解析出 buildext.apt 依赖（段结构变了？）" >&2
  exit 1
fi

for tool in dpkg-query dpkg apt-cache; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "verify-container-deps: 缺少 $tool，无法校验依赖（构建基座应为 Debian）" >&2
    exit 1
  fi
done

fail=0
while IFS= read -r pkg; do
  info=$(dpkg-query -W -f='${Status}|${Version}' "$pkg" 2>/dev/null || true)
  status=${info%%|*}
  installed=${info##*|}
  if [ "$status" != "install ok installed" ]; then
    echo "FAIL $pkg: dpkg 状态为「${status:-无记录}」，该依赖没有装上（buildext 的 '|| echo \$?' 会吞掉 apt 失败）" >&2
    fail=1
    continue
  fi

  candidate=$(apt-cache policy "$pkg" 2>/dev/null | awk '/Candidate:/ { print $2; exit }' || true)
  if [ -n "$candidate" ] && [ "$candidate" != "(none)" ] && [ "$candidate" != "$installed" ]; then
    echo "FAIL $pkg: 已装 $installed，apt 候选为 $candidate —— 本次安装未生效。若这是有意钉住的版本，请调整 buildext.apt 清单，而不是绕过本校验。" >&2
    fail=1
  fi

  verify_out=$(dpkg --verify "$pkg" 2>&1 || true)
  if [ -n "$verify_out" ]; then
    echo "FAIL $pkg: dpkg --verify 报告实体与包记录不一致（写入未落盘）：" >&2
    printf '%s\n' "$verify_out" | sed 's/^/       /' >&2
    fail=1
  fi

  files=$(mktemp)
  dpkg-query -L "$pkg" 2>/dev/null > "$files" || true
  while IFS= read -r f; do
    [ -e "$SYSROOT$f" ] || continue
    if [ -c "$SYSROOT$f" ]; then
      echo "FAIL $pkg: $f 是字符设备而非普通文件（overlay 写入未落盘）" >&2
      fail=1
    fi
  done < "$files"
  rm -f "$files"
done < "$DEPS"

# 中间态残留与具体哪个包无关，统一扫一次：失败现场实测留下 5,830 个字符设备
# `*.dpkg-new`，是「写了但没落盘」最直接的证据。
residue=$(find "$SYSROOT/usr" -name '*.dpkg-new' 2>/dev/null | head -5 || true)
if [ -n "$residue" ]; then
  echo "FAIL /usr 下残留 dpkg 中间态文件（写入未落盘）：" >&2
  printf '%s\n' "$residue" | sed 's/^/       /' >&2
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo "verify-container-deps: 依赖未真正就位，已中止构建；本状态下导出的包会静默沿用基础层旧版本。" >&2
  exit 1
fi

echo "verify-container-deps: buildext.apt 依赖校验通过"
exit 0

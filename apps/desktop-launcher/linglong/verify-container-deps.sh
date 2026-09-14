#!/bin/sh
# 在 build: 容器阶段硬校验 buildext.apt.build_depends 是否真的装进了容器。
#
# 为什么只校验 build_depends：ll-builder 生成的命令只装 build_depends（实测构建容器内
# `linglong/buildext.sh` 全文只有 `apt update` 与 `apt -y install libwebkit2gtk-4.1-0`），
# 而 `depends` 是在 build 段**之后**（preCommit 的合并阶段）才安装并合进 $PREFIX——同样的
# 机制 verify-tools.sh 的头部注释早已写明。因此在本阶段要求 depends 已安装是一道永远过不
# 去的闸门：2026-09-14 首次真实构建即被它拦下，8 个 depends 包全部报「没有装上」。depends
# 的落点由宿主侧的 verify-merged-deps.sh 在合并产物树上校验。
#
# 为什么需要这个脚本：ll-builder 由 buildext 声明生成的 apt 命令行以 `|| echo "$?"`
# 结尾，apt 失败只打印一个数字、不中止构建；而实测（AUDIT N19）还发现即便 apt 报告
# Unpacking/Setting up 成功，写入也可能静默不落盘——overlay upperdir 里只剩字符设备
# `*.dpkg-new`，实体停在基础层旧版本。两种情况都会让「依赖升级」被静默丢弃，产出
# 装得上但行为陈旧的包。因此在组装之前硬失败，把静默问题变成构建故障。
#
# 校验项（任一不满足即非零退出）：
#   1. 每个 build_depends 包 dpkg 状态为 install ok installed；
#   2. 已装版本与 apt 候选版本一致（否则说明本次安装没有生效）；
#   3. `dpkg --verify` 无输出（实体内容与包记录的 md5sums 一致）——基础镜像裁剪的文档与
#      手册路径除外，理由见比对处的注释；
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

# 依赖清单取自 linglong.yaml 的 buildext.apt.build_depends 段，保持单一事实来源：
# 加包只改 yaml，这里自动跟着走。depends 段刻意不读（原因见文件头）。
awk '
  /^[a-zA-Z0-9_-]+:/ { in_bx = ($1 == "buildext:") ? 1 : 0; in_apt = 0; in_bd = 0; next }
  in_bx && /^  apt:/ { in_apt = 1; next }
  in_bx && in_apt && /^    build_depends:/ { in_bd = 1; next }
  in_bx && in_apt && /^    [a-zA-Z]/ { in_bd = 0; next }
  in_bx && in_apt && in_bd && /^      - / {
    sub(/^      - /, ""); sub(/[[:space:]]*#.*$/, "");
    if ($0 != "" && !seen[$0]++) print
  }
' "$YAML" > "$DEPS"

if [ ! -s "$DEPS" ]; then
  echo "verify-container-deps: 未能从 $YAML 解析出 buildext.apt.build_depends（段结构变了？）" >&2
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
    echo "FAIL $pkg: dpkg 状态为「${status:-无记录}」，该 build_depends 没有装上（基座变更或 apt 失败被吞）" >&2
    fail=1
    continue
  fi

  candidate=$(apt-cache policy "$pkg" 2>/dev/null | awk '/Candidate:/ { print $2; exit }' || true)
  if [ -n "$candidate" ] && [ "$candidate" != "(none)" ] && [ "$candidate" != "$installed" ]; then
    echo "FAIL $pkg: 已装 $installed，apt 候选为 $candidate —— 本次安装未生效。若这是有意钉住的版本，请调整 buildext.apt 清单，而不是绕过本校验。" >&2
    fail=1
  fi

  verify_out=$(dpkg --verify "$pkg" 2>&1 || true)
  # 基础镜像制作时裁剪了文档与手册：基座层 org.deepin.base 的 .list 保留 /usr/share/doc
  # 与 /usr/share/man 条目，实体却不存在（实测：基座层 614 个包、/usr/share/doc 实体 0 条，
  # libgtk-3-0:amd64.list 列了 6 条 doc 路径且全部缺失）。继承自基座的包因此永远报
  # 「missing」——那是基础镜像的既定状态，不是本次安装失败，且文档缺失不影响功能。真正要
  # 防的实体缺失（库、可执行）不落在这些路径上；实体被写成字符设备的另一种失败由下面的
  # -c 检查独立覆盖。
  verify_out=$(printf '%s\n' "$verify_out" | grep -vE '^missing[[:space:]]+/usr/share/(doc|man)/' || true)
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
  echo "verify-container-deps: build_depends 未真正就位，已中止构建；本状态下导出的包会静默沿用基础层旧版本。" >&2
  exit 1
fi

echo "verify-container-deps: buildext.apt.build_depends 校验通过"
exit 0

#!/bin/sh
# 校验 ll-builder 的构建日志里没有「静默丢文件」的告警。
#
# 为什么需要：构建器把复制失败降级为一行 `failed to copy …: 无效的参数` 警告，之后
# 照常 [Install Files]/[Commit Contents] 并导出产物；包内那份文件停留在基础层的旧
# 版本，而两个环节都不报错（AUDIT N17）。能出包不等于出的是这次构建的东西，所以
# 在导出前把该告警升级为硬失败。
#
# 唯一的例外是 webkit：build 段自己从 apt 候选的 .deb 解出实体、打补丁并写进
# $PREFIX（AUDIT N19），构建器再想把基础层那份旧库收进 _build 与本产物无关，它的
# 失败反而避免了旧库覆盖补丁版。是否真的交付了 webkit 改由产物侧断言负责
# （verify-merged-deps.sh：存在、是普通文件、带补丁标记），不再依赖日志。
#
# 用法: verify-builder-log.sh <ll-builder 的构建日志>
set -eu

LOG=${1:?usage: verify-builder-log.sh <build-log>}
[ -f "$LOG" ] || { echo "verify-builder-log: 找不到构建日志 $LOG" >&2; exit 1; }

# 已知冗余项：只放行「复制 webkit 共享库」这一种，其余 failed to copy 一律失败。
EXEMPT='failed to copy .*/libwebkit2gtk-4\.1\.so\.0([[:space:]]|$)'

total=$(grep -c 'failed to copy' "$LOG" || true)
exempt=$(grep -E "$EXEMPT" "$LOG" | grep -c . || true)
remaining=$(grep 'failed to copy' "$LOG" | grep -vE "$EXEMPT" | grep -c . || true)

if [ "$remaining" -ne 0 ]; then
  echo "FAIL 构建器报 $remaining 处未豁免的 'failed to copy'（共 $total 处，其中 $exempt 处是已知冗余的 webkit）：这些文件不会进入产物，包会静默沿用基础层旧版本。" >&2
  grep -n 'failed to copy' "$LOG" | grep -vE "$EXEMPT" | head -5 | sed 's/^/     /' >&2
  echo "     完整日志: $LOG" >&2
  exit 1
fi

echo "verify-builder-log: 构建日志无未豁免的 'failed to copy'（共 $total 处，豁免 webkit $exempt 处）"

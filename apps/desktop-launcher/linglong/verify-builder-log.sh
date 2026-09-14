#!/bin/sh
# 校验 ll-builder 的构建日志里没有「静默丢文件」的告警。
#
# 为什么需要：构建器把复制失败降级为一行 `failed to copy …: 无效的参数` 警告，之后
# 照常 [Install Files]/[Commit Contents] 并导出产物；包内那份文件停留在基础层的旧
# 版本，而两个环节都不报错（AUDIT N17）。能出包不等于出的是这次构建的东西，所以
# 在导出前把该告警升级为硬失败。
#
# 用法: verify-builder-log.sh <ll-builder 的构建日志>
set -eu

LOG=${1:?usage: verify-builder-log.sh <build-log>}
[ -f "$LOG" ] || { echo "verify-builder-log: 找不到构建日志 $LOG" >&2; exit 1; }

if grep -q 'failed to copy' "$LOG"; then
  count=$(grep -c 'failed to copy' "$LOG")
  echo "FAIL 构建器报 'failed to copy'（$count 处）：这些文件不会进入产物，包会静默沿用基础层旧版本。" >&2
  grep -n 'failed to copy' "$LOG" | head -5 | sed 's/^/     /' >&2
  echo "     完整日志: $LOG" >&2
  exit 1
fi

echo "verify-builder-log: 构建日志无 'failed to copy'（$LOG）"

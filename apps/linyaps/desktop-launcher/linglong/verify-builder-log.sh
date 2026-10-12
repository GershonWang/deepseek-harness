#!/bin/sh
# 校验 ll-builder 的构建日志里没有「静默丢文件」的告警。
#
# 为什么需要：构建器把复制失败降级为一行 `failed to copy …: 无效的参数` 警告，之后
# 照常 [Install Files]/[Commit Contents] 并导出产物；包内那份文件停留在基础层的旧
# 版本，而两个环节都不报错（AUDIT N17）。能出包不等于出的是这次构建的东西，所以
# 在导出前把该告警升级为硬失败。
#
# 例外是「build 段自己从 apt 候选 .deb 交付」的那几个文件：构建器再想把容器 /usr 上层
# 那份收进 _build 与本产物无关，收失败反而避免了旧副本覆盖 build 段交付的那一份
# （AUDIT N19）。当前两类：
#   1. webkit 共享库——build 段解出实体、打 exec-path 补丁并写进 $PREFIX；
#   2. BLAS/LAPACK——build 段（place-alternatives-libs.sh）交付实体与包内相对链接，
#      合并若成功反而会把 /etc/alternatives 的断链写回产物。
# 是否真的交付了它们由产物侧断言负责，不再依赖日志：verify-merged-deps.sh 检查实体存在、
# webkit 带补丁标记，并扫 lib/x86_64-linux-gnu 一层里的断链。
#
# 用法: verify-builder-log.sh <ll-builder 的构建日志>
set -eu

LOG=${1:?usage: verify-builder-log.sh <build-log>}
[ -f "$LOG" ] || { echo "verify-builder-log: 找不到构建日志 $LOG" >&2; exit 1; }

# 已知冗余项：只放行上面两类，其余 failed to copy 一律失败。写成前缀匹配而不是钉死完整
# 路径：alternatives 的实体在 blas/、lapack/ 子目录下，链接在上一层，两种形态都要放行。
EXEMPT='failed to copy .*/libwebkit2gtk-4\.1\.so\.0([[:space:]]|$)'
EXEMPT="$EXEMPT"'|failed to copy .*/libblas\.so\.3([[:space:]]|$)'
EXEMPT="$EXEMPT"'|failed to copy .*/liblapack\.so\.3([[:space:]]|$)'

total=$(grep -c 'failed to copy' "$LOG" || true)
exempt=$(grep -E "$EXEMPT" "$LOG" | grep -c . || true)
remaining=$(grep 'failed to copy' "$LOG" | grep -vE "$EXEMPT" | grep -c . || true)

if [ "$remaining" -ne 0 ]; then
  echo "FAIL 构建器报 $remaining 处未豁免的 'failed to copy'（共 $total 处，其中 $exempt 处是 build 段自行交付的 webkit/BLAS/LAPACK）：这些文件不会进入产物，包会静默沿用基础层旧版本。" >&2
  grep -n 'failed to copy' "$LOG" | grep -vE "$EXEMPT" | head -5 | sed 's/^/     /' >&2
  echo "     完整日志: $LOG" >&2
  exit 1
fi

echo "verify-builder-log: 构建日志无未豁免的 'failed to copy'（共 $total 处，豁免 $exempt 处）"

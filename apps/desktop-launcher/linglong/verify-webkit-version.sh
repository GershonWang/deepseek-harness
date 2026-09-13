#!/bin/bash
# 打包链 webkit 落地自检：确认「构建容器里 apt 装的 webkit 版本」与「最终包里的版本」一致。
#
# 为什么需要它：两轮独立打包（见 AUDIT N17/N18/N19）出现同一结果——构建容器 apt 装的是
# webkit2gtk 2.50.4，最终包内仍是 4 月的 2.48.5，全程无一处报错。原因是构建器的合并拷贝
# 失败只警告（N17），并回落到构建器缓存里的陈旧产物（N19）。本脚本不依赖上游修复，
# 只回答一个问题：这次打包到底把哪个版本装进了包。
#
# 用法：sh apps/desktop-launcher/linglong/verify-webkit-version.sh [日志路径]
#   日志缺省时取 ~/Desktop/日志.txt，再缺省取仓库根最新的 *.log。
# 退出码：0 = 未发现「陈旧库被沿用」的迹象；1 = 发现迹象（应视为失败）。
set -u

REPO=$(cd "$(dirname "$0")/../../.." && pwd)
PREFIX=/opt/apps/com.deepseek.dsh-desktop/files
LIBDIR="$PREFIX/lib/x86_64-linux-gnu"
CACHE="$HOME/.cache/linglong-builder"
WANT=libwebkit2gtk-4.1.so.0

LOG=${1:-}
if [ -z "$LOG" ]; then
  for c in "$HOME/Desktop/日志.txt" "$REPO"/*.log; do
    [ -f "$c" ] && { LOG=$c; break; }
  done
fi

fail=0
say() { printf '   %-30s %s\n' "$1" "$2"; }

echo "== 1. 构建容器里 apt 装的 webkit 版本 =="
if [ -n "$LOG" ] && [ -f "$LOG" ]; then
  echo "   日志: $LOG"
  grep -a -oE "libwebkit2gtk-4\.1-0[^)]*2\.[0-9]+\.[0-9]+[^ ]*" "$LOG" | sort -u | sed 's/^/   /'
  installed=$(grep -a -oE "Setting up libwebkit2gtk-4\.1-0:amd64 \([0-9.]+" "$LOG" | grep -oE "[0-9]+\.[0-9]+\.[0-9]+" | tail -1)
  say "容器内 Setting up 版本:" "${installed:-未找到}"
else
  say "日志:" "未指定且未找到默认路径"
fi

echo
echo "== 2. 最终包里的 webkit 实体 =="
if [ -d "$LIBDIR" ]; then
  for f in "$LIBDIR"/$WANT.*; do
    [ -e "$f" ] || continue
    printf '   %s  size=%s  sha256=%s\n' "$(basename "$f")" \
      "$(stat -c %s "$f")" "$(sha256sum "$f" | cut -c1-24)"
  done
else
  say "$LIBDIR" "不存在（未安装打包产物？）"
fi

echo
echo "== 3. 构建器缓存里是否有同内容的陈旧库（N19 的源头）=="
if [ -d "$CACHE" ]; then
  raw=$(mktemp); uniq=$(mktemp)
  find "$CACHE" -name "$WANT.*" -type f 2>/dev/null | while IFS= read -r f; do
    printf '%s %s\n' "$(sha256sum "$f" | cut -c1-24)" "$f"
  done > "$raw"
  echo "   缓存内 webkit 实体总数: $(wc -l < "$raw" | tr -d ' ')"
  cut -d' ' -f1 "$raw" | sort -u > "$uniq"
  for h in $(cat "$uniq"); do
    n=$(grep -c "^$h" "$raw" | tr -d ' ')
    first=$(grep -m1 "^$h" "$raw" | cut -d' ' -f2-)
    rel=${first#"$CACHE"/}
    echo "   · $h  ×$n  例如 $rel"
    for f in "$LIBDIR"/$WANT.*; do
      [ -e "$f" ] || continue
      if [ "$(sha256sum "$f" | cut -c1-24)" = "$h" ]; then
        say "⚠ 包内实体与缓存同内容:" "$(basename "$f")"
        fail=1
      fi
    done
  done
  rm -f "$raw" "$uniq"
else
  say "$CACHE" "不存在（缓存已清，符合预期）"
fi

echo
echo "== 4. 日志里的旧签名（两轮打包均命中，出现即说明升级未生效）=="
if [ -n "$LOG" ] && [ -f "$LOG" ]; then
  n=$(grep -a -c "failed to copy" "$LOG" 2>/dev/null | tr -d ' \n')
  say "failed to copy 次数:" "${n:-0}"
  [ "${n:-0}" != "0" ] && fail=1
  patchline=$(grep -a "patch-webkit: replaced" "$LOG" | head -1)
  say "patch-webkit 目标:" "${patchline##*in }"
  case "$patchline" in
    *so.0.19.7*) say "⚠ 仍打在 2.48 的实体上:" "说明容器新版本没进包"; fail=1 ;;
  esac
fi

echo
if [ "$fail" = 0 ]; then
  echo "结论：未发现陈旧库被沿用的迹象。"
else
  echo "结论：发现陈旧库被沿用的迹象（见上方 ⚠）——升级未生效，见 AUDIT N17/N18/N19。"
fi
exit "$fail"

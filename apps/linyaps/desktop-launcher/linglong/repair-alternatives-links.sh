#!/bin/sh
# 修复 ${PREFIX} 内指向 /etc/alternatives 的断链。
#
# 背景：buildext.apt.depends 合并依赖时原样保留符号链接，而 Debian 的 BLAS/LAPACK
# 走 alternatives 机制：/usr/lib/x86_64-linux-gnu/libblas.so.3 是指向
# /etc/alternatives/libblas.so.3-x86_64-linux-gnu 的绝对链接，后者再指向
# /usr/lib/x86_64-linux-gnu/blas/libblas.so.3。玲珑只把「新装差异」合进 $PREFIX
# （/etc/alternatives 由基础层提供，不在其中），于是包内这两个链接的目标不存在。
#
# 后果不是「链接报错」而是「整块能力静默失效」：libgstlibav.so（GStreamer 的 FFmpeg
# 后端）动态依赖 libblas/liblapack，链接断掉时 GStreamer 插件扫描器只打印一行
# "Failed to load plugin ...: libblas.so.3: cannot open shared object file" 就跳过该
# 插件——构建成功、安装成功、GUI 正常启动，只有依赖它的功能不工作。
#
# 处理：链接目标实体其实已经随包进来了（lib/x86_64-linux-gnu/blas/libblas.so.3.11.0），
# 只是顶层链接指向容器外的绝对路径。改成指向包内实体的相对链接即可自洽。
# 用相对链接而非绝对 ${PREFIX} 路径，是因为产物层最终挂载点由玲珑决定，写死绝对
# 路径会在挂载点变化时再次断掉。
#
# 用法：sh repair-alternatives-links.sh <PREFIX>
#
# 只处理确认断链的条目：目标仍可解析的链接一概不动，避免把包内其它正常链接
# （例如 webkit 那条自己建的 so.0）改坏。修不了时非零退出——带着断链出包会
# 再次变成上面那种静默失效。
set -eu

PREFIX=${1:?usage: repair-alternatives-links.sh <PREFIX>}
LIBDIR="$PREFIX/lib/x86_64-linux-gnu"
[ -d "$LIBDIR" ] || { echo "alternatives: 缺少 $LIBDIR" >&2; exit 1; }

repaired=0
# find -type l 只取符号链接；-xtype l 进一步只取「链接本身存在、目标不可解析」的
# 断链（GNU find 语义），正是本脚本要修的那一类。
for link in $(find "$LIBDIR" -maxdepth 1 -type l ! -exec test -e {} \; -print 2>/dev/null); do
  base=${link##*/}
  # 实体按 Debian 惯例放在与库同名的子目录里，且带完整版本号后缀
  # （libblas.so.3 -> blas/libblas.so.3.11.0）。没有实体可指就无法修复。
  # 实体按 Debian 惯例放在「库名去掉 lib 前缀」的同名子目录里，且带完整版本号后缀
  # （libblas.so.3 -> blas/libblas.so.3.11.0）。目录名不含 lib 前缀，用 libblas 去找
  # 会一无所获，进而把「可修」误判成「无实体」——测试用例 1 正是这么暴露出来的。
  sub=${base#lib}
  sub=${sub%%.so.*}
  target=
  for candidate in "$LIBDIR/$sub/$base".*; do
    [ -f "$candidate" ] || continue
    target="$sub/${candidate##*/}"
    break
  done
  if [ -z "$target" ]; then
    echo "alternatives: $base 是断链，且 $LIBDIR/$sub/ 下没有可比对的实体" >&2
    exit 1
  fi
  # 原目标要在改写前读出来，否则打印的是刚写进去的新值。
  previous=$(readlink "$link")
  ln -sfn "$target" "$link"
  echo "alternatives: $base -> $target（原目标 $previous）"
  repaired=$((repaired + 1))
done

echo "alternatives: 修复 $repaired 个断链"
exit 0

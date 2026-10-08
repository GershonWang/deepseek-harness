#!/bin/sh
# 导出树的体积与内容断言：能出包不等于出的是这次构建的东西（AUDIT 9/10）。
#
# 作用对象是**导出树**（`linglong/output/binary/files`），也就是 .uab 的内容，而不是
# ll-builder 的合并缓存 `~/.cache/linglong-builder/merged/**`：裁剪前的合并缓存里
# `lib/gcc` 仍在（实测 33.7 MB），拿它当判据会恒真、等于没断言。
#
# 三类断言：
#   1. 不该存在的必须不存在——裁剪脚本静默失效时立刻失败，而不是产出一个大几十 MB 的包；
#   2. 该存在的必须存在——组装缺件时立刻失败（只补现有校验脚本未覆盖的两个入口）；
#   3. 体积上限——抓数量级回归（多出上百 MB 的编译器或无关栈），不钉死字节数。
#
# 阈值取「实测值 + 约 15% 余量」。上游增长属正常现象，需要人工复核后上调并更新取数日期；
# 阈值太紧会让每次上游升级都红灯，太松等于没有。
#
# 体积用 `du -sb`（表观字节数）而非 `du -sh`：后者按块大小取整，不同文件系统会得到
# 不同数字，无法作为稳定判据。
#
# 用法: verify-artifact-size.sh <merged-prefix>
set -eu

PREFIX=${1:?usage: verify-artifact-size.sh <merged-prefix>}

# 2026-10-07 实测（0.1.5.1 导出树）：整棵 937,166,505 B（893 MiB），
# lib/x86_64-linux-gnu 304,866,155 B（290 MiB）。
MAX_LIB_X86=$((340 * 1024 * 1024))
MAX_TREE=$((1024 * 1024 * 1024))

fail=0

must_be_absent() {
  if [ -e "$PREFIX/$1" ]; then
    echo "FAIL 体积断言: $1 不应存在（裁剪失效）" >&2
    fail=1
  else
    echo "OK   $1 不存在"
  fi
}

must_exist() {
  if [ -e "$PREFIX/$1" ]; then
    echo "OK   $1 存在"
  else
    echo "FAIL 体积断言: $1 缺失" >&2
    fail=1
  fi
}

within() { # $1 相对路径（`.` 表示整棵） $2 上限字节
  got=$(du -sb "$PREFIX/$1" | cut -f1)
  if [ "$got" -le "$2" ]; then
    echo "OK   $1 $(($got / 1048576)) MiB ≤ $(($2 / 1048576)) MiB"
  else
    echo "FAIL 体积断言: $1 为 $(($got / 1048576)) MiB，超过上限 $(($2 / 1048576)) MiB" >&2
    fail=1
  fi
}

# 1. 不该存在的：这两个目录由 prune-gcc-toolchain.sh 与 prepare-offline.sh 删除，
#    此前没有任何断言；脚本因上游布局变化而空转时，构建会照常成功。
must_be_absent lib/gcc
must_be_absent node/include

# 2. 该存在的：只补现有校验脚本未覆盖的入口。webkit 库与 bin/ 下的工具分别由
#    verify-merged-deps.sh 与 verify-tools.sh 负责，这里不重复。
must_exist bin/node
must_exist harness/lib/bin.js
# 中文字体随包（AUDIT N26）：字族与 fontconfig 配置早已就位，唯一会静默丢的是这个实
# 体——构建段抽取失败而断言不在时，产物照常导出，用户端只在缺中文字体的机器上看到
# 豆腐块。这里把它钉进出品条件。
must_exist share/dsh-fonts/wqy-microhei.ttc

# 3. 构建中间态不得进产物：tsc 的增量编译元数据只在构建期有意义，运行时无人读取
#    （AUDIT N29）。按后缀匹配，否则会漏掉 gaxios 的 tsconfig.cjs.tsbuildinfo。
# 用 -print -quit 取第一个命中就停：不接管道，避免 head/grep 提前退出时
# find 收到 SIGPIPE 打出无意义的报错。
TSBUILD=$(find "$PREFIX" -name '*.tsbuildinfo' -print -quit)
if [ -n "$TSBUILD" ]; then
  echo "FAIL 体积断言: 产物里出现 *.tsbuildinfo（构建中间态）: $TSBUILD" >&2
  fail=1
else
  echo "OK   无 *.tsbuildinfo"
fi

# 4. 体积上限：先单项再整棵，单项超限时先给出更具体的路径。
within lib/x86_64-linux-gnu "$MAX_LIB_X86"
within . "$MAX_TREE"

[ "$fail" -eq 0 ] || exit 1
echo "体积与内容断言通过"

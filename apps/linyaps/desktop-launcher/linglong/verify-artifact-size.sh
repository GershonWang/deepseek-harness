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
# 用法: verify-artifact-size.sh <merged-prefix> [整棵上限字节]
set -eu

PREFIX=${1:?usage: verify-artifact-size.sh <merged-prefix> [max-tree-bytes]}

# 2026-10-07 实测（0.1.5.1 导出树）：整棵 937,166,505 B（893 MiB），
# lib/x86_64-linux-gnu 304,866,155 B（290 MiB）。
MAX_LIB_X86=$((340 * 1024 * 1024))
# 整棵上限允许调用方覆盖：双客户端包（com.deepseek.dsh-desktop）在同一棵树里还带着官方
# Electron 客户端，必然超过薄壳版的上限，而两包共用同一份内容断言（该在的、不该在的、
# lib 单项上限）才有意义——各写一份等于放弃其中一份的维护。只有整棵这一项按包分档。
MAX_TREE=${2:-$((1024 * 1024 * 1024))}

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

warn_if_present() { # $1 相对路径 $2 原因
  if [ -e "$PREFIX/$1" ]; then
    echo "WARN $1 存在：$2"
  else
    echo "OK   $1 不存在"
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

# 1. 不该存在的：node/include 由 prepare-offline.sh 在宿主机暂存阶段删除，因此它进不进包
#    完全取决于那一步，这里保持硬断言。
must_be_absent node/include
#    桌面应用包被 pnpm 11.28 的 legacy deploy 一并 hoist 进生产闭包时立刻失败：它带着
#    .desktop-build 构建残留（本机实测 2.4 GB），不是运行时内容。prepare-offline 的
#    「移除被 hoist 的非运行时应用包」步骤负责删除，这条断言防止那一步静默失效，
#    也防止上游/包管理器行为再变时体积悄悄回到 4.5 GB。
must_be_absent harness/node_modules/@deepseek-ai/dsh-desktop
#    lib/gcc 只能告警：它由 buildext.apt.depends 的 apt 闭包带进产物，而该合并发生在 build
#    段之后、提交之前，yaml 没有可挂的钩子——裁剪脚本改的是提交层的物化副本，不会进包
#    （实测删掉 lib/gcc 后导出的 .uab 体积一个字节不变）。当前构建流程无法让这个目录不
#    存在，硬断言只会让每次构建都失败，所以如实告警并指向根因。
#    根因：apt 闭包把 Recommends 一起装了（本包实测一次拉取 146 MB，含 gcc-12、imagemagick、
#    aspell、gstreamer 插件等）。真正的修法是让依赖闭包显式化，属独立议题。
warn_if_present lib/gcc "apt 闭包带进来的编译器工具链（约 33 MiB），当前构建流程无法裁剪"

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

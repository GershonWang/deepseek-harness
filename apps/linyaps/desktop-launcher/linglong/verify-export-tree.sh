#!/bin/sh
# 导出树的验收断言：`ll-builder build` 之后、`ll-builder export` 之前必须跑的那一段。
#
# 为什么是「验收」而不是「后处理」：导出树是**提交层的物化副本**，改它不会进包。
# 实测在 build 之后改链接、删 lib/gcc、加一个标记文件，导出的 .uab 体积一个字节都不变
# （哈希会变，那是 erofs 内嵌的随机 Filesystem UUID）；`ll-builder export --ref` 打的是
# `ll-builder build` 提交好的那一层。而 yaml 没有「合并之后」的钩子：build 段生成
# entry.sh（末尾追加符号剥离），buildext.apt 的合并再跑，然后才提交。因此凡是「让产物变成
# 我们想要的样子」的修复都必须落在 build 段；这里只做断言。
#
# 断言跑在这棵树上是有意义的：树就是那一层的内容，安装后的层与它逐项一致（实测比对过
# lib/gcc 与 alternatives 链接）。反过来，如果这里还顺手改树，断言就会掩盖 build 段
# 没做对的事实——那正是这套脚本早先「看起来通过、实际没进包」的成因。
#
# 用法: verify-export-tree.sh <merged-prefix> [整棵上限字节]
#   <merged-prefix> 是导出树，即 linglong/output/binary/files。
#   [整棵上限字节] 按包分档；留空表示交给 verify-artifact-size.sh 的内置默认（薄壳版 1 GiB）。
set -eu

PREFIX=${1:?usage: verify-export-tree.sh <merged-prefix> [max-tree-bytes]}
MAX_TREE=${2:-}
LL_DIR=$(cd "$(dirname "$0")" && pwd)

echo "==> 校验 depends 实体是否真的进了合并产物树（容器内看不到它们）"
if sh "$LL_DIR/verify-merged-deps.sh" "$PREFIX"; then
  echo "==> depends 实体校验通过"
else
  echo "==> ✗ depends 实体校验失败：产物与 buildext.apt 声明不符，中止导出" >&2
  exit 1
fi

echo "==> 校验合并产物树工具清单（含 git-core helper，launcher 以 GIT_EXEC_PATH 指回它）"
if sh "$LL_DIR/verify-tools.sh" "$PREFIX"; then
  echo "==> 工具清单校验通过"
else
  echo "==> ⚠ 工具清单校验有失败项，继续导出（不影响包功能）" >&2
fi

# 走 alternatives 的库（libblas/liblapack）由上面的 verify-merged-deps.sh 一并断言：它扫
# lib/x86_64-linux-gnu 一层里的断链，正是这两个链接失效时的形态（详见该脚本 1b 段）。
# 修复落在 build 段的 place-alternatives-libs.sh——那里先占住路径，合并就不再补进断链；
# 这条断言因此同时守着「修复没生效」与「修复脚本被移除」两种回归。

# 体积与内容：裁剪（lib/gcc、node/include）此前只有脚本自觉、没有断言，脚本因上游布局变化
# 而空转时产物凭空大几十 MB 而构建照常成功；lib/x86_64-linux-gnu 的膨胀也完全没有上限。
echo "==> 校验导出树的体积与内容"
if sh "$LL_DIR/verify-artifact-size.sh" "$PREFIX" "$MAX_TREE"; then
  echo "==> 体积与内容断言通过"
else
  echo "==> ✗ 体积与内容断言失败：产物与组装预期不符，中止导出" >&2
  exit 1
fi

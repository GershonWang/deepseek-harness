#!/bin/sh
# 导出树的后处理与断言：`ll-builder build` 之后、`ll-builder export` 之前必须跑的那一段。
#
# 为什么单独成脚本：薄壳版（com.deepseek.dsh-desktop）与双客户端版（同一个包 ID 的升级款，
# 树里多一套官方 Electron 运行时）要的是同一段后处理——裁剪编译器、修复断链、校验依赖实体
# 与工具清单、断言体积。这段编排此前只写在薄壳版的 build-linglong.sh 里，双客户端那份漏掉了
# 它，于是打出的包带着约 140 MB 编译器工具链，且 libblas/liblapack 断链让 GStreamer 的 FFmpeg
# 后端静默加载失败（实测 ldd 报 libblas.so.3 => not found）——两个包各留一份编排，就是这次
# 漏步的成因。编排只留一份，才不会再次漏。
#
# 顺序不可调换：
#   1. 裁剪最先——后面的体积断言断言的正是裁剪后的状态；
#   2. 断链修复排在断言之前——断言校验的就是修复后的链接；
#   3. 断言最后——它给出中止导出的硬条件。
#
# 用法: postprocess-export-tree.sh <merged-prefix> [整棵上限字节]
#   <merged-prefix> 是导出树，即 linglong/output/binary/files。
#   [整棵上限字节] 按包分档；留空表示交给 verify-artifact-size.sh 的内置默认（薄壳版 1 GiB）。
set -eu

PREFIX=${1:?usage: postprocess-export-tree.sh <merged-prefix> [max-tree-bytes]}
MAX_TREE=${2:-}
LL_DIR=$(cd "$(dirname "$0")" && pwd)

echo "==> 清理 gcc 编译工具链（保留运行时库，减约 140 MB）"
sh "$LL_DIR/prune-gcc-toolchain.sh" "$PREFIX"

# depends 的合并发生在 build: 段之后，alternatives 断链那时才出现，所以修复必须在这里做，
# 不能放进 build: 段（放进去会因为链接尚不存在而空转，且没有任何报错）。
echo "==> 修复 depends 合并带进来的 alternatives 断链"
sh "$LL_DIR/repair-alternatives-links.sh" "$PREFIX"

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

# 裁剪（lib/gcc、node/include）此前只有脚本自觉、没有断言：脚本因上游布局变化而空转时，
# 产物凭空大几十 MB 而构建照常成功；lib/x86_64-linux-gnu 的膨胀也完全没有上限。
# 这里把「不该有的、该有的、体积上限」写成导出前的硬条件（AUDIT 9/10）。
echo "==> 校验导出树的体积与内容"
if sh "$LL_DIR/verify-artifact-size.sh" "$PREFIX" "$MAX_TREE"; then
  echo "==> 体积与内容断言通过"
else
  echo "==> ✗ 体积与内容断言失败：产物与裁剪/组装预期不符，中止导出" >&2
  exit 1
fi

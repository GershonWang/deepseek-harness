#!/bin/sh
# 构建后清理 gcc 工具链的冗余文件。
#
# 背景：buildext.apt.depends 合并 git/python 等包时，依赖链会意外带进完整的
# gcc-12 编译工具链（cc1/cc1plus/asan/tsan 等），占 ~140 MB。运行时只需要
# libgcc_s.so / libstdc++.so / libatomic.so 等运行时库，不需要编译器本身。
#
# 此脚本在 ll-builder build 之后、verify-tools.sh 之前执行，直接从
# output/binary/files/ 里删掉编译器本体，保留运行时 .so。
#
# 用法: sh prune-gcc-toolchain.sh <prefix>
#   例: sh prune-gcc-toolchain.sh linglong/output/binary/files
set -eu
PREFIX=${1:?usage: prune-gcc-toolchain.sh <prefix>}

echo "prune-gcc: 清理 gcc 编译工具链（保留运行时库）..."

# 1. 删除 lib/gcc/ 整个目录（编译器内部文件：cc1/cc1plus/crt*.o/asan/tsan 等）
#    这是最大的一块，约 103 MB
if [ -d "$PREFIX/lib/gcc" ]; then
  rm -rf "$PREFIX/lib/gcc"
  echo "  - 已删除 lib/gcc/ (编译器内部文件)"
fi

# 2. 删除 bin/ 下的 gcc/g++/cpp/gcov/lto/c++filt 等编译器可执行文件
#    保留用户态工具（git/python/curl 等不动）
for bin in gcc g++ cpp gcov gcov-dump gcov-tool c89-gcc c99-gcc \
  gcc-12 g++-12 cpp-12 gcov-12 gcov-dump-12 gcov-tool-12 \
  gcc-ar gcc-ar-12 gcc-nm gcc-nm-12 gcc-ranlib gcc-ranlib-12 \
  x86_64-linux-gnu-gcc x86_64-linux-gnu-g++ x86_64-linux-gnu-cpp \
  x86_64-linux-gnu-gcc-12 x86_64-linux-gnu-g++-12 x86_64-linux-gnu-cpp-12 \
  x86_64-linux-gnu-gcc-ar x86_64-linux-gnu-gcc-ar-12 \
  x86_64-linux-gnu-gcc-nm x86_64-linux-gnu-gcc-nm-12 \
  x86_64-linux-gnu-gcc-ranlib x86_64-linux-gnu-gcc-ranlib-12 \
  x86_64-linux-gnu-lto-dump-12 lto-dump-12 \
  g++-mapper-server cc1 cc1plus cc1obj cc1objplus cc1plus f951
do
  if [ -f "$PREFIX/bin/$bin" ]; then
    rm -f "$PREFIX/bin/$bin"
  fi
  if [ -L "$PREFIX/bin/$bin" ]; then
    rm -f "$PREFIX/bin/$bin"
  fi
done
echo "  - 已删除 bin/ 下的 gcc 系列可执行文件"

# 3. 删除 lib/x86_64-linux-gnu/ 下的 sanitizer 库（运行时不需要）
#    保留 libgcc_s.so / libstdc++.so / libatomic.so 等运行时库
for san in libasan libtsan libubsan liblsan libmsan; do
  for f in "$PREFIX"/lib/x86_64-linux-gnu/${san}.so*; do
    [ -e "$f" ] && rm -f "$f"
  done
done
echo "  - 已删除 sanitizer 库（asan/tsan/ubsan 等）"

# 4. 验证关键运行时库还在（不能误删）
for lib in libgcc_s.so.1 libstdc++.so.6 libatomic.so.1; do
  if [ ! -f "$PREFIX/lib/x86_64-linux-gnu/$lib" ]; then
    echo "  ⚠ 警告：关键运行时库 $lib 缺失，可能误删了" >&2
  fi
done

BEFORE=""
echo "prune-gcc: 完成。PREFIX/lib 剩余大小：$(du -sh "$PREFIX/lib" | cut -f1)"

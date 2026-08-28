#!/bin/sh
# 清除玲珑构建的缓存和中间产物，确保下一次构建从干净状态开始。
#
# 默认仅清构建产物（快，推荐日常使用）；加 --deep 连依赖和编译缓存一起清。
#
# 用法: 在仓库根运行
#   sh apps/desktop-launcher/clean-linglong.sh          # 普通清理（默认）
#   sh apps/desktop-launcher/clean-linglong.sh --deep   # 深度清理（依赖 + 构建）
#
# 普通清理（推荐日常用，几秒搞定）：
#   - linglong/stage/    prepare-offline 宿主暂存
#   - linglong/output/   ll-builder 构建输出
#   - linglong/cache/    ll-builder 层缓存
#   - *.uab              仓库根导出的安装包
#   - apps/cli/deploy/   deploy 残留
#
# 深度清理（遇到玄学问题时用，比较慢）：
#   - 以上全部
#   - node_modules/      全部依赖（需重新 pnpm install）
#   - */*/lib/           所有包的 tsc 构建产物
#   - apps/web/dist/     Web 前端构建产物
#   - .eslintcache 等    工具缓存文件
#
# 注意: 不会删除源码、pnpm-lock.yaml、package.json 等版本控制文件。
set -eu
cd "$(dirname "$0")/../.." # 仓库根

DEEP=false
if [ "${1:-}" = "--deep" ]; then
  DEEP=true
fi

LL_DIR=apps/desktop-launcher/linglong

if [ "$DEEP" = true ]; then
  echo "==> 深度清理玲珑构建（含依赖 + 编译产物，较慢）"
else
  echo "==> 清除玲珑构建缓存"
fi

# ---------- 普通清理 ----------

# 1. stage/ — prepare-offline.sh 的宿主构建暂存（harness 闭包、node、
#    go 二进制等）。最常需要清理：上游升级后旧闭包会导致奇怪问题。
if [ -d "$LL_DIR/stage" ]; then
  echo "  - $LL_DIR/stage/"
  rm -rf "$LL_DIR/stage"
fi

# 2. linglong/output/ — ll-builder 构建输出（layers、binary、.uab 源）
if [ -d "$LL_DIR/output" ]; then
  echo "  - $LL_DIR/output/"
  rm -rf "$LL_DIR/output"
fi

# 3. linglong/cache/ — ll-builder 的层缓存和增量构建缓存
if [ -d "$LL_DIR/cache" ]; then
  echo "  - $LL_DIR/cache/"
  rm -rf "$LL_DIR/cache"
fi

# 4. 仓库根的 .uab 导出产物（build-linglong.sh 会导出到这里）
uab_count=$(find . -maxdepth 1 -name "*.uab" -type f 2>/dev/null | wc -l)
if [ "$uab_count" -gt 0 ]; then
  echo "  - *.uab (共 $uab_count 个)"
  rm -f ./*.uab
fi

# 5. apps/cli 的 deploy 残留（prepare-offline 从这里 deploy，偶尔
#    会留下 node_modules 符号链接脏状态，导致下一次 deploy 行为异常）
if [ -d "apps/cli/deploy" ]; then
  echo "  - apps/cli/deploy/"
  rm -rf "apps/cli/deploy"
fi

# ---------- 深度清理 ----------

if [ "$DEEP" = true ]; then

  # 6. 根 node_modules — 全部依赖（最深的重置，之后需要 pnpm install）
  if [ -d "node_modules" ]; then
    echo "  - node_modules/  (全部依赖，之后需 pnpm install)"
    rm -rf node_modules
  fi

  # 7. 各包的 lib/ 构建产物（tsc 输出）
  lib_count=0
  for d in packages/*/*/lib apps/*/lib vendor/*/lib; do
    if [ -d "$d" ]; then
      lib_count=$((lib_count + 1))
      rm -rf "$d"
    fi
  done
  if [ "$lib_count" -gt 0 ]; then
    echo "  - */*/lib/  (共 $lib_count 个包的编译产物)"
  fi

  # 8. Web 前端 dist
  if [ -d "apps/web/dist" ]; then
    echo "  - apps/web/dist/"
    rm -rf apps/web/dist
  fi

  # 9. 各种工具缓存文件
  for f in .eslintcache .oxlintcache .tsbuildinfo; do
    if [ -f "$f" ]; then
      echo "  - $f"
      rm -f "$f"
    fi
  done

  echo ""
  echo "  提示: 深度清理后需要先执行 pnpm install，再构建。"
fi

echo ""
echo "==> 清理完成"
echo ""
echo "下一步: sh apps/desktop-launcher/build-linglong.sh"

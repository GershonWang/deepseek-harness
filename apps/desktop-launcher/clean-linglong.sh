#!/bin/sh
# 清除玲珑构建的缓存和中间产物，确保下一次构建从干净状态开始。
#
# 三级清理策略：
#   普通清理（默认）— 玲珑构建产物 + 通用构建缓存，不含 node_modules，几秒搞定
#   深度清理（--deep）— 以上全部 + 所有 node_modules + 依赖缓存，较慢
#   彻底清理（--nuclear）— 深度清理 + 清除 pnpm store 孤立包，最慢
#
# 用法: 在仓库根运行
#   sh apps/desktop-launcher/clean-linglong.sh          # 普通清理（默认）
#   sh apps/desktop-launcher/clean-linglong.sh --deep   # 深度清理（依赖 + 构建）
#   sh apps/desktop-launcher/clean-linglong.sh --nuclear # 彻底清理（最极端）
#
# 普通清理（推荐日常用，几秒搞定）：
#   - linglong/stage/    prepare-offline 宿主暂存
#   - linglong/output/   ll-builder 构建输出
#   - linglong/cache/    ll-builder 层缓存
#   - *.uab              仓库根导出的安装包
#   - apps/cli/deploy/   deploy 残留
#   - dsh-desktop-launcher  Go 编译的二进制产物
#   - */*/lib/           所有包的 tsc 构建产物（重建代价低）
#   - */*/types/         所有包的类型声明输出
#   - .dsh-build/        客户端构建环境元数据
#   - .typecheck/        TypeScript 类型检查缓存
#   - native/landlock-run/packages/entry/lib/  Native 模块编译产物
#   - apps/web/dist/     Web 前端构建产物
#   - *.tsbuildinfo      TypeScript 增量编译缓存
#
# 深度清理（遇到玄学问题时用，比较慢）：
#   - 以上全部
#   - node_modules/      全部依赖（需重新 pnpm install）
#   - .eslintcache 等    工具缓存文件
#
# 彻底清理（最极端，慎用）：
#   - 以上全部
#   - pnpm store 孤立包  释放磁盘空间
#
# 注意: 不会删除源码、pnpm-lock.yaml、package.json 等版本控制文件。
set -eu
cd "$(dirname "$0")/../.." # 仓库根

DEEP=false
NUCLEAR=false
if [ "${1:-}" = "--deep" ]; then
  DEEP=true
elif [ "${1:-}" = "--nuclear" ]; then
  DEEP=true
  NUCLEAR=true
fi

LL_DIR=apps/desktop-launcher/linglong

if [ "$NUCLEAR" = true ]; then
  echo "==> 彻底清理（含依赖 + 构建 + pnpm store，最慢）"
elif [ "$DEEP" = true ]; then
  echo "==> 深度清理（含依赖 + 编译产物，较慢）"
else
  echo "==> 普通清理（构建产物 + 缓存，几秒搞定）"
fi

# ====================================================================
# 普通清理（默认）
# ====================================================================

# 1. linglong/ 玲珑构建产物
#    - stage/: prepare-offline.sh 的宿主构建暂存（harness 闭包、node、
#      go 二进制等）。上游升级后旧闭包会导致奇怪问题。
#    - output/: ll-builder 构建输出（layers、binary、.uab 源）
#    - cache/: ll-builder 的层缓存和增量构建缓存
for sub in stage output cache; do
  if [ -d "$LL_DIR/$sub" ]; then
    echo "  - $LL_DIR/$sub/"
    rm -rf "$LL_DIR/$sub"
  fi
done

# 2. 仓库根的 .uab 导出产物（build-linglong.sh 会导出到这里）
uab_count=$(find . -maxdepth 1 -name "*.uab" -type f 2>/dev/null | wc -l)
if [ "$uab_count" -gt 0 ]; then
  echo "  - *.uab (共 $uab_count 个)"
  rm -f ./*.uab
fi

# 3. apps/cli 的 deploy 残留（prepare-offline 从这里 deploy，偶尔
#    会留下 node_modules 符号链接脏状态，导致下一次 deploy 行为异常）
if [ -d "apps/cli/deploy" ]; then
  echo "  - apps/cli/deploy/"
  rm -rf "apps/cli/deploy"
fi

# 4. 通用构建产物 — 重建代价低，不含 node_modules 不影响依赖
#    先统计再删除，确保报告准确

# 4a. Go 编译产物
if [ -f "$LL_DIR/dsh-desktop-launcher" ]; then
  echo "  - $LL_DIR/dsh-desktop-launcher (Go 二进制)"
  rm -f "$LL_DIR/dsh-desktop-launcher"
fi

# 4c. lib/ 编译产物
lib_count=0
for d in packages/*/*/lib apps/*/lib vendor/*/lib native/landlock-run/packages/entry/lib; do
  if [ -d "$d" ]; then
    lib_count=$((lib_count + 1))
    rm -rf "$d"
  fi
done
if [ "$lib_count" -gt 0 ]; then
  echo "  - lib/ (共 $lib_count 个包的编译产物)"
fi

# 4d. types/ 类型声明
types_count=0
for d in packages/*/*/types; do
  if [ -d "$d" ]; then
    types_count=$((types_count + 1))
    rm -rf "$d"
  fi
done
if [ "$types_count" -gt 0 ]; then
  echo "  - types/ (共 $types_count 个包的类型声明)"
fi

# 4e. dist/ Web 前端构建
for d in apps/*/dist; do
  if [ -d "$d" ]; then
    echo "  - $d/"
    rm -rf "$d"
  fi
done

# 4f. .dsh-build/ 客户端构建元数据
if [ -d ".dsh-build" ]; then
  echo "  - .dsh-build/"
  rm -rf .dsh-build
fi

# 4g. .typecheck/ 类型检查缓存
if [ -d ".typecheck" ]; then
  echo "  - .typecheck/"
  rm -rf .typecheck
fi

# 4h. *.tsbuildinfo 增量编译缓存
tsbuildinfo_count=0
for f in *.tsbuildinfo; do
  if [ -f "$f" ]; then
    tsbuildinfo_count=$((tsbuildinfo_count + 1))
    rm -f "$f"
  fi
done
if [ "$tsbuildinfo_count" -gt 0 ]; then
  echo "  - *.tsbuildinfo (共 $tsbuildinfo_count 个增量编译缓存)"
fi

# ====================================================================
# 深度清理（--deep 或 --nuclear）
# ====================================================================

if [ "$DEEP" = true ]; then

  # 5. 根 node_modules — 全部依赖（最深的重置，之后需要 pnpm install）
  if [ -d "node_modules" ]; then
    echo "  - node_modules/  (全部依赖，之后需 pnpm install)"
    rm -rf node_modules
  fi

  # 6. 所有子包的 node_modules
  sub_nm_count=0
  for d in packages/*/*/node_modules apps/*/node_modules vendor/*/node_modules native/*/node_modules website/node_modules python/*/node_modules; do
    if [ -d "$d" ]; then
      sub_nm_count=$((sub_nm_count + 1))
      rm -rf "$d"
    fi
  done
  if [ "$sub_nm_count" -gt 0 ]; then
    echo "  - 子包 node_modules/  (共 $sub_nm_count 个，之后需 pnpm install)"
  fi

  # 7. 工具缓存文件
  for f in .eslintcache .oxlintcache; do
    if [ -f "$f" ]; then
      echo "  - $f"
      rm -f "$f"
    fi
  done

  echo ""
  echo "  提示: 深度清理后需要先执行 pnpm install，再构建。"
fi

# ====================================================================
# 彻底清理（--nuclear）
# ====================================================================

if [ "$NUCLEAR" = true ]; then
  echo ""
  echo "  正在清理 pnpm store 中的孤立包..."
  pnpm store prune 2>/dev/null && echo "  - pnpm store prune 完成" || echo "  - pnpm store prune 跳过（未安装或无孤立包）"
fi

echo ""
echo "==> 清理完成"
echo ""
if [ "$DEEP" = true ]; then
  echo "下一步:"
  echo "  1. pnpm install"
  echo "  2. sh apps/desktop-launcher/build-linglong.sh"
else
  echo "下一步: sh apps/desktop-launcher/build-linglong.sh"
fi

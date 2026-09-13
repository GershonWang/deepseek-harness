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
#   - apps/desktop-launcher/linglong/stage/  prepare-offline 宿主暂存
#   - linglong/          ll-builder 构建工作区（output、cache、overlay）
#   - *.uab              仓库根导出的安装包
#   - apps/desktop-launcher/dsh-desktop-launcher  Go 编译的二进制产物
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

# launcher 应用目录：Go 二进制与玲珑打包脚本的所在目录。
APP_DIR=apps/desktop-launcher
# 玲珑源码目录：源码树内唯一可清理项是 prepare-offline.sh 的宿主暂存 stage/。
LL_SRC="$APP_DIR/linglong"
# ll-builder 构建工作区：ll-builder 以项目根（本脚本已 cd 到仓库根）为基准创建
# linglong/{output,cache,overlay}，并在每次构建时重写 entry.sh/buildext.sh/
# depends.yaml。基准与 build-linglong.sh 使用的 linglong/output/binary/files、
# 与 .gitignore 的 /linglong/ 一致。
LL_WORK=linglong
# 在 linglong.yaml 所在目录内直接执行 ll-builder 时的嵌套变体：路径不同、
# 内容同源，一并清理（.gitignore:39 已为该路径预留忽略规则）。
LL_WORK_NESTED="$LL_SRC/linglong"

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

# 1. 玲珑构建产物
#    - stage/: prepare-offline.sh 的宿主构建暂存（harness 闭包、node、
#      go 二进制等）。上游升级后旧闭包会导致奇怪问题。
#    - $LL_WORK/: ll-builder 工作区，含 output/（layers 与 binary 组装树、
#      .uab 源）、cache/（层缓存）、overlay/（基础层 overlay）。层缓存与
#      基础 overlay 正是「上游升级后旧闭包」的载体，必须与 stage/ 同时清。
#    - $LL_WORK_NESTED/: 上一项的路径变体，见常量处说明。
for target in "$LL_SRC/stage" "$LL_WORK" "$LL_WORK_NESTED"; do
  if [ -d "$target" ]; then
    echo "  - $target/"
    # overlayfs 挂载期间会把 workdir 置为 0000，ll-builder 异常退出后卸载会
    # 留下无法遍历的残渣；不补回属主权限，rm 会在中途失败并因 set -e 中断
    # 后续全部清理。目标均为属主自己的构建产物，chmod 失败即交由 rm 报错。
    chmod -R u+rwX "$target" 2>/dev/null || true
    rm -rf "$target"
  fi
done

# 2. 仓库根的 .uab 导出产物（build-linglong.sh 会导出到这里）
uab_count=$(find . -maxdepth 1 -name "*.uab" -type f 2>/dev/null | wc -l)
if [ "$uab_count" -gt 0 ]; then
  echo "  - *.uab (共 $uab_count 个)"
  rm -f ./*.uab
fi

# 3. 通用构建产物 — 重建代价低，不含 node_modules 不影响依赖
#    先统计再删除，确保报告准确

# 3a. Go 编译产物（Makefile 在 apps/desktop-launcher/ 内 go build -o，
#     产物留在该目录，.gitignore:38 与之对应）
if [ -f "$APP_DIR/dsh-desktop-launcher" ]; then
  echo "  - $APP_DIR/dsh-desktop-launcher (Go 二进制)"
  rm -f "$APP_DIR/dsh-desktop-launcher"
fi

# 3b. lib/ 编译产物
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

# 3c. types/ 类型声明
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

# 3d. dist/ Web 前端构建
for d in apps/*/dist; do
  if [ -d "$d" ]; then
    echo "  - $d/"
    rm -rf "$d"
  fi
done

# 3e. .dsh-build/ 客户端构建元数据
if [ -d ".dsh-build" ]; then
  echo "  - .dsh-build/"
  rm -rf .dsh-build
fi

# 3f. .typecheck/ 类型检查缓存
if [ -d ".typecheck" ]; then
  echo "  - .typecheck/"
  rm -rf .typecheck
fi

# 3g. *.tsbuildinfo 增量编译缓存
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

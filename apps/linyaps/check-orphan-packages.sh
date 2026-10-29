#!/bin/sh
# 打包前置检查：packages/<group>/<pkg> 下不允许存在缺少 package.json 的目录。
#
# 为什么需要它：上游删除或改名包时 git 只删跟踪文件，被 .gitignore 的 lib/ 与
# node_modules/ 会原地留下没有清单的空壳目录（fork-divergence.md 记录过同一现象）。
# 这类目录会被 check-workspace-constraints 与 tsdown 的 workspace 推导当成构建单元，
# 报出的却是伪装成根包的 "Cannot find entry"，既晚又不指向真正原因。打包前先查一次，
# 直接在等待任何构建之前点名目录并给出清理命令。
#
# 判定与 scripts/clean.ts 保持一致：已知残留只有 node_modules/、lib/、.typecheck/ 与
# *.tsbuildinfo；出现其它条目说明目录里可能还有人工内容，此时 pnpm run clean 会拒绝
# 删除，需要人工确认，因此这里分成两种结论分别提示。
#
# 用法：在仓库根运行
#   sh apps/linyaps/check-orphan-packages.sh
# 退出码：0 = 没有空壳目录；1 = 存在空壳目录（已在 stderr 点名）。
set -eu

cd "$(dirname "$0")/../.." # 仓库根

orphans=0
for group in packages/*/; do
  [ -d "$group" ] || continue
  for pkg_dir in "$group"*/; do
    [ -d "$pkg_dir" ] || continue
    pkg_dir=${pkg_dir%/}
    if [ -f "$pkg_dir/package.json" ]; then
      continue
    fi
    if [ "$orphans" -eq 0 ]; then
      echo "packages/ 下存在缺少 package.json 的目录（多为上游删包后残留的构建产物）:" >&2
    fi
    orphans=$((orphans + 1))
    leftovers=$(ls -A "$pkg_dir" | grep -Ev '^(node_modules|lib|\.typecheck)$' | grep -Ev '\.tsbuildinfo$' || true)
    if [ -n "$leftovers" ]; then
      echo "  $pkg_dir：含未知内容，pnpm run clean 会拒绝删除，需人工确认:" >&2
      printf '%s\n' "$leftovers" | sed 's/^/    /' >&2
    else
      echo "  $pkg_dir：仅构建残留" >&2
    fi
  done
done

if [ "$orphans" -gt 0 ]; then
  echo "补救：pnpm run clean 会删除只剩构建残留的孤儿目录与失效链接。" >&2
  exit 1
fi

#!/bin/sh
# 让走 alternatives 机制的库（BLAS/LAPACK）以**可解析的相对链接**进入产物。
#
# 机制（实测确定，与直觉相反，务必按它来改）：
#   ll-builder 的顺序是「build 段（entry.sh，末尾追加符号剥离）→ 依赖合并 → 提交」，
#   `ll-builder export --ref` 打的是**提交好的层**；构建工作区里的导出树
#   （linglong/output/binary/files）只是那层提交后的物化副本，在 build 之后改它不会进包
#   （实测改链接、删 lib/gcc、加标记文件，导出的 .uab 体积一个字节不变）。
#   yaml 也没有「合并之后」的钩子（ll-builder 1.14.3 只认 buildext.apt 的 buildDepends/depends）。
#
#   合并的源是**容器 /usr 的 overlay 上层**（构建日志里那条 `failed to copy
#   …/overlay/prepare_base/upperdir/usr/lib/x86_64-linux-gnu/…` 就是它），并且**会覆盖**
#   ${PREFIX} 里已有的同名路径。webkit 的补丁库之所以能存活，是因为那一次复制**失败**了
#   （源是 dpkg 写失败留下的字符设备，复制报「无效的参数」），而不是合并会跳过已有路径。
#
# 因此这里做两件事，缺一不可：
#   1. 把容器 /usr 上的 alternatives 绝对链接改成包内相对链接——合并带进产物的就是可解析的
#      那一份（这是决定性的一步：只改 ${PREFIX} 会被合并覆盖回去，实测仍然断链）；
#   2. 同时把实体与链接写进 ${PREFIX}，作为合并未覆盖该路径时的兜底。
#
# 为什么是这两个库：libgstlibav.so（GStreamer 的 FFmpeg 后端）动态依赖 libblas/liblapack。
# 合并带进来的链接指向 /etc/alternatives/...，而该目录由基础层提供、里面没有这两个条目，
# 插件因此静默加载失败（实测容器内 ldd 报 libblas.so.3 => not found），依赖它的媒体能力
# 不工作，而构建、安装、启动全程无报错。
#
# 用法: place-alternatives-libs.sh <PREFIX>
set -eu

PREFIX=${1:?usage: place-alternatives-libs.sh <PREFIX>}
LL_DIR=$(cd "$(dirname "$0")" && pwd)
LIBDIR=lib/x86_64-linux-gnu

# place_one 交付一个库：下载 apt 候选 .deb、解出实体与 soname，然后在产物树与容器 /usr
# 两处都落位（实体 + 相对链接）。
# @param $1 - 提供该库的包名
# @param $2 - 实体在 .deb 内相对 usr/<LIBDIR> 的子目录（alternatives 的实际落点）
place_one() {
  pkg=$1
  sub=$2
  tmp=$(mktemp -d)
  ( cd "$tmp" && apt-get download "$pkg" ) \
    || { echo "alternatives: apt-get download $pkg 失败" >&2; exit 1; }
  set -- "$tmp"/${pkg}_*.deb
  [ "$#" -eq 1 ] && [ -f "$1" ] \
    || { echo "alternatives: $pkg 的 .deb 未唯一命中（$# 个: $*）" >&2; exit 1; }
  deb=$1
  ver=${deb##*/}; ver=${ver#${pkg}_}; ver=${ver%_amd64.deb}
  # 版本必须等于 apt 候选：候选变了这里跟着走，避免下载成功却拿到别的版本这种静默偏差。
  cand_ver=$(apt-cache policy "$pkg" | awk '/Candidate:/ { print $2; exit }')
  [ "$ver" = "$cand_ver" ] \
    || { echo "alternatives: 下载到 $ver，apt 候选却是 $cand_ver" >&2; exit 1; }
  dpkg-deb -x "$deb" "$tmp/x"

  # 实体：子目录里 *.so.* 会同时命中 soname 链接与实体（libblas.so.3 与 libblas.so.3.11.0），
  # 因此按「普通文件且不是符号链接」筛而不是要求唯一命中；必须恰好一个，否则包布局已变，
  # 不能猜。两个条件都必要：-f 会跟随链接（把 soname 链接也判成实体），而写入未落盘时现场
  # 留下的是字符设备、-e 对它同样为真（AUDIT N19）。
  real=
  for cand_path in "$tmp/x/usr/$LIBDIR/$sub"/*.so.*; do
    [ -f "$cand_path" ] && [ ! -L "$cand_path" ] || continue
    [ -z "$real" ] \
      || { echo "alternatives: $pkg 解包出多个普通文件实体（$real 与 $cand_path）" >&2; exit 1; }
    real=$cand_path
  done
  [ -n "$real" ] \
    || { echo "alternatives: $pkg 解包后没有普通文件实体（包布局变化？）" >&2; exit 1; }
  name=${real##*/}

  # soname 取同一子目录里那个符号链接的名字（libblas.so.3 → libblas.so.3.11.0）。它既是
  # 动态链接器要找的名字，也是 alternatives 的入口名；.deb 里并没有顶层链接——那一条由
  # update-alternatives 在 postinst 里建，包里那条断链就是这么来的。
  # 不做字符串推导：推导错了只会让链接指向不存在的名字，插件继续静默不加载。
  soname=
  for link_path in "$tmp/x/usr/$LIBDIR/$sub"/*.so.*; do
    [ -L "$link_path" ] || continue
    [ -z "$soname" ] \
      || { echo "alternatives: $pkg 子目录里有多个 *.so.* 链接（$soname 与 ${link_path##*/}）" >&2; exit 1; }
    soname=${link_path##*/}
  done
  [ -n "$soname" ] \
    || { echo "alternatives: $pkg 子目录里没有 *.so.* 链接（包布局变化？）" >&2; exit 1; }

  # 相对链接而非绝对 ${PREFIX}：产物层的挂载点由玲珑决定，写死绝对路径会在挂载点变化时再次断掉。
  rel="$sub/$name"
  install -Dm644 "$real" "${PREFIX}/$LIBDIR/$rel"
  ln -sfn "$rel" "${PREFIX}/$LIBDIR/$soname"
  # 容器 /usr 是合并的源：改这里才决定最终进包的是哪一份。
  ln -sfn "$rel" "/usr/$LIBDIR/$soname"
  rm -rf "$tmp"
  echo "alternatives: 交付 $pkg $ver（$soname → $rel，产物树与容器 /usr 同步）"
}

place_one libblas3 blas
place_one liblapack3 lapack

# 兜底：产物树里若还有别的 alternatives 断链（合并已先落地等情形），统一改成相对链接。
# 链接改写的逻辑与用例在 repair-alternatives-links.sh 那边，这里不重写第二份。
sh "$LL_DIR/repair-alternatives-links.sh" "$PREFIX"

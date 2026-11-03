#!/bin/sh
# 容器内安装：把宿主构建好的 spike 二进制放进 ${PREFIX}，并把 WebKitGTK 6.0 的
# 实体从 apt 候选 .deb 里解出来、打完 exec-path 补丁后再交付。
#
# 为什么从 .deb 里解而不复制构建容器的 /usr：该 overlay 的写入不落盘（apt 报成功、
# /usr 下仍是基础层旧实体，upperdir 里可能只剩字符设备），复制等于把旧库打进包，
# 而构建日志完全看不出来。这个坑 desktop-launcher 已经踩过（AUDIT N19），此处照抄
# 同样的做法。
#
# helper 进程本体由 buildext 的 depends 合并进来（libwebkitgtk-6.0-4），
# 这里只负责被补丁替换过的那一个 .so 与运行期包装入口。
set -eu

STAGE=${1:-/project/apps/linyaps/desktop-v3-spike}
PREFIX=${PREFIX:?PREFIX 未设置：必须在 linglong build 段内运行}

mkdir -p "${PREFIX}/bin" "${PREFIX}/lib/x86_64-linux-gnu"

[ -f "${STAGE}/bin/dsh-v3-spike" ] || {
  echo "install-v3-runtime: 缺少 ${STAGE}/bin/dsh-v3-spike（先在宿主机执行 make build）" >&2
  exit 1
}
install -m755 "${STAGE}/bin/dsh-v3-spike" "${PREFIX}/bin/dsh-v3-spike"
install -m755 "${STAGE}/linglong/run-spike.sh" "${PREFIX}/bin/run-spike"

# WebKitGTK 6.0：解出 .so 并打补丁（见 patch-webkit-exec-path.sh 的说明）。
WEBKIT_PKG=libwebkitgtk-6.0-4
TMP=$(mktemp -d)
( cd "$TMP" && apt-get download "$WEBKIT_PKG" ) || { echo "install-v3-runtime: apt-get download $WEBKIT_PKG 失败" >&2; exit 1; }
set -- "$TMP"/${WEBKIT_PKG}_*.deb
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "install-v3-runtime: .deb 未唯一命中（$# 个: $*）" >&2; exit 1; }
DEB=$1
VER=${DEB##*/}; VER=${VER#${WEBKIT_PKG}_}; VER=${VER%_amd64.deb}
CAND=$(apt-cache policy "$WEBKIT_PKG" | awk '/Candidate:/ { print $2; exit }')
[ "$VER" = "$CAND" ] || { echo "install-v3-runtime: 下载 $VER，候选却是 $CAND" >&2; exit 1; }

dpkg-deb -x "$DEB" "$TMP/x"
set -- "$TMP"/x/usr/lib/x86_64-linux-gnu/libwebkitgtk-6.0.so.4.*
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "install-v3-runtime: .so 未唯一命中为普通文件（$# 个）" >&2; exit 1; }
SO=${1##*/}
cp -a "$1" "${PREFIX}/lib/x86_64-linux-gnu/"
sh "${STAGE}/linglong/patch-webkit-exec-path.sh" "${PREFIX}/lib/x86_64-linux-gnu/${SO}"
ln -sf "${SO}" "${PREFIX}/lib/x86_64-linux-gnu/libwebkitgtk-6.0.so.4"
echo "install-v3-runtime: 交付 WebKitGTK ${VER}（已打 exec-path 补丁）"

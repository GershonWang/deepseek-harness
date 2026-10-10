#!/bin/sh
# 生成 webkit2gtk-4.0.pc shim，指向系统唯一的 webkit2gtk-4.1。
#
# webview_go 在编译期硬编码 `pkg-config: webkit2gtk-4.0`（webview.go:9），
# 但其 C 库运行时优先加载 libwebkit2gtk-4.1.so（webview.h:1418）。
# deepin 25 / beige 只提供 webkit2gtk-4.1，无 4.0 的 .pc 文件，
# 因此构建前必须生成 4.0 shim。头文件路径与链接库均指向 4.1。
#
# 用法：
#   ./prepare-pkgconfig.sh /tmp/dsh-pkgconfig
#   PKG_CONFIG_PATH=/tmp/dsh-pkgconfig go build ...
set -eu

OUT_DIR=${1:?用法: prepare-pkgconfig.sh <输出目录>}
mkdir -p "$OUT_DIR"

cat > "$OUT_DIR/webkit2gtk-4.0.pc" <<'EOF'
prefix=/usr
exec_prefix=${prefix}
libdir=/usr/lib/x86_64-linux-gnu
includedir=${prefix}/include
revision=tarball

Name: WebKitGTK
Description: Web content engine for GTK (4.0 shim -> 4.1)
URL: https://webkitgtk.org
Version: 2.50.4
Requires: glib-2.0 gtk+-3.0 libsoup-3.0 javascriptcoregtk-4.1
Libs: -L${libdir} -lwebkit2gtk-4.1
Cflags: -I${includedir}/webkitgtk-4.1
EOF

echo "prepare-pkgconfig: wrote $OUT_DIR/webkit2gtk-4.0.pc"

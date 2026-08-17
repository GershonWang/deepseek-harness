#!/bin/sh
# 一键构建 linux .deb 程序包:
#   1. 宿主机构建全部产物并暂存(linglong/prepare-offline.sh)
#   2. 按玲珑同款布局组装(安装到 /opt/apps/<id>/files),并内置系统依赖声明
#   3. dpkg-deb 打包,产物为仓库根的 .deb
# 与玲珑包的区别:webkit2gtk/GTK 用宿主系统版本(Depends),不捆绑移植层,
# 无需字节补丁与 /tmp 符号链接。
# 用法: 在仓库根运行
#   sh apps/desktop-launcher/build-deb.sh [--no-prepare]
#   --no-prepare: 跳过 prepare-offline(复用现有 stage/,仅重打包)
set -eu
cd "$(dirname "$0")/../.." # 仓库根

YAML=apps/desktop-launcher/linglong/linglong.yaml
LL_ID=$(grep -oP '^\s+id: \K[0-9a-zA-Z.-]+' "$YAML" | head -1)
LL_VERSION=$(grep -oP '^\s+version: \K[0-9.]+' "$YAML" | head -1)
MNT=$(git config user.name 2>/dev/null || echo "DeepSeek Harness maintainers")
MNT_EMAIL=$(git config user.email 2>/dev/null || echo "noreply@deepseek.com")

STAGE=apps/desktop-launcher/linglong/stage
DEB_ROOT=/opt/apps/$LL_ID/files
DEB_FILE="${LL_ID}_${LL_VERSION}_amd64.deb"
echo "==> 目标 deb 包: $DEB_FILE (安装到 $DEB_ROOT)"

if [ "${1:-}" = "--no-prepare" ]; then
  echo "==> 跳过 prepare-offline(复用现有 stage/)"
else
  sh apps/desktop-launcher/linglong/prepare-offline.sh
fi
[ -x "$STAGE/bin/dsh-desktop-launcher" ] || {
  echo "stage 缺失: 先运行 prepare-offline 或去掉 --no-prepare" >&2; exit 1
}

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/$DEB_ROOT/bin" "$WORK/$DEB_ROOT/harness" "$WORK/$DEB_ROOT/node" \
         "$WORK/DEBIAN" \
         "$WORK/usr/share/applications" \
         "$WORK/usr/share/icons/hicolor/256x256/apps"

cp -a "$STAGE/harness/." "$WORK/$DEB_ROOT/harness/"
cp -a "$STAGE/node/." "$WORK/$DEB_ROOT/node/"
install -m755 "$STAGE/bin/dsh-desktop-launcher" "$WORK/$DEB_ROOT/bin/"
install -m644 apps/desktop-launcher/icons/dsh-desktop.png \
        "$WORK/usr/share/icons/hicolor/256x256/apps/dsh-desktop.png"
# .desktop 复用玲珑同款(Exec 指向 /opt/apps/<id>/files,两包布局一致)
install -m644 apps/desktop-launcher/linglong/com.deepseek.dsh-desktop.desktop \
        "$WORK/usr/share/applications/com.deepseek.dsh-desktop.desktop"

cat > "$WORK/DEBIAN/control" <<EOF
Package: $LL_ID
Version: $LL_VERSION
Section: utils
Priority: optional
Architecture: amd64
Maintainer: $MNT <$MNT_EMAIL>
Depends: libwebkit2gtk-4.1-0, libgtk-3-0, libglib2.0-0, libjavascriptcoregtk-4.1-0
Description: DeepSeek Harness Linux desktop client
 DeepSeek Harness 桌面客户端:以受监护子进程方式运行
 harness,通过系统 webkit2gtk 窗口加载其 Web GUI。
EOF

cat > "$WORK/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
# 刷新应用菜单与图标缓存(装完立即可见);工具缺失或失败不影响安装
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database -q /usr/share/applications 2>/dev/null || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -q -f /usr/share/icons/hicolor 2>/dev/null || true
fi
exit 0
EOF
chmod 755 "$WORK/DEBIAN/postinst"

dpkg-deb --build -Z xz "$WORK" "$DEB_FILE" >/dev/null
echo "==> 完成: $DEB_FILE ($(du -h "$DEB_FILE" | cut -f1))"
echo "==> 安装: sudo dpkg -i $DEB_FILE"
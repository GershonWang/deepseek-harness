#!/bin/sh
# 把随包字体装进层内、注册 fontconfig 目录，并把 CSS 字体栈用到的族名指过去。
#
# 背景（均为实测结论）：
#  1. 玲珑基础镜像清空了 /usr/share/fonts；运行容器的 /usr/share/fonts 又被宿主目录
#     整体挂载覆盖（mountinfo: /share/fonts → /usr/share/fonts, ro），因此字体放进
#     层内的 usr/share/fonts 会被遮住（层内 5 个文件、容器 fc-list 查不到）。
#  2. 层内的 etc/ 同样不进容器命名空间：容器 fc-conflist 只读 base 层那 47 条
#     conf.d，故层内 conf.d 规则不会被读取，必须由启动器以 FONTCONFIG_FILE 注入。
#  3. 只 include 系统配置会让 fontconfig 因「无可写缓存目录」整体加载失败
#     （容器内 /var/cache/fontconfig 是宿主挂载的只读目录），必须显式给 <cachedir>。
#  4. 正文拉丁与中文必须分开成两条：fontconfig 的 <prefer> 是整条 family 的备选
#     列表，不是「仅缺字回退」；若把等宽字体与中文族混进同一条，同一行里会出现
#     两套度量（等宽 7.80 与中文 13.00）并排。
#  5. CSS 栈以 -apple-system 开头，该名字是 fontconfig 内建兜底、别名改不动它；
#     未命中时引擎沿栈回退，而栈尾是 sans-serif——故这里同时注册 sans-serif。
#     （层内可见的其它族名如 BlinkMacSystemFont / PingFang SC 也一并别名，冗余覆盖。）
#
# 中文族不随包：由构建容器的 apt 依赖提供（fonts-wqy-microhei），与 webkit 同一机制。
#
# 用法：sh install-container-fonts.sh <PREFIX> <拉丁字体目录> <等宽字体目录>
set -eu

PREFIX=${1:?用法: install-container-fonts.sh <PREFIX> <拉丁目录> <等宽目录>}
LATIN_SRC=${2:?用法: install-container-fonts.sh <PREFIX> <拉丁目录> <等宽目录>}
MONO_SRC=${3:?用法: install-container-fonts.sh <PREFIX> <拉丁目录> <等宽目录>}
[ -d "$LATIN_SRC" ] || { echo "install-container-fonts: 目录不存在: $LATIN_SRC" >&2; exit 1; }
[ -d "$MONO_SRC" ] || { echo "install-container-fonts: 目录不存在: $MONO_SRC" >&2; exit 1; }

FONTDIR=$PREFIX/share/dsh-fonts
CONFDIR=$PREFIX/etc/fonts/conf.d
CONF=$CONFDIR/99-dsh-fonts.conf
# 启动器把它作为 FONTCONFIG_FILE 交给 fontconfig：层内 etc/ 不进容器命名空间，
# 不能依赖 conf.d 被自动读取，因此这份文件必须自包含（include 系统配置）。
LOADCONF=$PREFIX/etc/fonts/dsh-fonts.conf
CACHEDIR=$PREFIX/var/cache/fontconfig

install -d "$FONTDIR" "$CONFDIR" "$CACHEDIR"
install -m644 "$LATIN_SRC"/*.ttf "$FONTDIR/"
install -m644 "$MONO_SRC"/*.ttf "$FONTDIR/"
# 许可随字体一起分发，避免只在仓库里留存。
for d in "$LATIN_SRC" "$MONO_SRC"; do
  [ -f "$d/OFL.txt" ] || continue
  install -m644 "$d/OFL.txt" "$FONTDIR/OFL-$(basename "$d").txt"
done

# 分片配置：仅列目录。当前不被读出（见背景 2），保留以便未来 conf.d 可用的场景。
printf '%s\n' \
  '<?xml version="1.0"?>' \
  '<!DOCTYPE fontconfig SYSTEM "fonts.dtd">' \
  '<fontconfig>' \
  "  <dir>${FONTDIR}</dir>" \
  '</fontconfig>' > "$CONF"

# 自包含配置：include 系统配置（保留宿主既有的字体解析与中文回退）+ 可写缓存目录
# + 包内字体目录 + 家族别名。
printf '%s\n' \
  '<?xml version="1.0"?>' \
  '<!DOCTYPE fontconfig SYSTEM "fonts.dtd">' \
  '<fontconfig>' \
  '  <include ignore_missing="yes">/etc/fonts/fonts.conf</include>' \
  "  <cachedir>${CACHEDIR}</cachedir>" \
  "  <dir>${FONTDIR}</dir>" \
  '  <!-- 正文：拉丁走 Noto Sans Display，中文由文泉驿微米黑兜底（不与等宽混列） -->' \
  '  <alias binding="strong"><family>sans-serif</family><prefer>' \
  '    <family>Noto Sans Display</family>' \
  '    <family>WenQuanYi Micro Hei</family>' \
  '  </prefer></alias>' \
  '  <alias binding="strong"><family>BlinkMacSystemFont</family><prefer>' \
  '    <family>Noto Sans Display</family>' \
  '    <family>WenQuanYi Micro Hei</family>' \
  '  </prefer></alias>' \
  '  <alias binding="strong"><family>PingFang SC</family><prefer>' \
  '    <family>Noto Sans Display</family>' \
  '    <family>WenQuanYi Micro Hei</family>' \
  '  </prefer></alias>' \
  '  <alias binding="strong"><family>Hiragino Sans GB</family><prefer>' \
  '    <family>Noto Sans Display</family>' \
  '    <family>WenQuanYi Micro Hei</family>' \
  '  </prefer></alias>' \
  '  <alias binding="strong"><family>Microsoft YaHei</family><prefer>' \
  '    <family>Noto Sans Display</family>' \
  '    <family>WenQuanYi Micro Hei</family>' \
  '  </prefer></alias>' \
  '  <alias binding="strong"><family>Helvetica Neue</family><prefer>' \
  '    <family>Noto Sans Display</family>' \
  '  </prefer></alias>' \
  '  <!-- 等宽/代码：Linux 上不存在的名字统一指到 JetBrains Mono -->' \
  '  <alias binding="strong"><family>SF Mono</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <alias binding="strong"><family>Fira Code</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <alias binding="strong"><family>Menlo</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <alias binding="strong"><family>Consolas</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <alias binding="strong"><family>monospace</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '</fontconfig>' > "$LOADCONF"

echo "install-container-fonts: $(ls "$FONTDIR" | tr '\n' ' ')→ $FONTDIR"
echo "install-container-fonts: 配置 $CONF 与 $LOADCONF"

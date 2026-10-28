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
#  5. 正文段必须与等宽段一样排在 include 之前。fontconfig 的强别名按「先执行的
#     规则排在 family 列表更前」生效，而系统的 60-latin/49-sansserif 已经把
#     generic sans-serif 指到宿主字体；正文段写在 include 之后时，sans-serif 与
#     BlinkMacSystemFont 的别名会被它压过（实测 fc-match ':family=sans-serif'
#     返回思源黑体），前移到 include 之前后同一查询返回 Noto Sans Display。
#     这与宿主是否带 99-deepin.conf 无关：正文观感必须由随包字体决定，不能随
#     分发的机器漂移。
#     上游 CSS 栈以 -apple-system 开头，该名字是 fontconfig 内建兜底、别名改不动它
#     （实测 fc-match 恒返回宿主默认），栈里其后能命中别名的族名才是决定因素；
#     'Segoe UI'、Helvetica、Arial 同样在栈中，也要别名，否则会命中宿主实体字体。
#  6. 内嵌 Web GUI 的侧边栏终端默认栈以 ui-monospace 开头（客户端
#     ui-sidebar-terminal：ui-monospace, SFMono-Regular, Menlo, Consolas,
#     monospace），Shell 里需要等宽的位置也常写 generic monospace。这两个名字在
#     容器里都没有实体字体：fontconfig 对未知名回落到默认族（实测是思源黑体，比例
#     字体），WebKit 照旧把它画进 xterm 按宽字形量出的单元格，字形只占格宽一半、
#     列看着发虚。因此 ui-monospace、SFMono-Regular 与 generic monospace 一并
#     别名到随包 JetBrains Mono；不接管 generic monospace 时它会落到宿主自带的
#     Noto Mono，跨机器表现不一致。
#
# 中文族随包：fontconfig 配置早已把中文指到 WenQuanYi Micro Hei（见下面的 prefer
# 列表），缺的只是字体实体——基础镜像清空了 /usr/share/fonts，运行容器该目录又被宿主
# 挂载覆盖，宿主未必装了中文字体。实体由构建段从 fonts-wqy-microhei 的 .deb 里抽出后
# 作为第 4 个参数传进来（可选；不传则只装拉丁与等宽）。曾经以为它由构建容器的 apt
# 依赖提供，实测不成立——apt 装进来的字体在 /usr 下，而 /usr 不随 layer 导出
# （AUDIT N26）。
#
# 用法：sh install-container-fonts.sh <PREFIX> <拉丁字体目录> <等宽字体目录> [中文字体文件]
set -eu

PREFIX=${1:?用法: install-container-fonts.sh <PREFIX> <拉丁目录> <等宽目录> [中文字体文件]}
LATIN_SRC=${2:?用法: install-container-fonts.sh <PREFIX> <拉丁目录> <等宽目录> [中文字体文件]}
MONO_SRC=${3:?用法: install-container-fonts.sh <PREFIX> <拉丁目录> <等宽目录> [中文字体文件]}
CJK_SRC=${4:-}
[ -d "$LATIN_SRC" ] || { echo "install-container-fonts: 目录不存在: $LATIN_SRC" >&2; exit 1; }
[ -d "$MONO_SRC" ] || { echo "install-container-fonts: 目录不存在: $MONO_SRC" >&2; exit 1; }

FONTDIR=$PREFIX/share/dsh-fonts
# 启动器把它作为 FONTCONFIG_FILE 交给 fontconfig：层内 etc/ 不进容器命名空间，
# 不能依赖 conf.d 被自动读取，因此这份文件必须自包含（include 系统配置）。
LOADCONF=$PREFIX/etc/fonts/dsh-fonts.conf
CACHEDIR=$PREFIX/var/cache/fontconfig

install -d "$FONTDIR" "$CACHEDIR" "$(dirname "$LOADCONF")"
install -m644 "$LATIN_SRC"/*.ttf "$FONTDIR/"
install -m644 "$MONO_SRC"/*.ttf "$FONTDIR/"
# 中文族：单个 .ttc（文泉驿微米黑），随包后由上面 prefer 列表里的族名命中。缺文件是
# 构建配置错误（构建段应当已从 .deb 抽出），因此硬失败而不是静默跳过——静默跳过正是
# 「以为中文族有人管、实际没有」的老毛病。
if [ -n "$CJK_SRC" ]; then
  [ -f "$CJK_SRC" ] || { echo "install-container-fonts: 中文字体文件不存在: $CJK_SRC" >&2; exit 1; }
  install -m644 "$CJK_SRC" "$FONTDIR/"
fi
# 许可随字体一起分发，避免只在仓库里留存。
for d in "$LATIN_SRC" "$MONO_SRC"; do
  [ -f "$d/OFL.txt" ] || continue
  install -m644 "$d/OFL.txt" "$FONTDIR/OFL-$(basename "$d").txt"
done

# 自包含配置：可写缓存目录 + 包内字体目录 + 等宽别名 + 正文别名 → include 系统配置
# （保留宿主其余族名与中文回退）。
#
# 为什么两段都写在 include 之前：fontconfig 的强别名按「先执行的规则排在 family
# 列表更前面」生效（实测：同一条 generic monospace 别名写在 include 之前命中
# JetBrains Mono，写在之后则输给系统 60-latin.conf 给出的 Noto Mono；正文段同理，
# 写在之后时 sans-serif 输给系统规则指到的宿主字体）。要它们让位只能先写。
printf '%s\n' \
  '<?xml version="1.0"?>' \
  '<!DOCTYPE fontconfig SYSTEM "fonts.dtd">' \
  '<fontconfig>' \
  "  <cachedir>${CACHEDIR}</cachedir>" \
  "  <dir>${FONTDIR}</dir>" \
  '  <!-- 等宽/代码：Linux 上不存在的名字统一指到 JetBrains Mono。ui-monospace 与
       generic monospace 是客户端终端栈的头尾，不接管就会落到 fontconfig 默认族
       （思源黑体，比例字体）或宿主自带等宽字体。本段先于 include，见上方说明。 -->' \
  '  <alias binding="strong"><family>SF Mono</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <alias binding="strong"><family>Fira Code</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <alias binding="strong"><family>Menlo</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <alias binding="strong"><family>Consolas</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <alias binding="strong"><family>ui-monospace</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <alias binding="strong"><family>SFMono-Regular</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <alias binding="strong"><family>monospace</family><prefer><family>JetBrains Mono</family></prefer></alias>' \
  '  <!-- 正文：拉丁走 Noto Sans Display，中文由文泉驿微米黑兜底（不与等宽混列）。
       打包流程注入的 dsh-font-stack.css 把随包族名前置到 CSS 栈首，本段兜住
       CSS 未点名时的族名（用户自定义字体、插件里的样式）。 -->' \
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
  '  <alias binding="strong"><family>Segoe UI</family><prefer><family>Noto Sans Display</family></prefer></alias>' \
  '  <alias binding="strong"><family>Helvetica</family><prefer><family>Noto Sans Display</family></prefer></alias>' \
  '  <alias binding="strong"><family>Arial</family><prefer><family>Noto Sans Display</family></prefer></alias>' \
  '  <include ignore_missing="yes">/etc/fonts/fonts.conf</include>' \
  '</fontconfig>' > "$LOADCONF"

echo "install-container-fonts: $(ls "$FONTDIR" | tr '\n' ' ')→ $FONTDIR"
echo "install-container-fonts: 配置 $LOADCONF"

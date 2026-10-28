#!/bin/sh
# 把壳内 Web GUI 的字体栈覆盖样式注入打包产物 dist：
#   1) 复制 dsh-font-stack.css 到 dist/assets/（与页面同源，frontend-static 直接服务）
#   2) 在 dist/index.html 的 </head> 前追加 <link>（幂等：已含标记则跳过）
# 宿主侧在 prepare-offline.sh 的 dsh deploy 与链接桥注入之后调用；容器内只做组装，
# 不再碰 dist。
#
# 为什么用注入而不是改上游 ui-theme 的 base.css：那是发行版差异（随包字体只在玲珑
# 容器里存在），留在本模块；上游文件保持原样，fork 偏离面不扩大。这条样式把随包族名
# 前置到默认栈首，避免「包里装了字体、页面却不引用它」——上游默认栈只写系统族名，
# 容器里最终用哪个字体由宿主 fontconfig 决定，正文观感因此随机器漂移，历史上有过
# 字重/度量不一致造成的重叠错位。
#
# 用法: inject-font-stack.sh <dist-dir>
#   例: sh apps/linyaps/desktop-launcher/linglong/inject-font-stack.sh \
#       apps/linyaps/desktop-launcher/linglong/stage/harness/node_modules/@deepseek-ai/dsh-web-frontend/dist
set -eu

DIST=${1:?usage: inject-font-stack.sh <dist-dir>}
STYLE=$(dirname "$0")/dsh-font-stack.css
INDEX="$DIST/index.html"
MARKER='dsh-font-stack.css'

if [ ! -f "$INDEX" ]; then
  echo "inject-font-stack: $INDEX 不存在（dist 结构变化？），中止构建" >&2
  exit 1
fi
if [ ! -f "$STYLE" ]; then
  echo "inject-font-stack: $STYLE 不存在，中止构建" >&2
  exit 1
fi

mkdir -p "$DIST/assets"
cp "$STYLE" "$DIST/assets/dsh-font-stack.css"
chmod 644 "$DIST/assets/dsh-font-stack.css"

if grep -q "$MARKER" "$INDEX"; then
  echo "inject-font-stack: $INDEX 已含字体栈标签，跳过"
else
  # 插到 </head> 之前而不是 </body> 之前：样式要在首屏绘制前就位，避免先用系统字体
  # 画一帧再切换。找不到 </head> 说明 dist 结构变了，硬失败而不是静默跳过。
  if ! grep -q '</head>' "$INDEX"; then
    echo "inject-font-stack: $INDEX 缺少 </head>（dist 结构变化？），中止构建" >&2
    exit 1
  fi
  sed -i "s#</head>#  <link rel=\"stylesheet\" href=\"/assets/dsh-font-stack.css\">\n</head>#" "$INDEX"
  echo "inject-font-stack: 已注入到 $INDEX"
fi

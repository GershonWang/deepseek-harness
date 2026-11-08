#!/bin/sh
# 把壳内 Web GUI 的 WebKit 渲染降级样式注入打包产物 dist：
#   1) 复制 dsh-webkit-perf.css 到 dist/assets/（与页面同源，frontend-static 直接服务）
#   2) 在 dist/index.html 的 </head> 前追加 <link>（幂等：已含标记则跳过）
# 宿主侧在 prepare-offline.sh 的字体栈注入之后调用；容器内只做组装，不再碰 dist。
#
# 为什么需要（与 dsh-font-stack.css 同一机制、不同诉求）：官方 Electron 版与浏览器版
# 流畅、Wails 的 WebKitGTK 6.0 壳卡顿，差异来自引擎对布局属性动画的处理成本。降级
# 规则放在打包侧，上游 packages/client 保持原样，fork 并入上游时不产生冲突。
#
# 用法: inject-webkit-perf.sh <dist-dir>
#   例: sh apps/linyaps/desktop-launcher/linglong/inject-webkit-perf.sh \
#       apps/linyaps/desktop-launcher/linglong/stage/harness/node_modules/@deepseek-ai/dsh-web-frontend/dist
#
# 对照实验：DSH_WEBKIT_PERF=0 时整体跳过注入，用于在同一台机器上比较注入前后的
# 渲染开销（构建与启动都不报错，只是不注入）。
set -eu

DIST=${1:?usage: inject-webkit-perf.sh <dist-dir>}
STYLE=$(dirname "$0")/dsh-webkit-perf.css
INDEX="$DIST/index.html"
MARKER='dsh-webkit-perf.css'

if [ ! -f "$INDEX" ]; then
  echo "inject-webkit-perf: $INDEX 不存在（dist 结构变化？），中止构建" >&2
  exit 1
fi
if [ ! -f "$STYLE" ]; then
  echo "inject-webkit-perf: $STYLE 不存在，中止构建" >&2
  exit 1
fi

# 段选择：未设或 all 注入全部段；0/none 不注入（对照实验）；其余按逗号分隔的段名
# 只注入命中的段，用于逐条对比与回滚。
SELECT=${DSH_WEBKIT_PERF:-all}
case "$SELECT" in
  0 | none)
    echo "inject-webkit-perf: DSH_WEBKIT_PERF=$SELECT，跳过注入（对照实验）"
    exit 0
    ;;
  all) SELECT='layout-transition,content-visibility' ;;
esac

mkdir -p "$DIST/assets"
# 每条降级独立成段，这里只抽取选中的段：既保证产物最小，也让「关掉某一条」不必改
# 样式文件本身（构建期开关，launcher 运行期不读这个变量）。
{
  echo "/* 由 inject-webkit-perf.sh 生成，段: $SELECT */"
  awk -v want=",$SELECT," '
    /^\/\* @perf:begin / {
      name = $0
      sub(/^\/\* @perf:begin /, "", name)
      sub(/ \*\/$/, "", name)
      on = index(want, "," name ",") > 0
      next
    }
    /^\/\* @perf:end / { on = 0; next }
    on { print }
  ' "$STYLE"
} > "$DIST/assets/dsh-webkit-perf.css"

# 产物里至少要有一条规则：只剩头注释说明段名没命中（拼写错误），按硬失败处理。
if ! grep -q '{' "$DIST/assets/dsh-webkit-perf.css"; then
  echo "inject-webkit-perf: DSH_WEBKIT_PERF=$SELECT 未命中任何样式段（段名拼错？），中止构建" >&2
  exit 1
fi
chmod 644 "$DIST/assets/dsh-webkit-perf.css"

if grep -q "$MARKER" "$INDEX"; then
  echo "inject-webkit-perf: $INDEX 已含降级样式标签，跳过"
else
  # 插到 </head> 之前：样式要在首屏绘制前就位。找不到 </head> 说明 dist 结构变了，
  # 硬失败而不是静默跳过——注入失效的表现是「装得上但依然卡」，构建期失败远好过
  # 用户在真机上才发现。
  if ! grep -q '</head>' "$INDEX"; then
    echo "inject-webkit-perf: $INDEX 缺少 </head>（dist 结构变化？），中止构建" >&2
    exit 1
  fi
  sed -i "s#</head>#  <link rel=\"stylesheet\" href=\"/assets/dsh-webkit-perf.css\">\n</head>#" "$INDEX"
  echo "inject-webkit-perf: 已注入到 $INDEX"
fi

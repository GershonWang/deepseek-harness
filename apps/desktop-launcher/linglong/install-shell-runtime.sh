#!/bin/sh
# 薄壳客户端（Go + Wails）的容器内安装：把 stage/ 的产物铺进 ${PREFIX}，并完成只在
# 玲珑容器里才能做的处理——webkit 实体搬运与 exec-path 字节补丁、容器级字体、工具
# 清单注入、node-pty 回退，以及 node/pnpm/dsh 的 PATH 包装。
#
# 为什么单独成脚本：双客户端包（com.deepseek.dsh-desktop-dual）里也含薄壳客户端，
# 它需要完全相同的一套处理。两份内联的话，webkit 补丁这类「漏掉就是装得上但 GUI 起
# 不来、且构建期毫无报错」的步骤迟早会在其中一份里漂移——本仓库已经踩过一次。
#
# 用法（在 linglong.yaml 的 build 段内，${PREFIX} 由玲珑提供）：
#   sh /project/apps/desktop-launcher/linglong/install-shell-runtime.sh <stage 目录>
#
# 刻意不加 set -eu：以下逐条照搬原先内联在 linglong.yaml 里的写法，其中若干步骤用
# `|| true` 容错；改成硬失败会改变已验证过的构建行为。
STAGE=${1:?usage: install-shell-runtime.sh <stage-dir>}
# 捆绑的 Node 版本与 pnpm 包装器都读同一份单一来源（与 prepare-offline.sh 共用；
# 正常构建走 prepare-offline，这里只是 stage 缺失时的 fallback）。
NODE_VERSION=$(tr -d '[:space:]' < /project/apps/desktop-launcher/linglong/node-version.txt)
if [ ! -x "$STAGE/bin/dsh-desktop-launcher" ]; then
  echo "stage 缺失：先在仓库根运行 sh apps/desktop-launcher/linglong/prepare-offline.sh" >&2
  exit 1
fi
# 依赖硬校验放在最前：buildext 生成的 apt 命令行以 `|| echo "$?"` 结尾，apt 失败
# 不中止构建；实测还遇到 apt 报告成功但写入未落盘（overlay upperdir 只剩字符设备
# *.dpkg-new）。两种情况下后面每一步都会照常跑、照常出包，只是包内沿用基础层旧
# 依赖。这里提前失败，避免把静默缺陷带进产物（AUDIT N18/N19）。
# 只校验 build_depends：depends 要到 build 段之后（preCommit 合并）才装进 $PREFIX，
# 本阶段看不到它们，要求它们已装会让每次构建都失败；depends 的落点由宿主侧的
# verify-merged-deps.sh 在合并产物树上校验（build-linglong.sh 在 export 前调用）。
sh /project/apps/desktop-launcher/linglong/verify-container-deps.sh
# harness 运行时需要 Node >=24（node:zlib.createZstdDecompress、
# Promise.withResolvers、node:module.stripTypeScriptTypes），
# beige 的 nodejs 只有 20.15.1，必须捆绑 Node 24。
# 复用宿主机 prepare 阶段下载的 Node 二进制（npmmirror 镜像）。
if [ ! -x "$STAGE/node/bin/node" ]; then
  unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
  wget -q -O /tmp/node24.tar.gz "https://registry.npmmirror.com/-/binary/node/v$NODE_VERSION/node-v$NODE_VERSION-linux-x64.tar.gz"
  mkdir -p "$STAGE/node"
  tar -xzf /tmp/node24.tar.gz -C "$STAGE/node" --strip-components=1
fi
# pnpm 出厂内置（离线可用）：prepare-offline 已把 pnpm 装入
# stage/node/lib/node_modules/pnpm（版本取 package.json 的 packageManager）；
# stage 缺失时才在构建容器内下载。corepack 首启需联网且缓存落 $HOME/.cache，
# 容器内只读 HOME 下不可靠，故不依赖 corepack。
if [ ! -f "$STAGE/node/lib/node_modules/pnpm/bin/pnpm.mjs" ]; then
  PNPM_V=$(node -e "console.log(require('/project/package.json').packageManager.split('@')[1])")
  unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
  wget -q -O /tmp/pnpm.tgz "https://registry.npmmirror.com/pnpm/-/pnpm-$PNPM_V.tgz"
  mkdir -p "$STAGE/node/lib/node_modules/pnpm"
  tar -xzf /tmp/pnpm.tgz -C "$STAGE/node/lib/node_modules/pnpm" --strip-components=1
fi
# 薄包装：内容见 pnpm-wrapper.sh（与宿主侧 prepare-offline.sh 共用同一份）。
if [ -f "$STAGE/node/lib/node_modules/pnpm/bin/pnpm.mjs" ]; then
  cp /project/apps/desktop-launcher/linglong/pnpm-wrapper.sh "$STAGE/node/bin/pnpm"
  chmod +x "$STAGE/node/bin/pnpm"
fi
# Node 运行时瘦身：删掉运行时不需要的头文件，strip 二进制剥符号。
# 容器内 fallback 下载的 node 也做同样处理，保证 stage 有或没有都一致。
if [ -d "$STAGE/node/include" ]; then
  rm -rf "$STAGE/node/include"
fi
if command -v strip >/dev/null 2>&1 && [ -x "$STAGE/node/bin/node" ]; then
  strip --strip-unneeded "$STAGE/node/bin/node" 2>/dev/null || true
fi
mkdir -p ${PREFIX}/bin ${PREFIX}/harness ${PREFIX}/node
cp -a "$STAGE/harness/." ${PREFIX}/harness/
# 容器工具清单注入：把工具链段落追加进 standard 预设的 persona，使打包会话的模型
# 可见输入带上可用工具清单。上游已把预设改成 bundle 内的声明式 patch，注册行由随包
# dsh-web-app/presets/standard.patch.yml 发出，覆盖目标就是它；cordis 的补丁语义是
# 整体替换目标属性（vendor/include/src/index.ts），只补一条 persona 行不生效。
#
# 覆盖内容在容器组装前由 gen-preset-overlay.mjs 从该文件原文派生：roster 永远等于
# 上游，唯一自研增量是那段容器说明。必须写进包内 shipped 位置，不能另建
# ${PREFIX}/harness/config——bundle 的 patch 路径相对包目录解析，另建目录不会被扫描。
PRESETS=${PREFIX}/harness/node_modules/@deepseek-ai/dsh-web-app/presets
if [ ! -f "$PRESETS/standard.patch.yml" ]; then
  echo "预设包布局变化：$PRESETS/standard.patch.yml 不存在，overlay 无法注入" >&2
  exit 1
fi
install -Dm644 /project/apps/desktop-launcher/linglong/stage/preset-overlay/standard.patch.yml \
  "$PRESETS/standard.patch.yml"
cp -a "$STAGE/node/." ${PREFIX}/node/
# node/pnpm 上 PATH:玲珑只自动把 $PREFIX/bin 加进容器 PATH($PREFIX/node/bin 不在)。
# harness 自身经 $PREFIX/node/bin/node 绝对路径启动,但模型 bash 工具需要 PATH 上的
# node/corepack(pnpm);软链进 $PREFIX/bin 后容器内任何进程都能直接调用。
ln -sf ../node/bin/node ${PREFIX}/bin/node
ln -sf ../node/bin/corepack ${PREFIX}/bin/corepack
ln -sf ../node/bin/npm ${PREFIX}/bin/npm
ln -sf ../node/bin/npx ${PREFIX}/bin/npx
# pnpm 上 PATH：优先出厂内置 CLI（离线可用）；无内置时退回 corepack
# 薄包装（首次调用需联网下载 pnpm）。
if [ -f "$PREFIX/node/bin/pnpm" ]; then
  ln -sf ../node/bin/pnpm ${PREFIX}/bin/pnpm
else
  printf '%s\n' '#!/bin/sh' 'DIR=$(dirname "$0")' \
    'exec "$DIR/../node/bin/corepack" pnpm "$@"' > ${PREFIX}/bin/pnpm
  chmod +x ${PREFIX}/bin/pnpm
fi
# dsh 上 PATH：官方 CLI 就是 harness/lib/bin.js（package.json 的 bin.dsh 指向它），
# 玲珑只自动加 $PREFIX/bin，不会生成命令包装。薄包装用捆绑 Node 24 执行全部
# dsh 子命令（plugin、--profile、web 等），容器内（含模型 bash/终端）可直接使用
# 官方命令安装/管理插件。
if [ -x "${PREFIX}/harness/lib/bin.js" ]; then
  printf '%s\n' '#!/bin/sh' 'SELF=$(readlink -f "$0" 2>/dev/null || echo "$0")' \
    'DIR=$(dirname "$SELF")' \
    'exec "$DIR/../node/bin/node" "$DIR/../harness/lib/bin.js" "$@"' > ${PREFIX}/bin/dsh
  chmod +x ${PREFIX}/bin/dsh
fi
# node-pty 原生 addon:prepare-offline 已用捆绑 Node 24 在宿主编译
# build/Release/pty.node(ABI 精确匹配,运行时沙箱无 gcc/make,绝不触发
# node-gyp 源码编译)。若该产物缺失(如 --no-prepare 复用旧 stage),才回退
# 复制 prebuilds 里的 pty.node;不覆盖已编译的产物。
mkdir -p ${PREFIX}/harness/node_modules/node-pty/build/Release
if [ ! -f ${PREFIX}/harness/node_modules/node-pty/build/Release/pty.node ] \
   && [ -f ${PREFIX}/harness/node_modules/node-pty/prebuilds/linux-x64/pty.node ]; then
  cp ${PREFIX}/harness/node_modules/node-pty/prebuilds/linux-x64/pty.node \
     ${PREFIX}/harness/node_modules/node-pty/build/Release/pty.node
fi
install -m755 "$STAGE/bin/dsh-desktop-launcher" ${PREFIX}/bin/dsh-desktop-launcher
for s in 16 22 24 32 48 64 128 256 512; do
  install -Dm644 /project/apps/desktop-launcher/icons/hicolor/${s}x${s}/apps/dsh-desktop.png \
    ${PREFIX}/share/icons/hicolor/${s}x${s}/apps/dsh-desktop.png
done
# 桌面集成：.desktop 文件（generateEntries 会复制到 entries/share/applications，
# 玲珑启动器/应用菜单据此显示程序与图标）
install -Dm644 /project/apps/desktop-launcher/linglong/com.deepseek.dsh-desktop.desktop ${PREFIX}/share/applications/com.deepseek.dsh-desktop.desktop
# 字节补丁 webkit helper 路径：Debian 正式构建把 helper 目录硬编码为
# /usr/lib/x86_64-linux-gnu/webkit2gtk-4.1（WEBKIT_EXEC_PATH 需 DEVELOPER_MODE
# 才有效，运行时 /usr 只读、layer 不导出 /usr）。替换为 /tmp/dsh-webkit-4.1，
# launcher 启动时建符号链接指向 ${PREFIX} 下的真实 helper。
#
# webkit 实体从 apt 候选版本的 .deb 里自行解出来，**不复制构建容器 /usr 下的实体**：
# 该 overlay 写入不落盘——apt 报告 Unpacking/Setting up 成功，/usr 下的实体却停在
# 基础层旧版本，upperdir 里只剩 c 0,0 的 *.dpkg-new（AUDIT N19）。原先从 /usr 复制
# 等于把基础层的旧库打进包，而日志上完全看不出来。apt-get download + dpkg-deb -x
# 得到的是普通文件，绕开这条路；补丁与符号链接照旧。
#
# 版本由 apt 候选决定（候选变了这里跟着走），并显式比对「下载到的版本 == 候选」，
# 避免下载成功却拿到别的版本这种静默偏差。交付的是哪一版会打印出来。
mkdir -p ${PREFIX}/lib/x86_64-linux-gnu
WEBKIT_PKG=libwebkit2gtk-4.1-0
WEBKIT_TMP=$(mktemp -d)
( cd "$WEBKIT_TMP" && apt-get download "$WEBKIT_PKG" ) \
  || { echo "webkit: apt-get download $WEBKIT_PKG 失败" >&2; exit 1; }
set -- "$WEBKIT_TMP"/${WEBKIT_PKG}_*.deb
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "webkit: .deb 未唯一命中（$# 个: $*）" >&2; exit 1; }
WEBKIT_DEB=$1
WEBKIT_VER=${WEBKIT_DEB##*/}; WEBKIT_VER=${WEBKIT_VER#${WEBKIT_PKG}_}; WEBKIT_VER=${WEBKIT_VER%_amd64.deb}
WEBKIT_CAND=$(apt-cache policy "$WEBKIT_PKG" | awk '/Candidate:/ { print $2; exit }')
[ "$WEBKIT_VER" = "$WEBKIT_CAND" ] \
  || { echo "webkit: 下载到 $WEBKIT_VER，apt 候选却是 $WEBKIT_CAND" >&2; exit 1; }
dpkg-deb -x "$WEBKIT_DEB" "$WEBKIT_TMP/x"
set -- "$WEBKIT_TMP"/x/usr/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0.*
# 只接受唯一命中，且必须是普通文件：`-e` 对字符设备同样为真，而写入未落盘时现场
# 留下的正是字符设备，`cp -a` 会把设备节点复制进产物（AUDIT N19）。
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "webkit: 解包出的实体未唯一命中为普通文件（$# 个: $*）" >&2; exit 1; }
WEBKIT_SO=${1##*/}
cp -a "$1" ${PREFIX}/lib/x86_64-linux-gnu/
sh /project/apps/desktop-launcher/linglong/patch-webkit-exec-path.sh ${PREFIX}/lib/x86_64-linux-gnu/${WEBKIT_SO}
ln -sf ${WEBKIT_SO} ${PREFIX}/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0
ln -sf libwebkit2gtk-4.1.so.0 ${PREFIX}/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so
echo "webkit: 交付 $WEBKIT_VER（来自 apt 候选 .deb，已打 exec-path 补丁）"

# 容器级字体：基础镜像清空了 /usr/share/fonts，运行容器的该目录又被宿主整体
# 挂载覆盖（/share/fonts → /usr/share/fonts, ro），所以字体不能放进层内的
# usr/share/fonts——实测层内 5 个文件、容器 fc-list 查不到，即被该挂载遮住。
# 改落 ${PREFIX}/share/dsh-fonts：拉丁用 Noto Sans Display、等宽用 JetBrains Mono
# （都在仓库内），中文用文泉驿微米黑（AUDIT N26）。
#
# 中文族从 fonts-wqy-microhei 的 .deb 里自行解出来，**不装进 build_depends、也不从
# 构建容器的 /usr 复制**：apt 装进来的字体在 /usr 下，而 /usr 不随 layer 导出（与
# webkit 当初是同一回事），且 apt 报成功而写入不落盘时复制到的是缺失实体或字符设备
# （AUDIT N19）。apt-get download + dpkg-deb -x 得到的是普通文件，两条路都绕开。
# 产物增大约 5 MB；族名与 install-container-fonts.sh 里 prefer 的
# WenQuanYi Micro Hei 一致（实测 fc-query 报的正是这个族名）。
CJK_PKG=fonts-wqy-microhei
CJK_TMP=$(mktemp -d)
( cd "$CJK_TMP" && apt-get download "$CJK_PKG" ) \
  || { echo "fonts: apt-get download $CJK_PKG 失败" >&2; exit 1; }
set -- "$CJK_TMP"/${CJK_PKG}_*.deb
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "fonts: .deb 未唯一命中（$# 个: $*）" >&2; exit 1; }
dpkg-deb -x "$1" "$CJK_TMP/x"
CJK_TTC="$CJK_TMP/x/usr/share/fonts/truetype/wqy/wqy-microhei.ttc"
[ -f "$CJK_TTC" ] || { echo "fonts: 解包后没有 $CJK_TTC（包布局变化？）" >&2; exit 1; }
sh /project/apps/desktop-launcher/linglong/install-container-fonts.sh \
  "$PREFIX" \
  /project/apps/desktop-launcher/frontend/vendor/fonts/noto-sans-display \
  /project/apps/desktop-launcher/frontend/vendor/fonts/jetbrains-mono \
  "$CJK_TTC"

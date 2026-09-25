#!/bin/sh
# 在宿主机构建全部产物并暂存到 stage/，玲珑构建只做组装。
#
# 背景：玲珑构建容器环境问题多（Debian npm 代理 bug、无 HOME、beige 无
# Node 22、tsdown 在 Node 22 下加载配置失败），重工具链全部在宿主机跑，
# 容器内只复制组装。源码改动后需重新运行本脚本再打包。
#
# 用法：在仓库根运行
#   sh apps/desktop-launcher/linglong/prepare-offline.sh
set -eu
ROOT=$(pwd)
cd "$(dirname "$0")/../../.."   # 仓库根

# 捆绑的 Node 版本（唯一事实来源，全脚本引用此变量）。
# 升级版本只需改这里 + linglong.yaml 的 fallback 下载 URL（两者保持一致）。
NODE_VERSION="24.9.0"

STAGE=apps/desktop-launcher/linglong/stage
rm -rf "$STAGE"
mkdir -p "$STAGE/bin"

# 1. harness 全量构建（lib + web 前端）
pnpm install --frozen-lockfile
pnpm run build

# 1.1 doctor 单独构建。它已迁到 apps/desktop-launcher/doctor，既不是 pnpm
#     workspace 成员（apps/* 只匹配一级），也不在 tsdown 的 workspace globs 里，
#     所以上一步不会编译它。构建必须跟在根构建之后：doctor 的 project references
#     指向 packages/ 与 vendor/ 下的包，那些产物要先就位（doctor 不代建它们）。
node apps/desktop-launcher/tools/doctor-build.mjs

# 2. deploy dsh 闭包并修复（peer deps、符号链接实体化、legacy hoists）
#    用 --config.node-linker=hoisted + 默认 auto-install-peers 让 pnpm 尽量
#    装全 peer deps；legacy 模式下仍会漏掉纯 peer-only 的 workspace 包
#    （动态插件架构下很多包只以 peerDep 存在），在 2.1 步统一补齐。
#    allowUnusedPatches：仓库根的 patchedDependencies 还声明了上游 Electron
#    桌面壳（electron-builder → @electron/osx-sign）用的补丁，而本步只部署
#    @deepseek-ai/dsh 的生产闭包，闭包里没有它；pnpm 11 对未被使用的补丁是
#    硬报错（ERR_PNPM_UNUSED_PATCH），因此在这一步显式放行，只影响本步骤。
#    另需借出仓库根的 pnpm 工作区状态再原样交还：deploy 会在仓库根把它改写成
#    prod + hoisted，而仓库根的 node_modules 始终是第 1 步那个 dev + isolated
#    安装（deploy 只写 $STAGE/harness，不动它的布局）。不交还的话，此后仓库根
#    任何 `pnpm run` 都会拿这份失真快照判定依赖失配，去跑要先删掉 isolated
#    node_modules 的 `pnpm install --production`，无 TTY 时即中止。
WORKSPACE_STATE="node_modules/.pnpm-workspace-state-v1.json"
STATE_BACKUP=""
if [ -f "$WORKSPACE_STATE" ]; then
  STATE_BACKUP=$(mktemp)
  cp -p "$WORKSPACE_STATE" "$STATE_BACKUP"
fi

pnpm --filter @deepseek-ai/dsh deploy --legacy --prod \
  --config.node-linker=hoisted \
  --config.allowUnusedPatches=true \
  "$STAGE/harness"
node scripts/fix-deploy-closure.mjs "$STAGE/harness"

if [ -n "$STATE_BACKUP" ]; then
  cp -p "$STATE_BACKUP" "$WORKSPACE_STATE"
  rm -f "$STATE_BACKUP"
fi

# 2.0.5 生产闭包瘦身：删掉确定不是 runtime 依赖的大包。
#    typescript：运行时全是 .js，不需要 ts 编译器（~24 MB）。
#    注意：不要删 @img —— sharp 是 @deepseek-ai/dsh-attachment-local 的
#    硬依赖（静态 import sharp），而 sharp 的 dist/colour.mjs 运行时静态
#    import '@img/colour'；@img 下只有 colour + linux-x64 原生包（libvips
#    约 18 MB），全部是 linux x64 运行必需，没有可裁的多余平台包。
#    历史教训：曾 rm -rf @img 省 19 MB，结果 harness 启动即
#    ERR_MODULE_NOT_FOUND @img/colour，插件树加载失败（见 harness.log）。
echo "prepare-offline: 生产闭包瘦身..."
if [ -d "$STAGE/harness/node_modules/typescript" ]; then
  rm -rf "$STAGE/harness/node_modules/typescript"
  echo "  - 已删除 typescript"
fi

# 2.1 补装 pnpm deploy --legacy --prod 下被遗漏的 peer-only 包。
#     v0.1.2-alpha.1 起大量包改为 peerDependency + devDependency 模式，
#     deploy --prod 闭包里缺失。遍历 packages/ 和 vendor/ 下所有已构建的
#     包，将闭包里没有的从源码工作区直接复制进去。
#     跳过 test-support 与 typert-generator（仅开发/构建期用）。
inject_workspace_pkg() {
  pkgdir=$1
  # 分支切换/回退后可能残留只有 node_modules、没有 package.json 的目录；
  # 这类目录不是合法包，必须先做存在性守卫：否则下面的 node 调用报错退出，
  # 其非零状态会在 set -e 下中断整个 prepare-offline 脚本。
  [ -f "$pkgdir/package.json" ] || return 0
  # 跳过 experimental 包（AGENTS.md: excluded from official releases）
  case "$pkgdir" in
    packages/experimental/*|vendor/experimental/*) return 0 ;;
  esac
  pkgname=$(node -e "console.log(require('./$pkgdir/package.json').name)" 2>/dev/null || true)
  [ -z "$pkgname" ] && return 0
  # 跳过非 @deepseek-ai 域的包
  case "$pkgname" in @deepseek-ai/* ) ;; *) return 0 ;; esac
  short=${pkgname#@deepseek-ai/}
  dest="$STAGE/harness/node_modules/@deepseek-ai/$short"
  # 注入条件：目标目录不存在，或已存在但该包声明的入口文件不在（pnpm deploy
  # --prod 闭包可能通过软链创建了空壳目录，但 vendor 包作为 devDependency 不会被
  # deploy 安装实际内容，需要从工作区源码补入）。判据必须取自包自己的
  # package.json：曾经写死 lib/index.js，而 @deepseek-ai/schemastery 的入口是
  # lib/index.cjs —— 守卫恒真、每次都进注入分支，日志假装在补闭包，真正缺文件时
  # 反而永远补不上。
  inject=1
  if [ -d "$dest" ]; then
    # 入口候选 = module/main + exports["."]，去重后逐个比对；一个都没命中才补。
    for rel in $(node -e "
const p = require('./$pkgdir/package.json');
const ex = p.exports && p.exports['.'];
const fromExports = typeof ex === 'string' ? ex : (ex && (ex.import || ex.require));
const list = [p.module, p.main, fromExports].filter((v) => typeof v === 'string' && v);
process.stdout.write([...new Set(list)].join('\n'));
" 2>/dev/null || true); do
      if [ -f "$dest/${rel#./}" ]; then
        inject=0
        break
      fi
    done
  fi
  if [ "$inject" = "1" ] && [ -d "$pkgdir/lib" ]; then
    echo "prepare-offline: injecting $pkgname from $pkgdir"
    mkdir -p "$dest/lib"
    # 只拷运行时需要的：lib/ + bin/ + package.json + README*
    # 不拷 src/ tests/ tsconfig*.json tsdown.config.* 等开发文件（缩小闭包）
    # 源路径带 /. ：目标 lib/ 已存在时 `cp -a src/lib dest/lib` 会把整个目录当成
    # 子项放进去（产物里多出 lib/lib 的重复内容）。
    cp -a "$pkgdir/lib/." "$dest/lib/"
    # 这三处不再用 `2>/dev/null || true` 吞掉失败：补不进去时必须让构建失败，
    # 静默跳过只会产出缺文件的闭包，而失败现场已经没有人能看见了。
    if [ -d "$pkgdir/bin" ]; then
      mkdir -p "$dest/bin"
      cp -a "$pkgdir/bin/." "$dest/bin/"
    fi
    cp "$pkgdir/package.json" "$dest/package.json"
    # glob 必须锚定在 $pkgdir：裸 README* 在 CWD（仓库根）展开，匹配到的是仓库
    # 自己的 README，包内那份永远拷不进去。
    for f in "$pkgdir"/README*; do
      if [ -f "$f" ]; then
        cp "$f" "$dest/"
      fi
    done
  fi
}

for pkgdir in packages/*/*/; do
  case "$pkgdir" in
    */test-support/*|*/typert/generator/) continue ;;
  esac
  inject_workspace_pkg "$pkgdir"
done
for vendir in vendor/*/; do
  inject_workspace_pkg "${vendir%/}"
done

# 2.2 stage launcher 私有的 doctor。
#     doctor 已迁到 apps/desktop-launcher/doctor，不再是 pnpm workspace 成员
#     （pnpm-workspace.yaml 的 apps/* 只匹配一级），也不在 @deepseek-ai/dsh 的
#     生产闭包里，因此 deploy 不会带上它，必须显式拷贝。
#     放在 harness 树**内部**而不是旁边：doctor 对 @deepseek-ai/dsh-app-boot 等
#     包的 import 靠 Node 逐级向上查找即可命中 $STAGE/harness/node_modules，
#     无需任何符号链接或额外安装步骤。放到 harness 外面就得自己造一套解析链。
#     只拷构建产物与包清单：doctor 的运行形态就是 lib/types 下的平铺产物，源码、
#     测试与 devDependencies 都不进包。
echo "prepare-offline: stage doctor..."
DOCTOR_DEST="$STAGE/harness/doctor"
rm -rf "$DOCTOR_DEST"
mkdir -p "$DOCTOR_DEST/lib/types"
cp -a apps/desktop-launcher/doctor/lib/types/. "$DOCTOR_DEST/lib/types/"
cp apps/desktop-launcher/doctor/package.json "$DOCTOR_DEST/package.json"

# 缺入口是最难排查的失败形态：构建与打包都成功，launcher 起来后才在运行期报错，
# 而且预检失败按设计不阻塞启动，用户只会看到"检查被跳过"。这里显式断言三个入口。
for entry in index.js cli.js loader-probe.js; do
  if [ ! -f "$DOCTOR_DEST/lib/types/$entry" ]; then
    echo "prepare-offline: doctor 产物缺少 $entry；先运行 node apps/desktop-launcher/tools/doctor-build.mjs" >&2
    exit 1
  fi
done

# 2.5 注入外部链接桥：桌面壳 GUI 里的 target=_blank 外链在 Wails WebKitGTK
#     中开不了新窗口，需在打包的 GUI dist 里注入脚本，把点击 URL 经
#     postMessage 交给启动器（BrowserOpenURL → 随包 xdg-open → 宿主 portal）。
sh apps/desktop-launcher/linglong/inject-link-bridge.sh \
  "$STAGE/harness/node_modules/@deepseek-ai/dsh-web-frontend/dist"

# 3. Go 启动器（wails，webkit2gtk-4.1；用 -tags webkit2_41 显式选 4.1）
#    必须在 module 目录内构建：仓库根没有 go.mod，从根 go build 会报
#    "cannot find main module"
# 注入玲珑包版本到关于弹框(从 linglong.yaml 的 package.version 提取；
# Version 变量在 internal/packaging 包，ldflags 需带完整 import 路径)
LL_VERSION=$(grep -oP '^\s+version: \K[0-9.]+' apps/desktop-launcher/linglong/linglong.yaml | head -1)
( cd apps/desktop-launcher && CGO_ENABLED=1 \
  go build -tags "production webkit2_41" \
  -ldflags "-X github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging.Version=$LL_VERSION" \
  -o "$ROOT/$STAGE/bin/dsh-desktop-launcher" . )

# 4. 捆绑 Node（harness 运行时需要 >=24：node:zlib.createZstdDecompress、
#    Promise.withResolvers、node:module.stripTypeScriptTypes；beige 只有 20 跑不起来）
#    linglong.yaml 组装时直接复用 stage/node，容器内不再下载。
if [ ! -x "$STAGE/node/bin/node" ]; then
  echo "prepare-offline: 下载 Node $NODE_VERSION..."
  unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
  wget -q -O /tmp/node24.tar.gz "https://registry.npmmirror.com/-/binary/node/v$NODE_VERSION/node-v$NODE_VERSION-linux-x64.tar.gz"
  mkdir -p "$STAGE/node"
  tar -xzf /tmp/node24.tar.gz -C "$STAGE/node" --strip-components=1
fi

# 4.5 捆绑 pnpm（随包离线可用）：corepack 首次调用需联网下载 pnpm，且缓存
#     落 $HOME/.cache（容器内可能只读）；改为出厂直连捆绑 CLI。版本取
#     package.json 的 packageManager 字段，保证与仓库锁定的 pnpm 一致。
PNPM_V=$(node -e "console.log(require('./package.json').packageManager.split('@')[1])")
if [ ! -f "$STAGE/node/lib/node_modules/pnpm/bin/pnpm.cjs" ]; then
  echo "prepare-offline: 下载 pnpm $PNPM_V..."
  unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
  wget -q -O /tmp/pnpm.tgz "https://registry.npmmirror.com/pnpm/-/pnpm-$PNPM_V.tgz"
  mkdir -p "$STAGE/node/lib/node_modules/pnpm"
  tar -xzf /tmp/pnpm.tgz -C "$STAGE/node/lib/node_modules/pnpm" --strip-components=1
fi
# node/bin/pnpm 薄包装（路径由脚本位置推导，任意机器一致）。
# 注意：$0 可能经 $PREFIX/bin/pnpm 的软链调用（dirname 只拿到软链目录），
# 先用 readlink -f 解析真实位置（<node>/bin）；pnpm 装在
# <node>/lib/node_modules/pnpm，入口用 bin/pnpm.mjs（pnpm 11 的 bin/pnpm.cjs
# 只是 import('./pnpm.mjs') 兼容存根，真正 CLI 由 pnpm.mjs 加载 ../dist/pnpm.mjs）。
printf '%s\n' '#!/bin/sh' 'SELF=$(readlink -f "$0" 2>/dev/null || echo "$0")' \
  'DIR=$(dirname "$SELF")' \
  'exec "$DIR/node" "$DIR/../lib/node_modules/pnpm/bin/pnpm.mjs" "$@"' \
  > "$STAGE/node/bin/pnpm"
chmod +x "$STAGE/node/bin/pnpm"

# 5. 用捆绑 Node 24 在宿主机预编译 node-pty（运行时沙箱无 gcc/make，
#    一旦触发 node-gyp 源码编译终端就不可用；必须在此编译好 pty.node 打进包）。
#    宿主需有 make/gcc/python3。--nodedir 用捆绑 node 自带头文件，避免联网下载。
if [ -d "$STAGE/harness/node_modules/node-pty" ]; then
  echo "prepare-offline: 编译 node-pty (bundled node $($STAGE/node/bin/node --version))..."
  NODE_GYP="$ROOT/$STAGE/node/lib/node_modules/npm/node_modules/node-gyp/bin/node-gyp.js"
  ( cd "$ROOT/$STAGE/harness" && "$ROOT/$STAGE/node/bin/node" "$NODE_GYP" rebuild \
      --nodedir="$ROOT/$STAGE/node" --directory=node_modules/node-pty )
fi

# 6. 体积瘦身：node-pty 编译完后，运行时不需要的东西统统删掉。
#    - include/ 头文件：运行时用不到，省 ~67 MB
#    - strip node 二进制：剥调试符号，省 ~20-40 MB
#    注意：不删 Node 自带的 npm/npx —— npm 是 Node 官方发行版标准组件，
#    删掉后 lefthook pre-push 的 typecheck（npm run）与用户习惯的 npm/npx
#    都会失效；省 20 MB 不值这些副作用。pnpm 仍是主力包管理器，两者共存。
echo "prepare-offline: 精简 Node 运行时..."
if [ -d "$STAGE/node/include" ]; then
  rm -rf "$STAGE/node/include"
  echo "  - 已删除 node/include/"
fi
if command -v strip >/dev/null 2>&1 && [ -x "$STAGE/node/bin/node" ]; then
  strip --strip-unneeded "$STAGE/node/bin/node" 2>/dev/null || true
  echo "  - 已 strip node 二进制 ($(du -h "$STAGE/node/bin/node" | cut -f1))"
fi

echo "prepare-offline: 产物已暂存到 $STAGE"
echo "  下一步：ll-builder build -f apps/desktop-launcher/linglong/linglong.yaml"

#!/bin/sh
# 在宿主机产出「官方 Electron Desktop 客户端」的 Linux 可运行目录，暂存到 stage/，
# 玲珑构建容器只做复制组装。
#
# 为什么不直接复用 apps/desktop/scripts/package-target.ts：
# 那个入口把目标类型绑在「发布目标」上（DesktopPackageTargetName 只含 mac-arm64 /
# mac-x64 / win-x64），而 Linux 不是发布目标——它没有签名、公证、electron-updater
# 与 COS 上传，分发完全交给玲珑包。把 linux 塞进那套类型会牵动整条上传/更新链路
# （实测一次性引发 12 处类型错误），因此这里只调用它的**底层步骤**。
#
# 步骤顺序与参数取自 package-target.ts 的编排（build:official → release:pack →
# native/system → prepare:runtime → prepare:packages → prepare:dsh →
# electron-builder --dir），上游调整编排时需同步核对本脚本。
#
# 用法：在仓库根运行
#   sh apps/desktop-linglong/prepare-offline.sh
set -eu

LL_DIR=$(cd "$(dirname "$0")" && pwd)
cd "$LL_DIR/../.." # 仓库根

# 目标平台固定 linux-x64。应用 ID 是 electron-builder 的 appId：Linux 没有 .env 平台
# 文件（.env.macos/.env.windows 那套是为签名与公证凭据设计的），该值只能从环境传入。
export DSH_DESKTOP_TARGET_PLATFORM=linux
export DSH_DESKTOP_TARGET_ARCH=x64
export DSH_DESKTOP_APP_ID=com.deepseek.dsh-desktop-official

# 构建路径由 desktop-build-paths.mjs 解析，不在本脚本里重复上游的目录结构，
# 否则上游调整 .desktop-build 布局时这里会静默取到空目录。
eval "$(node -e "
import('./apps/desktop/scripts/desktop-build-paths.mjs').then(m => {
  const paths = m.desktopTargetBuildPaths('linux-x64')
  console.log(Object.entries(paths).map(([key, value]) => 'PATHS_' + key + '=' + JSON.stringify(value)).join('\n'))
})
")"

echo "==> 目标目录: $PATHS_root"

echo "==> 1/9 安装依赖"
pnpm install --frozen-lockfile

echo "==> 2/9 构建仓库（lib + web 前端 + desktop bundle）"
pnpm run build:official

echo "==> 3/9 打包 dsh 包集"
pnpm run release:pack --family dsh --out "$PATHS_packedDsh"
pnpm --dir apps/desktop-host pack --pack-destination "$PATHS_packedDsh"

echo "==> 4/9 打包 vendor 包集"
pnpm run release:pack --family vendor --out "$PATHS_packedVendor"

echo "==> 5/9 构建 native/system 并打包 landlock"
pnpm --dir native/system run build:ts
rm -rf "$PATHS_packedLandlock"
mkdir -p "$PATHS_packedLandlock"
pnpm --dir native/system/packages/entry pack --pack-destination "$PATHS_packedLandlock"

echo "==> 6/9 准备 Electron 与主运行时"
(cd apps/desktop && pnpm run prepare:runtime)

echo "==> 7/9 准备包集"
(cd apps/desktop && pnpm run prepare:packages)

echo "==> 8/9 准备 dsh 树"
(cd apps/desktop && pnpm run prepare:dsh)

echo "==> 9/9 electron-builder 产出 Linux 可运行目录"
(cd apps/desktop && pnpm exec electron-builder \
  --config electron-builder.config.mjs \
  --linux --x64 --publish never --dir)

# electron-builder 的 --dir 在 artifacts 下产出 linux-unpacked/。
UNPACKED="$PATHS_artifacts/linux-unpacked"
if [ ! -d "$UNPACKED" ]; then
  echo "linux-unpacked 未产出（期望 $UNPACKED）；electron-builder 的目录名可能已变" >&2
  exit 1
fi

STAGE=apps/desktop-linglong/stage
rm -rf "$STAGE"
mkdir -p "$STAGE"
cp -a "$UNPACKED/." "$STAGE/"

echo "==> 暂存完成: $STAGE"
du -sh "$STAGE"

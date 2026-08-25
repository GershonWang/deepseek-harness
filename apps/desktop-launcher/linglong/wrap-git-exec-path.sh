#!/bin/sh
# 玲珑合并产物树中随包 git 的 exec-path 修复：buildext 把 Debian git 的文件树
# 搬到 <prefix>（/usr/bin/git -> <prefix>/bin/git、/usr/lib/git-core ->
# <prefix>/lib/git-core），但 git 二进制里编译期写死的 exec-path 仍是
# /usr/lib/git-core，容器内不存在，导致 git-remote-* 等 helper 失联、push/fetch
# 开箱即坏。本脚本把 <prefix>/bin/git 换成薄包装：按自身位置推导前缀并设置
# GIT_EXEC_PATH 后 exec 真 git（真二进制改名 git.real）。路径全部由脚本位置
# 推导，任意机器一致（可移植）。
#
# 用法: wrap-git-exec-path.sh <merged-prefix>   e.g. linglong/output/binary/files
set -eu
PREFIX=${1:?usage: wrap-git-exec-path.sh <merged-prefix>}
BIN="$PREFIX/bin"

if [ ! -x "$BIN/git" ]; then
  echo "OK   git: buildext 未合并 git，跳过包装"
  exit 0
fi
# 幂等判据：git.real 只由本脚本创建（真实 git 二进制含 GIT_EXEC_PATH 字样，
# 不能用 grep 内容作判据）。
if [ -f "$BIN/git.real" ]; then
  echo "OK   git: 已是包装脚本，跳过"
  exit 0
fi

mv "$BIN/git" "$BIN/git.real"
cat > "$BIN/git" <<'WRAPPER'
#!/bin/sh
# 随包 git 包装（wrap-git-exec-path.sh 生成，勿手改）：git 编译期 exec-path
# 指向 /usr/lib/git-core（容器内不存在），按自身位置推导前缀并指到包内
# lib/git-core，保证 git-remote-*、git-credential-* 等 helper 任意机器可用。
DIR=$(dirname "$0")
PREFIX=$(dirname "$DIR")
export GIT_EXEC_PATH="${GIT_EXEC_PATH:-$PREFIX/lib/git-core}"
exec "$DIR/git.real" "$@"
WRAPPER
chmod 755 "$BIN/git"
echo "OK   git: 已包装为 git.real + GIT_EXEC_PATH=$PREFIX/lib/git-core"
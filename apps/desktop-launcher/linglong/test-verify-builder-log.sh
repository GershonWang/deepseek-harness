#!/bin/sh
# verify-builder-log.sh 的通过/失败路径。
# 用法: sh apps/desktop-launcher/linglong/test-verify-builder-log.sh
set -eu

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
VERIFY="$ROOT/apps/desktop-launcher/linglong/verify-builder-log.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# 通过路径：正常构建日志（含 [Install Files]/[Commit Contents] 等正常行）
cat > "$TMP/ok.log" <<'EOF'
[Build] start
[Install Files] copying files
[Commit Contents] done
[Runtime Check] pass
EOF
if out=$("$VERIFY" "$TMP/ok.log" 2>&1); then
  echo "PASS: 正常日志应通过"
else
  echo "FAIL: 正常日志未通过" >&2; echo "$out" >&2; exit 1
fi

# 失败路径：审计记录的原始告警（libwebkit2gtk 复制失败但构建照常完成）
cat > "$TMP/bad.log" <<'EOF'
[Build] start
failed to copy /usr/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0 to linglong/output/binary/files/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0: 无效的参数
[Install Files] copying files
[Commit Contents] done
[Runtime Check] pass
EOF
set +e
out=$("$VERIFY" "$TMP/bad.log" 2>&1)
status=$?
set -e
if [ "$status" -eq 0 ]; then
  echo "FAIL: 含 'failed to copy' 的日志应失败" >&2; exit 1
fi
for needle in "failed to copy" "libwebkit2gtk-4.1.so.0"; do
  case "$out" in
    *"$needle"*) ;;
    *) echo "FAIL: 失败输出缺少「$needle」" >&2; echo "$out" >&2; exit 1 ;;
  esac
done
echo "PASS: 含 'failed to copy' 时非零退出并指明位置"

# 失败路径：日志文件不存在（例如构建器没跑到写日志那一步）
set +e
"$VERIFY" "$TMP/nonexistent.log" >/dev/null 2>&1
status=$?
set -e
if [ "$status" -eq 0 ]; then
  echo "FAIL: 日志缺失应失败" >&2; exit 1
fi
echo "PASS: 日志缺失时非零退出"

echo
echo "test-verify-builder-log: 全部通过"

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

# 通过路径：只含 webkit 那条已知冗余的复制失败。
# webkit 实体由 build 段从 apt 候选 .deb 解出并写进 $PREFIX（AUDIT N19），构建器这条
# 失败不改变产物内容；产物侧另有断言（verify-merged-deps.sh）负责它是否真的交付。
cat > "$TMP/webkit.log" <<'EOF'
[Build] start
failed to copy /home/u/proj/linglong/overlay/prepare_base/upperdir/usr/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0 to /home/u/proj/linglong/output/_build/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0: 无效的参数
[Install Files] copying files
[Commit Contents] done
EOF
set +e
out=$("$VERIFY" "$TMP/webkit.log" 2>&1)
status=$?
set -e
if [ "$status" -ne 0 ]; then
  echo "FAIL: 仅含已知冗余的 webkit 复制失败时应通过" >&2; echo "$out" >&2; exit 1
fi
case "$out" in
  *"豁免 webkit 1 处"*) ;;
  *) echo "FAIL: 通过输出未说明豁免了几处 webkit" >&2; echo "$out" >&2; exit 1 ;;
esac
echo "PASS: 仅 webkit 的已知冗余复制失败时通过并说明豁免"

# 失败路径：webkit 之外的文件复制失败仍必须失败
cat > "$TMP/bad.log" <<'EOF'
[Build] start
failed to copy /home/u/proj/linglong/overlay/prepare_base/upperdir/usr/lib/x86_64-linux-gnu/libgtk-3.so.0 to /home/u/proj/linglong/output/_build/lib/x86_64-linux-gnu/libgtk-3.so.0: 无效的参数
[Install Files] copying files
[Commit Contents] done
EOF
set +e
out=$("$VERIFY" "$TMP/bad.log" 2>&1)
status=$?
set -e
if [ "$status" -eq 0 ]; then
  echo "FAIL: webkit 之外的 'failed to copy' 应失败" >&2; exit 1
fi
for needle in "failed to copy" "libgtk-3.so.0"; do
  case "$out" in
    *"$needle"*) ;;
    *) echo "FAIL: 失败输出缺少「$needle」" >&2; echo "$out" >&2; exit 1 ;;
  esac
done
echo "PASS: webkit 之外的 'failed to copy' 仍非零退出并指明位置"

# 失败路径：豁免项与其它失败并存时仍失败（豁免不能盖住真实失败）
cat > "$TMP/mixed.log" <<'EOF'
[Build] start
failed to copy /home/u/proj/linglong/overlay/prepare_base/upperdir/usr/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0 to /home/u/proj/linglong/output/_build/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0: 无效的参数
failed to copy /home/u/proj/linglong/overlay/prepare_base/upperdir/usr/lib/x86_64-linux-gnu/libgtk-3.so.0 to /home/u/proj/linglong/output/_build/lib/x86_64-linux-gnu/libgtk-3.so.0: 无效的参数
[Commit Contents] done
EOF
set +e
out=$("$VERIFY" "$TMP/mixed.log" 2>&1)
status=$?
set -e
if [ "$status" -eq 0 ]; then
  echo "FAIL: 豁免与真实失败并存时应失败" >&2; exit 1
fi
case "$out" in
  *"1 处未豁免"*) ;;
  *) echo "FAIL: 失败输出未点明未豁免的处数" >&2; echo "$out" >&2; exit 1 ;;
esac
echo "PASS: 豁免项不掩盖其它复制失败"

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

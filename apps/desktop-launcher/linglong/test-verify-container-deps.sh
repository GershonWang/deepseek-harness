#!/bin/sh
# verify-container-deps.sh 的通过/失败路径。
# 用法: sh apps/desktop-launcher/linglong/test-verify-container-deps.sh
#
# dpkg-query / dpkg / apt-cache 由 fixture 里的 stub 提供（放在 PATH 前部），
# 因此不需要真实 dpkg 数据库，也不需要给生产脚本加测试开关。
set -eu

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
SCRIPT="$ROOT/apps/desktop-launcher/linglong/verify-container-deps.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok()  { echo "PASS: $1"; pass=$((pass + 1)); }
bad() { echo "FAIL: $1" >&2; fail=$((fail + 1)); }

# expect_fail <说明> <输出文件> <期望出现的子串...>：每条子串都必须出现在输出里。
expect_fail() {
  desc=$1; out=$2; shift 2
  for needle in "$@"; do
    if ! grep -qF -- "$needle" "$out"; then
      bad "$desc（输出缺少「$needle」）"
      sed 's/^/       /' "$out" >&2
      return
    fi
  done
  ok "$desc"
}

write_stubs() {
  bindir=$1
  cat > "$bindir/dpkg-query" <<'STUB'
#!/bin/sh
last=""; for a in "$@"; do last=$a; done
case "$1" in
  -L) cat "$VCD_STATE/files/$last" 2>/dev/null; exit 0 ;;
esac
if [ -f "$VCD_STATE/installed/$last" ]; then
  printf 'install ok installed|%s\n' "$(cat "$VCD_STATE/installed/$last")"
  exit 0
fi
printf 'deinstall ok config-files|\n'
exit 1
STUB
  cat > "$bindir/apt-cache" <<'STUB'
#!/bin/sh
last=""; for a in "$@"; do last=$a; done
[ -f "$VCD_STATE/candidate/$last" ] || exit 0
printf '%s:\n  Installed: %s\n  Candidate: %s\n' "$last" \
  "$(cat "$VCD_STATE/installed/$last" 2>/dev/null)" "$(cat "$VCD_STATE/candidate/$last")"
STUB
  cat > "$bindir/dpkg" <<'STUB'
#!/bin/sh
last=""; for a in "$@"; do last=$a; done
[ -f "$VCD_STATE/verify/$last" ] && cat "$VCD_STATE/verify/$last"
exit 0
STUB
  chmod +x "$bindir/dpkg-query" "$bindir/apt-cache" "$bindir/dpkg"
}

# new_case <名称>：一个独立 fixture：脚本与被测 yaml 同目录（脚本按 dirname $0 找 yaml）。
new_case() {
  CASE="$TMP/$1"
  mkdir -p "$CASE/bin" "$CASE/state/installed" "$CASE/state/candidate" "$CASE/state/files" "$CASE/state/verify"
  cp "$SCRIPT" "$CASE/verify-container-deps.sh"
  write_stubs "$CASE/bin"
  SYSROOT="$TMP/$1/root"; mkdir -p "$SYSROOT"
  STATE="$CASE/state"
}

# install_pkg <pkg> <版本>：造出「已装、版本与候选一致、dpkg --verify 无输出」的状态。
install_pkg() {
  printf '%s' "$2" > "$STATE/installed/$1"
  printf '%s' "$2" > "$STATE/candidate/$1"
  printf '/usr/lib/x86_64-linux-gnu/%s.so\n' "$1" > "$STATE/files/$1"
}

# install_file <pkg>：按 dpkg-query -L 给出的清单在 sysroot 里造出普通文件实体。
# 实体路径直接取自清单，避免 fixture 里"造的文件"与"声明的文件"各写一遍而漂移。
install_file() {
  while IFS= read -r f; do
    mkdir -p "$SYSROOT$(dirname "$f")"
    : > "$SYSROOT$f"
  done < "$STATE/files/$1"
}

# replace_with_chardev <pkg>：把清单里的实体换成字符设备（软链到 /dev/null——
# 普通用户无 mknod 权限，且 -c 跟随软链），复现 overlay 写入未落盘的现场。
replace_with_chardev() {
  while IFS= read -r f; do
    rm -f "$SYSROOT$f"
    ln -s /dev/null "$SYSROOT$f"
  done < "$STATE/files/$1"
}

# write_yaml：fixture yaml 覆盖 build_depends 与 depends 两段、整行注释、行尾注释，
# 以及同时出现在两段的重复包名（去重与注释剥离都在这里被固定下来）。
write_yaml() {
  cat > "$CASE/linglong.yaml" <<'EOF'
version: "1"
package:
  id: test
buildext:
  apt:
    build_depends:
      - libfoo-1.0
    depends:
      - libfoo-1.0
      # 整行注释应被跳过
      - libbar-2.0
      - libbaz-3.0   # 行尾注释应被剥离
EOF
}

# run_case <输出文件>：在 fixture 环境里跑被测脚本，返回它的退出码。
run_case() {
  set +e
  PATH="$CASE/bin:$PATH" VCD_STATE="$STATE" sh "$CASE/verify-container-deps.sh" "$SYSROOT" > "$1" 2>&1
  status=$?
  set -e
  return $status
}

# 三个依赖的健康状态；各场景再按需打破其中一处。
healthy_state() {
  install_pkg libfoo-1.0 1.0
  install_pkg libbar-2.0 2.0
  install_pkg libbaz-3.0 3.0
  install_file libfoo-1.0
  install_file libbar-2.0
  install_file libbaz-3.0
}

# --- 场景 1：依赖齐全、版本一致、无残留 → 通过 ---
new_case healthy
healthy_state
write_yaml
if run_case "$TMP/out-healthy"; then
  ok "依赖齐全时应通过（build_depends/depends 去重与注释剥离同时成立）"
else
  bad "依赖齐全时未通过"; sed 's/^/       /' "$TMP/out-healthy" >&2
fi

# --- 场景 2：已装版本落后于 apt 候选（本次安装未生效）→ 失败 ---
new_case stale
healthy_state
printf '3.1' > "$STATE/candidate/libbaz-3.0"
write_yaml
if run_case "$TMP/out-stale"; then
  bad "版本落后于候选时应失败"
else
  expect_fail "版本落后于 apt 候选时非零退出并指名" "$TMP/out-stale" "FAIL libbaz-3.0" "本次安装未生效"
fi

# --- 场景 3：依赖根本没装上（apt 失败被 '|| echo $?' 吞掉）→ 失败 ---
new_case missing
healthy_state
rm -f "$STATE/installed/libbaz-3.0" "$STATE/candidate/libbaz-3.0"
write_yaml
if run_case "$TMP/out-missing"; then
  bad "依赖未安装时应失败"
else
  expect_fail "依赖未安装时非零退出并指名" "$TMP/out-missing" "FAIL libbaz-3.0" "没有装上"
fi

# --- 场景 4：包内条目是字符设备（overlay 写入未落盘）→ 失败 ---
new_case chardev
healthy_state
replace_with_chardev libbaz-3.0
write_yaml
if run_case "$TMP/out-chardev"; then
  bad "包内条目为字符设备时应失败"
else
  expect_fail "字符设备实体非零退出并指名" "$TMP/out-chardev" "FAIL libbaz-3.0" "字符设备"
fi

# --- 场景 5：/usr 下残留 *.dpkg-new → 失败 ---
new_case residue
healthy_state
ln -s /dev/null "$SYSROOT/usr/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0.19.7.dpkg-new"
write_yaml
if run_case "$TMP/out-residue"; then
  bad "存在 *.dpkg-new 残留时应失败"
else
  expect_fail "dpkg 中间态残留非零退出" "$TMP/out-residue" "残留 dpkg 中间态文件" "libwebkit2gtk-4.1.so.0.19.7.dpkg-new"
fi

# --- 场景 6：dpkg --verify 报不一致（实体与包记录不符）→ 失败 ---
new_case mismatch
healthy_state
printf '??5?????? /usr/lib/x86_64-linux-gnu/libbaz-3.0.so\n' > "$STATE/verify/libbaz-3.0"
write_yaml
if run_case "$TMP/out-mismatch"; then
  bad "dpkg --verify 报不一致时应失败"
else
  expect_fail "实体与包记录不一致非零退出" "$TMP/out-mismatch" "FAIL libbaz-3.0" "dpkg --verify"
fi

# --- 场景 7：真实 linglong.yaml 必须能被完整解析 ---
new_case realyaml
cp "$ROOT/apps/desktop-launcher/linglong/linglong.yaml" "$CASE/linglong.yaml"
if run_case "$TMP/out-real"; then
  bad "空状态下真实 yaml 不应通过"
else
  # 三个包分别只在 depends 段、且带注释或行尾注释，命中才能证明真实 yaml 被完整解析
  expect_fail "真实 linglong.yaml 依赖被完整解析" "$TMP/out-real" \
    "FAIL libwebkit2gtk-4.1-0" "FAIL fonts-wqy-microhei" "FAIL xdg-utils"
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "test-verify-container-deps: $fail 项失败（通过 $pass 项）" >&2
  exit 1
fi
echo "test-verify-container-deps: 全部 $pass 项通过"

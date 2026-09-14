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

# expect_absent <说明> <输出文件> <不得出现的子串...>
expect_absent() {
  desc=$1; out=$2; shift 2
  for needle in "$@"; do
    if grep -qF -- "$needle" "$out"; then
      bad "$desc（输出不应出现「$needle」）"
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

# --- 场景 1：只装 build_depends 即通过 ---
# 这是本脚本 2026-09-14 修正后的核心语义：depends 由 ll-builder 在 build 段之后才安装，
# 本阶段看不到它们；要求它们已装会让构建永远失败（真实构建的 8 个 depends 包即因此误报）。
new_case healthy
install_pkg libfoo-1.0 1.0
install_file libfoo-1.0
write_yaml
if run_case "$TMP/out-healthy"; then
  ok "只装 build_depends 时应通过（depends 未装不算失败）"
else
  bad "只装 build_depends 时未通过"; sed 's/^/       /' "$TMP/out-healthy" >&2
fi

# --- 场景 2：build_depends 已装版本落后于 apt 候选（本次安装未生效）→ 失败 ---
new_case stale
install_pkg libfoo-1.0 1.0
install_file libfoo-1.0
printf '1.1' > "$STATE/candidate/libfoo-1.0"
write_yaml
if run_case "$TMP/out-stale"; then
  bad "版本落后于候选时应失败"
else
  expect_fail "版本落后于 apt 候选时非零退出并指名" "$TMP/out-stale" "FAIL libfoo-1.0" "本次安装未生效"
fi

# --- 场景 3：build_depends 根本没装上 → 失败 ---
new_case missing
write_yaml
if run_case "$TMP/out-missing"; then
  bad "build_depends 未安装时应失败"
else
  expect_fail "build_depends 未安装时非零退出并指名" "$TMP/out-missing" "FAIL libfoo-1.0" "没有装上"
fi

# --- 场景 4：包内条目是字符设备（overlay 写入未落盘）→ 失败 ---
new_case chardev
install_pkg libfoo-1.0 1.0
install_file libfoo-1.0
replace_with_chardev libfoo-1.0
write_yaml
if run_case "$TMP/out-chardev"; then
  bad "包内条目为字符设备时应失败"
else
  expect_fail "字符设备实体非零退出并指名" "$TMP/out-chardev" "FAIL libfoo-1.0" "字符设备"
fi

# --- 场景 5：/usr 下残留 *.dpkg-new → 失败 ---
new_case residue
install_pkg libfoo-1.0 1.0
install_file libfoo-1.0
ln -s /dev/null "$SYSROOT/usr/lib/x86_64-linux-gnu/libwebkit2gtk-4.1.so.0.19.7.dpkg-new"
write_yaml
if run_case "$TMP/out-residue"; then
  bad "存在 *.dpkg-new 残留时应失败"
else
  expect_fail "dpkg 中间态残留非零退出" "$TMP/out-residue" "残留 dpkg 中间态文件" "libwebkit2gtk-4.1.so.0.19.7.dpkg-new"
fi

# --- 场景 6：dpkg --verify 报实体缺失（库文件不在）→ 失败 ---
new_case mismatch
install_pkg libfoo-1.0 1.0
install_file libfoo-1.0
printf 'missing     /usr/lib/x86_64-linux-gnu/libfoo-1.0.so\n' > "$STATE/verify/libfoo-1.0"
write_yaml
if run_case "$TMP/out-mismatch"; then
  bad "dpkg --verify 报实体缺失时应失败"
else
  expect_fail "实体与包记录不一致非零退出" "$TMP/out-mismatch" "FAIL libfoo-1.0" "dpkg --verify"
fi

# --- 场景 7：dpkg --verify 只报基础镜像裁剪的文档/手册缺失 → 通过 ---
# 基座层 org.deepin.base 保留 .list 条目却裁剪了实体（实测：614 个包、/usr/share/doc
# 实体 0 条）。这类「missing」不是本次安装失败，放行它才能让继承自基座的包不被误判。
new_case docpruned
install_pkg libfoo-1.0 1.0
install_file libfoo-1.0
printf 'missing     /usr/share/doc/libfoo-1.0\nmissing     /usr/share/man/man1/foo.1.gz\n' > "$STATE/verify/libfoo-1.0"
write_yaml
if run_case "$TMP/out-docpruned"; then
  ok "仅文档/手册缺失时通过（基础镜像裁剪放行）"
else
  bad "仅文档/手册缺失时不应失败"; sed 's/^/       /' "$TMP/out-docpruned" >&2
fi

# --- 场景 8：同一次 verify 里既有文档缺失又有实体缺失 → 仍失败 ---
new_case docandlib
install_pkg libfoo-1.0 1.0
install_file libfoo-1.0
printf 'missing     /usr/share/doc/libfoo-1.0\nmissing     /usr/lib/x86_64-linux-gnu/libfoo-1.0.so\n' > "$STATE/verify/libfoo-1.0"
write_yaml
if run_case "$TMP/out-docandlib"; then
  bad "文档缺失掩盖实体缺失时应失败"
else
  expect_fail "文档缺失不掩盖实体缺失" "$TMP/out-docandlib" "FAIL libfoo-1.0" "libfoo-1.0.so"
fi

# --- 场景 9：真实 linglong.yaml 必须能被解析，且只校验 build_depends ---
new_case realyaml
cp "$ROOT/apps/desktop-launcher/linglong/linglong.yaml" "$CASE/linglong.yaml"
if run_case "$TMP/out-real"; then
  bad "空状态下真实 yaml 不应通过"
else
  expect_fail "真实 linglong.yaml 的 build_depends 被解析并报缺失" "$TMP/out-real" "FAIL libwebkit2gtk-4.1-0"
  # depends 包在本阶段不应被要求（它们由 ll-builder 在 build 段之后安装）
  expect_absent "真实 linglong.yaml 不再要求 depends 已装" "$TMP/out-real" \
    "FAIL fonts-wqy-microhei" "FAIL git:" "FAIL xdg-utils" "FAIL jq" "FAIL xxd" "FAIL wget"
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "test-verify-container-deps: $fail 项失败（通过 $pass 项）" >&2
  exit 1
fi
echo "test-verify-container-deps: 全部 $pass 项通过"

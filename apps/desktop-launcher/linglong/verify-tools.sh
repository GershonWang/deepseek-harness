#!/bin/sh
# 校验玲珑合并产物树中 tools.yaml 清单工具的可用性。
# 宿主侧在 ll-builder build 之后运行(buildext depends 的合并发生在 preCommit,
# build: 容器阶段看不到合并结果)。
# 用法: verify-tools.sh <merged-prefix>   e.g. linglong/output/binary/files
set -eu
PREFIX=${1:?usage: verify-tools.sh <merged-prefix>}
YAML=$(dirname "$0")/tools.yaml
LIST=$(mktemp)
trap 'rm -f "$LIST"' EXIT

# 解析受约束的 YAML 子集,仅在 tools: 段内识别工具,输出 "name|binary|verify|shim|base"
# (installable:/excluded: 内的 2 空格 name 不算工具)
awk '
  /^[a-zA-Z0-9_-]+:$/ {
    if (name != "") emit();
    name = "";
    sec = $1; sub(/:$/, "", sec);
    in_tools = (sec == "tools") ? 1 : 0;
    next;
  }
  in_tools && /^  [a-zA-Z0-9_-]+:$/ {
    if (name != "") emit();
    name = $1; sub(/:$/, "", name);
    binary=""; verify=""; shim=0; base=0;
    next;
  }
  in_tools && /^    binary: / { binary=$2; next; }
  in_tools && /^    verify: / { sub(/^    verify: /, ""); verify=$0; next; }
  in_tools && /^    shim: true$/ { shim=1; next; }
  in_tools && /^    base: true$/ { base=1; next; }
  END { if (name != "") emit(); }
  function emit() {
    printf "%s|%s|%s|%d|%d\n", name, binary, verify, shim, base;
    name="";
  }
' "$YAML" > "$LIST"

fail=0
while IFS='|' read -r name binary verify shim base; do
  if [ "$shim" = "1" ]; then
    if [ -x "$PREFIX/node/bin/pnpm" ] \
       && "$PREFIX/node/bin/pnpm" --version >/dev/null 2>&1; then
      echo "OK   $name (bundled pnpm)"
    elif [ -x "$PREFIX/node/bin/corepack" ] \
       && "$PREFIX/node/bin/corepack" pnpm --version >/dev/null 2>&1; then
      echo "OK   $name (corepack shim)"
    else
      echo "FAIL $name: bundled pnpm / corepack 均不可用" >&2; fail=1
    fi
    continue
  fi
  if [ -z "$binary" ]; then
    echo "FAIL $name: 缺少 binary 且非 shim" >&2; fail=1
    continue
  fi
  if [ -x "$PREFIX/$binary" ]; then
    if [ -n "$verify" ]; then
      if PATH="$PREFIX/bin:$PATH" sh -c "$verify" >/dev/null 2>&1; then
        echo "OK   $name ($verify)"
      else
        echo "FAIL $name: 执行 '$verify' 失败" >&2; fail=1
      fi
    else
      echo "OK   $name"
    fi
  elif [ "$base" = "1" ]; then
    # 基础运行时提供(org.deepin.base /usr/bin),不进 $PREFIX;运行时以
    # ll-builder run --exec 逐项确认,此处只记录来源含义。
    echo "OK   $name (base-provided)"
  else
    echo "FAIL $name: $PREFIX/$binary 缺失或不可执行" >&2; fail=1
  fi
done < "$LIST"

# installable 段校验：sha256 必须填实（不含占位符 "<"）。运行时实际生效的
# 清单在 launcher 的 internal/toolchain/catalog.go，本段与其同步；占位即视为
# 白名单未就绪并中止导出，防止"界面可安装、实际必失败"的假承诺。
INST=$(mktemp)
awk '
  /^[a-zA-Z0-9_-]+:$/ {
    sec = $1; sub(/:$/, "", sec);
    in_inst = (sec == "installable") ? 1 : 0;
    next;
  }
  in_inst && /^  [a-zA-Z0-9_-]+:$/ {
    name = $1; sub(/:$/, "", name);
    next;
  }
  in_inst && /^    sha256: / {
    sub(/^    sha256: /, "");
    print name "|" $0
  }
' "$YAML" > "$INST"
while IFS='|' read -r name sha; do
  case "$sha" in
    *'<'*|'""'|'') echo "FAIL installable/$name: sha256 未填实（占位或为空）" >&2; fail=1 ;;
    *) echo "OK   installable/$name (sha256 已填实)" ;;
  esac
done < "$INST"
rm -f "$INST"

# installable 与运行时 index.json 的一致性校验：工具 ID 列表必须
# 完全一致，避免"tools.yaml 加了但 index.json 没更"（或反过来）导致
# UI 上能看到但安装不了、或者能安装但清单里没有的不对称状态。
# index.json 是运行时实际生效的单一事实来源，tools.yaml 是打包侧
# 的白名单 + sha256 占位校验，两者的 installable 工具集合必须对齐。
INDEX_JSON=$(dirname "$0")/../internal/toolchain/tools/index.json
if [ ! -f "$INDEX_JSON" ]; then
  # 没有 else 的版本会让校验与失败判定一起消失：index.json 被改名或删除时构建
  # 照常成功，而"界面能装、实际必失败"正是这段要拦的东西。
  echo "FAIL installable/index: 找不到 $INDEX_JSON（运行时清单是工具安装的唯一事实来源）" >&2
  fail=1
else
  TOOLS_YAML_IDS=$(mktemp)
  INDEX_IDS=$(mktemp)
  TOOLS_YAML_RECOMMENDED=$(mktemp)
  INDEX_RECOMMENDED=$(mktemp)
  INDEX_PROBLEMS=$(mktemp)
  trap 'rm -f "$TOOLS_YAML_IDS" "$INDEX_IDS" "$TOOLS_YAML_RECOMMENDED" "$INDEX_RECOMMENDED" "$INDEX_PROBLEMS"' EXIT
  awk '
    /^[a-zA-Z0-9_-]+:$/ {
      sec = $1; sub(/:$/, "", sec);
      in_inst = (sec == "installable") ? 1 : 0;
      next;
    }
    in_inst && /^  [a-zA-Z0-9_-]+:$/ {
      name = $1; sub(/:$/, "", name);
      print name;
    }
  ' "$YAML" | sort > "$TOOLS_YAML_IDS"
  # tools.yaml 每个工具只声明一组 version/sha256（即推荐版本）。取出来与 index.json 的
  # versions[0] 对账：两份文件漂移时要在这里红，而不是等用户点安装才发现。
  awk '
    /^[a-zA-Z0-9_-]+:$/ {
      sec = $1; sub(/:$/, "", sec);
      in_inst = (sec == "installable") ? 1 : 0;
      next;
    }
    in_inst && /^  [a-zA-Z0-9_-]+:$/ {
      if (name != "") emit();
      name = $1; sub(/:$/, "", name);
      version = ""; sha = "";
      next;
    }
    in_inst && /^    version: / { v = $0; sub(/^    version: /, "", v); gsub(/"/, "", v); version = v; next; }
    in_inst && /^    sha256: / { s = $0; sub(/^    sha256: /, "", s); gsub(/"/, "", s); sha = s; next; }
    END { if (name != "") emit(); }
    function emit() { printf "%s|%s|%s\n", name, version, sha; name = ""; }
  ' "$YAML" | sort > "$TOOLS_YAML_RECOMMENDED"
  if ! command -v python3 >/dev/null 2>&1; then
    # 不能只打印 SKIP 就走：跳过等于这条防线不存在，而构建仍然成功。python3 是
    # 打包链路的既有依赖（见 tools.yaml 的 installable 段说明与 verify-container-deps.sh）。
    echo "FAIL installable/index: 找不到 python3，无法比对 tools.yaml 与 index.json 的工具列表" >&2
    fail=1
  else
    # 逐版本校验 index.json（运行时唯一事实来源），并导出工具 ID 与推荐版本三元组。
    # 只比对 ID 集合会漏掉多版本清单：jdk 有五条版本线，而 tools.yaml 的 installable
    # 每个工具只有一组 sha256，占位符检查因此只覆盖推荐版本，其余四条写成占位符也能
    # 通过构建（AUDIT N16）。逐版本校验 index.json 才拦得住。
    python3 - "$INDEX_JSON" "$INDEX_IDS" "$INDEX_RECOMMENDED" > "$INDEX_PROBLEMS" <<'PY'
import json, re, sys

data = json.load(open(sys.argv[1]))
ids = open(sys.argv[2], 'w')
recommended = open(sys.argv[3], 'w')
# 按 id 排序输出：下面用 diff 与 tools.yaml 的排序结果对账，顺序不一致会假报漂移。
for tool in sorted(data['tools'], key=lambda t: t['id']):
    versions = tool.get('versions') or []
    if not versions:
        print(f"{tool['id']}: 没有任何版本")
        continue
    for v in versions:
        label = f"{tool['id']}@{v.get('version', '?')}"
        if not str(v.get('version', '')).strip():
            print(f"{label}: version 为空")
        url = str(v.get('url', ''))
        if not url.startswith('https://'):
            print(f"{label}: url 必须是 https 地址（得到 {url!r}）")
        sha = str(v.get('sha256', ''))
        if not re.fullmatch(r'[0-9a-f]{64}', sha):
            print(f"{label}: sha256 必须是 64 位小写十六进制（得到 {sha!r}）")
    first = versions[0]
    recommended.write(f"{tool['id']}|{first.get('version', '')}|{first.get('sha256', '')}\n")
    ids.write(tool['id'] + "\n")
ids.close()
recommended.close()
PY
    if [ -s "$INDEX_PROBLEMS" ]; then
      echo "FAIL installable/index: index.json 里有版本的 url/sha256 不合规（含非推荐版本）" >&2
      sed 's/^/       /' "$INDEX_PROBLEMS" >&2
      fail=1
    else
      echo "OK   index.json 全部版本（含非推荐版本）url/sha256 均合规"
    fi
    diff_ids=$(diff "$TOOLS_YAML_IDS" "$INDEX_IDS" || true)
    if [ -n "$diff_ids" ]; then
      echo "FAIL installable/index 不一致：tools.yaml vs index.json 工具列表不同" >&2
      echo "$diff_ids" >&2
      fail=1
    else
      echo "OK   installable 与 index.json 工具列表一致"
    fi
    diff_rec=$(diff "$TOOLS_YAML_RECOMMENDED" "$INDEX_RECOMMENDED" || true)
    if [ -n "$diff_rec" ]; then
      echo "FAIL installable/index 漂移：tools.yaml 的推荐版本与 index.json 的 versions[0] 不一致" >&2
      echo "$diff_rec" >&2
      fail=1
    else
      echo "OK   installable 的推荐版本与 index.json 一致"
    fi
  fi
fi

# git 功能探测（宿主侧静态）：launcher 启动时为整个 harness 进程树注入
# GIT_EXEC_PATH=<prefix>/lib/git-core（packagedGitExecPath 按可执行文件位置
# 推导，任意机器一致），因此 lib/git-core 里的远程 helper 必须随包存在；
# helper 缺失即 git push/fetch 全部不可用。缺失即视为失败，避免
# "git --version 通过但 git push 必挂"的虚假自检。
if [ -x "$PREFIX/bin/git" ] && [ ! -f "$PREFIX/lib/git-core/git-remote-https" ]; then
  echo "FAIL git: lib/git-core/git-remote-https 缺失（GIT_EXEC_PATH 指向它），容器内远程操作将不可用" >&2
  fail=1
fi

exit $fail

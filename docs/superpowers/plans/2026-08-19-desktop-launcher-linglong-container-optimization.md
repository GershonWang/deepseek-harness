# 玲珑沙箱容器可用性优化实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 desktop-launcher 玲珑容器内 harness 工具链的"运行时碰壁"前移为三层防线:构建期工具清单校验、运行时自检与按需安装、凭据/挂载可达。

**Architecture:** 三层防线全部落在 `apps/desktop-launcher/`:第一层用 `tools.yaml`(单一事实来源)驱动 `verify-tools.sh` 在宿主侧校验 `ll-builder build` 的合并产物树;第三层用纯 Go 实现 `$HOME/.dsh-tools` 静态产物安装与 PATH/LD 注入;凭据层用纯 Go 读写 `$HOME/.git-credentials`。GUI 只做薄 GTK 层,可测逻辑全部下沉到纯 Go 函数。harness 侧模型可见清单注入单列为 Phase D(独立子项目,先调查再定实现,不新增 `packages/` 包)。

**Tech Stack:** POSIX sh(校验脚本)、Go 1.22(launcher,`go test` 纯逻辑)、GO 标准库 net/http/archive/tar、GTK3 cgo 薄层、YAML 子集解析(无新依赖)。

**前置环境:** 执行机需可运行 `go test ./...`(apps/desktop-launcher 模块)与 `sh`;`ll-builder` 构建验证在用户机器执行(当前沙箱无 go/ll-builder)。

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `linglong/tools.yaml`(新) | 工具清单单一事实来源:tools(必须校验)+ installable(按需白名单,含 url/sha256)+ excluded(有意不包含) |
| `linglong/verify-tools.sh`(新) | 宿主侧校验 `$PREFIX/<binary>` 与 shim,缺失即退出非零 |
| `linglong/test-verify-tools.sh`(新) | verify-tools.sh 的失败/通过路径测试(临时产物树) |
| `linglong/linglong.yaml`(改) | `buildext.apt.depends` 增补 |
| `build-linglong.sh`(改) | export 前调用 verify-tools.sh |
| `toolinstall.go`(新) | 按需安装:目录布局、下载、sha256、原子解包、列表、删除 |
| `toolinstall_test.go`(新) | httptest + 内存 tar 包的安装/校验/删除测试 |
| `toolcheck.go`(新) | 运行时探测 `git/python3/node/curl/jq/pnpm --version` |
| `toolcheck_test.go`(新) | PATH 桩可执行文件的探测测试 |
| `env.go`(改) | `configurePackagedEnv()` 注入 `$HOME/.dsh-tools/bin|lib` |
| `env_test.go`(改) | dshToolsEnv 与注入测试 |
| `gitcred.go`(新) | `$HOME/.git-credentials` github.com 条目读写清除 |
| `gitcred_test.go`(新) | 临时 HOME 读写/重写/清除测试 |
| `ui_state.go`(改) | `toolPanelState` / `credentialPanelState` 纯函数 |
| `ui_state_test.go`(改) | 上述状态函数测试 |
| `ui.go`(改) | 设置弹框"工具/凭据"分区 + 薄处理器(GTK 薄层) |
| `linglong/config.d/20-host-credentials.json`(新) | 可选宿主凭据只读挂载模板 |
| `README.md`(改) | 打包要点、凭据、按需安装、挂载、代理说明 |
| `.agents/notes/implemented/feature/2026-08-19-linglong-container-toolchain.md`(新) | 决策记录(含验证要求) |

**Phase D(单独子项目,见后):** harness 侧模型可见工具清单注入(preset overlay + 登录事件或复用既有指令日志路径)。

---

## Phase A:构建期工具清单与校验

### Task 1:`linglong/tools.yaml` 清单

**Files:**
- Create: `apps/desktop-launcher/linglong/tools.yaml`

- [ ] **Step 1:创建清单文件**

```yaml
# 容器内工具清单:verify-tools.sh 逐项校验;installable 为按需安装白名单
# 格式契约(解析器基于本文件,勿引入 YAML 全特性):
#   tools 下每项: 2 空格缩进的 name: 段,其下 binary:/verify:/shim: 均 4 空格缩进
tools:
  git:
    binary: bin/git
    verify: git --version
  python3:
    binary: bin/python3
    verify: python3 --version
  curl:
    binary: bin/curl
    verify: curl --version
  wget:
    binary: bin/wget
    verify: wget --version
  jq:
    binary: bin/jq
    verify: jq --version
  unzip:
    binary: bin/unzip
  xxd:
    binary: bin/xxd
  pnpm:
    shim: true
    verify: pnpm --version
# 按需安装白名单:url/sha256 在执行 Task 4 时从官方页面取最新值填入
installable:
  go:
    version: "1.23.2"
    url: "https://go.dev/dl/go1.23.2.linux-amd64.tar.gz"
    sha256: "<Task4 填入官方 sha256>"
  ripgrep:
    version: "14.1.0"
    url: "https://github.com/BurntSushi/ripgrep/releases/download/14.1.0/ripgrep-14.1.0-x86_64-unknown-linux-musl.tar.gz"
    sha256: "<Task4 填入官方 sha256>"
# 有意不包含(包体控制):编译器链不进包体、不进按需白名单
excluded:
  - gcc
  - clang
  - rustc
```

- [ ] **Step 2:格式 sanity(本地可跑)**

Run: `grep -c '^  [a-z0-9_-]*:$' apps/desktop-launcher/linglong/tools.yaml`
Expected: `8`(tools 6 项 + installable 2 项段名不匹配该正则除外——输出 ≥6 即可,解析器以 Task 2 测试为准)

- [ ] **Step 3:提交**

```bash
git add apps/desktop-launcher/linglong/tools.yaml
git commit -m "chore(desktop-launcher): add linglong tool manifest"
```

### Task 2:`verify-tools.sh` 与失败/通过路径测试

**Files:**
- Create: `apps/desktop-launcher/linglong/verify-tools.sh`
- Create: `apps/desktop-launcher/linglong/test-verify-tools.sh`

- [ ] **Step 1:写失败路径测试(先测脚本存在与解析)**

```sh
#!/bin/sh
set -eu
# test-verify-tools.sh:verify-tools.sh 的通过/失败路径
# 用法: sh apps/desktop-launcher/linglong/test-verify-tools.sh
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)   # 仓库根
VERIFY="$ROOT/apps/desktop-launcher/linglong/verify-tools.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

mkbin() { mkdir -p "$(dirname "$TMP/$1")"; : > "$TMP/$1"; chmod +x "$TMP/$1"; }

# 通过路径:齐全的产物树
mkbin bin/git; mkbin bin/python3; mkbin bin/curl; mkbin bin/wget
mkbin bin/jq; mkbin bin/unzip; mkbin bin/xxd
mkdir -p "$TMP/node/bin"; : > "$TMP/node/bin/corepack"; chmod +x "$TMP/node/bin/corepack"
if "$VERIFY" "$TMP" >/dev/null 2>&1; then
  echo "PASS: 齐全产物树应通过"
else
  echo "FAIL: 齐全产物树未通过" >&2; exit 1
fi

# 失败路径:删 git
rm "$TMP/bin/git"
if "$VERIFY" "$TMP" >/dev/null 2>&1; then
  echo "FAIL: git 缺失应失败" >&2; exit 1
fi
echo "PASS: git 缺失时退出非零"
```

- [ ] **Step 2:运行,确认失败(脚本未实现)**

Run: `sh apps/desktop-launcher/linglong/test-verify-tools.sh`
Expected: FAIL(`verify-tools.sh` 不存在或退出 127)

- [ ] **Step 3:实现 verify-tools.sh**

```sh
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

# 解析受约束的 YAML 子集,仅在 tools: 段内识别工具,输出 "name|binary|verify|shim"
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
    binary=""; verify=""; shim=0;
    next;
  }
  in_tools && /^    binary: / { binary=$2; next; }
  in_tools && /^    verify: / { sub(/^    verify: /, ""); verify=$0; next; }
  in_tools && /^    shim: true$/ { shim=1; next; }
  END { if (name != "") emit(); }
  function emit() {
    printf "%s|%s|%s|%d\n", name, binary, verify, shim;
    name="";
  }
' "$YAML" > "$LIST"

fail=0
while IFS='|' read -r name binary verify shim; do
  if [ "$shim" = "1" ]; then
    if [ -x "$PREFIX/node/bin/corepack" ] \
       && "$PREFIX/node/bin/corepack" pnpm --version >/dev/null 2>&1; then
      echo "OK   $name (corepack shim)"
    else
      echo "FAIL $name: corepack pnpm --version 失败" >&2; fail=1
    fi
    continue
  fi
  if [ -n "$binary" ] && [ -x "$PREFIX/$binary" ]; then
    if [ -n "$verify" ]; then
      if PATH="$PREFIX/bin:$PATH" sh -c "$verify" >/dev/null 2>&1; then
        echo "OK   $name ($verify)"
      else
        echo "FAIL $name: 执行 '$verify' 失败" >&2; fail=1
      fi
    else
      echo "OK   $name"
    fi
  else
    echo "FAIL $name: $PREFIX/$binary 缺失或不可执行" >&2; fail=1
  fi
done < "$LIST"

exit $fail
```

- [ ] **Step 4:运行测试,确认通过**

Run: `sh apps/desktop-launcher/linglong/test-verify-tools.sh`
Expected: 两行 `PASS`

- [ ] **Step 5:提交**

```bash
git add apps/desktop-launcher/linglong/verify-tools.sh apps/desktop-launcher/linglong/test-verify-tools.sh
git commit -m "feat(desktop-launcher): verify linglong tool manifest post-build"
```

### Task 3:`linglong.yaml` 依赖增补与 `build-linglong.sh` 集成

**Files:**
- Modify: `apps/desktop-launcher/linglong/linglong.yaml:59-72`(buildext.apt.depends)
- Modify: `apps/desktop-launcher/build-linglong.sh:23-24`

- [ ] **Step 1:linglong.yaml 增补运行时依赖**

在 `apps/desktop-launcher/linglong/linglong.yaml` 的 `buildext.apt.depends` 列表(git 之后)追加:

```yaml
      # 层1 工具链自包含:随包工具与其运行时依赖
      - python3
      - python3-pip
      - curl
      - wget
      - unzip
      - zip
      - jq
      - vim-common   # xxd
      - ca-certificates   # git/https/python 校验
```

- [ ] **Step 2:build-linglong.sh 在 export 前校验**

把 `apps/desktop-launcher/build-linglong.sh` 第 24 行前插入:

```sh
echo "==> 校验合并产物树工具清单"
sh apps/desktop-launcher/linglong/verify-tools.sh linglong/output/binary/files
```

- [ ] **Step 3:验证(用户机器)**

Run: `sh apps/desktop-launcher/build-linglong.sh`
Expected: `ll-builder build` 成功 → `校验合并产物树工具清单` 逐项 OK → `ll-builder export` 成功 → 输出 .uab 路径

- [ ] **Step 4:提交**

```bash
git add apps/desktop-launcher/linglong/linglong.yaml apps/desktop-launcher/build-linglong.sh
git commit -m "feat(desktop-launcher): bundle core toolchain and verify before export"
```

---

## Phase B:运行时按需安装(层3)

### Task 4:`toolinstall.go` 按需安装机制

**Files:**
- Create: `apps/desktop-launcher/toolinstall.go`
- Create: `apps/desktop-launcher/toolinstall_test.go`

- [ ] **Step 1:写失败测试**

```go
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fakeTar 构建 "<dir>/bin/<name>" 的 tar.gz 并返回其 sha256。
func fakeTar(t *testing.T, dir, name string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("#!/bin/sh\necho fake-" + name + "\n")
	_ = tw.WriteHeader(&tar.Header{
		Name: dir + "/bin/" + name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg,
	})
	_, _ = tw.Write(content)
	_ = tw.Close()
	_ = gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func TestInstallTool_AtomicAndListed(t *testing.T) {
	home := t.TempDir()
	blob, sum := fakeTar(t, "go", "go")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()

	if err := InstallTool(ToolInstallDir(home), "go", "1.23.2", srv.URL+"/go.tar.gz", sum); err != nil {
		t.Fatalf("InstallTool: %v", err)
	}
	root := filepath.Join(ToolInstallDir(home), "go-1.23.2")
	if _, err := os.Stat(filepath.Join(root, "bin", "go")); err != nil {
		t.Fatalf("extracted binary missing: %v", err)
	}
	current, err := os.Readlink(filepath.Join(ToolInstallDir(home), "current", "go"))
	if err != nil || current != root {
		t.Fatalf("current symlink: %v -> %q", err, current)
	}
	names := ListTools(ToolInstallDir(home))
	if len(names) != 1 || names[0] != "go" {
		t.Fatalf("ListTools: got %v", names)
	}
}

func TestInstallTool_ShaMismatch(t *testing.T) {
	home := t.TempDir()
	blob, _ := fakeTar(t, "go", "go")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	err := InstallTool(ToolInstallDir(home), "go", "1.23.2", srv.URL+"/go.tar.gz", "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected sha256 mismatch error")
	}
	if _, statErr := os.Stat(ToolInstallDir(home)); statErr == nil {
		t.Fatal("failed install must leave no install dir")
	}
}

func TestRemoveTool(t *testing.T) {
	home := t.TempDir()
	blob, sum := fakeTar(t, "rg", "rg")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	dir := ToolInstallDir(home)
	if err := InstallTool(dir, "rg", "14.1.0", srv.URL+"/rg.tar.gz", sum); err != nil {
		t.Fatalf("InstallTool: %v", err)
	}
	if err := RemoveTool(dir, "rg"); err != nil {
		t.Fatalf("RemoveTool: %v", err)
	}
	if names := ListTools(dir); len(names) != 0 {
		t.Fatalf("after remove, ListTools: %v", names)
	}
}
```

- [ ] **Step 2:运行,确认失败**

Run: `go test ./... -run 'TestInstallTool|TestRemoveTool'`
Expected: FAIL(undefined: InstallTool / ToolInstallDir / ListTools / RemoveTool)

- [ ] **Step 3:实现 toolinstall.go**

```go
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ToolInstallDir 返回按需安装根目录(home 下 .dsh-tools)。
func ToolInstallDir(home string) string {
	return filepath.Join(home, ".dsh-tools")
}

// InstallTool 下载 url 的 tar.gz,校验 sha256 后原子解包到
// <dir>/<name>-<version>,并更新 <dir>/current/<name> 软链。
// 任一环节失败不残留半成品。
func InstallTool(dir, name, version, url, sha256Hex string) error {
	downloaded, err := download(url)
	if err != nil {
		return err
	}
	if sum := sha256.Sum256(downloaded); hex.EncodeToString(sum[:]) != strings.ToLower(sha256Hex) {
		return fmt.Errorf("sha256 mismatch for %s", name)
	}
	tmp, err := os.MkdirTemp(dir, ".install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := extractTarGz(downloaded, tmp); err != nil {
		return err
	}
	// tar 顶层目录剥离:解包出的唯一目录上移一层
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return fmt.Errorf("unexpected tarball layout for %s", name)
	}
	root := filepath.Join(dir, name+"-"+version)
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(tmp, entries[0].Name()), root); err != nil {
		return err
	}
	current := filepath.Join(dir, "current", name)
	if err := os.MkdirAll(filepath.Dir(current), 0o755); err != nil {
		return err
	}
	return os.Symlink(root, current)
}

// ListTools 返回当前已安装的工具名列表(按 current 软链)。
func ListTools(dir string) []string {
	cur := filepath.Join(dir, "current")
	entries, err := os.ReadDir(cur)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// RemoveTool 删除 <dir>/<name>-<version>(current 软链目标)与 <dir>/current/<name>。
func RemoveTool(dir, name string) error {
	current := filepath.Join(dir, "current", name)
	target, err := os.Readlink(current)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(current); err != nil && !os.IsNotExist(err) {
		return err
	}
	if target != "" {
		return os.RemoveAll(target)
	}
	return nil
}

func download(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<30))
}

func extractTarGz(data []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// 防路径逃逸
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe tar path: %s", hdr.Name)
		}
		target := filepath.Join(dest, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			_ = f.Close()
		}
	}
}
```

- [ ] **Step 4:运行,确认通过**

Run: `go test ./... -run 'TestInstallTool|TestRemoveTool' -v`
Expected: PASS ×3

- [ ] **Step 5:更新 tools.yaml 的 url/sha256(数据录入)**

Run(用户机器,取官方校验值):
```sh
curl -sL https://go.dev/dl/go1.23.2.linux-amd64.tar.gz.sha256
curl -sL https://github.com/BurntSushi/ripgrep/releases/download/14.1.0/ripgrep-14.1.0-x86_64-unknown-linux-musl.tar.gz.sha256  # 或从发布页取
```
将两个真实 sha256 替换 `tools.yaml` 中 `<Task4 填入官方 sha256>`,把 `installable` 段整理进 commit。

- [ ] **Step 6:提交**

```bash
git add apps/desktop-launcher/toolinstall.go apps/desktop-launcher/toolinstall_test.go apps/desktop-launcher/linglong/tools.yaml
git commit -m "feat(desktop-launcher): install on-demand tools into dsh-tools dir"
```

### Task 5:`env.go` 注入按需工具目录

**Files:**
- Modify: `apps/desktop-launcher/env.go:105-112`
- Modify: `apps/desktop-launcher/env_test.go`

- [ ] **Step 1:写失败测试(追加到 env_test.go)**

```go
func TestDshToolsEnv(t *testing.T) {
	bin, ld := dshToolsEnv("/home/u")
	if bin != "/home/u/.dsh-tools/bin" || ld != "/home/u/.dsh-tools/lib" {
		t.Fatalf("dshToolsEnv: bin=%q ld=%q", bin, ld)
	}
}

func TestConfigurePackagedEnv_PrependsToolsWhenPresent(t *testing.T) {
	home := t.TempDir()
	bin := home + "/.dsh-tools/bin"
	lib := home + "/.dsh-tools/lib"
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	oldLd := os.Getenv("LD_LIBRARY_PATH")
	os.Unsetenv("LD_LIBRARY_PATH")
	defer func() {
		_ = os.Setenv("PATH", oldPath)
		_ = os.Setenv("LD_LIBRARY_PATH", oldLd)
	}()
	configurePackagedEnvForHome(home)
	if got := os.Getenv("PATH"); got != bin+string(os.PathListSeparator)+oldPath {
		t.Fatalf("PATH not prepended: %q", got)
	}
	if got := os.Getenv("LD_LIBRARY_PATH"); got != lib {
		t.Fatalf("LD_LIBRARY_PATH not set: %q", got)
	}
}

func TestConfigurePackagedEnv_SkipsWhenAbsent(t *testing.T) {
	home := t.TempDir() // 无 .dsh-tools
	oldPath := os.Getenv("PATH")
	defer func() { _ = os.Setenv("PATH", oldPath) }()
	configurePackagedEnvForHome(home)
	if got := os.Getenv("PATH"); got != oldPath {
		t.Fatalf("PATH changed when tools dir absent: %q", got)
	}
}
```

- [ ] **Step 2:运行,确认失败**

Run: `go test ./... -run 'TestDshToolsEnv|TestConfigurePackagedEnv'`
Expected: FAIL(undefined: dshToolsEnv / configurePackagedEnvForHome)

- [ ] **Step 3:实现**

把 `apps/desktop-launcher/env.go` 的 `configurePackagedEnv` 改为委托新函数:

```go
// dshToolsEnv 返回按需工具目录的 PATH 与 LD_LIBRARY_PATH 段(home/.dsh-tools)。
func dshToolsEnv(home string) (pathSeg, ldSeg string) {
	return filepath.Join(home, ".dsh-tools", "bin"), filepath.Join(home, ".dsh-tools", "lib")
}

// configurePackagedEnv 为打包态设置子进程环境;按需工具目录存在时前置进
// PATH/LD_LIBRARY_PATH。
func configurePackagedEnv() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	configurePackagedEnvForHome(home)
}

// configurePackagedEnvForHome 以显式 HOME 调用,便于测试。
func configurePackagedEnvForHome(home string) {
	_ = os.Setenv("GTK_A11Y", "none")
	_ = os.Setenv("DSH_DIRECTORY_PICKER", "browse")
	bin, lib := dshToolsEnv(home)
	if info, err := os.Stat(bin); err == nil && info.IsDir() {
		_ = os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	if info, err := os.Stat(lib); err == nil && info.IsDir() {
		old := os.Getenv("LD_LIBRARY_PATH")
		if old != "" {
			_ = os.Setenv("LD_LIBRARY_PATH", lib+string(os.PathListSeparator)+old)
		} else {
			_ = os.Setenv("LD_LIBRARY_PATH", lib)
		}
	}
}
```

- [ ] **Step 4:运行,确认通过**

Run: `go test ./... -run 'TestDshToolsEnv|TestConfigurePackagedEnv' -v`
Expected: PASS ×3

- [ ] **Step 5:提交**

```bash
git add apps/desktop-launcher/env.go apps/desktop-launcher/env_test.go
git commit -m "feat(desktop-launcher): put dsh-tools on PATH and LD_LIBRARY_PATH"
```

### Task 6:`toolcheck.go` 运行时探测

**Files:**
- Create: `apps/desktop-launcher/toolcheck.go`
- Create: `apps/desktop-launcher/toolcheck_test.go`

- [ ] **Step 1:写失败测试**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckTools_StubPath(t *testing.T) {
	dir := t.TempDir()
	stubs := map[string]string{
		"git":     "git version 2.40.0",
		"python3": "Python 3.11.2",
		"node":    "v24.9.0",
		"curl":    "curl 8.5.0",
		"jq":      "jq-1.7.1",
	}
	for name, out := range stubs {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+out+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	defer func() { _ = os.Setenv("PATH", old) }()

	checks := CheckTools(DefaultToolSpecs())
	byName := map[string]ToolCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if !byName["git"].OK || byName["git"].Version != "2.40.0" {
		t.Fatalf("git check: %+v", byName["git"])
	}
	if !byName["python3"].OK {
		t.Fatalf("python3 check: %+v", byName["python3"])
	}
}

func TestCheckTools_Missing(t *testing.T) {
	dir := t.TempDir()
	old := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir)
	defer func() { _ = os.Setenv("PATH", old) }()
	checks := CheckTools(DefaultToolSpecs())
	for _, c := range checks {
		if c.OK {
			t.Fatalf("expected %s missing, got OK", c.Name)
		}
	}
}
```

- [ ] **Step 2:运行,确认失败**

Run: `go test ./... -run TestCheckTools`
Expected: FAIL(undefined: CheckTools / DefaultToolSpecs / ToolCheck)

- [ ] **Step 3:实现 toolcheck.go**

```go
package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// ToolCheck 记录一次工具探测结果。
type ToolCheck struct {
	Name    string
	OK      bool
	Version string // OK 时的版本字符串(若有)
	Err     string // 失败原因
}

// ToolSpec 声明一个可探测工具。
type ToolSpec struct {
	Name    string
	Command []string // 探测命令,如 {"git","--version"}
}

// DefaultToolSpecs 返回自检面板覆盖的关键工具。
func DefaultToolSpecs() []ToolSpec {
	return []ToolSpec{
		{Name: "git", Command: []string{"git", "--version"}},
		{Name: "python3", Command: []string{"python3", "--version"}},
		{Name: "node", Command: []string{"node", "--version"}},
		{Name: "curl", Command: []string{"curl", "--version"}},
		{Name: "jq", Command: []string{"jq", "--version"}},
		{Name: "pnpm", Command: []string{"pnpm", "--version"}},
	}
}

// CheckTools 依序探测,单工具失败不影响其余。
func CheckTools(specs []ToolSpec) []ToolCheck {
	out := make([]ToolCheck, 0, len(specs))
	for _, s := range specs {
		c := ToolCheck{Name: s.Name}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var buf bytes.Buffer
		cmd := exec.CommandContext(ctx, s.Command[0], s.Command[1:]...)
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		cancel()
		if err != nil {
			c.Err = err.Error()
		} else {
			c.OK = true
			c.Version = firstLine(buf.String())
		}
		out = append(out, c)
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
```

- [ ] **Step 4:运行,确认通过**

Run: `go test ./... -run TestCheckTools -v`
Expected: PASS ×2

- [ ] **Step 5:提交**

```bash
git add apps/desktop-launcher/toolcheck.go apps/desktop-launcher/toolcheck_test.go
git commit -m "feat(desktop-launcher): probe key container tools at startup"
```

---

## Phase C:凭据与 GUI 面板

### Task 7:`gitcred.go` 凭据读写

**Files:**
- Create: `apps/desktop-launcher/gitcred.go`
- Create: `apps/desktop-launcher/gitcred_test.go`

- [ ] **Step 1:写失败测试**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func gitcredFile(home string) string { return filepath.Join(home, ".git-credentials") }

func TestGitCredentials_RoundTrip(t *testing.T) {
	home := t.TempDir()
	if err := WriteGitCredentials(home, "u1", "tok1"); err != nil {
		t.Fatalf("write: %v", err)
	}
	user, token, found := ReadGitCredentials(home)
	if !found || user != "u1" || token != "tok1" {
		t.Fatalf("read: user=%q token=%q found=%v", user, token, found)
	}
	data, _ := os.ReadFile(gitcredFile(home))
	if string(data) != "https://u1:tok1@github.com\n" {
		t.Fatalf("file content: %q", data)
	}
}

func TestGitCredentials_OverwriteAndClear(t *testing.T) {
	home := t.TempDir()
	_ = WriteGitCredentials(home, "u1", "tok1")
	_ = WriteGitCredentials(home, "u2", "tok2")
	if _, token, _ := ReadGitCredentials(home); token != "tok2" {
		t.Fatalf("overwrite failed: %q", token)
	}
	if err := ClearGitCredentials(home); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, _, found := ReadGitCredentials(home); found {
		t.Fatal("expected cleared")
	}
	if _, err := os.Stat(gitcredFile(home)); !os.IsNotExist(err) {
		t.Fatalf("file should be removed, stat err: %v", err)
	}
}
```

- [ ] **Step 2:运行,确认失败**

Run: `go test ./... -run TestGitCredentials`
Expected: FAIL(undefined: WriteGitCredentials / ReadGitCredentials / ClearGitCredentials)

- [ ] **Step 3:实现 gitcred.go**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gitCredentialsPath 返回 git store 凭据文件路径。
func gitCredentialsPath(home string) string { return filepath.Join(home, ".git-credentials") }

// ReadGitCredentials 读取 github.com 条目;无条目时 found=false。
func ReadGitCredentials(home string) (user, token string, found bool) {
	data, err := os.ReadFile(gitCredentialsPath(home))
	if err != nil {
		return "", "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "https://")
		if !ok {
			continue
		}
		host, cred, ok := strings.Cut(rest, "@")
		if !ok || host != "github.com" {
			continue
		}
		user, token, ok = strings.Cut(cred, ":")
		if ok {
			return user, token, true
		}
	}
	return "", "", false
}

// WriteGitCredentials 写入/覆盖 github.com 条目,保留其它 host 行。
func WriteGitCredentials(home, user, token string) error {
	path := gitCredentialsPath(home)
	lines := []string{}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			rest, ok := strings.CutPrefix(strings.TrimSpace(line), "https://")
			if ok {
				host, _, hasAt := strings.Cut(rest, "@")
				if hasAt && host == "github.com" {
					continue // 丢弃旧 github.com 行
				}
			}
			if strings.TrimSpace(line) != "" {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
	}
	lines = append(lines, fmt.Sprintf("https://%s:%s@github.com", user, token))
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	data := []byte(strings.Join(lines, "\n") + "\n")
	return os.WriteFile(path, data, 0o600)
}

// ClearGitCredentials 删除 github.com 条目;文件为空/无条目时删除文件。
func ClearGitCredentials(home string) error {
	path := gitCredentialsPath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	kept := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "https://")
		if ok {
			host, _, hasAt := strings.Cut(rest, "@")
			if hasAt && host == "github.com" {
				continue
			}
		}
		kept = append(kept, strings.TrimSpace(line))
	}
	if len(kept) == 0 {
		return os.Remove(path)
	}
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600)
}
```

- [ ] **Step 4:运行,确认通过**

Run: `go test ./... -run TestGitCredentials -v`
Expected: PASS ×2

- [ ] **Step 5:提交**

```bash
git add apps/desktop-launcher/gitcred.go apps/desktop-launcher/gitcred_test.go
git commit -m "feat(desktop-launcher): manage git credentials store entries"
```

### Task 8:工具/凭据面板状态(纯函数)与 GUI 薄层

**Files:**
- Modify: `apps/desktop-launcher/ui_state.go`
- Modify: `apps/desktop-launcher/ui_state_test.go`
- Modify: `apps/desktop-launcher/ui.go`

- [ ] **Step 1:写失败测试(追加 ui_state_test.go)**

```go
package main

import (
	"strings"
	"testing"
)

func TestToolPanelState_Lines(t *testing.T) {
	checks := []ToolCheck{
		{Name: "git", OK: true, Version: "2.40.0"},
		{Name: "python3", OK: false, Err: "exec: not found"},
	}
	state := toolPanelState(checks, []string{"go"})
	joined := strings.Join(state.Installable, "\n")
	if !strings.Contains(joined, "go") {
		t.Fatalf("installable missing go: %q", joined)
	}
	if len(state.Installed) != 1 || state.Installed[0] != "go" {
		t.Fatalf("installed: %v", state.Installed)
	}
}

func TestCredentialPanelState_HasToken(t *testing.T) {
	home := t.TempDir()
	_ = WriteGitCredentials(home, "u", "tok")
	state := credentialPanelState(home, gitCredentialsPath(home))
	if !state.HasToken || state.User != "u" || state.StoragePath == "" {
		t.Fatalf("state: %+v", state)
	}
}
```

- [ ] **Step 2:运行,确认失败**

Run: `go test ./... -run 'TestToolPanelState|TestCredentialPanelState'`
Expected: FAIL(undefined: toolPanelState / credentialPanelState)

- [ ] **Step 3:实现 ui_state.go 追加**

```go
// ToolPanelState 是设置弹框"工具"分区的渲染数据。
type ToolPanelState struct {
	Checks      []ToolCheck
	Installed   []string
	Installable []string
}

// toolPanelState 由探测结果与已安装列表推导面板状态。
func toolPanelState(checks []ToolCheck, installed []string) ToolPanelState {
	return ToolPanelState{Checks: checks, Installed: installed, Installable: []string{"go", "ripgrep"}}
}

// CredentialPanelState 是设置弹框"Git 凭据"分区的渲染数据。
type CredentialPanelState struct {
	HasToken    bool
	User        string
	StoragePath string
}

// credentialPanelState 读取当前凭据并给出存储位置展示。
func credentialPanelState(home, storagePath string) CredentialPanelState {
	user, _, found := ReadGitCredentials(home)
	return CredentialPanelState{HasToken: found, User: user, StoragePath: storagePath}
}
```

- [ ] **Step 4:运行,确认通过**

Run: `go test ./... -run 'TestToolPanelState|TestCredentialPanelState' -v`
Expected: PASS ×2

- [ ] **Step 5:GUI 薄层(ui.go,遵循既有 dshOnServerStart 模式)**

在 `apps/desktop-launcher/ui.go` 设置弹框加"工具"与"Git 凭据"两个分区:

```go
//export dshOnToolCheck
func dshOnToolCheck() {
	checks := CheckTools(DefaultToolSpecs())
	installed := ListTools(ToolInstallDir(mustHome()))
	_ = dshToolPanel(checks, installed) // GTK 薄层:dshToolPanel 刷新分区控件(沿用既有对话刷新风格)
}

//export dshOnCredentialSave
func dshOnCredentialSave(user, token *C.char) {
	home := mustHome()
	_ = WriteGitCredentials(home, C.GoString(user), C.GoString(token))
	dshRefreshStatus()
}

//export dshOnCredentialClear
func dshOnCredentialClear() {
	_ = ClearGitCredentials(mustHome())
	dshRefreshStatus()
}

func mustHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
```

GTK 布局与回调注册参照现有服务器状态分区(`ui.go` 内 `dshOnServerStatusClicked` 附近的 C 侧控件与导出桥),视觉细节交 designer;不透传任何明文 token 到状态栏(仅显示 `已保存(user)`)。

- [ ] **Step 6:编译检查**

Run: `go build ./...`
Expected: 成功(无 cgo 错误)

- [ ] **Step 7:提交**

```bash
git add apps/desktop-launcher/ui_state.go apps/desktop-launcher/ui_state_test.go apps/desktop-launcher/ui.go
git commit -m "feat(desktop-launcher): tools and git credentials panels"
```

### Task 9:宿主凭据挂载模板与 README

**Files:**
- Create: `apps/desktop-launcher/linglong/config.d/20-host-credentials.json`
- Modify: `apps/desktop-launcher/README.md`

- [ ] **Step 1:创建可选挂载模板**

```json
{
  "mounts": [
    {
      "type": "bind",
      "source": "/home/<USER>/.git-credentials",
      "destination": "/home/<USER>/.git-credentials",
      "options": ["ro", "bind"]
    },
    {
      "type": "bind",
      "source": "/home/<USER>/.ssh",
      "destination": "/home/<USER>/.ssh",
      "options": ["ro", "bind"]
    }
  ]
}
```

- [ ] **Step 2:README 增补(打包要点段后新增小节)**

在 `apps/desktop-launcher/README.md` 的"玲珑打包"后新增 `## 容器可用性(工具链/凭据/挂载)`:

```markdown
- 工具链自包含:`buildext.apt.depends` 随包带入 git/python3/curl/wget/unzip/zip/jq/xxd/ca-certificates;清单与校验见 `linglong/tools.yaml` 与 `verify-tools.sh`(宿主侧在 export 前校验合并产物树)。
- 按需安装:重/罕见工具(go、ripgrep)经 GUI"工具"分区装到 `$HOME/.dsh-tools`(容器内,宿主磁盘,卸载默认保留),launcher 自动注入 PATH/LD_LIBRARY_PATH。
- git 凭据:GUI"Git 凭据"区写入 `~/.git-credentials`(容器 HOME = 宿主主目录;ll-cli uninstall 不清理用户数据);可选只读挂载宿主同名文件(模板见 `linglong/config.d/20-host-credentials.json`)。
- 代理:linyaps 默认转发宿主 `http_proxy/https_proxy/all_proxy`;公司私有 CA 追加到容器可写区并 `update-ca-certificates`。
```

- [ ] **Step 3:提交**

```bash
git add apps/desktop-launcher/linglong/config.d/20-host-credentials.json apps/desktop-launcher/README.md
git commit -m "docs(desktop-launcher): credential mount template and container usability notes"
```

---

## Phase D:harness 侧模型可见工具清单(独立子项目)

先决条件:Phase A–C 落地后单独执行;本阶段触及 harness 内部机制,遵循仓库规约("model-visible ⟺ logged"、REAL-composition 测试、keyless 快照)。

### Task 10:调查指令文本的日志路径并定实现分支

**Files:** 无(只读调查)

- [ ] **Step 1:确认模型可见指令的来源与日志**

Run:
```sh
grep -rn "log\|SessionEventMap" packages/context/agent-instructions/src --include='*.ts' | head -30
grep -rn "persona\|instructions" packages/context/agent-instructions/src/index.ts | head -20
rg -n "agent-instructions" packages/session packages/core --include='*.ts' -l | head
```
Expected: 判定指令文本(agent-instructions 内容)是否已被 session 日志以事件形式记录。

- [ ] **Step 2:按调查结果二选一**

- **分支 A(指令文本已日志化):** 在 desktop-launcher 的 harness overlay 中新增预设(见 Task 11),仅追加静态工具清单段,不加新事件。
- **分支 B(未日志化):** 在 session 事件映射为清单注入新增 **ignorable** 事件成员,并随注入同步触发(遵循 `SessionEventMap` 声明合并与 "required-on-read / ignorable" 机制),随后补 keyless 快照。

将结论与所选分支记录在 Task 12 的 Agent Note 中。

### Task 11:desktop-launcher harness overlay 预设

**Files:**
- Create: `apps/desktop-launcher/linglong/harness-overlay/config/agent-presets/desktop-tools/agent.cordis.yml`
- Create: `apps/desktop-launcher/linglong/harness-overlay/config/agent-presets/desktop-tools/preset.yml`
- Modify: `apps/desktop-launcher/linglong/linglong.yaml`(build: 内 overlay 复制)

- [ ] **Step 1:创建预设(preset.yml)**

```yaml
name: 桌面工具
description: 容器工具链自检清单注入(desktop-launcher 内置)。
order: 99
```

- [ ] **Step 2:创建 agent.cordis.yml 骨架**

```yaml
# 由 Task 10 的分支结论填充:内容 = 将 tools.yaml 的可用工具清单以
# 指令文本追加到 persona/instructions(分支 A),或增加额外注入事件(分支 B)。
# 结构参照 apps/cli/config/agent-presets/standard/agent.cordis.yml 的 realm 规则。
```

- [ ] **Step 3:linglong.yaml build 中 overlay 复制**

在 `linglong.yaml` 的 `build:` 复制 harness 后追加:

```sh
cp -a /project/apps/desktop-launcher/linglong/harness-overlay/config/agent-presets/. \
      ${PREFIX}/harness/config/agent-presets/
```

- [ ] **Step 4:验证与提交**

Run: `pnpm run test -- -t agent-presets`(仓库既有 preset 测试)与用户机器的玲珑 build。
```bash
git add apps/desktop-launcher/linglong/harness-overlay apps/desktop-launcher/linglong/linglong.yaml
git commit -m "feat(desktop-launcher): overlay desktop tools preset into harness"
```

### Task 12:Agent Note 与收尾

**Files:**
- Create: `.agents/notes/implemented/feature/2026-08-19-linglong-container-toolchain.md`

- [ ] **Step 1:写 Agent Note**

内容覆盖:三层防线设计、buildext 合并时序(preCommit → 宿主侧校验)、HOME/卸载语义、按需安装的静态产物策略、凭据分层、Phase D 分支结论(Task 10 记录)。遵循 `.agents/notes/README.md` 格式与 implemented-note 规则(现在时陈述已落地事实)。

- [ ] **Step 2:仓库门禁**

Run: `pnpm run lint && go test ./... -C apps/desktop-launcher`(apps/desktop-launcher 模块内)
Expected: 通过

- [ ] **Step 3:提交**

```bash
git add .agents/notes/implemented/feature/2026-08-19-linglong-container-toolchain.md
git commit -m "docs: add agent note for linglong container toolchain"
```

---

## 自审结果(写计划时已对照 spec)

- **Spec 覆盖**:层1(Task 1-3)、层3(Task 4-5)、自检面板(Task 6+8)、凭据分层(Task 7-9)、挂载/CA/代理(Task 9+模板)、harness 注入(Phase D Task 10-11)、Agent Note/门禁(Task 12)。spec 中"运行时 harness 侧清单"被明确为独立子项目(Phase D),避免在未调查指令日志路径前写死错误事件设计,与 `不新增 packages/ 包` 约束一致。
- **时序修正**:spec 原写"build: 内校验",核实 `buildext.apt.depends` 合并发生在 preCommit 后改为宿主侧 `verify-tools.sh` 校验合并产物树,spec 已同步更新(f20f073b)。
- **类型一致**:`ToolCheck`/`ToolSpec`/`DefaultToolSpecs`/`InstallTool(dir,name,version,url,sha256Hex)`/`ListTools(dir)`/`RemoveTool(dir,name)`/`ReadGitCredentials|WriteGitCredentials|ClearGitCredentials(home,...)`/`toolPanelState`/`credentialPanelState` 在任务间引用一致;`configurePackagedEnvForHome(home)` 为测试注入点,`configurePackagedEnv()` 保持零参调用方不变(main.go:11)。
- **无占位符**:唯一留空数据是 `tools.yaml` 中 go/ripgrep 的 sha256,由 Task 4 Step 5 的取数命令显式填充,非实现缺口。
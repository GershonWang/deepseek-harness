# Desktop launcher: Linglong sandbox container usability optimization design

English | [中文](2026-08-19-desktop-launcher-linglong-container-optimization-design.zh.md)

## Background and goals

`apps/desktop-launcher` runs the harness inside the Linglong container as a `dsh web` subprocess; the harness's bash toolchain (the `tool-bash`/shell capability) can only execute inside the container, and the available tools = the base runtime `org.deepin.base` + the software `buildext.apt.depends` brings in with the package. Pitfalls already hit: git is not in the package, so git operations on the repository are simply unavailable; Node must be bundled as 24 (beige's 20 cannot run); and the user's project directory, credentials, and CAs are unreachable from the container.

This design moves "hitting a wall at runtime" forward into three layers of defense: fixing and verifying the build-time tool inventory; a runtime self-check panel and a model-visible tool inventory; and on-demand runtime installation of heavy/rare tools. For the ordinary-user distribution scenario it takes **self-containment as the baseline** and treats the host environment as undependable.

Constraints: every change is in `apps/desktop-launcher/`, plus one harness-side inventory injection (a bundle/preset approach that adds no `packages/` package); the go/compiler chain is not included in the package body (size control).

## Confirmed requirements (aligned with the user)

| Decision | Conclusion |
|---|---|
| Target user | Ordinary-user distribution with no toolchain preinstalled on the host → a self-contained baseline |
| Overall direction | Move toward self-containment inside the container; borrowing the host toolchain is only an optional advanced item (such as read-only mounting host credentials) |
| Tool scope | git (already included) + common small tools (curl/wget/unzip/tar/jq/xxd) + python3 (+pip) + an extended package manager (through corepack; node 24 is already bundled); the go/compiler chain is **excluded** |
| On-demand runtime installation | **Included**: aimed at heavy/rare tools not in the package body, preferring static/self-contained artifacts, explicit, cancellable, and network-outage aware |
| git credentials | Layered: stored inside the container by default with the GUI showing the location/export/clear; an optional read-only mount of the host `~/.git-credentials` |
| Uninstall/reinstall | Linglong uninstall does not clear the host home or `~/.linglong/<appid>` (verified in source), so credentials survive by default, but the documentation states plainly "export a backup first" |

## Background facts (Linglong mechanics, verified in source)

- Linglong automatically writes `$PREFIX/bin` (`/opt/apps/<id>/files/bin`) into the container PATH and `$PREFIX/lib` into the ld search directories (linyaps `basic-notes.md`).
- `buildext.apt.depends` is merged into `$PREFIX` during the preCommit stage, stripping the `/usr` prefix (`/usr/bin` → `$PREFIX/bin`, `/usr/lib` → `$PREFIX/lib`), and transitive dependencies come along automatically.
- Inside the container `HOME` = the host's real user home directory (`bindHome` rbind, read-write); `~/.linglong/<appid>` is a private mapping area (masked inside the container), and `.ssh`/`.gnupg` are mapped into the private area in isolation by default; the host `~/.config`/`~/.cache` are still rbind-shared by default (replaced when a private subdirectory exists).
- `ll-cli uninstall` deletes only the application layer and `LINGLONG_ROOT/cache/<commit>`; it clears neither the host home nor `~/.linglong/<appid>` (verified in the PackageManager `removeCache` source).
- The application container shares the host network namespace, and proxy variables (`http_proxy`/`https_proxy`/`all_proxy`, and so on) are forwarded by `forwardDefaultEnv` by default, so the container can reach the internet and the host loopback directly.

## Overall architecture: three layers of defense

```
┌ 层1 构建期(打包时) ── tools.yaml 清单 ──► verify-tools.sh 逐项校验 → 缺失即构建失败
│
├ 层2 运行期(启动时) ── launcher 自检面板 + harness 模型可见工具清单(session event)
│
└ 层3 按需(运行时)  ── $HOME/.dsh-tools 静态产物安装 → PATH/LD_LIBRARY_PATH 注入
```

## Layer 1: Build-time tool inventory and verification

**`linglong/tools.yaml`** (new, single source of truth):

```yaml
tools:
  git:      # 仓库操作;via buildext apt depends
    binary: bin/git
    verify: git --version
  python3:  # 脚本/数据处理;via buildext apt depends(+pip)
    binary: bin/python3
    verify: python3 --version
  curl:     # https 请求/下载
    binary: bin/curl
    verify: curl --version
  wget:     # 下载回退/镜像脚本
    binary: bin/wget
    verify: wget --version
  jq:       # JSON 处理
    binary: bin/jq
    verify: jq --version
  unzip:
    binary: bin/unzip
  xxd:      # 十六进制转储/字节补丁脚本
    binary: bin/xxd
  pnpm:     # 包管理器;经 corepack(node 24 已捆)
    shim: true
    verify: shim_pnpm --version
# 说明:zip/unzip 经 buildext 带入;tar 由基础运行时 org.deepin.base 提供
# 第三层白名单:允许运行时按需安装的工具(不在包体内)
installable:
  - go
  - python3-standalone   # 需要更新版本时
  - ripgrep
# 有意不包含(体积控制):编译器链(gcc/clang/rustc)不随包,也不在按需白名单
excluded:
  - gcc
  - clang
  - rustc
```

**`linglong/verify-tools.sh`** (new): run on the **host machine** after `ll-builder build` completes, verifying the merged artifact tree (`linglong/output/binary/files`) — the `buildext.apt.depends` merge happens during preCommit, so the `build:` container stage cannot see the merged result and verification cannot happen inside the container. It checks item by item that `$PREFIX/<binary>` exists and is executable, and measures the version of shim-style tools (corepack). Any failing item → a non-zero exit, printing a hint about "which apt package this tool depends on and how to add it". The tool scope evolves by editing only `tools.yaml`; `buildext.apt.depends` and the verification script follow it.

**`linglong.yaml`**: `buildext.apt.depends` gains `python3`, `python3-pip`, `curl`, `wget`, `unzip`, `zip`, `jq`, `vim-common` (xxd), and `ca-certificates` (git is already there; tar comes from the base runtime).

**`build-linglong.sh`**: after `ll-builder build` and before `export`, call `verify-tools.sh linglong/output/binary/files`; a verification failure aborts, and no package is produced.

## Layer 2: Runtime self-check and a model-visible tool inventory

**GUI toolchain health panel** (launcher): probes key tools at startup (`git/python3/node/curl/jq/pnpm --version`) and shows them in the settings/status dialog; a missing item gets "one-click install (layer 3) or guidance".

**Harness-side inventory injection** (a bundle/preset approach that adds no `packages/` package): the desktop-launcher's harness deployment closure injects a tiny plugin through the preset cordis manifest, writing "the tools currently available in the container" into the session context injected into the system prompt so the model avoids calling commands that do not exist. It obeys the repository's iron rule "model-visible ⟺ logged": the injected content is also written as a new `SessionEventMap` event (a declaration-merging extension), so the model-visible input can be fully replayed from the session log.

**Error copy**: the bash tool's existing `command not found` passthrough is kept; the self-check panel is responsible for making things visible up front, without changing the tool itself.

## Layer 3: On-demand runtime installation (heavy/rare tools)

- **Storage**: `$HOME/.dsh-tools/<tool>-<ver>/`, versioned directories plus a `current` symlink, atomically unpacked after the sha256 check passes (`tar -x` into a temporary directory and then `mv`); `$HOME/.dsh-tools` lives on the host disk (mapped into the container HOME), survives uninstall by default, and is visible to and deletable by the user.
- **Visibility**: the launcher's `configurePackagedEnv()` (Go, an existing injection point) prepends `$HOME/.dsh-tools/bin` to PATH and adds `$HOME/.dsh-tools/lib` to LD_LIBRARY_PATH; the harness is restarted after installation for it to take effect.
- **Artifact form**: prefer static/self-contained runtimes — the official go tarball, a static jq binary, python-build-standalone (which bundles libpython) — avoiding glibc and postinst dependencies.
- **Download**: curl/wget (bundled by layer 1) plus the container's shared host network and forwarded proxy; the official source or npmmirror (the same pattern as the existing Node 24 download).
- **Entry point**: the GUI "tool management" page working together with the self-check panel (what is missing → what can be installed); explicit and cancellable, with a clear hint when the network is down, never failing silently.
- **Scope**: only tools on the `installable` allowlist in `tools.yaml`, preventing the model/user from installing arbitrary software.

## Credentials and user-data reachability

- **git credentials** (layered): by default enter a token in the GUI's "Git credentials" area, which is written to `$HOME/.git-credentials` inside the container (host disk, survives uninstall); the settings page states the storage location and offers copy/export/clear. An optional advanced path mounts the host `~/.git-credentials` (and `~/.ssh`) read-only into the container through a config.d template, so git inside the container uses the host credentials directly (lossless across uninstall/reinstall and machine changes; the documentation states the security semantics: the harness is already executing code on the user's behalf).
- **Project directories**: keep the `config.d/10-mounts.json` mechanism; the launcher guides the user through choosing the directory and placing the configuration in the GUI on first start, and verifies that the mount takes effect.
- **CA certificates**: `ca-certificates` ships in `$PREFIX` (git/https/python verification uses it); adding a corporate private CA is a documentation item (written into the container's writable area).
- **Proxy**: linyaps forwards by default; the documentation states it, with no code change.

## File changes (all under `apps/desktop-launcher/`, except the harness inventory injection)

| File | Change |
|---|---|
| `linglong/tools.yaml` (new) | The tool inventory's single source of truth (including the installable allowlist and excluded) |
| `linglong/verify-tools.sh` (new) | Build-time item-by-item verification; anything missing fails |
| `linglong/linglong.yaml` | `buildext.apt.depends` additions (python3/curl/wget/unzip/zip/jq/vim-common/ca-certificates) |
| `build-linglong.sh` | Call verify-tools.sh before export to verify the merged artifact tree |
| `env.go` | `configurePackagedEnv()` folds in `$HOME/.dsh-tools/bin` (PATH)/`lib` (LD_LIBRARY_PATH) |
| `ui.go` + new components | Toolchain health panel, tool management page, git credentials area, first-time mount guidance |
| harness inventory injection (bundle/preset, new) | The available-tool inventory injected into the system prompt + a new SessionEventMap event |
| `linglong/config.d/` (new template) | Examples of read-only mounts of the host `.git-credentials`/`.ssh` |
| `README.md` | Update the packaging points, credentials, on-demand installation, mounts, and proxy notes |

## Error handling

- A missing build-time inventory item → the build fails, hinting at which apt package is missing.
- A runtime self-check finds something missing → the panel hints plus one-click install/guidance.
- On-demand install failure / network outage / checksum failure → an explicit error report, never silent, and no half-finished leftovers (unpack in a temporary directory + atomic mv).
- A corrupt credentials file → the settings page hints and allows re-entry.

## Verification

- Build time: deliberately delete one item from `tools.yaml` → `verify-tools.sh` exits non-zero for the same artifact tree; with the full inventory → it passes.
- Runtime: run `git/python3/pnpm/jq --version` item by item inside the container through `ll-builder run --exec bash`.
- On-demand install: install go → restart the harness → `go version` works inside the container; a network outage yields a clear error.
- Harness inventory injection: per the repository's REAL-composition tests + a keyless snapshot (if it is product-visible behavior).
- Credentials: enter a token → assert the `git config --global credential.helper store` path inside the container + it is still there after uninstall/reinstall.

## Boundaries and risks

- **glibc compatibility**: non-static artifacts (such as user code built by go) depend on the container runtime's glibc version; the tool itself (the compiler) is fine, and running its artifacts is the user's responsibility, as the documentation states.
- **Package size growth**: python3 + pip is about +100MB; the compiler chain is left out per the excluded list.
- **The on-demand layer needs the network**: the first install requires connectivity, and offline only the baseline tools are usable; the entry point says so explicitly.
- **`.ssh` is isolated by default**: Linglong's private mapping means the container cannot see the host keys under `~/.ssh` by default; an optional mount template takes over.
- **mask `~/.linglong`**: the container cannot see `~/.linglong/<appid>` itself, so "stating the location" of credentials means the host path (the settings page shows the actual host path).

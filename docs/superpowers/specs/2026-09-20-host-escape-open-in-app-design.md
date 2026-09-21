# Desktop client: loading host developer tools in the Linglong sandbox — design

English | [中文](2026-09-20-host-escape-open-in-app-design.zh.md)

## Background and goals

`apps/desktop-launcher` runs the harness inside the Linglong container as a `dsh web` subprocess. The "Open in app" menu at the top of the Web GUI takes its inventory from [`dsh-host-open-in-app`](../../../packages/host/open-in-app/README.md) and is rendered by [`dsh-client-ui-open-in-app`](../../../packages/client/ui-open-in-app/README.md): when the harness runs directly on the host, the menu lists host developer tools such as VS Code and IntelliJ IDEA, yet the same package distributed into the Linglong container keeps only the single "File manager" entry.

Root cause: both detection and launch happen inside the namespace where the harness process runs. Linux entries have only two sources — `cli` (resolves the container PATH inside the process) and `desktop` (reads desktop entries under the container's `XDG_DATA_HOME`/`XDG_DATA_DIRS`); see [`catalog.ts`](../../../packages/host/open-in-app/src/catalog.ts) and [`resolver.ts`](../../../packages/host/open-in-app/src/resolver.ts). The container PATH carries no host IDE, and container `/usr/share/applications` holds only the entries shipped with the base runtime, so host IDEs neither appear nor launch. The only matching entry, "File manager", relies on a forwarding shell that Linglong provides itself (see below).

This design adds a **host escape channel** to open-in-app: inside the sandbox the harness can also detect and launch editors/IDEs installed on the host, opening the workspace directory on the host machine. It does not change sandbox policy, add sandbox permissions, or write host configuration (host files are read-only), and **behavior outside the sandbox stays byte-for-byte identical**.

## Confirmed requirements (aligned with the user)

| Decision | Conclusion |
|---|---|
| Approach | A1: desktop-launcher injects environment facts, and the harness side resolves them through `dsh-launch-environment` (isomorphic to the existing `launchedThroughSsh`) |
| Coverage | Reuse the existing catalog whitelist ids; zero changes to the client and i18n |
| User-level desktop directory | Include `~/.local/share/applications` (JetBrains desktop entries live there, with the same inode inside the container as on the host) |
| Behavior outside the sandbox | Without the fact the whole host branch takes no part in resolution; existing resolution results and the menu stay unchanged |
| SSH sessions | Keep the status quo (the existing fact already returns an empty inventory) |

## Background facts (measured on this machine)

Every conclusion was measured inside the current Linglong container (`LINGLONG_APPID=com.deepseek.dsh-desktop`), not inferred from documentation.

| Fact | Evidence |
|---|---|
| The host root filesystem is mounted read-only at `/run/host/rootfs` | `grep ' /run/host' /proc/self/mountinfo`; the host `/usr/share/applications` (95 desktop entries), `/usr/bin/code`, and icon themes are all readable |
| `$HOME` is the same directory with the same inode as on the host (read-write) | In mountinfo `/home/Jokul` is a bind mount of the host ext4 filesystem; `~/Documents/jetbrains/idea-IU-262.8665.258/bin/idea` and `~/.local/share/applications/jetbrains-idea.desktop` have the same path on both sides |
| The host session bus passes through | `/run/user/1000/bus` is connectable; `systemctl --user` finds the host `systemd --user` (PID 1311); `org.freedesktop.portal.Desktop` is reachable as well |
| Linglong ships its own host escape shell | The container `/bin/xdg-open` contains `systemd-run --user --service-type=forking /usr/bin/xdg-open "$@"`; `xdg-mime`/`xdg-email`/`xdg-settings` are built the same way. The file manager works because of it |
| `systemd-run` validates the outer executable only on the **container side** | `systemd-run --user … /usr/bin/code` → the client reports `Failed to find executable /usr/bin/code` (the host really has that file); container-private `/tmp/x.sh` → the client allows it and the host `execve` fails (`status=203`) |
| A path with the same inode on both sides can be executed by the host | Calling a script inside the workspace through `systemd-run` → `ls -d /run/host` inside the script reports that it does not exist, proving execution happens in the host namespace |
| A host GUI program really can be started | `systemd-run --user --service-type=exec -- /bin/sh -c 'exec "$0" "$@"' /usr/share/code/code --version` starts VS Code on the host (its PID and child processes are visible, with the host `DISPLAY=:0` and `XAUTHORITY=/home/Jokul/.Xauthority`) |
| The escape unit's environment comes from the host user manager | Inside the unit, `HOME=/home/Jokul`, `XAUTHORITY=/home/Jokul/.Xauthority`, `PATH=/usr/local/bin:/usr/bin:/bin:…` (not the container PATH) |
| Arguments are not expanded | With `--expand-environment=no`, `%` and `$` arguments arrive unchanged (`arg0=a%b%i%n`, `arg0=x$HOME-y${HOME}z`) |
| Launch failure is decidable inside the observation window | With `--service-type=exec` and no `--wait`: when the host `execve` fails, `systemd-run` exits with code 1; on success, 0 |
| The repository already has a precedent for the same mechanism | [`linux-scope.ts`](../../../packages/subprocess/subprocess-local/src/linux-scope.ts) uses `systemd-run --user --scope` to create `dsh-subprocess-*.scope` units; note that `--scope` forks on the client itself, so the process stays in the container namespace — only the service type is executed by the host |

## Overall architecture

```
┌ Go: desktop-launcher ────────────────────────────────────────────────┐
│ ConfigureChildEnv(): /run/host/rootfs 存在且 systemd-run 在 PATH       │
│   → 注入 DSH_HOST_ROOTFS / DSH_HOST_LAUNCH（未设置过才注入）           │
└───────────────────────────────┬──────────────────────────────────────┘
                                │ 环境（launch-environment 层次: process）
┌ harness: open-in-app ─────────▼──────────────────────────────────────┐
│ 1. hostEscapeOf(env) → { hostRootfs, launcher:'systemd-run' } | 无    │
│ 2. 通道探针: systemd-run --user --service-type=exec -- /bin/true      │
│    失败 → 整条 host-desktop 分支不参与解析                             │
│ 3. 探测: 白名单 desktopId → 宿主 desktop entry → Exec 首 token         │
│    路径在宿主视图/同 inode 视图下验证通过 → host-argv 启动器            │
│ 4. 启动: systemd-run --user … -- /bin/sh -c 'exec "$0" "$@"' <cmd> …   │
└──────────────────────────────────────────────────────────────────────┘
```

Every existing path (container `cli`/`desktop` matches, non-sandbox host, SSH) stays as it is: the host branch is only one link appended to the end of the locator chain, and it takes part only when the fact and the probe both hold.

## Interfaces and contracts

### 1. Environment fact: launch-environment

Add fact resolution isomorphic to `launchedThroughSsh` in [`packages/util/launch-environment`](../../../packages/util/launch-environment/src/index.ts).

```ts
/** 沙箱提供的宿主逃逸启动器;封闭枚举,新增沙箱以编译期扩展方式加入。 */
export type HostEscapeLauncher = 'systemd-run'

/** 本次启动可用的宿主逃逸通道;沙箱外为 undefined。 */
export interface HostEscapeFact {
  /** 沙箱内只读挂载宿主根文件系统的绝对路径。 */
  readonly hostRootfs: string
  readonly launcher: HostEscapeLauncher
}

/**
 * 从启动环境快照解析宿主逃逸事实。
 * 两个变量必须同时存在且取值合法,任一缺失即返回 undefined(按"没有该通道"处理)。
 */
export function hostEscapeOf(env: LaunchEnvironmentSnapshot): HostEscapeFact | undefined
```

Environment variable contract (injected by desktop-launcher, present only in this kind of sandbox):

| Variable | Value | Description |
|---|---|---|
| `DSH_HOST_ROOTFS` | `/run/host/rootfs` | Read-only host root mount point; must be an absolute path |
| `DSH_HOST_LAUNCH` | `systemd-run` | Escape launcher identifier; currently the only enum member |

Failure semantics of the values: `hostRootfs` is not an absolute path, `launcher` is not in the enum, or only one of the two variables appears → all are handled as "no channel" and startup continues. Here it deliberately does **not** fail loud: the fact is an optional enhancement, and its producer (the Go launcher) and its consumer (the harness package) can be upgraded and downgraded independently, so a mismatch in either direction must not keep the harness from starting. What really needs to fail loud is "the channel is declared but the directory is unusable", which the `stat` and the probe of the next step decide at resolution time.

### 2. Detection: catalog additions

`OpenInAppLocator` gains one member (all other members stay unchanged):

```ts
| {
    readonly kind: 'host-desktop'
    /** 候选 desktop id,按序尝试(覆盖上游与发行版的命名差异)。 */
    readonly desktopIds: readonly string[]
    /** 交给宿主应用的参数,通常为 [PATH_TOKEN]。 */
    readonly args: readonly string[]
  }
```

Lookup order (only the declared ids are looked up; host directories are **not** enumerated):

1. `<hostRoot>/usr/local/share/applications/<id>.desktop`
2. `<hostRoot>/usr/share/applications/<id>.desktop`
3. `<hostRoot>/opt/apps/*/files/share/applications/<id>.desktop` (Linglong apps installed on the host machine)
4. `~/.local/share/applications/<id>.desktop` (same inode as on the host)

Parsing and validation:

- Reuse the existing [`parseDesktopEntry`](../../../packages/host/open-in-app/src/resolver.ts) and `execCommand`: take the first `Exec` token (quoted form supported) as the host absolute path; replace `%f`/`%F`/`%u`/`%U` with `PATH_TOKEN` and drop every other field code unchanged.
- Path validation (all conditions required; a failed validation treats the entry as absent): see "Path validation matrix".
- The result is a `host-argv` launcher.

Linux entry additions (appended after the existing `cli`/`desktop`; anything resolvable inside the container still wins):

| catalog id | Appended `host-desktop` candidate id | Arguments |
|---|---|---|
| `vscode` | `code` | `[PATH_TOKEN]` |
| `vscodeinsiders` | `code-insiders` | `[PATH_TOKEN]` |
| `cursor` | `cursor` | `[PATH_TOKEN]` |
| `zed` | `dev.zed.Zed` | `[PATH_TOKEN]` |
| `sublimetext` | `sublime_text` | `[PATH_TOKEN]` |
| `androidstudio` | `android-studio` | `[PATH_TOKEN]` |
| `intellij` | `jetbrains-idea`, `intellij-idea` | `[PATH_TOKEN]` |
| `pycharm` | `jetbrains-pycharm`, `pycharm` | `[PATH_TOKEN]` |
| `webstorm` | `jetbrains-webstorm` | `[PATH_TOKEN]` |
| `phpstorm` | `jetbrains-phpstorm` | `[PATH_TOKEN]` |
| `goland` | `jetbrains-goland` | `[PATH_TOKEN]` |
| `rider` | `jetbrains-rider` | `[PATH_TOKEN]` |
| `rustrover` | `jetbrains-rustrover` | `[PATH_TOKEN]` |

On this machine `code.desktop`, `sublime_text.desktop`, `jetbrains-idea.desktop`, and `jetbrains-pycharm.desktop` were measured to exist. The ids of the remaining JetBrains products are declared as candidates following the naming convention of the upstream installer's "Create Desktop Entry"; multiple candidates are tried in order, so a naming guess does not harden into a dead path. Terminal and Git GUI entries work through the same mechanism but are out of scope this time (left as an extension point that needs no new code).

### 3. Launch: `host-argv` and the bridge argv

`OpenInAppLaunch` gains one member:

```ts
| {
    readonly kind: 'host-argv'
    /** 宿主命名空间中的绝对可执行路径(不是容器内路径)。 */
    readonly command: string
    readonly args: readonly string[]
  }
```

The `runLaunch` mapping (fixed argv, never assembled from a shell string):

```
systemd-run
  --user
  --collect
  --quiet
  --service-type=exec
  --expand-environment=no
  --unit=dsh-open-in-app-<appId>-<pid>-<12位十六进制>
  --
  /bin/sh -c 'exec "$0" "$@"' <command> <args…>
```

Key points and their basis:

- **`/bin/sh` as the bridge**: `systemd-run` validates the outer executable only on the container side, and `/bin/sh` exists on both sides so it passes validation; the host execs its own `/bin/sh`, which then execs the host-only path. This is how a host path that does not exist inside the container (`/usr/share/code/code`) can still launch.
- **It must be a service, not `--scope`**: `--scope` forks on the client itself and the process stays in the container namespace; `--service-type=exec` makes the host user manager `execve`, and a successful `execve` is what counts as a successful launch — a failure returns non-zero immediately (measured 1), which lands exactly in the observation-window semantics of the existing `launchDetachedApp`.
- **Arguments are not expanded**: `--expand-environment=no` guarantees `$` is not expanded; `%` was measured not to be treated by systemd-run as a specifier (arguments arrive unchanged). The bridge body passes argv with `exec "$0" "$@"` and uses no string interpolation.
- **Unit uniqueness**: `--unit` carries the pid and a random suffix to avoid name collisions; `--collect` makes sure transient exited units are reclaimed instead of accumulating.
- **env**: reuse `scrubbedParentEnv()` from `launchedThroughSsh`/`launchDetachedApp` (it keeps `DISPLAY`/`XAUTHORITY`/`DBUS_SESSION_BUS_ADDRESS` and only strips `*KEY*`/`*TOKEN*`/`*SECRET*`/`*PASSWORD*` and `DSH_*`); the host unit actually takes the environment of the host user manager, and `DISPLAY`/`XAUTHORITY`/`HOME` were all measured to be the host's correct values.
- **Workspace argument**: reuse the existing `launchArgs()` (`{path}` substitution, otherwise append), so the host IDE opens the same directory path as the session (the container and the host share the same path).

### 4. Channel probe (once, at resolution time)

Before resolution starts, if the fact exists, run a low-cost probe once:

```
systemd-run --user --collect --quiet --service-type=exec -- /bin/true
```

A non-zero exit code (host user manager unreachable, `systemd-run` trimmed away, sandbox policy changed) keeps the whole `host-desktop` branch out of resolution. That way the menu never shows an entry that errors when clicked — consistent with the existing contract of "show only the entries that can be verified on this machine".

### 5. Icons

When `host-desktop` matches, the icon lookup directories append `<hostRoot>/usr/local/share` and `<hostRoot>/usr/share` to the existing `xdgDataDirectories()` (the resolution logic for the hicolor theme and pixmaps reuses [`icons.ts`](../../../packages/host/open-in-app/src/icons.ts) as it stands). A miss falls back to the existing no-icon 404 and does not affect usability.

### 6. Client and i18n

Zero changes. The inventory lists existing ids whose copy is already in place in [`locales.ts`](../../../packages/client/ui-open-in-app/src/client/locales.ts); `OpenInAppController` only passes through the id list.

### 7. Go-side injection

In [`ConfigureChildEnv`](../../../apps/desktop-launcher/internal/appenv/env.go), inject `DSH_HOST_ROOTFS`/`DSH_HOST_LAUNCH` when "`/run/host/rootfs` exists" and "`systemd-run` resolves on PATH"; if either condition does not hold, inject neither. **An existing variable of the same name is not overwritten**, which keeps manual debugging and temporary disabling easy. It sits in the same place and follows the same style as the existing `/opt/host-tools` PATH injection.

## Relationship to existing decisions (found in passing)

The existing Agent Note [bundling a real xdg-open for host-browser opening](../../../.agents/notes/implemented/bug-fix/2026-08-27-bundle-xdg-open-for-host-browser-opening.md) records the base-layer `/bin/xdg-open` shell as "measured to recurse and fail", and relies on the premise that "the bundled xdg-utils land in `${PREFIX}/bin`, are first on the container PATH, and override that shell".

Checking on site contradicts that premise: the harness subprocess PATH is `/home/Jokul/.dsh-tools/bin:/bin:/usr/bin:/runtime/bin:/opt/apps/com.deepseek.dsh-desktop/files/bin:/usr/local/bin:…`, that is, `/bin/xdg-open` (a 75-byte shell) comes **before** `${PREFIX}/bin/xdg-open` (a real script of 32289 bytes); at the same time `/bin/xdg-open --help` forwarded through the shell to the host returns exit 0, and the recursion failure did not reproduce. The host-side PATH was measured to be the host default, and helper commands such as `xdg-mime` also resolve on the host to the host's real scripts, so no loop exists.

The conclusion has two parts:

- Relation to this design: the host channel in this design **does not depend** on any layer of xdg-open; the current state of the file manager entry is unrelated to it.
- Open item (outside this design's scope, but worth verifying in passing): whether the menu's "File manager" entry can really open the host file manager currently depends on that 75-byte shell; this needs one confirmation click on a real machine. If it proves unusable, the same host channel can cover the file manager entry too, but that would change the factual basis of the existing decision, so it should be raised as a separate change that corrects that Agent Note.

## Path validation matrix

The `Exec` target of `host-desktop` is validated with the host view first:

| Exec target form | Validation | Example on this machine |
|---|---|---|
| Host system path (`/usr/...`, `/opt/...`) | `stat(<hostRoot> + <path>)` exists and is executable | `/usr/share/code/code` ← `/run/host/rootfs/usr/share/code/code` |
| Path under `$HOME` (same inode on both sides) | `stat(<path>)` exists and is executable | `/home/Jokul/Documents/jetbrains/idea-IU-262.8665.258/bin/idea` |
| Other absolute paths | Neither side holds → the entry is unusable | — |

Only a path that passes validation produces a `host-argv` launcher, continuing the existing semantics of "a validated launcher, never a bare install record".

## Fallback matrix

| Environment | Fact | Container-side source | `host-desktop` | Result |
|---|---|---|---|---|
| Ordinary host (non-sandbox) | None | Matches | Not involved | Byte-for-byte identical to the status quo |
| SSH session | None (and the existing fact short-circuits) | — | Not involved | Empty inventory, consistent with the status quo |
| Linglong sandbox, rootfs present, host user manager available | Present | IDE does not match | Matches | A host IDE appears in the menu and launches on the host |
| Linglong sandbox, rootfs missing/unreadable | None, or `stat` fails | Does not match | Not involved | File manager only, consistent with the status quo |
| Linglong sandbox, probe fails | Present, but the probe fails | Does not match | Not involved | File manager only; no entry that cannot be clicked |
| Linglong sandbox, target unmounted at launch time | Present | — | Matched before | Existing `missing` → re-resolve once → on failure 502, reported through the existing error channel |

## Security model

- The channel **adds no capability**: `systemd-run` inside the container and Linglong's own `xdg-open` shell are reachable anyway, and the harness bash tool can call them too; this design only wires "launch a host program" into a constrained product feature.
- Constraint surface: it reads specific desktop entries only by whitelist id and never enumerates arbitrary host entries; the target path must be validated as existing and executable; arguments travel as argv, never as a shell string; on the web side every resolution result first passes the existing login cookie and Host/Origin fences ([`index.ts`](../../../packages/host/open-in-app/src/index.ts)).
- Trust surface change: including `~/.local/share/applications` means indexing desktop files the user can write (to cover the JetBrains desktop entries). Modifying that directory requires the same host privileges as the user, so the risk line is the same as the status quo, but it must be stated in the README and the Agent Note.
- Host applications run with the user's full privileges (the same as launching from the application menu), and the IDE's plugins, terminals, and language servers all live in the host environment; the container toolchain is a separate environment, and the documentation must make this difference explicit.
- Optional hardening (out of scope here): a resident helper / D-Bus service on the host side owns the whitelist and argument validation, moving the decision out of the container.

## Test case inventory

Unit tests (vitest):

- `launch-environment`: both variables present → the fact holds; one missing, empty value, relative path, non-enum `launcher` → no fact; non-Linux platforms are unaffected.
- `resolver` (with a fixture host rootfs, injected through `OpenInAppInternals`):
  - `host-desktop` matches and produces `host-argv` (first `Exec` token, quoted form, `%F`/`%f` substitution, appended arguments).
  - The target path holds on neither the host view nor the same-inode view → the entry is unusable.
  - Probe failure → the whole branch takes no part, and the inventory equals the one with no fact.
  - No fact → the resolution result is exactly the existing fixture (regression baseline).
  - `host-argv` argv construction: `--unit` uniqueness, the `/bin/sh -c` bridge, `--expand-environment=no`, workspace path injection.
  - The launcher returns non-zero → `failed`; `ENOENT` → `missing` and triggers the existing single re-resolution path.
- `icons`: host icon directory matched; a miss falls back to no icon.

Real composition tests (repository-mandated, product-visible plugin):

- The open-in-app real-composition spec: with a fixture host rootfs + the launcher seam of `internals.catalog`, assert that `GET /open-in-app/apps` contains `vscode` and that `POST /open-in-app/open` reaches the `host-argv` branch; the control group (no fact) asserts the inventory matches the status quo.

Go tests (`apps/desktop-launcher`):

- `ConfigureChildEnv`: rootfs present + `systemd-run` on PATH → both variables injected; either missing → neither injected; a variable of the same name already present → not overwritten.

On-device verification (no repackaging):

- Run `resolveOpenInAppApps()` directly from source through tsx, injecting the real `DSH_HOST_ROOTFS=/run/host/rootfs`, and print the resolution result → host IDE entries should appear.
- Start the source harness with the same environment and confirm the entries appear in the Web GUI menu (confirm with the user before actually launching an IDE).
- Full-chain acceptance requires repackaging and installing (`build-linglong.sh`), which is a high-risk operation that replaces an installed application; run it only after obtaining separate user consent.

Regression checks:

- `pnpm run test:gui` (the open-in-app host-side and client suites).
- Targeted `pnpm run typecheck` / `lint` for the packages touched.
- No session output or browser artifacts are involved, so no GUI snapshot refresh is expected; if measurement shows an impact, run `DSH_SNAPSHOT=replay pnpm run test:web` per repository rules.

## Commit breakdown (Conventional Commits, one commit per feature)

| # | Commit | Main files |
|---|---|---|
| 1 | `feat(launch-environment): recognize the sandbox host escape fact` | [`packages/util/launch-environment`](../../../packages/util/launch-environment/src/index.ts) + tests |
| 2 | `feat(desktop-launcher): inject the host escape environment fact` | [`appenv/env.go`](../../../apps/desktop-launcher/internal/appenv/env.go) + tests |
| 3 | `feat(open-in-app): support host-side desktop detection and escape launch` | [`catalog.ts`](../../../packages/host/open-in-app/src/catalog.ts), [`resolver.ts`](../../../packages/host/open-in-app/src/resolver.ts), [`icons.ts`](../../../packages/host/open-in-app/src/icons.ts) + tests |
| 4 | `docs(open-in-app): record the host escape contract and limitations` | Bilingual README + Agent Note (sandbox escape is long-term decision evidence) |

## File changes

| File | Change |
|---|---|
| [`packages/util/launch-environment/src/index.ts`](../../../packages/util/launch-environment/src/index.ts) | Add `HostEscapeLauncher`/`HostEscapeFact`/`hostEscapeOf` |
| [`packages/host/open-in-app/src/catalog.ts`](../../../packages/host/open-in-app/src/catalog.ts) | Add the `host-desktop` locator and the host candidates for each Linux entry |
| [`packages/host/open-in-app/src/resolver.ts`](../../../packages/host/open-in-app/src/resolver.ts) | Host desktop lookup and path validation, channel probe, `host-argv` launch |
| [`packages/host/open-in-app/src/index.ts`](../../../packages/host/open-in-app/src/index.ts) | Inject the host fact at the resolution entry point |
| [`packages/host/open-in-app/src/icons.ts`](../../../packages/host/open-in-app/src/icons.ts) | Host icon directories |
| [`apps/desktop-launcher/internal/appenv/env.go`](../../../apps/desktop-launcher/internal/appenv/env.go) | Conditionally inject the two environment facts |
| README (open-in-app bilingual, desktop-launcher) | Contract, limitations, security semantics |

## Deviations when implemented

Each difference comes from a measurement or from a smaller change surface, recorded so this document keeps matching the shipped code:

1. **Host data directories.** The document listed `${hostRoot}/opt/apps/*/files/share/applications` (a per-application scan of `/opt/apps`). The measured host exports Linglong application entries to `${hostRoot}/var/lib/linglong/entries/share/applications`, which already holds every exported entry, so the locator reads that one directory and does not scan `/opt/apps`.
2. **Field-code handling.** The document planned to turn `%f`/`%F`/`%u`/`%U` into the path token. The implementation reuses the existing `desktop` locator's rule instead — the first token of `Exec` is the program and the locator's own declared argv carries the directory — so no field-code substitution exists.
3. **Launch-failure classification.** A `host-argv` failure is always reported as `missing` (a stale resolution) so the open route re-resolves that one entry: at this layer the forwarding launcher's `exec` semantics cannot distinguish "the host program is gone" from "the host user manager is unreachable", and re-resolution is the only repair available inside the sandbox.
4. **Launcher value validation.** The resolver does not re-validate the `DSH_HOST_LAUNCH` value; `hostEscapeOf()` alone owns that decision (an unknown value means no channel), so one rule has one implementation.

## Known limitations and open questions

- Host applications outside the whitelist (such as `zcode` and `opencode` on this machine) are invisible; covering them requires new catalog ids, copy, and an icon fallback.
- Icons are best-effort: entries the host icon theme does not cover have no icon and take the existing 404 fallback.
- Whether `/run/host/rootfs` exists depends on the Linglong version and privilege configuration, so the fact and the probe are both conditional; when it is missing, everything silently falls back to the status quo.
- Coverage is limited to Linglong (`systemd-run`). Other sandboxes such as flatpak/snap extend by adding an enum member, and are not implemented here.
- No launch deduplication or auditing: repeated clicks reopen the host application (consistent with existing behavior); if needed, recording can be added later on the host launch path.
- The host IDE and the container toolchain are two separate environments, and the IDE's terminal/language server run on the host; users who need to "run tools inside the container" take the existing host toolchain mount (`hosttools`) and on-demand installation route, which complements this design.

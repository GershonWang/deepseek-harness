# 桌面客户端:玲珑沙箱下加载宿主开发工具 设计

[English](2026-09-20-host-escape-open-in-app-design.md) | 中文

## 背景与目标

`apps/desktop-launcher` 以 `dsh web` 子进程方式在玲珑容器内运行 harness。Web GUI 顶部的"在应用中打开"菜单由 [`dsh-host-open-in-app`](../../../packages/host/open-in-app/README.zh.md) 提供清单、[`dsh-client-ui-open-in-app`](../../../packages/client/ui-open-in-app/README.zh.md) 渲染:宿主上直接跑 harness 时菜单能列出 VS Code、IntelliJ IDEA 等宿主开发工具,同一个包分发到玲珑容器里只剩"文件管理器"一项。

根因:探测与启动都发生在 harness 进程所在的命名空间内。Linux 条目的来源只有两类——`cli`(在进程内解析容器 PATH)与 `desktop`(读容器 `XDG_DATA_HOME`/`XDG_DATA_DIRS` 下的 desktop entry),见 [`catalog.ts`](../../../packages/host/open-in-app/src/catalog.ts) 与 [`resolver.ts`](../../../packages/host/open-in-app/src/resolver.ts)。容器 PATH 上没有宿主 IDE,容器 `/usr/share/applications` 只有基础运行时自带条目,于是宿主 IDE 既不出现也无法启动。唯一命中的"文件管理器"靠的是玲珑自己提供的一层转发壳(见下)。

本设计给 open-in-app 增加一条**宿主逃逸通道**:沙箱内也能探测并启动宿主已安装的编辑器/IDE,工作区目录在宿主机上打开。不修改沙箱策略、不新增沙箱权限、不写宿主配置(只读宿主文件),并且**沙箱外行为逐字节不变**。

## 需求确认(已与用户对齐)

| 决策项 | 结论 |
|---|---|
| 方案 | A1:由 desktop-launcher 注入环境事实,harness 侧经 `dsh-launch-environment` 解析(与既有 `launchedThroughSsh` 同构) |
| 覆盖范围 | 沿用现有 catalog 白名单 id,客户端与 i18n 零改动 |
| 用户级 desktop 目录 | 纳入 `~/.local/share/applications`(JetBrains 桌面项在此,容器内与宿主同 inode) |
| 沙箱外行为 | 无事实时整条宿主分支不参与解析,现有解析结果与菜单不变 |
| SSH 会话 | 维持现状(已有 fact 直接返回空清单) |

## 背景事实(本机实测)

所有结论都在当前玲珑容器(`LINGLONG_APPID=com.deepseek.dsh-desktop`)内实测得到,不是文档推断。

| 事实 | 证据 |
|---|---|
| 宿主根文件系统只读挂在 `/run/host/rootfs` | `grep ' /run/host' /proc/self/mountinfo`;宿主 `/usr/share/applications` 95 个 desktop entry、`/usr/bin/code`、图标主题均可读 |
| `$HOME` 与宿主同目录同 inode(读写) | mountinfo 中 `/home/Jokul` 为宿主 ext4 绑定挂载;`~/Documents/jetbrains/idea-IU-262.8665.258/bin/idea`、`~/.local/share/applications/jetbrains-idea.desktop` 两侧同路径 |
| 宿主会话总线透传 | `/run/user/1000/bus` 可连;`systemctl --user` 查到宿主 `systemd --user`(PID 1311);`org.freedesktop.portal.Desktop` 亦可达 |
| 玲珑自带宿主逃逸壳 | 容器 `/bin/xdg-open` 内容为 `systemd-run --user --service-type=forking /usr/bin/xdg-open "$@"`;`xdg-mime`/`xdg-email`/`xdg-settings` 同构。文管能用即因为它 |
| `systemd-run` 只在**容器侧**校验外层可执行文件 | `systemd-run --user … /usr/bin/code` → 客户端报 `Failed to find executable /usr/bin/code`(宿主确实有该文件);容器私有 `/tmp/x.sh` → 客户端放行,宿主 execve 失败(`status=203`) |
| 两侧同 inode 的路径可被宿主执行 | 以工作区内脚本经 `systemd-run` 调用 → 脚本内 `ls -d /run/host` 报不存在,证明执行发生在宿主命名空间 |
| 宿主 GUI 程序确实能被拉起 | `systemd-run --user --service-type=exec -- /bin/sh -c 'exec "$0" "$@"' /usr/share/code/code --version` 在宿主启动 VS Code(PID 与子进程可见,带宿主 `DISPLAY=:0`、`XAUTHORITY=/home/Jokul/.Xauthority`) |
| 逃逸单元的环境来自宿主 user manager | 单元内 `HOME=/home/Jokul`、`XAUTHORITY=/home/Jokul/.Xauthority`、`PATH=/usr/local/bin:/usr/bin:/bin:…`(非容器 PATH) |
| 参数不被展开 | `%` 与 `$` 传参经 `--expand-environment=no` 后原样到达(`arg0=a%b%i%n`、`arg0=x$HOME-y${HOME}z`) |
| 启动失败可在观察窗内判定 | `--service-type=exec` 且不带 `--wait`:宿主 execve 失败时 `systemd-run` 退出码 1,成功时 0 |
| 仓库已有同类机制先例 | [`linux-scope.ts`](../../../packages/subprocess/subprocess-local/src/linux-scope.ts) 用 `systemd-run --user --scope` 建 `dsh-subprocess-*.scope`;注意 `--scope` 由客户端自己 fork,进程仍在容器命名空间,只有 service 类型才由宿主 exec |

## 整体架构

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

现有路径(容器内 `cli`/`desktop` 命中、非沙箱宿主、SSH)全部保持原状:宿主分支只是在定位器链尾部追加的一环,且只有事实与探针同时成立时才参与。

## 接口与契约

### 1. 环境事实:launch-environment

在 [`packages/util/launch-environment`](../../../packages/util/launch-environment/src/index.ts) 增加与 `launchedThroughSsh` 同构的事实解析。

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

环境变量契约(由 desktop-launcher 注入,只在这类沙箱内出现):

| 变量 | 取值 | 说明 |
|---|---|---|
| `DSH_HOST_ROOTFS` | `/run/host/rootfs` | 宿主根只读挂载点,必须是绝对路径 |
| `DSH_HOST_LAUNCH` | `systemd-run` | 逃逸启动器标识,当前仅此一枚举成员 |

取值的失败语义:`hostRootfs` 非绝对路径、`launcher` 不在枚举内、或两变量只出现一个 → 一律按"无通道"处理并继续启动。这里**不** fail loud:该事实是可选增强,且其生产者(Go launcher)与消费者(harness 包)可独立升降级,任一方向的不匹配都不该让 harness 起不来。真正需要 fail loud 的是"通道声明存在但目录不可用",由下一步的 `stat` 与探针在解析期判定。

### 2. 探测:catalog 增量

`OpenInAppLocator` 增加一个成员(其余成员不动):

```ts
| {
    readonly kind: 'host-desktop'
    /** 候选 desktop id,按序尝试(覆盖上游与发行版的命名差异)。 */
    readonly desktopIds: readonly string[]
    /** 交给宿主应用的参数,通常为 [PATH_TOKEN]。 */
    readonly args: readonly string[]
  }
```

查找顺序(只查声明的 id,**不枚举**宿主目录):

1. `<hostRoot>/usr/local/share/applications/<id>.desktop`
2. `<hostRoot>/usr/share/applications/<id>.desktop`
3. `<hostRoot>/opt/apps/*/files/share/applications/<id>.desktop`(宿主机上安装的玲珑应用)
4. `~/.local/share/applications/<id>.desktop`(与宿主同 inode)

解析与验证:

- 复用既有 [`parseDesktopEntry`](../../../packages/host/open-in-app/src/resolver.ts) 与 `execCommand`:取 `Exec` 首 token(支持引号形式)为宿主绝对路径;`%f`/`%F`/`%u`/`%U` 替换为 `PATH_TOKEN`,其余 field code 原样丢弃。
- 路径验证(缺一不可,验证不过即视为该条目不存在):见"路径校验矩阵"。
- 产物为 `host-argv` 启动器。

Linux 条目的增量(在既有 `cli`/`desktop` 之后追加,容器内可解析的仍优先):

| catalog id | 追加的 `host-desktop` 候选 id | 参数 |
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

本机实测存在 `code.desktop`、`sublime_text.desktop`、`jetbrains-idea.desktop`、`jetbrains-pycharm.desktop`。JetBrains 其余产品的 id 按上游安装器的 "Create Desktop Entry" 命名约定声明为候选;多候选按序尝试,避免把命名猜测固化成死路径。终端与 Git GUI 类条目同机制可用,本次不纳入(留作扩展点,无需新代码)。

### 3. 启动:`host-argv` 与桥接 argv

`OpenInAppLaunch` 增加一个成员:

```ts
| {
    readonly kind: 'host-argv'
    /** 宿主命名空间中的绝对可执行路径(不是容器内路径)。 */
    readonly command: string
    readonly args: readonly string[]
  }
```

`runLaunch` 的映射(固定 argv,绝不拼 shell 字符串):

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

要点与依据:

- **`/bin/sh` 作桥**:`systemd-run` 只在容器侧校验外层可执行文件,`/bin/sh` 两侧都存在因而通过校验;宿主 exec 的是宿主自己的 `/bin/sh`,再由它 exec 宿主专属路径。这样容器内不存在的宿主路径(`/usr/share/code/code`)也能启动。
- **必须是 service 而不是 `--scope`**:`--scope` 由客户端 fork,进程留在容器命名空间;`--service-type=exec` 由宿主 user manager execve,`execve` 成功才算启动成功,失败立即返回非零(实测 1),正好落进现有 `launchDetachedApp` 的观察窗语义。
- **参数不展开**:`--expand-environment=no` 保证 `$` 不被展开;`%` 实测不被 systemd-run 当作说明符(传参原样到达)。桥体用 `exec "$0" "$@"` 传递 argv,不使用任何字符串插值。
- **单元唯一性**:`--unit` 带 pid 与随机后缀,避免同名冲突;`--collect` 保证退出的瞬时单元被回收,不累积。
- **env**:沿用 `launchedThroughSsh`/`launchDetachedApp` 的 `scrubbedParentEnv()`(它保留 `DISPLAY`/`XAUTHORITY`/`DBUS_SESSION_BUS_ADDRESS`,只清 `*KEY*`/`*TOKEN*`/`*SECRET*`/`*PASSWORD*` 与 `DSH_*`);宿主单元实际取的是宿主 user manager 的环境,实测 `DISPLAY`/`XAUTHORITY`/`HOME` 均为宿主正确值。
- **工作区参数**:沿用既有 `launchArgs()`(`{path}` 替换,否则追加),因此宿主 IDE 打开的是与会话同一个目录路径(容器与宿主同路径)。

### 4. 通道探针(解析期一次)

解析开始前,若事实存在,先跑一次低成本探针:

```
systemd-run --user --collect --quiet --service-type=exec -- /bin/true
```

退出码非 0(宿主 user manager 不可达、`systemd-run` 被裁剪、沙箱策略变化)则整条 `host-desktop` 分支不参与解析。这样菜单里不会出现"点了报错"的条目——与现有"只显示本机能够验证的条目"契约一致。

### 5. 图标

`host-desktop` 命中时,图标查找目录在现有 `xdgDataDirectories()` 基础上追加 `<hostRoot>/usr/local/share`、`<hostRoot>/usr/share`(hicolor 主题与 pixmaps 的解析逻辑复用 [`icons.ts`](../../../packages/host/open-in-app/src/icons.ts) 现状)。查不到即走现有 404 无图标回退,不影响可用性。

### 6. 客户端与 i18n

零改动。清单里出现的是既有 id,文案已在 [`locales.ts`](../../../packages/client/ui-open-in-app/src/client/locales.ts) 就绪;`OpenInAppController` 只透传 id 列表。

### 7. Go 侧注入

在 [`ConfigureChildEnv`](../../../apps/desktop-launcher/internal/appenv/env.go) 中,满足"`/run/host/rootfs` 存在"且"`systemd-run` 在 PATH 上可解析"时注入 `DSH_HOST_ROOTFS`/`DSH_HOST_LAUNCH`;两个条件任一不成立则都不注入。**已存在同名变量时不覆盖**,便于手工调试与临时关闭。与既有 `/opt/host-tools` PATH 注入同一处、同一风格。

## 与既有决策的关系(顺带发现)

既有 Agent Note [为宿主浏览器打开随包合并真实 xdg-open](../../../.agents/notes/implemented/bug-fix/2026-08-27-bundle-xdg-open-for-host-browser-opening.zh.md) 把基础层 `/bin/xdg-open` 壳记为"实测递归失败",并依赖"随包的 xdg-utils 落在 `${PREFIX}/bin`,是容器 PATH 首位、盖过该壳"这一前提。

现场核对与此前提不符:harness 子进程的 PATH 为 `/home/Jokul/.dsh-tools/bin:/bin:/usr/bin:/runtime/bin:/opt/apps/com.deepseek.dsh-desktop/files/bin:/usr/local/bin:…`,即 `/bin/xdg-open`(75 字节壳)**排在** `${PREFIX}/bin/xdg-open`(32289 字节真实脚本)之前;同时 `/bin/xdg-open --help` 经壳转发到宿主后返回 exit 0,未复现递归失败。宿主侧 PATH 实测为宿主默认值,`xdg-mime` 等辅助命令在宿主解析到的也是宿主真实脚本,不构成回环。

结论分两点:

- 与本设计的关系:本设计的宿主通道**不依赖** xdg-open 任何一层,文件管理器条目的现状与它无关。
- 待确认项(不属于本设计范围,但建议顺带核实):菜单里"文件管理器"目前是否真的能打开宿主文管,取决于那层 75 字节壳;需要一次真机点击确认。若实测不可用,同一套宿主通道也能覆盖文件管理器条目,但它会改变既有决策的事实基础,应单独提一个改动去订正该 Agent Note。

## 路径校验矩阵

`host-desktop` 的 `Exec` 目标按宿主视图优先验证:

| Exec 目标形态 | 校验方式 | 本机例子 |
|---|---|---|
| 宿主系统路径(`/usr/...`、`/opt/...`) | `stat(<hostRoot> + <path>)` 存在且可执行 | `/usr/share/code/code` ← `/run/host/rootfs/usr/share/code/code` |
| `$HOME` 下路径(两侧同 inode) | `stat(<path>)` 存在且可执行 | `/home/Jokul/Documents/jetbrains/idea-IU-262.8665.258/bin/idea` |
| 其它绝对路径 | 两侧均不成立 → 该条目不可用 | — |

只有通过验证的路径才会生成 `host-argv` 启动器,延续现有"已验证的启动器,绝不是裸安装记录"的语义。

## 回退矩阵

| 环境 | 事实 | 容器内来源 | `host-desktop` | 结果 |
|---|---|---|---|---|
| 普通宿主(非沙箱) | 无 | 命中 | 不参与 | 与现状逐字节一致 |
| SSH 会话 | 无(且既有 fact 短路) | — | 不参与 | 空清单,与现状一致 |
| 玲珑沙箱,rootfs 存在,宿主 user manager 可用 | 有 | IDE 不命中 | 命中 | 菜单出现宿主 IDE,启动落在宿主 |
| 玲珑沙箱,rootfs 缺失/不可读 | 无,或 `stat` 失败 | 不命中 | 不参与 | 仅文管,与现状一致 |
| 玲珑沙箱,探针失败 | 有,但探针失败 | 不命中 | 不参与 | 仅文管,不出现点不动的条目 |
| 玲珑沙箱,启动瞬间目标被卸载 | 有 | — | 曾命中 | 现有 `missing` → 重解析一次 → 失败则 502,提示走现有错误通道 |

## 安全模型

- 该通道**不新增能力**:容器内 `systemd-run` 与玲珑自带的 `xdg-open` 壳本就可达,harness 的 bash 工具同样能调用;本设计只是把"启动宿主程序"接进一个受约束的产品功能。
- 约束面:只按白名单 id 读取特定 desktop entry,不枚举任意宿主条目;目标路径必须验证存在且可执行;参数以 argv 传递,不用 shell 字符串;web 侧进入任何解析结果前先过既有登录 cookie 与 Host/Origin 栅栏([`index.ts`](../../../packages/host/open-in-app/src/index.ts))。
- 信任面变化:纳入 `~/.local/share/applications` 意味着索引用户可写的 desktop 文件(为覆盖 JetBrains 桌面项)。改动该目录需要与用户同等的宿主权限,风险边界与现状一致,但需在 README 与 Agent Note 中写明。
- 宿主应用以用户级完整权限运行(与从应用菜单启动一致),IDE 的插件、终端、语言服务器都在宿主环境;容器内工具链是另一套环境,文档需明确这一差异。
- 可选加固(不在本次范围):宿主侧常驻 helper / D-Bus 服务承担白名单与参数校验,把决策移出容器。

## 测试用例清单

单元测试(vitest):

- `launch-environment`:两变量齐全 → 事实成立;缺一、空值、相对路径、非枚举 `launcher` → 无事实;非 Linux 平台不影响。
- `resolver`(以 fixture 宿主 rootfs,经 `OpenInAppInternals` 注入):
  - `host-desktop` 命中并产出 `host-argv`(`Exec` 首 token、引号形式、`%F`/`%f` 替换、参数追加)。
  - 目标路径在宿主视图与同 inode 视图下均不成立 → 条目不可用。
  - 探针失败 → 整条分支不参与,清单与无事实时一致。
  - 无事实 → 解析结果与现有 fixture 完全一致(回归基线)。
  - `host-argv` 的 argv 构造:`--unit` 唯一性、`/bin/sh -c` 桥、`--expand-environment=no`、工作区路径注入。
  - launcher 返回非零 → `failed`;`ENOENT` → `missing` 并触发既有重解析一次路径。
- `icons`:宿主图标目录命中;缺失回退无图标。

真实组合测试(仓库强制,产品可见插件):

- open-in-app 的 real-composition spec:以 fixture 宿主 rootfs + `internals.catalog` 的 launcher seam,断言 `GET /open-in-app/apps` 含 `vscode`、`POST /open-in-app/open` 走到 `host-argv` 分支;对照组(无事实)断言清单与现状一致。

Go 测试(`apps/desktop-launcher`):

- `ConfigureChildEnv`:rootfs 存在 + `systemd-run` 在 PATH → 注入两变量;缺任一 → 两者都不注入;同名变量已存在 → 不覆盖。

真机验证(不重打包):

- 以 tsx 直跑源码调用 `resolveOpenInAppApps()`,注入真实 `DSH_HOST_ROOTFS=/run/host/rootfs` 打印解析结果 → 应出现宿主 IDE 条目。
- 用同一环境启动源码 harness,在 Web GUI 菜单中确认条目出现(真实拉起 IDE 前先与用户确认)。
- 全链路验收需重打包安装(`build-linglong.sh`),属替换已安装应用的高风险操作,单独取得用户同意后执行。

回归检查:

- `pnpm run test:gui`(open-in-app 宿主侧与客户端套件)。
- 触及包定向 `pnpm run typecheck` / `lint`。
- 不涉及会话输出与浏览器产物,预计无需刷新 GUI 快照;若实测有影响则按仓库规则跑 `DSH_SNAPSHOT=replay pnpm run test:web`。

## 提交切分(Conventional Commits,一功能一提交)

| # | 提交 | 主要文件 |
|---|---|---|
| 1 | `feat(launch-environment): 识别沙箱宿主逃逸事实` | [`packages/util/launch-environment`](../../../packages/util/launch-environment/src/index.ts) + tests |
| 2 | `feat(desktop-launcher): 注入宿主逃逸环境事实` | [`appenv/env.go`](../../../apps/desktop-launcher/internal/appenv/env.go) + tests |
| 3 | `feat(open-in-app): 支持宿主侧 desktop 探测与逃逸启动` | [`catalog.ts`](../../../packages/host/open-in-app/src/catalog.ts)、[`resolver.ts`](../../../packages/host/open-in-app/src/resolver.ts)、[`icons.ts`](../../../packages/host/open-in-app/src/icons.ts) + tests |
| 4 | `docs(open-in-app): 记录宿主逃逸契约与局限` | 双语 README + Agent Note(沙箱逃逸属长期决策依据) |

## 文件变更

| 文件 | 变更 |
|---|---|
| [`packages/util/launch-environment/src/index.ts`](../../../packages/util/launch-environment/src/index.ts) | 新增 `HostEscapeLauncher`/`HostEscapeFact`/`hostEscapeOf` |
| [`packages/host/open-in-app/src/catalog.ts`](../../../packages/host/open-in-app/src/catalog.ts) | 新增 `host-desktop` 定位器与各 Linux 条目的宿主候选 |
| [`packages/host/open-in-app/src/resolver.ts`](../../../packages/host/open-in-app/src/resolver.ts) | 宿主 desktop 查找与路径验证、通道探针、`host-argv` 启动 |
| [`packages/host/open-in-app/src/index.ts`](../../../packages/host/open-in-app/src/index.ts) | 解析入口注入宿主事实 |
| [`packages/host/open-in-app/src/icons.ts`](../../../packages/host/open-in-app/src/icons.ts) | 宿主图标目录 |
| [`apps/desktop-launcher/internal/appenv/env.go`](../../../apps/desktop-launcher/internal/appenv/env.go) | 条件注入两个环境事实 |
| README(open-in-app 双语、desktop-launcher) | 契约、限制、安全语义 |

## 落地差异(实施后回填)

以下差异都来自实测或更小的改动面,记录在此以便本文档与已落地代码保持一致:

1. **宿主数据目录。** 文档原列 `${hostRoot}/opt/apps/*/files/share/applications`(逐应用扫描 `/opt/apps`)。实测宿主把玲珑应用条目统一导出到 `${hostRoot}/var/lib/linglong/entries/share/applications`,该目录已包含全部导出条目,因此实际只读这一个目录,不再逐应用扫描。
2. **字段码处理。** 文档原计划把 `%f`/`%F`/`%u`/`%U` 替换为路径 token。实现改为复用既有 `desktop` locator 的规则——取 `Exec` 的首个 token 作为程序,目录由 locator 自己声明的 argv 传入——因此不存在字段码替换逻辑。
3. **启动失败归类。** `host-argv` 的失败一律按 `missing`(失效解析)上报,让 open 路由只重解析该条目一次:在这一层,转发启动器的 `exec` 语义无法区分「宿主程序已消失」与「宿主用户管理器不可达」,而重解析是沙箱内唯一可用的修复手段。
4. **启动器取值校验。** 解析器不重复校验 `DSH_HOST_LAUNCH` 的取值,该判定只由 `hostEscapeOf()` 承担(未知取值即无通道),同一条规则只有一处实现。

## 已知限制与未决问题

- 白名单之外的宿主应用(如本机的 `zcode`、`opencode`)不可见;要覆盖需新增 catalog id、文案与图标兜底。
- 图标尽力而为:宿主图标主题未覆盖的条目无图标,走既有 404 回退。
- `/run/host/rootfs` 的存在取决于玲珑版本与权限配置,因此事实与探针都是条件性的;缺失时静默退回现状。
- 覆盖范围限于玲珑(`systemd-run`)。flatpak/snap 等其它沙箱以枚举成员方式扩展,本次不实现。
- 未做启动去重与审计:重复点击会重复打开宿主应用(与现有行为一致);若需要,可后续在宿主启动路径上加记录。
- 宿主 IDE 与容器内工具链是两套环境,IDE 的终端/语言服务器运行在宿主;需要"在容器里跑工具"的用户走既有的宿主工具链挂载(`hosttools`)与按需安装路线,与本设计互补。

# 桌面启动器:玲珑沙箱容器可用性优化设计

## 背景与目标

`apps/desktop-launcher` 以 `dsh web` 子进程方式在玲珑容器内运行 harness;harness 的 bash 工具链(`tool-bash`/shell 能力)只能在容器内执行,可用工具 = 基础运行时 `org.deepin.base` + `buildext.apt.depends` 随包带入的软件。已发生的碰壁:git 不在包内,仓库 git 操作直接不可用;Node 必须捆绑 24(beige 的 20 跑不起来);用户项目目录、凭据、CA 对容器不可达。

本设计把"运行时碰壁"前移为三层防线:构建期工具清单固化与校验;运行时自检面板与模型可见工具清单;重/罕见工具的运行时按需安装。普通用户分发场景下以**自包含为基线**,宿主环境视为不可依赖。

约束:所有改动在 `apps/desktop-launcher/`,另加一个 harness 侧清单注入(bundle/预设方式,不新增 `packages/` 包);不把 go/编译器链纳入包体(体积控制)。

## 需求确认(已与用户对齐)

| 决策项 | 结论 |
|---|---|
| 目标用户 | 普通用户分发,宿主机上不预设工具链 → 自包含基线 |
| 大方向 | 容器内向自包含;借用宿主工具链仅作可选高级项(如只读挂载宿主凭据) |
| 工具范围 | git(已含)+ 常用小工具(curl/wget/unzip/tar/jq/xxd)+ python3(+pip)+ 扩展包管理器(经 corepack,node 24 已捆);**排除** go/编译器链 |
| 运行时按需安装 | **纳入**:面向不在包体内的重/罕见工具,静态/自带运行时产物优先,显式、可取消、断网可感知 |
| git 凭据 | 分层:默认容器内存储 + GUI 明示位置/导出/清除;可选宿主 `~/.git-credentials` 只读挂载 |
| 卸载重装 | 玲珑卸载不清宿主 home 与 `~/.linglong/<appid>`(源码核实),凭据默认保留,但文档明示"建议导出备份" |

## 背景事实(玲珑机制,源码核实)

- 玲珑运行自动把 `$PREFIX/bin`(`/opt/apps/<id>/files/bin`)写进容器 PATH,`$PREFIX/lib` 进 ld 搜索目录(linyaps `basic-notes.md`)。
- `buildext.apt.depends` 在 preCommit 阶段合并进 `$PREFIX`,剥离 `/usr` 前缀(`/usr/bin` → `$PREFIX/bin`,`/usr/lib` → `$PREFIX/lib`),传递依赖自动带入。
- 容器内 `HOME` = 宿主真实用户主目录(`bindHome` rbind、读写);`~/.linglong/<appid>` 是私有映射区(容器内掩蔽),`.ssh`/`.gnupg` 默认被隔离映射进私有区;宿主 `~/.config`/`~/.cache` 默认仍 rbind 共享(私有子目录存在时替换)。
- `ll-cli uninstall` 只删除应用层与 `LINGLONG_ROOT/cache/<commit>`,不清宿主 home、也不清 `~/.linglong/<appid>`(PackageManager `removeCache` 源码核实)。
- 应用容器共享宿主网络命名空间,代理变量(`http_proxy`/`https_proxy`/`all_proxy` 等)由 `forwardDefaultEnv` 默认转发,容器内可直连外网与宿主 loopback。

## 整体架构:三层防线

```
┌ 层1 构建期(打包时) ── tools.yaml 清单 ──► verify-tools.sh 逐项校验 → 缺失即构建失败
│
├ 层2 运行期(启动时) ── launcher 自检面板 + harness 模型可见工具清单(session event)
│
└ 层3 按需(运行时)  ── $HOME/.dsh-tools 静态产物安装 → PATH/LD_LIBRARY_PATH 注入
```

## 第一层:构建期工具清单与校验

**`linglong/tools.yaml`**(新,单一事实来源):

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

**`linglong/verify-tools.sh`**(新):在 `build:` 阶段末尾执行,逐项检查 `$PREFIX/<binary>` 存在且可执行;shim 类工具(corepack)实测版本。任一项失败 → 构建退出非零,并打印"该工具依赖什么 apt 包、如何补"提示。工具范围演进只改 `tools.yaml`;`buildext.apt.depends` 与校验脚本以它为准。

**`linglong.yaml`**:`buildext.apt.depends` 新增 `python3`、`python3-pip`、`curl`、`wget`、`unzip`、`zip`、`jq`、`vim-common`(xxd)、`ca-certificates`(git 已加;tar 由基础运行时提供);`build:` 末尾调用 `verify-tools.sh`。

## 第二层:运行时自检与模型可见工具清单

**GUI 工具链健康度面板**(launcher):启动时探测关键工具(`git/python3/node/curl/jq/pnpm --version`),在设置/状态弹框展示;缺失项给"一键安装(第三层)或指引"。

**harnes 侧清单注入**(bundle/预设方式,不新增 `packages/` 包):desktop-launcher 的 harness 部署闭包通过预设 cordis 清单注入一个微型插件,把"容器内当前可用工具"写入会话上下文注入系统提示,让模型避免调用不存在的命令。遵守仓库铁律 "model-visible ⟺ logged":注入内容同时写入一条新 `SessionEventMap` 事件(声明合并扩展),模型可见输入可完整从 session 日志重放。

**报错文案**:bash 工具既有 `command not found` 透传保留;自检面板负责前置可见,不改工具本身。

## 第三层:运行时按需安装(重/罕见工具)

- **存储**:`$HOME/.dsh-tools/<tool>-<ver>/`,版本目录化 + `current` 软链,sha256 校验通过后原子解包(`tar -x` 到临时目录再 `mv`);`$HOME/.dsh-tools` 位于宿主磁盘(容器 HOME 映射),卸载默认保留、用户可见可删。
- **可见性**:launcher `configurePackagedEnv()`(Go,已有注入点)把 `$HOME/.dsh-tools/bin` 前置进 PATH、`$HOME/.dsh-tools/lib` 进 LD_LIBRARY_PATH;安装完成后重启 harness 生效。
- **产物形态**:优先静态/自带运行时——go 官方 tar、jq 静态二进制、python-build-standalone(自带 libpython),避开 glibc 与 postinst 依赖。
- **下载**:curl/wget(经第一层随包)+ 容器共享宿主网络与转发代理;官方源或 npmmirror(与现有 Node 24 下载同模式)。
- **入口**:GUI"工具管理"页 + 自检面板联动(缺什么 → 提示可装什么);显式、可取消,断网给明确提示,不静默失败。
- **范围**:仅允许在 `tools.yaml` 的 `installable` 白名单内,防止模型/用户安装任意软件。

## 凭据与用户数据可达性

- **git 凭据**(分层):默认经 GUI"Git 凭据"区填入令牌,写入容器内 `$HOME/.git-credentials`(宿主磁盘、卸载保留);设置页明示存储位置,提供复制/导出/清除。可选高级:config.d 模板只读挂载宿主 `~/.git-credentials`(及 `~/.ssh`)进容器,容器内 git 直用宿主凭据(卸载重装、换机无损;文档说明安全语义:harness 本就在替用户执行代码)。
- **项目目录**:保留 `config.d/10-mounts.json` 机制;launcher 首次启动在 GUI 内引导完成选择目录与放置配置,并校验挂载生效。
- **CA 证书**:`ca-certificates` 随包进 `$PREFIX`(git/https/python 校验走它);公司私有 CA 追加作文档项(写入容器可写区)。
- **代理**:linyaps 默认转发,文档写明,无代码改动。

## 文件变更(全在 `apps/desktop-launcher/`,harness 清单注入除外)

| 文件 | 变更 |
|---|---|
| `linglong/tools.yaml`(新) | 工具清单单一事实来源(含 installable 白名单与 excluded) |
| `linglong/verify-tools.sh`(新) | 构建期逐项校验,缺失即失败 |
| `linglong/linglong.yaml` | `buildext.apt.depends` 增补;`build:` 末尾调 verify-tools.sh |
| `env.go` | `configurePackagedEnv()` 汇入 `$HOME/.dsh-tools/bin`(PATH)/`lib`(LD_LIBRARY_PATH) |
| `ui.go` + 新组件 | 工具链健康度面板、工具管理页、Git 凭据区、首次挂载引导 |
| harness 清单注入(bundle/预设,新) | 可用工具清单注入系统提示 + 新 SessionEventMap 事件 |
| `linglong/config.d/`(新模板) | 宿主 `.git-credentials`/`.ssh` 只读挂载示例 |
| `README.md` | 更新打包要点、凭据、按需安装、挂载与代理说明 |

## 错误处理

- 构建期清单缺失 → 构建失败,提示缺哪个 apt 包。
- 运行期自检发现缺失 → 面板提示 + 一键安装/指引。
- 按需安装失败/断网/校验失败 → 显式错误上报,不静默,不残留半成品(临时目录解包 + 原子 mv)。
- 凭据文件损坏 → 设置页提示并允许重新录入。

## 验证

- 构建期:故意从 `tools.yaml` 删一项 → `ll-builder build` 失败;全量清单 → 构建通过。
- 运行期:`ll-builder run --exec bash` 容器内逐项 `git/python3/pnpm/jq --version`。
- 按需安装:安装 go → 重启 harness → 容器内 `go version` 可用;断网场景给明确错误。
- harness 清单注入:按仓库 REAL-composition 测试 + keyless 快照(若为产品可见行为)。
- 凭据:录入令牌 → 容器内 `git config --global credential.helper store` 路径断言 + 卸载重装后仍在。

## 边界与风险

- **glibc 兼容**:非静态产物(如 go 编译出的用户代码)依赖容器运行时 glibc 版本;工具自身(编译器)无碍,跑其产物是用户责任,文档写明。
- **包体增大**:python3 + pip 约 +100MB;编译器链按排除清单不纳入。
- **按需层依赖网络**:首次安装需联网,离线场景仅基线工具可用;入口显式提示。
- **`.ssh` 默认隔离**:玲珑私有映射使容器内 `~/.ssh` 默认看不到宿主密钥;可选挂载模板承接。
- **mask `~/.linglong`**:容器内看不到 `~/.linglong/<appid>` 本身,凭据"位置明示"指宿主路径(设置页展示实际宿主路径)。
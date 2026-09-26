# 工具链索引改版的构建与发布步骤

本清单对应的改动：工具版本按大版本线分组、`jdk21` 改名为 `jdk` 并迁移既有安装、
索引数据重写（43 项升版 + 11 条新大版本条目 + 每版本显式声明 `major`）。

**这些步骤需要 `ll-builder`，当前开发环境没有该工具**（也没有 `make`），因此构建与
发布必须在本机之外的、具备玲珑构建器的机器上执行。清单里的每条命令都注明了它做什么
以及失败时会怎样，便于逐步核对而不是一把梭。

---

## 第 0 步：确认待发布的提交

```sh
git log --oneline origin/linglong-dev..HEAD
git rev-list --count origin/linglong-dev..HEAD
```

预期看到 4 笔提交（顺序从新到旧）：

```
c039c5c8c3 test(toolchain): 补工具 ID 改名迁移的用例
6f50940fdf feat(toolchain): 卡片版本选择改为大版本与小版本两级联动
631b06af1b refactor(toolchain): 工具 ID jdk21 改名为 jdk，并迁移既有安装
54823291f0 feat(toolchain): 工具版本按大版本线分组，更新只在同线内提示
```

**若数量不是 4，先停下核对**——多出来的提交会被一起带进发布。

---

## 第 1 步：推送 `linglong-dev` 并取得新索引的提交哈希

索引地址固定到提交哈希（`remote.go:44`），因此必须先让承载新 `index.json` 的提交在
公网可达，才能拿到哈希。**这一步会产生外部可见的推送。**

```sh
git push origin linglong-dev
INDEX_COMMIT=$(git rev-parse HEAD)
echo "索引提交: $INDEX_COMMIT"

# 确认该提交里的 index.json 与工作区一致（blob 哈希必须相同）
git rev-parse "$INDEX_COMMIT:apps/desktop-launcher/internal/toolchain/tools/index.json"
git hash-object apps/desktop-launcher/internal/toolchain/tools/index.json
```

两个哈希**必须完全相同**。不同就说明索引有未提交的改动，此时固定到 `$INDEX_COMMIT`
会指向一份与本地不一致的索引。

推送后验证该文件确实可从公网取到（返回 200，且内容为 43 个工具的新格式）：

```sh
curl -sS -o /dev/null -w "%{http_code}\n" \
  "https://raw.githubusercontent.com/GershonWang/deepseek-harness/$INDEX_COMMIT/apps/desktop-launcher/internal/toolchain/tools/index.json"

curl -sS "https://raw.githubusercontent.com/GershonWang/deepseek-harness/$INDEX_COMMIT/apps/desktop-launcher/internal/toolchain/tools/index.json" \
  | head -c 200
```

**为什么要先确认可达**：`remote.go` 的注释写明索引因此被客户端自身的发布过程锚定。
若把地址指向一个取不到的提交，运行时索引拉取失败，客户端会回退到内置索引——表现为
「索引没更新」，而日志里只有一条拉取错误，排查成本很高。

---

## 第 2 步：把 `defaultIndexURL` 换成新提交

编辑 `apps/desktop-launcher/internal/toolchain/remote.go`，把第 44 行的 40 位哈希替换
为第 1 步得到的 `$INDEX_COMMIT`（**只换哈希，路径部分保持不变**）：

```sh
# 替换后自查：应打印新的提交哈希，且路径不变
grep -n "defaultIndexURL =" apps/desktop-launcher/internal/toolchain/remote.go
```

替换后必须让守卫测试通过（它校验引用是 40 位哈希而非分支名，以及路径正确）：

```sh
cd apps/desktop-launcher
GOPATH=<可写目录> GOCACHE=<可写目录> go test ./internal/toolchain/ -run TestDefaultIndexURL_PinnedToCommit -v
```

**这条测试会拦住把地址退回分支名的改动**——分支名引用意味着控制该分支即可整体替换
索引及其里的 sha256，校验方无从察觉。

---

## 第 3 步：升版本号并提交

`linglong` 分支当前停在 `0.1.3.3`，而 `linglong-dev` 上 `linglong.yaml` 已是 `0.1.4.4`。
本次是**索引格式与工具 ID 的变更**（用户可见：卡片版本选择变成两级、JDK 多了几条线、
老安装会被迁移），按既有轨迹（`0.1.2.5 → 0.1.2.6 → 0.1.2.7 → 0.1.3.2 → 0.1.3.3`）
应继续升到 `0.1.4.5`。

```sh
# 改 apps/desktop-launcher/linglong/linglong.yaml 里的 version: 0.1.4.4 → 0.1.4.5
grep -m1 "^  version:" apps/desktop-launcher/linglong/linglong.yaml

git add apps/desktop-launcher/internal/toolchain/remote.go \
        apps/desktop-launcher/linglong/linglong.yaml
git commit -m "chore(toolchain): 索引地址重钉到新提交，玲珑包版本升至 0.1.4.5"
```

**为什么必须重新发版**：索引地址是编译期常量，不重编客户端，运行中的程序仍会拉旧
索引。这是「索引地址固定到提交」这一设计换来的安全性所付出的代价，`remote.go` 的
注释里已写明。

---

## 第 4 步：构建玲珑包

```sh
sh apps/desktop-launcher/build-linglong.sh
```

流程为 `prepare-offline.sh`（宿主机构建全部产物并暂存）→ `ll-builder build`（容器内
组装）→ `ll-builder export`（导出 `.uab` 到仓库根）。

复用已有 `stage/` 而不重新准备产物时加 `--no-prepare`；但**本次改了 Go 源码与前端
资源，必须重新 prepare**，不能跳过。

---

## 第 5 步：构建后验证

```sh
# 5.1 清单一致性与 sha256 非占位（不需要容器，可直接跑）
sh apps/desktop-launcher/linglong/test-verify-tools.sh

# 5.2 合并产物树的工具可用性（需要 ll-builder build 之后的合并结果）
sh apps/desktop-launcher/linglong/verify-tools.sh linglong/output/binary/files
```

其余闸门（`verify-builder-log.sh`、`verify-container-deps.sh`、
`verify-merged-deps.sh`）的用法见各自脚本头部注释。

---

## 第 6 步：安装前先备份工具链目录（**建议，非可选**）

本次发布包含**工具 ID 改名**，客户端首次启动会把 `~/.dsh-tools/jdk21-*` 搬到
`jdk-*`。迁移代码已有 5 个用例覆盖（含变异验证：软链指向错误路径会让 3 条用例失败，
去掉覆盖保护会让「目标已存在」那条失败），但**它从未在真实的 `~/.dsh-tools` 上运行
过**——`main` 包依赖 cgo + 系统 GTK 开发库，在无 gcc 的环境里编译不了。

```sh
cp -a ~/.dsh-tools ~/.dsh-tools.bak-$(date +%Y%m%d)
du -sh ~/.dsh-tools
```

**迁移是幂等的**：目标 ID 下已存在同名版本时保留既有那份并跳过，重复启动只会重复
打印「目标已存在，保留现有」。因此万一需要回退，把备份目录换回来即可。

---

## 第 7 步：安装并观察启动日志

安装新 `.uab` 后，从终端启动以便看到 stderr：

```sh
# 关注这两类行：
#   工具链 ID 迁移: [jdk 21.0.12.1: 已迁移]
#   工具链软链自愈有被拒绝的条目: ...
```

**迁移发生在软链自愈之前**：先把目录与 `current` 软链搬到新 ID，再由自愈统一重建
`~/.dsh-tools/bin/` 下的命令软链。因此日志里「已迁移」之后不应再出现与 `jdk21`
相关的条目。

逐项核对：

```sh
ls -d ~/.dsh-tools/jdk-* 2>/dev/null      # 应列出各 JDK 版本目录
ls -d ~/.dsh-tools/jdk21-* 2>/dev/null    # 应为空（空即迁移成功）
readlink ~/.dsh-tools/current/jdk         # 应指向 jdk-<版本>
java -version                             # 应能执行（PATH 里 ~/.dsh-tools/bin 优先）
```

---

## 第 8 步：确认 GUI 行为符合设计

打开工具链市场，逐项确认：

| 检查项 | 预期 |
|---|---|
| JDK 卡片 | 出现**两个**下拉：一级是大版本线 8/11/17/21/25，二级是该线内可选项 |
| 已装 21.0.12.1 时选中 21 线 | 二级显示 `v21.0.12.1 · 当前` |
| 切到未装的 17 线 | 二级**只显示** `v17.0.20.1 · 可安装`（该线没装，只给最新） |
| 切到 8 线 | 二级显示 `v8u504`（已装，可切换/卸载） |
| 单版本工具（如 bat） | **不出现**大版本下拉，只有一个版本下拉 |
| 装 JDK 8 的用户 | **不应**被提示「可更新到 21」（跨大版本不提示更新） |

---

## 回退方式

| 情况 | 做法 |
|---|---|
| 构建/安装失败 | 不安装新包，`linglong` 分支未动，已发布版本不受影响 |
| 迁移出错 | `mv ~/.dsh-tools ~/.dsh-tools.broken && mv ~/.dsh-tools.bak-<日期> ~/.dsh-tools` |
| 索引内容有误 | 改回旧哈希并重新提交、重新构建（索引地址是编译期常量，改地址必须重编） |

---

## 兼容性说明（已实测确认）

已发布版本（`0.1.3.3` 及所有未升级的）**不受本次改动影响**，有两层保障：

1. **它们拉的是各自钉死的索引。** `0.1.3.3` 的地址指向不可变提交 `ff0b924d…`，Git
   提交哈希是内容指纹，该提交的内容在数学上不可能被改动。本次改的是工作区文件，
   未推送、也不指向 `ff0b924d`。
2. **即使拿到新索引也不会崩。** 实测新旧索引的唯一结构差异是多出 `major` 字段；
   `version` 仍是 `1`（`ParseIndex` 只校验这一项），Go 的 `encoding/json` 对未知字段
   静默忽略，`jdk21` 消失只是让老客户端「按 ID 查不到该工具就跳过」。

需要注意的是 `linglong` 与 `linglong-dev` 是**双向分叉**（分叉点 `a8b925f97e`，
`linglong` 比 `linglong-dev` 少 3635 个提交），发布时按既有做法把 `linglong-dev`
合并进 `linglong`。

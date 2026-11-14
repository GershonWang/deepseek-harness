# 工具链索引发布分支

工具链市场从 `toolchain-index` 分支读取索引，客户端据此知道有哪些工具、每个版本的下载地址与 SHA-256。

## 为什么需要一个专用分支

索引里同时带着归档的下载地址和它的 SHA-256，两者出自同一份数据。因此哈希只能证明「归档与索引一致」，不能证明「索引本身可信」：谁能改索引，谁就能同时换掉地址和哈希，客户端会「验证通过」一个恶意归档。

早期实现把默认地址固定到 40 位提交哈希，想让索引内容由客户端自身的发布过程锚定。它失效了：索引路径在模块迁移到 `apps/linyaps` 之后变了，而固定的那个提交里根本没有该文件，于是 raw 地址永久 404，索引再也拉不到——工具列表停止更新，且没有任何报错，因为客户端会静默回退到内置索引。

现在的取舍是专用分支：

- 索引要能更新，否则工具市场只剩随包那份快照。
- 暴露面靠两点收敛：分支只承载这一个文件，所以改动是一条独立提交，不会被埋进功能提交里；以及限制谁能推这个分支。

固定哈希更安全，但要求每次改索引都改代码并发版，而它已经因为路径迁移失效过一次。专用分支加写权限限制是这两个约束下的折中。

## 发布一次索引更新

在 `toolchain-index` 分支上只提交 `index.json`，路径必须与主干一致：

```
apps/linyaps/desktop-launcher/internal/toolchain/tools/index.json
```

客户端按固定 URL 取这个文件，因此路径不能改；改动路径等于再次制造死链。

```sh
git switch toolchain-index
cp <新的索引> apps/linyaps/desktop-launcher/internal/toolchain/tools/index.json
git add apps/linyaps/desktop-launcher/internal/toolchain/tools/index.json
git commit -m "chore(toolchain): 更新工具索引"
git push origin toolchain-index
```

`index.json` 的格式约定（每个字段的含义、`bin_dirs`/`bin_names`/`lib_rel` 的用途）由 [目录模块](../../desktop/src/toolchain/catalog.ts) 的解析代码定义；新增字段时先改那里的校验，再改数据。

客户端有 24 小时缓存，过期后下次启动或用户在窗口里点「刷新索引」时取到新内容。如果要立刻生效，只能靠用户主动刷新或清掉缓存目录——这是缓存策略的既定行为，不是缺陷。

## 保护这个分支（GitHub 操作步骤）

未配置保护前，任何有推送权限的人都能改索引，此时专用分支与直接用主干分支没有区别。以下是仓库管理员在网页上的操作步骤。

### 1. 开放分支保护设置

仓库页 → **Settings** → 左侧 **Branches** → **Add branch protection rule**（旧界面）或 **Add classic branch protection rule**（新界面显示为 *Rules* → *New ruleset*）。

GitHub 在 2024 年之后把入口做成 **Rulesets**，两者都能达到目的：

- **Rulesets**（推荐，新界面）：**Settings** → **Rules** → **Rulesets** → **New ruleset** → **New branch ruleset**
- **Classic**（旧界面）：**Settings** → **Branches** → **Add branch protection rule**

### 2. 设定目标分支

- Ruleset 名称：`toolchain-index protection`
- **Target branches** → *Add target* → *Include by pattern* → 填 `toolchain-index`
- Enforcement status 保持 **Active**

### 3. 开启必要规则

| 选项 | 取值 | 作用 |
|---|---|---|
| Restrict deletions | 开启 | 禁止删掉该分支，删了就等于断掉索引来源 |
| Block force pushes | 开启 | 禁止强推改写历史；索引历史需要可审计 |
| Require a pull request before merging | 开启 | 索引改动走 PR，留下评审记录 |
| Required approvals | 1 | 至少一人复核 |
| Require review from Code Owners | 开启（可选） | 需要 `CODEOWNERS` 指定该路径的负责人，见下一步 |

其余选项（status checks、merge queue 等）对该分支没有意义：这个分支不参与构建。

### 4. 限制谁能推（可选，但这是保护的核心）

分支保护只能阻止强推和直推主干，不能阻止有写权限的人开 PR 合并。要真正限制范围，用 `CODEOWNERS`：

在仓库根目录的 `.github/CODEOWNERS` 里加一行：

```
/apps/linyaps/desktop-launcher/internal/toolchain/tools/index.json  @<维护者用户名>
```

再在第 3 步打开 **Require review from Code Owners**，索引的任何改动就至少需要该维护者批准。

### 5. 验证保护已生效

```sh
# 应当被拒绝
git push origin --force toolchain-index
# 应当被拒绝（若开启了 PR 要求）
git push origin toolchain-index
```

两条命令都报错即说明保护生效。若仍能推送，检查 Ruleset 的 enforcement status 是否为 Active，以及目标分支的 pattern 是否写对（`toolchain-index` 不带 `refs/heads/` 前缀）。

## 客户端侧的可覆盖入口

`DSH_TOOLCHAIN_INDEX_URL` 可以覆盖默认地址，用于内网镜像或自托管部署。环境变量与二进制同属一个信任域，因此这不构成额外的攻击面。覆盖后客户端不再读取本分支。

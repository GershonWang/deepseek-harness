# Agent Note: The desktop launcher ships only the Linglong packaging path

Status: implemented

[English](2026-09-13-remove-deb-packaging-path.md) | 中文

## Problem

`apps/desktop-launcher/build-deb.sh` 组装一个安装到 `/opt/apps/com.deepseek.dsh-desktop/files` 的 Linux `.deb`。它复用 `linglong/prepare-offline.sh` 产出暂存构建，随后分道：玲珑包经 `linglong.yaml` 自带 webkit2gtk，而 `.deb` 声明依赖宿主机的那一份。

有两个事实同时让它不可用且有误导性。

它产不出包。脚本只创建 `usr/share/icons/hicolor/256x256/apps`，却用 `install -m644` 安装九种尺寸的图标。GNU `install` 在缺少 `-D` 时要求目标目录已存在，因此在 `set -eu` 下第一轮（`s=16`）就以 `No such file or directory` 中止。`linglong.yaml` 对同一个循环用的是 `install -Dm644`，会自动建目录。

没有消费者。没有任何 CI 作业、git 钩子、闸门、测试或其它脚本引用该文件，也没有任何打包发布流程会运行它。README 却宣称「打成如意玲珑（Linglong）包在 Deepin 25 上分发，另支持 Linux `.deb` 与 `.rpm`」，而仓内从未存在过任何 `.rpm` 入口。

## Decision

桌面启动器只有一条打包路径。`apps/desktop-launcher/build-linglong.sh` 在宿主机跑 `prepare-offline.sh`，在玲珑容器内组装，裁剪 gcc 工具链，校验合并后的工具树，然后导出 `.uab`。

`apps/desktop-launcher/build-deb.sh` 被删除。`.gitignore` 去掉其 `com.deepseek.dsh-desktop_*.deb` 条目。`apps/desktop-launcher/README.md` 与 `README.zh.md` 均以玲珑包作为分发形态，且不再声称支持 `.deb` 或 `.rpm`。

本次删除不伴随任何 Go 源码改动。该模块不含 `.deb` 专用分支：`packaging.ConfigureWebKitHelperPath` 在包内 webkit helper 缺失时提前返回，那是未打包态与开发态的路径，不是 `.deb` 支持，予以保留。

## Alternatives considered

**修掉缺失的 `-D` 并保留该路径。** 否决：没有消费者，且它在本仓库从未产出过包。修复它意味着要在 `linglong.yaml` 之外长期维护同一套 `/opt/apps/<id>/files` 布局的第二份组装，图标安装、`.desktop` 安装与 pnpm 闭包全都重复。

**保留脚本但标注为不支持。** 否决：文档仍会成为读者与坏命令之间唯一的屏障，而恰恰是「脚本在场但已损坏」招来了 README 里那句声明。

**把 `.deb` 配方保留为文档片段。** 否决：其布局逻辑与 `linglong.yaml` 重复，片段会随同样的上游变化腐坏，却没有任何可执行校验兜底。

**只删脚本，保留 README 声明。** 否决：那正是让文档失真的状态。声明本身正是本次删除必须一并触及文档的原因。

## Consequences

`.deb` 安装会让 harness 运行在玲珑沙箱之外，直接面对宿主 X 服务端与宿主 webkit2gtk。这项能力被放弃：启动器如今只有一套运行环境，因此仅存在于容器之外的行为差异——尤其是剪贴板读取路径——失去了它那条不经桥接的第二条演练路径。若要重新引入第二种分发，需要新增一个与 `prepare-offline.sh` 及 `linglong.yaml` 的图标、`.desktop` 安装共享实现的构建脚本，而不是把它们重述一遍。

有两条 implemented Agent Note 曾把 `.deb` 路径记为现存事实，本次一并修正，因为[implemented note 需与已交付实现保持一致](../AGENTS.md)。[Desktop entry needs StartupWMClass for dock icon association](../bug-fix/2026-09-12-desktop-launcher-dock-icon-association.zh.md) 不再把第二条打包路径算作桌面条目的消费者。[Clipboard INCR transfer and X11 event offsets](../bug-fix/2026-09-11-clipboard-incr-transfer-and-event-offsets.zh.md) 不再把 `.deb` 安装表述为 `SelectionNotify` 偏移修正的受益方。

## Testing

缺失性经机械检查。对受控源码执行 `grep -riE 'build-deb|\.deb'` 只命中 `scripts/prepare-ci-bubblewrap.sh` 与 `.github/workflows/ci-master.yml`，它们为 bubblewrap 与 Wine 下载互不相关的 `.deb` 归档；`grep -ri rpm` 除本 note 外无命中。`git ls-files apps/desktop-launcher` 不再列出 `build-deb.sh`。

两份 README 与两份被修正 Agent Note 的双语配对记录均已重录，`verify-translation-pairing` 报告相关配对一致。本次未改动任何 Go 文件，因此启动器构建与其 `go test ./...` 结果不受影响。

## Related

[Desktop launcher on Linux/Linglong](../feature/2026-08-14-desktop-launcher-linux-linglong.zh.md) owns the packaging path this note leaves as the only one.

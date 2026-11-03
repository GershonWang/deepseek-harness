# 玲珑运行库证据：GTK4 与 WebKitGTK 6.0

本文件记录升级到 Wails v3 前对玲珑（Linyaps）运行环境的核查结果与出处，供后续评审复核。核查时间为 2026-11-03，环境为本机玲珑 1.14.x + Deepin 25 容器。

## 结论

1. 官方 base `org.deepin.base/25.2.2` 不提供 GTK4 与 WebKitGTK：包清单里只有 GTK3 3.24.41 与 GTK2 2.24.33，没有 `libgtk-4-1`，也没有任何 webkit 包。
2. 官方 runtime（`org.deepin.runtime.dtk`、`org.deepin.runtime.webengine` 等）同样不提供：它们只带 Qt6、DTK6 与 Qt WebEngine，后者是 Chromium 内核，与 WebKitGTK 不是一套 API。
3. deepin 25 的发行版仓库里有这些库：`libgtk-4-1 4.18.6`、`libgtk-4-dev`、`libwebkitgtk-6.0-4 2.52.5`、`libwebkitgtk-6.0-dev`。因此可行路径是随包自带，而不是指望 base 或 runtime。
4. 官方对「依赖库缺失」给的唯一明确答案是应用自带，放到 `$PREFIX/lib`（即 `/opt/apps/<id>/files/lib`）；玲珑为容器生成的 `ld.so.conf` 已把该目录加入动态库搜索路径，不需要额外设置 `LD_LIBRARY_PATH`。
5. 社区存在 `org.deepin.runtime.gtk4` 这类自建 runtime，但它只在社区自建测试源、以 WebKit2GTK 4.1（GTK3 API）为主，不能作为 GTK4 + WebKitGTK 6.0 的依赖来源。

## 一手核查（本机执行）

| 结论 | 依据 | 出处 |
| --- | --- | --- |
| base 25.2.2 无 GTK4、无 webkit | 容器内 `/packages.list`（614 个包）中 gtk 相关仅 `libgtk-3-0 3.24.41-1deepin5`、`libgtk-3-common`、`libgtk2.0-0`、`libgtk2.0-common`、`fcitx5-frontend-gtk2/3` 与 `gtk-update-icon-cache 4.18.6`；无 `libgtk-4-1`，无任何 `webkit` 包 | 本机 `grep -iE 'gtk|webkit' /packages.list` |
| `gtk-update-icon-cache` 不代表装了 GTK4 | 该工具随 gtk4 源码树发布，但对应的运行库包 `libgtk-4-1` 未安装 | 同上 |
| deepin 25 仓库提供 GTK4 运行库与开发包 | 仓库目录 `pool/main/g/gtk4/` 下有 `libgtk-4-1_4.18.6+ds-2deepin1_amd64.deb`、`libgtk-4-dev`、`libgtk-4-bin`、`libgtk-4-media-gstreamer` | https://community-packages.deepin.com/deepin/beige/pool/main/g/gtk4/ |
| deepin 25 仓库提供 WebKitGTK 6.0 运行库与开发包 | 仓库目录 `pool/main/w/webkit2gtk/` 下有 `libwebkitgtk-6.0-4_2.52.5-1~deb13u1deepin1_amd64.deb`、`libwebkitgtk-6.0-dev` | https://community-packages.deepin.com/deepin/beige/pool/main/w/webkit2gtk/ |
| 自带的库会被加入容器动态库搜索路径 | `ld-cache-generator` 生成的 `ld.so.conf` 包含 `/opt/apps/${app_id}/files/lib` 与 `/opt/apps/${app_id}/files/lib/${triplet}`，随后 `ldconfig -C` 生成缓存 | https://github.com/OpenAtom-Linyaps/linyaps/blob/master/misc/libexec/linglong/ld-cache-generator |
| 官方 FAQ 明确「库文件放到 $PREFIX/lib」 | FAQ 中出现 `$PREFIX/lib` 的说明条目 | https://linglong.dev/guide/tips-and-faq/faq.html |

## 二手证据（文档与社区，未逐条复跑）

| 结论 | 依据 | 出处 |
| --- | --- | --- |
| 官方稳定 runtime 列表里没有 Gtk/WebKit 类 runtime | 文档只列 `org.deepin.runtime.dtk`、`org.deepin.runtime.webengine`、`org.deepin.Runtime` 等；dtk runtime 的包清单里 gtk 只有 `qt6-gtk-platformtheme` 一条 | https://linglong.dev/guide/reference/runtime.html |
| base 25.2.2 的包清单与本地容器一致 | 官方发布的 binary 包清单（只有 GTK3/GTK2，webkit 0 命中） | https://linglong.dev/guide/reference/org.deepin.base_25.2.2_binary.list.html |
| 官方不允许往 runtime 里追加库，只能自建新 runtime | FAQ：「目前不允许往 runtime 里面添加额外的库文件」 | https://linglong.dev/guide/tips-and-faq/faq.html |
| 社区 GTK4 runtime 存在但非官方 | 提测申请仍为 open；仓库只用 apt 拉 `libgtk-4-1` 与 `libwebkit2gtk-4.1-0`，并带 webkit2gtk 补丁 | https://github.com/OpenAtom-Linyaps/linyaps/issues/1374 · https://github.com/LFRon/org.deepin.runtime.gtk4 |

## 对 spike 的含义

- 构造上不必等待官方 runtime：把 GTK4 与 WebKitGTK 6.0 连同依赖打进包内 `files/lib` 即可，这与现有包自带 WebKit2GTK 4.1 的做法一致。
- 需要额外确认的是 WebKit 的 helper 进程路径与 GStreamer 插件路径。现有 `install-shell-runtime.sh` 已处理过 4.1 的同类问题，6.0 的目录名与 helper 名称不同，必须在真机上逐项验证。
- 体积会变化：GTK4 + WebKitGTK 6.0 的运行库比 4.1 更大，需要在打包阶段重新核算包体。

## 未解决的问题

1. 没有找到任何官方文档正面描述「GTK4 / WebKitGTK 的打包指南」，结论由包清单缺项、FAQ 通用规则与 `ld.so.conf` 生成逻辑推出。
2. 官方 runtime 的 binary 层文件清单未公示，本文件对 runtime 的判断基于包列表与 runtime 仓库的 `linglong.yaml` 交叉验证，未逐文件核对。
3. `libwebkitgtk-6.0-4`（2.52.5）能否在玲珑容器内正常启动、helper 进程是否可用，尚未在真机验证；这正是 spike 要回答的下一项。

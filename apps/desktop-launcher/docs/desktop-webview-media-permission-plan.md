# 桌面版内嵌 WebView 麦克风权限实施方案（方案 A：壳内 CGO 接管 permission-request）

> **状态：已落地。** 下列 7 个任务已全部实现并合入 `linglong` 分支，随 `v0.1.4.7` 封装发布；复选框未回头勾选，**不代表未完成**。权威决策记录见 `.agents/notes/implemented/architecture/2026-09-25-desktop-webview-media-permission.{md,zh.md}`（`docs(desktop-launcher)` 提交）。
>
> 实现提交共 8 个，全部是 2026-09-25 当天落在 `apps/desktop-launcher` 的提交，依次覆盖：放行策略、GTK 侧接管信号、非 Linux 空实现、诊断日志白名单、`OnDomReady` 挂载、iframe 授权、GStreamer 插件路径、`make` 构建标签。需要按提交号逐条复核时，用 `git log --since=2026-09-25 --until=2026-09-26 --oneline -- apps/desktop-launcher` 即可列全——本文档不写死哈希，以免与仓库的引用闸门冲突。
>
> 归档说明：本文档原为会话产出的实施方案（原路径 `~/Documents/dsh-desktop-mic-permission-plan.md`），归档时移入本目录；文中引用的两个一次性探针脚本（§0.2、§0.2.1）已在归档时清理，其验证结论保留在正文。唯一未由方案作者自验的验收项是文末「验收标准」第 1 条（重启桌面应用后的产品端到端）——写作会话本身运行在该应用内，重启会终止会话。


> **面向执行者：** 本方案按任务拆分，每个任务由 2–5 分钟的最小动作组成，步骤用复选框跟踪进度。动手前请先读 §0 的背景与已验证结论；§0.3 的函数名全部经过头文件核实，**不要凭记忆改写**。

**目标：** 让 Linux 桌面版内嵌 WebView 中的语音输入可用——由启动器放行麦克风（仅音频）采集权限，使 `getUserMedia` 不再抛 `NotAllowedError`。

**架构：** Wails v2 的 Linux 后端从不连接 `WebKitWebView::permission-request`，而 WebKitGTK 对该信号的默认处理是拒绝，于是内嵌界面的 `getUserMedia` 必然失败。Wails 把 `WebKitWebView*` 关在 `internal/` 包里，应用层拿不到指针；本方案改为从 GTK 侧用 `gtk_window_list_toplevels()` 遍历顶层窗口取回该实例、自行连接信号。放行策略只允许「音频且不含视频/屏幕」的请求，判断逻辑用纯 Go 实现并单测，C 侧只做取事实与 allow/deny 的胶水。

**技术栈：** Go 1.23 + CGO、GTK3、webkit2gtk-4.1、Wails v2.15.0；构建走 `make build`（`-tags "production webkit2_41"`）与 `linglong/prepare-offline.sh`。

**当前状态：** 方案可行性已在运行容器内用独立探针完成对照实验验证（见 §0.2）。

---

## 0. 背景与已验证结论

### 0.1 根因

**这里有三个彼此独立的根因；根因一与根因二症状完全相同（`NotAllowedError`），根因三症状不同（`OverconstrainedError`）。只修任何一个都不能让麦克风可用。**

| 环节 | 事实 | 证据 |
|---|---|---|
| 产品表现 | 桌面版点麦克风直接提示「麦克风权限未开启，请在浏览器和系统设置中允许访问。」 | 用户截图 |
| 文案来源 | 该文案只在 `NotAllowedError` 时产生 | `packages/experimental/client-ui-voice-input/src/client/audio.ts:65` |
| **根因一** | Wails v2.15.0 的 Linux 后端没有任何 `permission-request` 连接点，而 WebKitGTK 对该信号的默认处理是拒绝；请求能到达信号，然后被默认处理拒掉 | `internal/frontend/desktop/linux/window.c` 全文无该信号；launcher 二进制中 `permission-request` 出现 0 次 |
| **根因二** | 壳用 `<iframe>` 嵌入 harness UI，而壳自身 origin 与 `http://127.0.0.1:<port>` **跨源**；iframe 上没有 `allow="microphone"`。`microphone` 的 permissions policy 默认白名单是 `self`，于是请求在**发出权限信号之前**就被拦掉 | `frontend/index.html:56`；§0.2 的 iframe 实验 |
| **根因三** | WebKit 的 WebProcess 找不到包内的 GStreamer 插件：容器内 `/usr/lib/x86_64-linux-gnu/gstreamer-1.0` 只有 `coreelements` 与 `coretracers` 两个插件，而包内 `<PREFIX>/lib/x86_64-linux-gnu/gstreamer-1.0` 有 259 个。没有插件搜索路径时 `appsink` 加载失败、音频设备枚举到 0 个，`getUserMedia` 以 `OverconstrainedError` 失败——这与权限被拒是完全不同的失败 | §0.7 的环境对照实验 + 运行中 `WebKitWebProcess` 的环境变量实测 |
| 指针拿不到 | Wails 把 `webview` 放在 `internal/` 包（受 Go `internal` 规则约束），应用层无法直接引用，所以根因一只能从 GTK 侧解 | `internal/frontend/desktop/linux/window.go:46` |
| 浏览器为何正常 | 用户是在浏览器里**直接打开 Web 版**（顶层文档），既不经壳的 iframe，也没有 Wails 的默认拒绝；两个根因都没被触发 | 用户实测（弹窗列出 3 个麦克风 + 实时波形） |

> 这正是先前只测「顶层文档 + 权限信号」会漏掉根因二的原因：浏览器验证走的是顶层文档，而产品路径是**跨源 iframe**。

### 0.2 探针对照实验（已完成，方案可行性依据）

在容器内用 Python ctypes 直接驱动 GTK3/WebKit2GTK，复刻 Wails 的窗口层级 `GtkWindow → GtkBox → WebKitWebView`，执行方案 A 的核心动作：

| 模式 | 页面结果 |
|---|---|
| `baseline`（不接管权限） | `PROBE_FAIL NotAllowedError`（与产品现象逐字一致） |
| `probe`（遍历捞取 + 接管权限） | **`PROBE_OK tracks=1`** |

`probe` 输出：

```
[mic-probe] 命中 WebKitWebView（深度 2）
[mic-probe] 从 1 个顶层窗口中遍历捞取成功
[mic-probe] 已连接 permission-request 信号
[mic-probe] 收到 UserMedia 权限请求 → 允许
[mic-probe] 页面结果: PROBE_OK tracks=1
```

结论：遍历捞取可行（命中深度 2，与 Wails 层级一致）、信号可接管、放行后 `getUserMedia` 真的返回音频轨道。探针脚本存于 `~/Documents/dsh-mic-probe.py`。

### 0.2.1 跨源 iframe 实验（已完成，根因二的依据）

上面的探针把测试页加载为**顶层文档**，与产品路径（跨源 iframe）不同，因此不足以判定根因二。补充实验：起两个 `127.0.0.1` 端口充当两个 origin，父页面（`:8801`）内嵌子页面（`:8802`），子页面调用 `getUserMedia` 并把结果 `postMessage` 回父页面写进标题。两种父页面只差 iframe 上的 `allow` 属性，**且两种都挂了权限处理**：

| 变体 | 结果 |
|---|---|
| `noallow`（`<iframe src=...>` 无 `allow`） | `FAIL NotAllowedError`，且日志里**没有出现**「收到 UserMedia 权限请求」——请求根本没走到信号 |
| `allow`（`<iframe src=... allow="microphone">`） | 先「收到 UserMedia 权限请求 → 允许」，再 `OK tracks=1` |

结论：根因二真实存在，且它在链路上**先于**根因一；两个修复都必须做。探针脚本存于 `~/.cache/dsh-mic-probe/probe_iframe.py`。

### 0.7 GStreamer 插件路径（根因三的依据；本节结论推翻了初稿的 §0.6 判断）

在真实 Wails 窗口里做环境对照实验，逐个改变量、其余保持一致：

| 变体 | 环境差异 | 枚举结果 | 页面结果 |
|---|---|---|---|
| c | 真实启动器环境（不含任何 `GST_*`） | `总数=0 音频输入=0`，日志 `appsink not found` | `FAIL OverconstrainedError` |
| d | `GST_PLUGIN_PATH=<包内插件目录>` | `总数=2 音频输入=1` | **`OK tracks=1`** |
| e | `GST_PLUGIN_SYSTEM_PATH=<包内插件目录>` | `总数=2 音频输入=1` | `OK tracks=1` |
| f | `GST_PLUGIN_PATH_1_0=<包内插件目录>` | `总数=2 音频输入=1` | `OK tracks=1` |
| g | `GST_PLUGIN_SYSTEM_PATH_1_0=<包内插件目录>` | `总数=2 音频输入=1` | `OK tracks=1` |

旁证与排除项：

- 容器内插件目录默认只有 2 个插件（`libgstcoreelements.so`、`libgstcoretracers.so`），包内目录有 259 个。
- 页面在**立即**与 **3 秒后**各枚举一次，两次结果完全相同，排除「预热竞态」这一解释。
- 直接读运行中 `WebKitWebProcess`（PID 513497）的 `/proc/PID/environ`，确认发行版实际**没有**导出任何 `GST_*` 变量，因此变体 c 就是产品真实环境。

**决定性验证（最终采用的证据）**：把探针按包布局放到 `<ROOT>/bin/`，由 `<ROOT>/lib/x86_64-linux-gnu/gstreamer-1.0` 提供插件目录，在真实启动器环境下调用产品自己的 `packaging.ConfigureGStreamerPlugins()`：

```
麦克风权限处理已挂载
页面结果: 枚举(立即) 总数=2 音频输入=1
页面结果: 枚举(3 秒后) 总数=2 音频输入=1
页面判定: OK tracks=1 (音频输入=1)
```

结论：根因三真实存在，且 `GST_PLUGIN_PATH` 是附加语义（不覆盖系统默认路径），因此实施时选用它；修复必须在 `wails.Run` 之前完成，因为做枚举的 WebProcess 继承本进程环境。

#### 初稿为什么判错（留档，避免重犯）

初稿用「产品显示权限文案 ⇒ `NotAllowedError` ⇒ 枚举成功 ⇒ GStreamer 正常」这条链否定了根因三。它错在把 `NotAllowedError` 当成枚举成功的充分证据：跨源 iframe 的 permissions policy 拒绝同样给出该名字，且发生在枚举之前。根因二当时还没修，它把根因三的症状完全遮住了，于是「产品上没见过 `OverconstrainedError`」并不能推出「枚举正常」。

### 0.3 已核实 API（照抄，勿改）

| 用途 | 确切名称 | 核实来源 |
|---|---|---|
| 列出顶层窗口 | `GList* gtk_window_list_toplevels(void)` | `/usr/include/gtk-3.0/gtk/gtkwindow.h:339` |
| 取子控件 | `GList* gtk_container_get_children(GtkContainer*)` | `/usr/include/gtk-3.0/gtk/gtkcontainer.h:169` |
| 判断是否媒体采集请求 | `WEBKIT_IS_USER_MEDIA_PERMISSION_REQUEST(obj)` | `webkit/WebKitUserMediaPermissionRequest.h:34` |
| 取实例 GType | `webkit_user_media_permission_request_get_type()` | 同上 :31 |
| 是否要音频设备 | `webkit_user_media_permission_is_for_audio_device(req)` | 同上 :50-51 |
| 是否要视频设备 | `webkit_user_media_permission_is_for_video_device(req)` | 同上 :53-54 |
| 是否要屏幕设备 | `webkit_user_media_permission_is_for_display_device(req)` | 同上 :56-57 |
| 允许 / 拒绝 | `webkit_permission_request_allow/deny(req)` | `webkit/WebKitPermissionRequest.h:51,54` |
| 信号签名 | `gboolean (*permission_request)(WebKitWebView*, WebKitPermissionRequest*)` | `webkit/WebKitWebView.h:272-274` |

> **易错点**：`webkit_user_media_permission_request_is_for_audio_capture` 这类名字**不存在**（那是别的语言绑定里的写法）。本环境头文件中只有上表三个 `..._is_for_*_device`。

### 0.4 线程前提

`OnDomReady` 由 Wails 从 GTK 主循环内的 C 回调直接进入：`internal/frontend/desktop/linux/frontend.go:471` 的 `processMessage` 由 `window.c` 的 `load-changed` 处理器调用。因此在该回调里直接访问 GTK 控件树是安全的，**不需要 `g_idle_add` 中转**。

### 0.5 构建前提

- wails 在 `webkit2_41` 标签下用 `#cgo webkit2_41 pkg-config: webkit2gtk-4.1`（`internal/frontend/desktop/linux/webkit2.go:7`）。本方案沿用同一 pc 名，**不新增任何构建依赖**。
- `linglong/prepare-pkgconfig.sh` 生成的 `webkit2gtk-4.0.pc` shim 只服务于 wails 不带标签的那条分支，本方案不受影响。
- 启动器在**宿主机**构建（`prepare-offline.sh` 第 175-178 行），构建宿主需已有 `libgtk-3-dev` 与 `libwebkit2gtk-4.1-dev`——构建 wails 本来就需要它们。

### 0.6 非目标（明确不做）

| 不做 | 原因 |
|---|---|
| ~~不设置任何 GStreamer 环境变量~~（本行结论已作废，见 §0.7） | 初稿的推理是错的：`NotAllowedError` 并非只可能出现在枚举成功之后——跨源 iframe 的 permissions policy 拒绝同样产生 `NotAllowedError`，而且发生在枚举**之前**；用它反推「枚举正常」属无效推理。实测证明容器的 GStreamer 枚举确实失败，因此本项已改为根因三并纳入实施（见任务 8）。 |
| 不放行摄像头、屏幕共享 | 语音输入只需要麦克风；按最小权限原则拒绝，且用户侧无该需求。将来若真要摄像头，改 `policy.go` 并补测试即可。 |
| 不改 Wails 依赖、不做 fork | 方案 A 在壳内自闭环，无需等上游。给上游提 PR 属后续独立事项。 |
| 不引入开关环境变量 | 放行范围已由策略限制为「仅音频」，且作用对象是本应用自己的内嵌界面，不存在需要用户关闭的场景（参考 `DSH_DESKTOP_DMABUF_RENDERER` 那种逃生舱，此处的触发条件是一个真实机器差异，这里没有）。 |

---

## 文件结构

| 文件 | 职责 | 变更 |
|---|---|---|
| `apps/desktop-launcher/internal/webviewperm/policy.go` | 放行策略与事实模型（纯 Go，无 CGO，可脱离 GTK 单测） | 新增 |
| `apps/desktop-launcher/internal/webviewperm/policy_test.go` | 策略表驱动单测 | 新增 |
| `apps/desktop-launcher/internal/webviewperm/permission_linux.go` | CGO 胶水：`Install()` 入口、`//export` 决策桥、日志 | 新增 |
| `apps/desktop-launcher/internal/webviewperm/permission_linux.c` | GTK 控件树遍历 + `permission-request` 回调 | 新增 |
| `apps/desktop-launcher/internal/webviewperm/permission_other.go` | 非 Linux 空实现（保持可编译） | 新增 |
| `apps/desktop-launcher/frontend/index.html` | 给嵌入 harness UI 的跨源 iframe 加 `allow="microphone"`（根因二） | 修改 |
| `apps/desktop-launcher/main.go` | 挂 `OnDomReady` → `webviewperm.Install()` | 修改 |
| `apps/desktop-launcher/README.md` | 记录该行为、分层说明、文件表 | 修改 |
| `.agents/notes/implemented/architecture/2026-09-25-desktop-webview-media-permission.{md,zh.md,i18n.yaml}` | 记录「绕开 Wails 内部拿 WebView 指针」这一决策与取舍 | 新增 |

**分层说明：** README 现称 `packaging` 等包为「pure Go (stdlib only)」。`webviewperm` 是启动器里**第一个 CGO 包**，因此单列而不并入 `packaging`，以保住原有的纯 Go 可测性承诺；README 的分层句需要同步补充。

---

## 任务 1：放行策略（纯 Go + 单测）

**文件：** 新增 `apps/desktop-launcher/internal/webviewperm/policy.go`、`apps/desktop-launcher/internal/webviewperm/policy_test.go`

- [ ] **步骤 1：先写失败测试**

创建 `apps/desktop-launcher/internal/webviewperm/policy_test.go`：

```go
package webviewperm

import "testing"

func TestAllowPermission(t *testing.T) {
	tests := []struct {
		name  string
		facts PermissionFacts
		want  bool
	}{
		{"仅麦克风", PermissionFacts{IsUserMedia: true, IsAudio: true}, true},
		{"仅摄像头", PermissionFacts{IsUserMedia: true, IsVideo: true}, false},
		{"麦克风加摄像头", PermissionFacts{IsUserMedia: true, IsAudio: true, IsVideo: true}, false},
		{"仅屏幕共享", PermissionFacts{IsUserMedia: true, IsDisplay: true}, false},
		{"麦克风加屏幕共享", PermissionFacts{IsUserMedia: true, IsAudio: true, IsDisplay: true}, false},
		{"三者都要", PermissionFacts{IsUserMedia: true, IsAudio: true, IsVideo: true, IsDisplay: true}, false},
		{"非媒体权限请求", PermissionFacts{}, false},
		{"有音频标志但非媒体请求", PermissionFacts{IsAudio: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllowPermission(tt.facts); got != tt.want {
				t.Fatalf("AllowPermission(%+v) = %v, 期望 %v", tt.facts, got, tt.want)
			}
		})
	}
}
```

- [ ] **步骤 2：运行测试确认失败**

```sh
cd apps/desktop-launcher
go test ./internal/webviewperm/ -run TestAllowPermission -v
```

预期：编译失败，报 `undefined: PermissionFacts` 与 `undefined: AllowPermission`。

- [ ] **步骤 3：写最小实现**

创建 `apps/desktop-launcher/internal/webviewperm/policy.go`：

```go
// Package webviewperm 让桌面壳内嵌的 WebKitGTK 视图放行麦克风采集权限。
//
// 存在的理由：Wails v2 的 Linux 后端不连接 WebKitWebView::permission-request，
// 而 WebKitGTK 对该信号的默认处理是拒绝，于是内嵌界面里的 getUserMedia 一定抛
// NotAllowedError——语音输入在桌面版不可用，同一个 Web 版用浏览器打开却正常。
// Wails 把 WebKitWebView 指针放在 internal/ 包内，应用层拿不到，因此改为从 GTK
// 侧遍历顶层窗口取回该实例并自行连接信号（见 permission_linux.c）。
package webviewperm

// PermissionFacts 是一次权限请求的可判定事实。
//
// 单独建模而不直接传 WebKit 类型，是为了让放行策略能脱离 GTK 单测：C 侧只负责从
// WebKitPermissionRequest 提取事实，判断逻辑留在 Go。
type PermissionFacts struct {
	// IsUserMedia 表示这是 getUserMedia 一类的媒体采集请求。
	IsUserMedia bool
	// IsAudio 表示请求包含音频采集（麦克风）。
	IsAudio bool
	// IsVideo 表示请求包含视频采集（摄像头）。
	IsVideo bool
	// IsDisplay 表示请求包含屏幕采集。
	IsDisplay bool
}

// AllowPermission 判定是否放行一次权限请求。
//
// 只放行「音频且不含视频/屏幕」的采集：语音输入只需要麦克风，按最小权限原则拒绝
// 摄像头与屏幕共享。其余类型的权限请求（通知、地理位置、指针锁定等）同样拒绝——
// 内嵌界面没有需要它们的产品功能，放行只会扩大暴露面。
func AllowPermission(facts PermissionFacts) bool {
	return facts.IsUserMedia && facts.IsAudio && !facts.IsVideo && !facts.IsDisplay
}
```

- [ ] **步骤 4：运行测试确认通过**

```sh
go test ./internal/webviewperm/ -run TestAllowPermission -v
```

预期：8 个子测试全部 `--- PASS`，末行 `ok`。

- [ ] **步骤 5：提交**

```sh
git add apps/desktop-launcher/internal/webviewperm/policy.go apps/desktop-launcher/internal/webviewperm/policy_test.go
git commit -m "feat(desktop-launcher): 新增内嵌 WebView 采集权限放行策略"
```

---

## 任务 2：CGO 胶水（GTK 遍历 + 信号接管）

**文件：** 新增 `permission_linux.c`、`permission_linux.go`

- [ ] **步骤 1：写 C 侧实现**

创建 `apps/desktop-launcher/internal/webviewperm/permission_linux.c`：

```c
// 内嵌 WebView 的权限信号接管。
//
// Wails v2 的 Linux 后端只连接 load-changed、drag 等信号，从不连接
// permission-request，而 WebKitGTK 对该信号的默认处理是拒绝。Wails 把
// WebKitWebView* 关在 internal/ 包里，应用层拿不到，所以这里从 GTK 侧遍历顶层
// 窗口取回实例并自行连接信号。
//
// 文件名后缀 _linux 即构建约束：本文件只在 Linux 参与编译。

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

#include "_cgo_export.h"

// dshPermissionHandlerKey 是打在已连接视图上的标记，避免重复连接同一实例。
static const char *dshPermissionHandlerKey = "dsh-media-permission-handler";

// dshFindWebView 在控件子树中深度优先查找第一个 WebKitWebView。
//
// Wails 当前的层级是 GtkWindow > GtkBox > WebKitWebView（实测命中深度 2），这里
// 仍写成递归遍历而不硬编码层级：Wails 调整布局时只会退化为「找不到并记日志」，
// 而不是连到错误的控件上。
static GtkWidget *dshFindWebView(GtkWidget *widget)
{
	if (widget == NULL) {
		return NULL;
	}
	if (WEBKIT_IS_WEB_VIEW(widget)) {
		return widget;
	}
	if (!GTK_IS_CONTAINER(widget)) {
		return NULL;
	}
	GtkWidget *found = NULL;
	GList *children = gtk_container_get_children(GTK_CONTAINER(widget));
	for (GList *node = children; node != NULL; node = node->next) {
		found = dshFindWebView(GTK_WIDGET(node->data));
		if (found != NULL) {
			break;
		}
	}
	g_list_free(children);
	return found;
}

// dshOnPermissionRequest 把请求事实交给 Go 侧策略判定，再放行或拒绝。
//
// 返回 TRUE 表示本信号已处理，从而阻止 WebKitGTK 默认的「拒绝」处理。
static gboolean dshOnPermissionRequest(WebKitWebView *webView, WebKitPermissionRequest *request, gpointer userData)
{
	(void)webView;
	(void)userData;

	int isUserMedia = WEBKIT_IS_USER_MEDIA_PERMISSION_REQUEST(request) ? 1 : 0;
	int isAudio = 0;
	int isVideo = 0;
	int isDisplay = 0;
	if (isUserMedia) {
		WebKitUserMediaPermissionRequest *media = WEBKIT_USER_MEDIA_PERMISSION_REQUEST(request);
		isAudio = webkit_user_media_permission_is_for_audio_device(media) ? 1 : 0;
		isVideo = webkit_user_media_permission_is_for_video_device(media) ? 1 : 0;
		isDisplay = webkit_user_media_permission_is_for_display_device(media) ? 1 : 0;
	}

	if (dshShouldAllowPermission(isUserMedia, isAudio, isVideo, isDisplay)) {
		webkit_permission_request_allow(request);
	} else {
		webkit_permission_request_deny(request);
	}
	return TRUE;
}

// dshInstallPermissionHandler 遍历本进程顶层窗口，找到内嵌 WebKitWebView 并连接
// permission-request。返回 1 表示已挂载（含此前已挂载），0 表示未找到视图。
//
// 找不到时不重试也不报错：界面本身可用，缺麦克风权限不应拦住启动或让进程退出。
int dshInstallPermissionHandler(void)
{
	GList *toplevels = gtk_window_list_toplevels();
	GtkWidget *webView = NULL;
	for (GList *node = toplevels; node != NULL && webView == NULL; node = node->next) {
		webView = dshFindWebView(GTK_WIDGET(node->data));
	}
	g_list_free(toplevels);

	if (webView == NULL) {
		return 0;
	}
	if (g_object_get_data(G_OBJECT(webView), dshPermissionHandlerKey) != NULL) {
		return 1;
	}
	g_signal_connect(G_OBJECT(webView), "permission-request", G_CALLBACK(dshOnPermissionRequest), NULL);
	g_object_set_data(G_OBJECT(webView), dshPermissionHandlerKey, GINT_TO_POINTER(1));
	return 1;
}
```

- [ ] **步骤 2：写 Go 侧胶水**

创建 `apps/desktop-launcher/internal/webviewperm/permission_linux.go`：

```go
//go:build linux

package webviewperm

/*
#cgo linux pkg-config: gtk+-3.0
#cgo !webkit2_41 pkg-config: webkit2gtk-4.0
#cgo webkit2_41 pkg-config: webkit2gtk-4.1

// 由 permission_linux.c 实现：遍历顶层窗口找到内嵌 WebKitWebView 并连接
// permission-request 信号。返回 1 表示已挂载，0 表示未找到视图。
int dshInstallPermissionHandler(void);
*/
import "C"

import "log"

// Install 挂载内嵌 WebView 的麦克风权限处理。
//
// 必须在 GTK 主线程调用。Wails 的 OnDomReady 满足该前提：它由 GTK 主循环内的 C
// 回调直接进入（wails/v2 internal/frontend/desktop/linux/frontend.go:471 的
// processMessage，由 window.c 的 load-changed 处理器调用），因此这里直接访问
// 控件树，不做线程切换。此时页面已就绪，WebView 必然已经创建并放入窗口。
//
// 找不到 WebView 时只记一条日志：界面已经可用，麦克风权限缺失不该影响启动。
func Install() {
	if C.dshInstallPermissionHandler() == 0 {
		log.Printf("未找到内嵌 WebKitWebView，麦克风权限未挂载")
		return
	}
	log.Printf("麦克风权限处理已挂载")
}

// dshShouldAllowPermission 是 C 侧回调的决策入口：把三个采集标志映射成
// PermissionFacts 后交给 AllowPermission，返回值 1 表示放行。
//
// 返回值与参数都用 C.int 而非 bool，因为该函数由 cgo 导出给 C 调用，签名必须与
// _cgo_export.h 中的声明一致。
//
//export dshShouldAllowPermission
func dshShouldAllowPermission(isUserMedia, isAudio, isVideo, isDisplay C.int) C.int {
	allowed := AllowPermission(PermissionFacts{
		IsUserMedia: isUserMedia != 0,
		IsAudio:     isAudio != 0,
		IsVideo:     isVideo != 0,
		IsDisplay:   isDisplay != 0,
	})
	if allowed {
		return 1
	}
	return 0
}
```

> **注意**：Go 文件的 cgo 前导块里**只放声明、不放任何头文件包含**。使用 `//export` 时前导块会被复制进两个生成文件，任何定义（含头文件里的静态内联函数）都可能引发重复符号；GTK/WebKit 头文件因此只出现在 `.c` 文件里。

- [ ] **步骤 3：确认能编译**

```sh
cd apps/desktop-launcher
go build -tags "production webkit2_41" ./internal/webviewperm/
```

预期：无输出（成功）。若报 `Package webkit2gtk-4.1 was not found`，说明构建宿主缺 `libwebkit2gtk-4.1-dev`，先安装再重试。

- [ ] **步骤 4：确认单测仍通过**

```sh
go test -tags "production webkit2_41" ./internal/webviewperm/ -v
```

预期：`TestAllowPermission` 8 个子测试全部 PASS。

- [ ] **步骤 5：提交**

```sh
git add apps/desktop-launcher/internal/webviewperm/permission_linux.c apps/desktop-launcher/internal/webviewperm/permission_linux.go
git commit -m "feat(desktop-launcher): 从 GTK 侧接管 WebView 采集权限信号"
```

---

## 任务 3：非 Linux 空实现

**文件：** 新增 `apps/desktop-launcher/internal/webviewperm/permission_other.go`

- [ ] **步骤 1：写空实现**

创建 `apps/desktop-launcher/internal/webviewperm/permission_other.go`：

```go
//go:build !linux

package webviewperm

// Install 在非 Linux 平台是空实现。
//
// 该缺陷来自 WebKitGTK 对 permission-request 的默认拒绝；其它平台的 Wails 后端
// 使用各自的 WebView（Windows 走 WebView2），不经过这个信号。保留空实现是为了让
// 启动器在非 Linux 上仍能编译，而不是把平台判断散落到 main。
func Install() {}
```

- [ ] **步骤 2：确认交叉编译不受影响**

```sh
cd apps/desktop-launcher
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./internal/webviewperm/
```

预期：无输出（成功）。这一步证明新增包没有在非 Linux 上引入 CGO 依赖。

- [ ] **步骤 3：提交**

```sh
git add apps/desktop-launcher/internal/webviewperm/permission_other.go
git commit -m "feat(desktop-launcher): 补齐 WebView 权限包的非 Linux 空实现"
```

---

## 任务 4：接入 Wails 生命周期

**文件：** 修改 `apps/desktop-launcher/main.go`

- [ ] **步骤 1：导入新包与 context**

把 `main.go` 的 import 块改成：

```go
import (
	"context"
	"embed"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/app"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/appenv"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/toolchain"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/webviewperm"
)
```

- [ ] **步骤 2：挂上 OnDomReady**

在 `wails.Run(&options.App{...})` 里、`OnStartup` 之后插入：

```go
		OnStartup:        controller.OnStartup,
		// 内嵌 WebView 的麦克风权限只能在页面就绪后挂：此时 WebKit 已创建视图并把
		// 它放进窗口，GTK 侧才遍历得到（见 internal/webviewperm）。此前挂会找不到
		// 视图，此后再挂则可能错过用户点击。
		OnDomReady: func(context.Context) {
			webviewperm.Install()
		},
		OnShutdown: controller.OnShutdown,
```

- [ ] **步骤 3：编译并跑全量静态检查**

```sh
cd apps/desktop-launcher
make build
make vet
go test -tags "production webkit2_41" ./...
```

预期：`make build` 产出 `dsh-desktop-launcher`；`vet` 与 `test` 均无失败。

- [ ] **步骤 4：确认仓库其他部分没被牵连**

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
git status --short
```

预期：只出现本方案涉及的文件（`main.go` 及 `internal/webviewperm/` 下的新增文件）。

- [ ] **步骤 5：提交**

```sh
git add apps/desktop-launcher/main.go
git commit -m "feat(desktop-launcher): 页面就绪时挂载内嵌 WebView 麦克风权限"
```

---

## 任务 5：放行 iframe 的麦克风（根因二）

**文件：** 修改 `apps/desktop-launcher/frontend/index.html`

壳把 harness UI 放进**跨源** `<iframe>`，而 `microphone` 的 permissions policy 默认白名单是 `self`，因此必须显式授权。**不授权时权限信号根本不会触发**（§0.2.1）：即使任务 1–4 全部完成，麦克风依然不可用。

- [ ] **步骤 1：给 iframe 加 allow 属性**

把 `apps/desktop-launcher/frontend/index.html:56` 的这一行：

```html
      <iframe id="harness" class="hidden" allowfullscreen></iframe>
```

改为：

```html
      <!-- allow 里显式写上 fullscreen 与 microphone 并保留 allowfullscreen：harness UI
           与壳跨源，microphone 的 permissions policy 默认白名单是 self，不显式授权则
           getUserMedia 在发出权限信号前就被拒绝（见 internal/webviewperm 与 README
           「Embedded WebView media permissions」）。两处都写 fullscreen 是为了不依赖
           各引擎对 allow 与 allowfullscreen 谁覆盖谁的细节。只授权 microphone，
           不放开 camera。 -->
      <iframe id="harness" class="hidden" allowfullscreen allow="fullscreen; microphone"></iframe>
```

- [ ] **步骤 2：确认既有前端测试不受影响**

```sh
cd apps/desktop-launcher
node --test frontend/test-app.cjs
node --test frontend/test-i18n.cjs
```

预期：与改动前一致全绿。这两个用例经 DOM 桩驱动 `app.js`，只校验消息来源与文案回填，不校验 iframe 的属性；`main_test.go` 校验的是 `go:embed` 清单（属性变更不影响清单）。

- [ ] **步骤 3：跑布局不变量（有 Chromium 时）**

```sh
node frontend/tools/preview.mjs verify
```

预期：通过。若本机没有浏览器，该工具会说明原因并以非失败退出，可跳过。

- [ ] **步骤 4：提交**

```sh
git add apps/desktop-launcher/frontend/index.html
git commit -m "fix(desktop-launcher): 授权内嵌 iframe 使用麦克风"
```

---

## 任务 6：端到端验证（必须真实执行并留证）

**文件：** 无改动；产出为验证记录。

- [ ] **步骤 1：构建并启动（在终端运行，便于读 launcher 自身 stderr）**

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
pnpm run build
cd apps/desktop-launcher && make build && ./dsh-desktop-launcher
```

预期：窗口正常出现（无 GTK 崩溃），启动日志中出现：

```
麦克风权限处理已挂载
```

这一行**同时证明遍历在真实 Wails 窗口上命中**——它是本方案最大的未知项，必须看到。

- [ ] **步骤 2：验证语音输入真的可用**

在桌面窗口中打开语音输入并点击麦克风，说话片刻。

预期：出现实时录音波形，停止后能返回识别文本（与浏览器 Web 版表现一致）。

- [ ] **步骤 3：验证没有放宽到视频**

单测已经锁定「视频一律拒绝」（任务 1 步骤 4 的「仅摄像头」「麦克风加摄像头」用例）。此处只需复核代码路径：`permission_linux.c` 的 `dshOnPermissionRequest` 确实把三个设备标志都传给了 `dshShouldAllowPermission`，且 `policy.go` 对 `IsVideo` / `IsDisplay` 取反。

- [ ] **步骤 4：记录证据**

在下面的表格中填写实跑结果（用于随交付物一并给出）：

| 检查 | 命令 / 操作 | 实测输出 | 结论 |
|---|---|---|---|
| 遍历命中 | 看启动日志 | | |
| iframe 授权 | 核对 `frontend/index.html` 的 `allow` 属性 | | |
| 语音可用 | 点麦克风录音 | | |
| 单测 | `go test -tags "production webkit2_41" ./...` | | |
| vet | `make vet` | | |

> 若步骤 1 未出现「麦克风权限处理已挂载」，**停下来**按顺序排查：窗口是否已加载页面 → `gtk_window_list_toplevels()` 是否返回窗口 → 控件树里是否存在 `WebKitWebView`。此时功能退化为改动前的行为（麦克风不可用），**不会更糟**，可据此判断是否需要回到探针复核层级。

---

## 任务 7：文档与决策记录

**文件：** 修改 `apps/desktop-launcher/README.md`；新增 `.agents/notes/implemented/architecture/2026-09-25-desktop-webview-media-permission.{md,zh.md,i18n.yaml}`

- [ ] **步骤 1：更新 README 分层说明**

在 `## Architecture` 的 layering rules 段落里，把当前这句：

```
Layering rules: `domain` has zero dependencies; `supervisor`/`connector`/`toolchain`/`appenv`/`packaging` are pure Go (stdlib only) and unit-testable; `app` orchestrates them and talks to the frontend; `main` only assembles.
```

改为（在 `packaging` 之后补上例外说明，保留原句其余部分）：

```
Layering rules: `domain` has zero dependencies; `supervisor`/`connector`/`toolchain`/`appenv`/`packaging` are pure Go (stdlib only) and unit-testable; `webviewperm` is the one CGO package (GTK/WebKit, Linux only) and keeps its decision logic in pure Go so `policy.go` still tests without a display; `app` orchestrates them and talks to the frontend; `main` only assembles.
```

- [ ] **步骤 2：在 README 的 File layout 代码块里登记新包**

在 `internal/packaging/` 那一行之后插入：

```
internal/webviewperm/   内嵌 WebView 采集权限（GTK 侧遍历取回视图 + permission-request 策略）
```

- [ ] **步骤 3：新增 README 小节，说明行为与原因**

在 `## Architecture` 之后、`## Startup phases and loading-page progress` 之前插入（英文正文，与该文件既有语言一致）：

```markdown
## Embedded WebView media permissions

The launcher grants the embedded WebView permission to capture **audio only**, so voice input works in the desktop build exactly as it does when the same Web GUI is opened in a browser.

Wails v2's Linux backend never connects `WebKitWebView::permission-request`, and WebKitGTK's default for that signal is *deny*. Every `getUserMedia` call from the embedded page therefore fails with `NotAllowedError`, which the voice-input client reports as "microphone permission is off" — with no setting the user could change. Wails keeps the `WebKitWebView*` in an `internal/` package, so the launcher cannot reach it directly; `internal/webviewperm` instead walks the process's top-level windows with `gtk_window_list_toplevels()`, finds the view, and connects the signal itself. The walk happens in `OnDomReady`, which Wails enters on the GTK main thread, so no thread hop is needed.

Independently, the shell embeds the harness UI in a **cross-origin** iframe, and `microphone`'s permissions policy allowlist defaults to `self`; that iframe therefore carries `allow="fullscreen; microphone"`. Without it the request is rejected before WebKit ever emits the permission signal, so fixing only the signal would leave voice input broken. Both changes are required.

`internal/webviewperm/policy.go` owns the decision and is unit-tested without GTK: a request is allowed only when it is a user-media request for audio and asks for neither video nor display. Camera, screen sharing, and every non-media permission request stay denied — the embedded UI has no feature that needs them. If a view cannot be found, the launcher logs one line and continues; a missing microphone permission must never block startup.
```

- [ ] **步骤 4：新增 Agent Note 三件套**

按 `.agents/notes/README.md` 的格式创建英文、中文与 sidecar 三个文件，路径为 `.agents/notes/implemented/architecture/2026-09-25-desktop-webview-media-permission.md` / `.zh.md` / `.i18n.yaml`。中文文件头部格式与既有的 `.agents/notes/implemented/architecture/2026-09-24-web-default-schedule-composition.zh.md` 一致：

```markdown
# Agent Note: 桌面版内嵌 WebView 的媒体采集权限

Status: implemented

[English](2026-09-25-desktop-webview-media-permission.md) | 中文
```

正文至少覆盖三节：

- **Problem**：三个彼此独立的根因叠加，且前两个症状相同（都是 `NotAllowedError`）。其一，Wails v2 Linux 后端不连接 `permission-request`，WebKitGTK 默认拒绝，而 `WebKitWebView*` 位于 `internal/` 包内、应用层无法直接引用；其二，壳把 harness UI 放在跨源 iframe 里，`microphone` 的 permissions policy 默认白名单是 `self`，缺 `allow="microphone"` 时请求在发出权限信号之前就被拒绝；其三，WebKit 的 WebProcess 找不到包内 GStreamer 插件（容器内插件目录只有 2 个，包内 259 个），`appsink` 加载失败使枚举到 0 个音频输入，请求以 `OverconstrainedError` 失败。
- **Decision**：新增 `internal/webviewperm`，在 `OnDomReady`（GTK 主线程）用 `gtk_window_list_toplevels()` 遍历控件树取回视图并连接信号；策略集中在纯 Go 的 `policy.go`，仅放行「音频且无视频/屏幕」的采集。同时给壳的 iframe 加 `allow="fullscreen; microphone"`，并新增 `packaging.ConfigureGStreamerPlugins()` 在 `wails.Run` 之前设置附加语义的 `GST_PLUGIN_PATH`。三处缺一不可。
- **Alternatives considered**：给 Wails 上游提 PR 并 `replace` 到 fork（周期不可控，保留为后续独立事项）；`LD_PRELOAD` 注入（侵入性最强）；修正提示文案（不解决功能，属可并行的体验修复）。

同时记录后续可简化条件：若 Wails 上游支持媒体权限，`internal/webviewperm` 可整体删除。

- [ ] **步骤 5：重录双语配对哈希**

```sh
cd /home/Jokul/Documents/GitHub/deepseek-harness
pnpm run verify-translation-pairing --write .agents/notes/implemented/architecture/2026-09-25-desktop-webview-media-permission.md
```

- [ ] **步骤 6：跑文档门禁**

```sh
pnpm run test:docs
```

预期：通过。若报 README 或 Agent Note 的语言配对问题，按提示补齐再重跑。

- [ ] **步骤 7：提交**

```sh
git add apps/desktop-launcher/README.md .agents/notes/implemented/architecture/2026-09-25-desktop-webview-media-permission.md .agents/notes/implemented/architecture/2026-09-25-desktop-webview-media-permission.zh.md .agents/notes/implemented/architecture/2026-09-25-desktop-webview-media-permission.i18n.yaml
git commit -m "docs(desktop-launcher): 记录内嵌 WebView 麦克风权限行为与决策"
```

---

## 风险与回退

| 风险 | 影响 | 处置 |
|---|---|---|
| Wails 升级后窗口层级变化，遍历找不到视图 | 麦克风回到不可用 | 遍历写成递归、不硬编码层级；找不到只记日志。**不会崩溃，也不会误连其它控件** |
| 放行范围被无意放宽 | 摄像头/屏幕共享被放行 | 策略集中在 `policy.go` 并由 8 条表驱动用例锁定；C 侧只做取事实，不改判断 |
| 在非 GTK 主线程访问控件树 | 未定义行为/崩溃 | `Install()` 只从 `OnDomReady` 调用，该回调已确认在 GTK 主线程（§0.4）；GoDoc 中写明该前提 |
| 构建宿主缺 GTK/WebKit 开发包 | 编译失败 | 与构建 wails 的既有要求相同，不新增依赖（§0.5） |
| 覆盖用户或宿主已配置的插件搜索路径 | 第三方 GStreamer 插件失效 | 用附加语义的 `GST_PLUGIN_PATH`（不覆盖系统默认路径），并把环境里已有的取值追加在后面而非丢弃；单测锁定 |
| 在 `wails.Run` 之后才设置 `GST_PLUGIN_PATH` | 对已启动的 WebProcess 无效，修复形同虚设 | 调用点固定在 `wails.Run` 之前，并在 GoDoc 与调用处注释写明原因 |
| 改动引入回归 | 启动失败 | 回退只需删除 `main.go` 的 `OnDomReady` 挂载与 `ConfigureGStreamerPlugins()` 调用（或删除整个包），不涉及数据、配置或迁移 |
| **只修了部分根因** | 麦克风仍不可用；根因一/二已修但根因三未修时，症状还会从「权限未开启」**变成另一种失败**，容易误判成改坏了 | 三个根因必须全部修复。区分方法：若日志里**没有**「收到 UserMedia 权限请求」，卡在根因二（iframe 未授权）；若出现了该日志却仍未取到音频，回到根因一排查；若错误名是 `OverconstrainedError`，则是根因三（插件路径） |
| iframe 加 `allow` 影响全屏 | 舞台无法全屏 | 变更里 `allow` 显式带上 `fullscreen` 且保留 `allowfullscreen`，不依赖各引擎「谁覆盖谁」的细节；`preview.mjs` 的布局不变量可复核 |

## 验收标准

1. 桌面版内嵌窗口点击麦克风后出现实时波形，停止后返回识别文本——与浏览器 Web 版一致。
2. 启动日志出现「麦克风权限处理已挂载」，证明遍历在真实 Wails 窗口上命中。
3. 壳的 iframe 带 `allow` 且其中含 `microphone`：缺它时权限信号**不会**触发（§0.2.1），因此这是与第 2 条并列的必要条件，不是可选项。
4. WebProcess 能加载包内插件：在包布局下枚举到 1 个音频输入（§0.7 的决定性验证），而不是 0 个。
5. `go test -tags "production webkit2_41" ./...` 与 `make vet` 全绿，且摄像头/屏幕共享仍被拒绝（单测锁定）。
6. 不覆盖环境里已有的 `GST_PLUGIN_PATH`，且开发态（包内无插件目录）不设置任何 `GST_*` 变量——单测与 GoDoc 共同锁定。

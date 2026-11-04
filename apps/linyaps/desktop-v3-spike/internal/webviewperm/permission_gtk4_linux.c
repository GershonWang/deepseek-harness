// GTK4 + WebKitGTK 6.0 下的权限信号接管实现。
//
// 与客户端现有 GTK3 版（desktop-launcher/internal/webviewperm/permission_linux.c）
// 的差异，逐条对应 GTK4 的 API 变化：
//   - gtk_window_list_toplevels() 已删除 → gtk_window_get_toplevels() 返回 GListModel；
//   - gtk_container_get_children() 在 GTK4 里不再是通用遍历手段 →
//     gtk_widget_get_first_child() / gtk_widget_get_next_sibling()；
//   - 头文件与 pkg-config 包名换成 webkitgtk-6.0（<webkit/webkit.h>）；
//   - 不再能从 OnDomReady 的 GTK 回调里同步进入，改为 g_timeout_add 在主循环上重试。
//
// 文件名后缀与 Go 侧的构建约束一致：只在 Linux + cgo + 非 gtk3 构建里参与编译。

#include <gtk/gtk.h>
#include <webkit/webkit.h>

#include "_cgo_export.h"

// dshPermissionHandlerKey 是打在已连接视图上的标记，避免重复连接同一实例。
static const char *dshPermissionHandlerKey = "dsh-media-permission-handler";

// dshInstallAttempts 记录本次挂在主循环上重试了几次。
static int dshInstallAttempts = 0;

// dshTopLevelsSeen 是最近一次遍历时的顶层窗口数量。
static int dshTopLevelsSeen = 0;

// dshViewsSeen 记录本次遍历遇到的 WebKitWebView 数量。
// 与「顶层窗口数」一起上报：只有这两个数能把失败原因分开。
static int dshViewsSeen = 0;

// dshFindWebView 在控件子树中深度优先查找第一个 WebKitWebView。
//
// 不硬编码层级（实测 Wails 的层级可能随版本变化）：找不到只会退化成「未挂载并记日志」，
// 而不会连到错误的控件上。
static GtkWidget *dshFindWebView(GtkWidget *widget)
{
	if (widget == NULL) {
		return NULL;
	}
	if (WEBKIT_IS_WEB_VIEW(widget)) {
		dshViewsSeen++;
		return widget;
	}
	for (GtkWidget *child = gtk_widget_get_first_child(widget); child != NULL;
	     child = gtk_widget_get_next_sibling(child)) {
		GtkWidget *found = dshFindWebView(child);
		if (found != NULL) {
			return found;
		}
	}
	return NULL;
}

// dshFindWebViewInToplevels 遍历当前进程的全部顶层窗口，返回第一个 WebKitWebView。
//
// gtk_window_get_toplevels 返回的是 GListModel（transfer full），既要用
// g_list_model_get_item 取元素（每个元素也是一份引用），也要在结束时unref，
// 否则每 100ms 重试一次就会持续泄漏。
static GtkWidget *dshFindWebViewInToplevels(void)
{
	GListModel *toplevels = gtk_window_get_toplevels();
	GtkWidget *webView = NULL;
	guint count = g_list_model_get_n_items(toplevels);
	// 遍历期间的真实顶层窗口数：重试时窗口可能还没建出来，这个数字随之变化。
	dshTopLevelsSeen = (int)count;
	for (guint i = 0; i < count && webView == NULL; i++) {
		GtkWindow *window = g_list_model_get_item(toplevels, i);
		if (window == NULL) {
			continue;
		}
		webView = dshFindWebView(GTK_WIDGET(window));
		g_object_unref(window);
	}
	g_object_unref(toplevels);
	return webView;
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

	int allowed = dshShouldAllowPermission(isUserMedia, isAudio, isVideo, isDisplay);
	if (allowed) {
		webkit_permission_request_allow(request);
	} else {
		webkit_permission_request_deny(request);
	}
	dshRecordPermissionRequest(isUserMedia, isAudio, isVideo, isDisplay, allowed);
	return TRUE;
}

// dshTryInstall 是主循环上的重试回调：找到视图即挂信号并停止，超时则放弃。
//
// 最多等 5 秒（50 次 × 100ms）。放弃时只报告「未挂载」：界面本身可用，
// 缺麦克风权限不该拦住启动或让进程退出。
static gboolean dshTryInstall(gpointer userData)
{
	(void)userData;
	dshInstallAttempts++;
	dshViewsSeen = 0;

	GtkWidget *webView = dshFindWebViewInToplevels();
	if (webView == NULL) {
		if (dshInstallAttempts >= 50) {
			dshReportInstall(dshInstallAttempts, 0, dshTopLevelsSeen, dshViewsSeen);
			return G_SOURCE_REMOVE;
		}
		return G_SOURCE_CONTINUE;
	}

	if (g_object_get_data(G_OBJECT(webView), dshPermissionHandlerKey) == NULL) {
		g_signal_connect(G_OBJECT(webView), "permission-request",
		                 G_CALLBACK(dshOnPermissionRequest), NULL);
		g_object_set_data(G_OBJECT(webView), dshPermissionHandlerKey, GINT_TO_POINTER(1));
	}
	dshReportInstall(dshInstallAttempts, 1, dshTopLevelsSeen, dshViewsSeen);
	return G_SOURCE_REMOVE;
}

// dshInstallPermissionHandlerGTK4 把挂载动作排到 GTK 主循环。
//
// 返回 1 表示已调度。刻意不在这里同步遍历：调用点可能不在主线程，而 GTK 控件树
// 只能在主线程访问。
int dshInstallPermissionHandlerGTK4(void)
{
	dshInstallAttempts = 0;
	g_timeout_add(100, dshTryInstall, NULL);
	return 1;
}

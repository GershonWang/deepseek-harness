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

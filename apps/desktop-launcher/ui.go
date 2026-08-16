package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
#include <stdint.h>
#include <stdlib.h>

extern void dshOnServerStatusClicked(void);
extern void dshOnAboutClicked(void);
extern void dshRefreshStatus(void);
extern void dshOnServerStart(void);
extern void dshOnServerRestart(void);
extern void dshOnServerStop(void);
extern void dshOnServerDialogDestroyed(void);

// ---- 窗口居中:按屏幕尺寸移动窗口到中心 ----
static void dsh_center_window(GtkWindow *win, gint ww, gint wh) {
  GdkScreen *screen = gtk_window_get_screen(win);
  if (screen == NULL) {
    return;
  }
  gint sw = gdk_screen_get_width(screen);
  gint sh = gdk_screen_get_height(screen);
  gint x = (sw - ww) / 2;
  gint y = (sh - wh) / 2;
  gtk_window_move(win, x > 0 ? x : 0, y > 0 ? y : 0);
}

// ---- 状态栏按钮回调 ----
static void dsh_server_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerStatusClicked(); }
static void dsh_about_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnAboutClicked(); }

// ---- 把 webview 摘进 vbox,底部插状态栏;返回状态指示 label ----
// GTK3 浮动引用:webkit_web_view_new() 返回浮动引用,gtk_container_add 时被
// 窗口容器 sink,窗口持有唯一引用。直接 gtk_container_remove 会释放该引用,
// webview 引用计数归零被销毁(finalize),后续 Navigate 因 webview_go 持有的
// m_webview 悬空而失败。重挂前先 g_object_ref 保住引用,pack 进 vbox 后释放。
static GtkWidget *dsh_install_status_bar(GtkWindow *win) {
  GtkWidget *webview = gtk_bin_get_child(GTK_BIN(win));
  GtkWidget *vbox = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);
  GtkWidget *bar = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 4);
  GtkWidget *label = gtk_label_new("● 启动中");
  gtk_widget_set_halign(label, GTK_ALIGN_START);
  GtkWidget *btn_server = gtk_button_new_with_label("服务器状态");
  GtkWidget *btn_about = gtk_button_new_with_label("关于");
  g_signal_connect(btn_server, "clicked", G_CALLBACK(dsh_server_clicked), NULL);
  g_signal_connect(btn_about, "clicked", G_CALLBACK(dsh_about_clicked), NULL);
  gtk_box_pack_start(GTK_BOX(bar), label, TRUE, TRUE, 4);
  gtk_box_pack_end(GTK_BOX(bar), btn_about, FALSE, FALSE, 4);
  gtk_box_pack_end(GTK_BOX(bar), btn_server, FALSE, FALSE, 4);
  g_object_ref(webview);
  gtk_container_remove(GTK_CONTAINER(win), webview);
  gtk_box_pack_start(GTK_BOX(vbox), webview, TRUE, TRUE, 0);
  g_object_unref(webview);
  gtk_box_pack_start(GTK_BOX(vbox), bar, FALSE, FALSE, 0);
  gtk_container_add(GTK_CONTAINER(win), vbox);
  gtk_widget_show_all(vbox);
  return label;
}

// ---- 状态轮询(1s,GTK 主循环回调) ----
// 必须保持 static:ui.go 含 //export 函数,其 preamble 会被 cgo 同时编译进
// ui.cgo2.c 和 _cgo_export.c(_cgo_export.h 内嵌整段 preamble),非 static
// 定义会重复符号导致链接失败;而 static 函数又不能被 Go 侧取地址,所以由
// dsh_start_status_tick 在 C 内部注册回调,Go 只做直接调用。
static gboolean dsh_status_tick(gpointer d) {
  (void)d;
  dshRefreshStatus();
  return G_SOURCE_CONTINUE;
}

// 注册 1s 状态轮询;在 C 内取 dsh_status_tick 的地址。
static void dsh_start_status_tick(void) {
  g_timeout_add(1000, dsh_status_tick, NULL);
}

// ---- 服务器状态弹框 ----
// 单实例弹框,构建时保存子控件指针供刷新直接使用。不能依赖
// gtk_container_get_children 的索引:GtkGrid 在 GTK 3.24 下返回顺序并非
// attach 顺序(实测先返回 3 个按钮再返回 2 个 label),按索引取会拿到
// 错误控件。销毁时清空指针,防止刷新访问已销毁控件。
static GtkWidget *dsh_dlg_state = NULL;
static GtkWidget *dsh_dlg_detail = NULL;
static GtkWidget *dsh_dlg_btn_start = NULL;
static GtkWidget *dsh_dlg_btn_restart = NULL;
static GtkWidget *dsh_dlg_btn_stop = NULL;

static void dsh_server_start_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerStart(); }
static void dsh_server_restart_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerRestart(); }
static void dsh_server_stop_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerStop(); }
static void dsh_server_dialog_destroyed(GtkWidget *w, gpointer d) {
  (void)w; (void)d;
  dsh_dlg_state = dsh_dlg_detail = NULL;
  dsh_dlg_btn_start = dsh_dlg_btn_restart = dsh_dlg_btn_stop = NULL;
  dshOnServerDialogDestroyed();
}
static void dsh_dialog_response(GtkDialog *dlg, gint resp, gpointer d) {
  (void)d;
  if (resp == GTK_RESPONSE_CLOSE || resp == GTK_RESPONSE_DELETE_EVENT) {
    gtk_widget_destroy(GTK_WIDGET(dlg));
  }
}

static GtkWidget *dsh_make_server_dialog(GtkWindow *parent) {
  GtkWidget *dlg = gtk_dialog_new_with_buttons(
      "服务器状态", parent, GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
      "_关闭", GTK_RESPONSE_CLOSE, NULL);
  g_signal_connect(dlg, "response", G_CALLBACK(dsh_dialog_response), NULL);
  g_signal_connect(dlg, "destroy", G_CALLBACK(dsh_server_dialog_destroyed), NULL);
  GtkWidget *content = gtk_dialog_get_content_area(GTK_DIALOG(dlg));
  GtkWidget *grid = gtk_grid_new();
  gtk_grid_set_row_spacing(GTK_GRID(grid), 8);
  gtk_grid_set_column_spacing(GTK_GRID(grid), 8);
  dsh_dlg_state = gtk_label_new("状态: …");
  dsh_dlg_detail = gtk_label_new("");
  gtk_widget_set_halign(dsh_dlg_state, GTK_ALIGN_START);
  gtk_widget_set_halign(dsh_dlg_detail, GTK_ALIGN_START);
  dsh_dlg_btn_start = gtk_button_new_with_label("启动");
  dsh_dlg_btn_restart = gtk_button_new_with_label("重启");
  dsh_dlg_btn_stop = gtk_button_new_with_label("停止");
  g_signal_connect(dsh_dlg_btn_start, "clicked", G_CALLBACK(dsh_server_start_clicked), NULL);
  g_signal_connect(dsh_dlg_btn_restart, "clicked", G_CALLBACK(dsh_server_restart_clicked), NULL);
  g_signal_connect(dsh_dlg_btn_stop, "clicked", G_CALLBACK(dsh_server_stop_clicked), NULL);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_state, 0, 0, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_detail, 0, 1, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_btn_start, 0, 2, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_btn_restart, 1, 2, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_btn_stop, 2, 2, 1, 1);
  gtk_container_add(GTK_CONTAINER(content), grid);
  gtk_widget_show_all(dlg);
  return dlg;
}

// ---- 刷新服务器弹框内容与按钮可用性(tick 调用) ----
static void dsh_update_server_dialog(GtkWidget *dlg, const char *state, const char *detail,
                                     gboolean can_start, gboolean can_restart, gboolean can_stop) {
  (void)dlg;
  gtk_label_set_text(GTK_LABEL(dsh_dlg_state), state);
  gtk_label_set_text(GTK_LABEL(dsh_dlg_detail), detail);
  gtk_widget_set_sensitive(dsh_dlg_btn_start, can_start);
  gtk_widget_set_sensitive(dsh_dlg_btn_restart, can_restart);
  gtk_widget_set_sensitive(dsh_dlg_btn_stop, can_stop);
}

// ---- 关于弹框(GtkAboutDialog,run 阻塞式) ----
static void dsh_show_about_dialog(GtkWindow *parent, const char *program,
                                  const char *version, const char *comments,
                                  const char *website, const char *author) {
  const char *authors[] = { author, NULL };
  GtkWidget *dlg = gtk_about_dialog_new();
  gtk_about_dialog_set_program_name(GTK_ABOUT_DIALOG(dlg), program);
  gtk_about_dialog_set_version(GTK_ABOUT_DIALOG(dlg), version);
  gtk_about_dialog_set_comments(GTK_ABOUT_DIALOG(dlg), comments);
  gtk_about_dialog_set_website(GTK_ABOUT_DIALOG(dlg), website);
  gtk_about_dialog_set_website_label(GTK_ABOUT_DIALOG(dlg), website);
  gtk_about_dialog_set_authors(GTK_ABOUT_DIALOG(dlg), authors);
  gtk_window_set_transient_for(GTK_WINDOW(dlg), parent);
  gtk_dialog_run(GTK_DIALOG(dlg));
  gtk_widget_destroy(dlg);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// 包级 UI 状态:单一窗口实例,由 installDesktopUI 初始化,GTK 回调使用。
var (
	activeSupervisor *Supervisor
	mainWindow       *C.GtkWindow
	statusLabel      *C.GtkWidget
	serverDialog     *C.GtkWidget
)

// installDesktopUI 挂载底部状态栏、注册 1s 状态轮询并居中窗口。
// 必须在 w.Run() 之前调用;win 来自 webview.WebView.Window()。
func installDesktopUI(win unsafe.Pointer, sup *Supervisor) {
	if win == nil {
		return
	}
	activeSupervisor = sup
	mainWindow = (*C.GtkWindow)(win)
	statusLabel = C.dsh_install_status_bar(mainWindow)
	C.dsh_center_window(mainWindow, 1280, 800)
	C.dsh_start_status_tick()
	dshRefreshStatus()
}

//export dshRefreshStatus
func dshRefreshStatus() {
	sup := activeSupervisor
	if sup == nil {
		return
	}
	st := sup.Status()

	bar := C.CString(statusBarText(st))
	C.gtk_label_set_text((*C.GtkLabel)(unsafe.Pointer(statusLabel)), bar)
	C.free(unsafe.Pointer(bar))

	if serverDialog != nil {
		d := serverDialogState(st)
		state := C.CString(d.State)
		detail := C.CString(d.Detail)
		C.dsh_update_server_dialog(serverDialog, state, detail,
			boolToGboolean(d.CanStart), boolToGboolean(d.CanRestart), boolToGboolean(d.CanStop))
		C.free(unsafe.Pointer(state))
		C.free(unsafe.Pointer(detail))
	}
}

//export dshOnServerStatusClicked
func dshOnServerStatusClicked() {
	if activeSupervisor == nil {
		return
	}
	if serverDialog != nil {
		C.gtk_window_present((*C.GtkWindow)(unsafe.Pointer(serverDialog)))
		return
	}
	serverDialog = C.dsh_make_server_dialog(mainWindow)
	dshRefreshStatus()
}

//export dshOnServerDialogDestroyed
func dshOnServerDialogDestroyed() {
	serverDialog = nil
}

//export dshOnServerStart
func dshOnServerStart() {
	if activeSupervisor != nil {
		activeSupervisor.Start()
	}
}

//export dshOnServerRestart
func dshOnServerRestart() {
	if activeSupervisor != nil {
		activeSupervisor.Restart()
	}
}

//export dshOnServerStop
func dshOnServerStop() {
	if activeSupervisor != nil {
		activeSupervisor.StopHarness()
	}
}

//export dshOnAboutClicked
func dshOnAboutClicked() {
	version := fmt.Sprintf("harness %s\n玲珑包 %s", resolveHarnessVersion(), packageVersion)
	prog := C.CString("DeepSeek Harness")
	ver := C.CString(version)
	comments := C.CString("DeepSeek Harness 桌面客户端,以受监护子进程运行 harness 并加载其 Web GUI。")
	website := C.CString(githubRepo)
	author := C.CString("GershonWang")
	C.dsh_show_about_dialog(mainWindow, prog, ver, comments, website, author)
	C.free(unsafe.Pointer(prog))
	C.free(unsafe.Pointer(ver))
	C.free(unsafe.Pointer(comments))
	C.free(unsafe.Pointer(website))
	C.free(unsafe.Pointer(author))
}

func boolToGboolean(b bool) C.gboolean {
	if b {
		return 1
	}
	return 0
}

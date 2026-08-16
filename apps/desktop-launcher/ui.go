package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
#include <gdk-pixbuf/gdk-pixbuf.h>
#include <string.h>
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

// ---- 自定义样式(GtkCssProvider)----
// 全部样式内联在此,不依赖外部 css 文件,便于随二进制分发。颜色尽量取自
// GTK 主题变量(@theme_bg_color / @borders 等),亮暗主题下自动适配。
// 底部状态栏:浅色渐变底 + 顶部发丝分隔线,与窗口内容区区分
static const char *dsh_css =
    ".dsh-statusbar {\n"
    "  padding: 6px 14px;\n"
    "  background-image: linear-gradient(to bottom,\n"
    "      shade(@theme_bg_color, 0.99), shade(@theme_bg_color, 0.955));\n"
    "  border-top: 1px solid alpha(@borders, 0.8);\n"
    "}\n"
    ".dsh-statusbar .dsh-status-label {\n"
    "  color: alpha(@theme_fg_color, 0.9);\n"
    "}\n"
    // 状态栏按钮:统一舒适内边距与圆角,层级由按钮类区分
    ".dsh-statusbar button {\n"
    "  padding: 4px 14px;\n"
    "  border-radius: 6px;\n"
    "}\n"
    // 关于按钮:安静的文字按钮(透明底 + 细描边,悬停/按下轻着色)
    ".dsh-statusbar button.dsh-btn-quiet {\n"
    "  background-color: transparent;\n"
    "  background-image: none;\n"
    "  border: 1px solid alpha(@theme_fg_color, 0.28);\n"
    "  color: @theme_fg_color;\n"
    "  box-shadow: none;\n"
    "}\n"
    ".dsh-statusbar button.dsh-btn-quiet:hover {\n"
    "  background-color: alpha(@theme_fg_color, 0.07);\n"
    "}\n"
    ".dsh-statusbar button.dsh-btn-quiet:active {\n"
    "  background-color: alpha(@theme_fg_color, 0.13);\n"
    "}\n"
    // 服务器状态弹框:键列弱化右对齐,状态行强调,圆点按状态着色
    ".dsh-dialog-key {\n"
    "  color: alpha(@theme_fg_color, 0.62);\n"
    "}\n"
    ".dsh-dialog-state {\n"
    "  font-size: 14px;\n"
    "  font-weight: 600;\n"
    "}\n"
    ".dsh-state-dot {\n"
    "  font-weight: 700;\n"
    "}\n"
    ".dsh-state-running { color: #2ea043; }\n"
    ".dsh-state-starting { color: #c69026; }\n"
    ".dsh-state-stopped { color: #8b949e; }\n"
    ".dsh-dialog-actions button {\n"
    "  min-width: 72px;\n"
    "  padding: 5px 16px;\n"
    "}\n";

// 以应用级优先级把样式挂到整个屏幕,状态栏与弹框共享同一套样式。
static void dsh_apply_style(GtkWindow *win) {
  GdkScreen *screen = gtk_window_get_screen(win);
  if (screen == NULL) {
    return;
  }
  GtkCssProvider *provider = gtk_css_provider_new();
  GError *err = NULL;
  if (!gtk_css_provider_load_from_data(provider, dsh_css, -1, &err)) {
    g_warning("dsh: 加载自定义样式失败: %s", err != NULL ? err->message : "unknown");
    g_clear_error(&err);
    g_object_unref(provider);
    return;
  }
  gtk_style_context_add_provider_for_screen(
      screen, GTK_STYLE_PROVIDER(provider), GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
  g_object_unref(provider);
}

// ---- 状态栏文本:给前导 ● 按状态着色,正文保持主题默认色 ----
// state 与 Go 侧 HarnessState 对齐:0=启动中(琥珀),1=运行中(绿),其余=已停止(灰)。
// 文本以 UTF-8 圆点 "●"(E2 97 8F)开头,其余部分原样保留。
static void dsh_set_status_label(GtkLabel *label, const char *text, int state) {
  if (text == NULL) {
    text = "";
  }
  const char *dot_color = state == 1 ? "#2ea043" : (state == 0 ? "#c69026" : "#8b949e");
  if (strncmp(text, "\xe2\x97\x8f", 3) != 0) {
    gtk_label_set_text(label, text);
    return;
  }
  char *escaped = g_markup_escape_text(text, -1);
  GString *s = g_string_new(NULL);
  g_string_append_printf(s, "<span foreground=\"%s\" weight=\"bold\">●</span>%s",
                         dot_color, escaped + 3);
  gtk_label_set_markup(label, s->str);
  g_string_free(s, TRUE);
  g_free(escaped);
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
  GtkWidget *bar = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 12);
  gtk_style_context_add_class(gtk_widget_get_style_context(bar), "dsh-statusbar");
  GtkWidget *label = gtk_label_new("● 启动中");
  gtk_style_context_add_class(gtk_widget_get_style_context(label), "dsh-status-label");
  gtk_widget_set_halign(label, GTK_ALIGN_START);
  dsh_set_status_label(GTK_LABEL(label), "● 启动中", 0); // 首帧即按启动中着色
  GtkWidget *btn_server = gtk_button_new_with_label("服务器状态");
  GtkWidget *btn_about = gtk_button_new_with_label("关于");
  // 视觉层级:服务器状态是主操作(主题强调色),关于是安静的文字按钮
  gtk_style_context_add_class(gtk_widget_get_style_context(btn_server), "suggested-action");
  gtk_style_context_add_class(gtk_widget_get_style_context(btn_about), "dsh-btn-quiet");
  g_signal_connect(btn_server, "clicked", G_CALLBACK(dsh_server_clicked), NULL);
  g_signal_connect(btn_about, "clicked", G_CALLBACK(dsh_about_clicked), NULL);
  gtk_box_pack_start(GTK_BOX(bar), label, TRUE, TRUE, 0);
  gtk_box_pack_end(GTK_BOX(bar), btn_about, FALSE, FALSE, 0);
  gtk_box_pack_end(GTK_BOX(bar), btn_server, FALSE, FALSE, 0);
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
static GtkWidget *dsh_dlg_state = NULL;  // 状态值 label(粗体强调)
static GtkWidget *dsh_dlg_dot = NULL;    // 状态圆点(按状态着色)
static GtkWidget *dsh_dlg_detail = NULL; // 详情区 GtkGrid(两行 key/value)
static GtkWidget *dsh_dlg_key1 = NULL, *dsh_dlg_val1 = NULL; // 详情第一行
static GtkWidget *dsh_dlg_key2 = NULL, *dsh_dlg_val2 = NULL; // 详情第二行
static GtkWidget *dsh_dlg_btn_start = NULL;
static GtkWidget *dsh_dlg_btn_restart = NULL;
static GtkWidget *dsh_dlg_btn_stop = NULL;

// 状态字符串 -> 圆点 CSS 类;未知状态一律按"已停止"灰色处理。
// 状态文案来自 ui_state.go(运行中/启动中/已停止),文案调整需同步此处。
static const char *dsh_state_class(const char *state) {
  if (state != NULL && strcmp(state, "运行中") == 0) {
    return "dsh-state-running";
  }
  if (state != NULL && strcmp(state, "启动中") == 0) {
    return "dsh-state-starting";
  }
  return "dsh-state-stopped";
}

// 圆点样式类只保留当前状态,避免多个着色类叠加。
static void dsh_set_state_class(GtkWidget *dot, const char *state) {
  GtkStyleContext *ctx = gtk_widget_get_style_context(dot);
  gtk_style_context_remove_class(ctx, "dsh-state-running");
  gtk_style_context_remove_class(ctx, "dsh-state-starting");
  gtk_style_context_remove_class(ctx, "dsh-state-stopped");
  gtk_style_context_add_class(ctx, dsh_state_class(state));
}

// 详情区单行:行内首个 ": " 拆成 键(弱化)/值 两列;无分隔符时整行放值列。
static void dsh_set_detail_row(GtkWidget *key, GtkWidget *val, const char *line) {
  if (line == NULL) {
    line = "";
  }
  const char *sep = strstr(line, ": ");
  if (sep != NULL) {
    char *k = g_strndup(line, (gsize)(sep - line));
    gtk_label_set_text(GTK_LABEL(key), k);
    g_free(k);
    gtk_label_set_text(GTK_LABEL(val), sep + 2);
    gtk_widget_show(key);
  } else {
    gtk_widget_hide(key);
    gtk_label_set_text(GTK_LABEL(val), line);
  }
  gtk_widget_show(val);
}

// 详情区刷新:当前数据源至多两行("地址/PID"),超出部分忽略。
static void dsh_update_detail(const char *detail) {
  if (detail == NULL) {
    detail = "";
  }
  gchar **parts = g_strsplit(detail, "\n", 3);
  dsh_set_detail_row(dsh_dlg_key1, dsh_dlg_val1, parts[0]);
  if (parts[1] != NULL) {
    dsh_set_detail_row(dsh_dlg_key2, dsh_dlg_val2, parts[1]);
  } else {
    gtk_widget_hide(dsh_dlg_key2);
    gtk_widget_hide(dsh_dlg_val2);
  }
  g_strfreev(parts);
}

static void dsh_server_start_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerStart(); }
static void dsh_server_restart_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerRestart(); }
static void dsh_server_stop_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnServerStop(); }
static void dsh_server_dialog_destroyed(GtkWidget *w, gpointer d) {
  (void)w; (void)d;
  dsh_dlg_state = dsh_dlg_dot = dsh_dlg_detail = NULL;
  dsh_dlg_key1 = dsh_dlg_val1 = dsh_dlg_key2 = dsh_dlg_val2 = NULL;
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
  gtk_widget_set_size_request(dlg, 420, -1);

  GtkWidget *content = gtk_dialog_get_content_area(GTK_DIALOG(dlg));
  GtkWidget *vbox = gtk_box_new(GTK_ORIENTATION_VERTICAL, 14);

  // 键/值两列网格:键右对齐且弱化,值左对齐;状态行带彩色圆点。
  GtkWidget *grid = gtk_grid_new();
  gtk_grid_set_row_spacing(GTK_GRID(grid), 10);
  gtk_grid_set_column_spacing(GTK_GRID(grid), 14);
  gtk_widget_set_margin_start(grid, 18);
  gtk_widget_set_margin_end(grid, 18);
  gtk_widget_set_margin_top(grid, 18);

  GtkWidget *key_state = gtk_label_new("状态");
  gtk_widget_set_halign(key_state, GTK_ALIGN_END);
  gtk_style_context_add_class(gtk_widget_get_style_context(key_state), "dsh-dialog-key");

  dsh_dlg_dot = gtk_label_new("●");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_dot), "dsh-state-dot");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_dot), "dsh-state-stopped");
  dsh_dlg_state = gtk_label_new("…");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_state), "dsh-dialog-state");
  gtk_widget_set_halign(dsh_dlg_state, GTK_ALIGN_START);
  GtkWidget *state_row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 6);
  gtk_box_pack_start(GTK_BOX(state_row), dsh_dlg_dot, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(state_row), dsh_dlg_state, FALSE, FALSE, 0);

  // 详情两行(地址/PID 或 上次退出),值可选中便于复制
  dsh_dlg_key1 = gtk_label_new("");
  dsh_dlg_val1 = gtk_label_new("");
  dsh_dlg_key2 = gtk_label_new("");
  dsh_dlg_val2 = gtk_label_new("");
  GtkWidget *detail_keys[] = {dsh_dlg_key1, dsh_dlg_key2};
  GtkWidget *detail_vals[] = {dsh_dlg_val1, dsh_dlg_val2};
  for (int i = 0; i < 2; i++) {
    gtk_style_context_add_class(gtk_widget_get_style_context(detail_keys[i]), "dsh-dialog-key");
    gtk_widget_set_halign(detail_keys[i], GTK_ALIGN_END);
    gtk_widget_set_halign(detail_vals[i], GTK_ALIGN_START);
    gtk_label_set_selectable(GTK_LABEL(detail_vals[i]), TRUE);
  }

  gtk_grid_attach(GTK_GRID(grid), key_state, 0, 0, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), state_row, 1, 0, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_key1, 0, 1, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_val1, 1, 1, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_key2, 0, 2, 1, 1);
  gtk_grid_attach(GTK_GRID(grid), dsh_dlg_val2, 1, 2, 1, 1);
  gtk_box_pack_start(GTK_BOX(vbox), grid, FALSE, FALSE, 0);

  // 控制按钮行:启动(主题强调)/重启(中性)/停止(危险),整行右对齐。
  // 三个按钮的启用/禁用仍由 dsh_update_server_dialog 的状态驱动逻辑决定。
  dsh_dlg_btn_start = gtk_button_new_with_label("启动");
  dsh_dlg_btn_restart = gtk_button_new_with_label("重启");
  dsh_dlg_btn_stop = gtk_button_new_with_label("停止");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_btn_start), "suggested-action");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_btn_stop), "destructive-action");
  g_signal_connect(dsh_dlg_btn_start, "clicked", G_CALLBACK(dsh_server_start_clicked), NULL);
  g_signal_connect(dsh_dlg_btn_restart, "clicked", G_CALLBACK(dsh_server_restart_clicked), NULL);
  g_signal_connect(dsh_dlg_btn_stop, "clicked", G_CALLBACK(dsh_server_stop_clicked), NULL);
  GtkWidget *actions = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
  gtk_widget_set_halign(actions, GTK_ALIGN_END);
  gtk_widget_set_margin_bottom(actions, 12);
  gtk_style_context_add_class(gtk_widget_get_style_context(actions), "dsh-dialog-actions");
  gtk_box_pack_start(GTK_BOX(actions), dsh_dlg_btn_start, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(actions), dsh_dlg_btn_restart, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(actions), dsh_dlg_btn_stop, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(vbox), actions, FALSE, FALSE, 0);

  gtk_container_add(GTK_CONTAINER(content), vbox);
  gtk_widget_show_all(dlg);
  return dlg;
}

// ---- 刷新服务器弹框内容与按钮可用性(tick 调用) ----
static void dsh_update_server_dialog(GtkWidget *dlg, const char *state, const char *detail,
                                     gboolean can_start, gboolean can_restart, gboolean can_stop) {
  (void)dlg;
  gtk_label_set_text(GTK_LABEL(dsh_dlg_state), state);
  dsh_set_state_class(dsh_dlg_dot, state);
  dsh_update_detail(detail);
  gtk_widget_set_sensitive(dsh_dlg_btn_start, can_start);
  gtk_widget_set_sensitive(dsh_dlg_btn_restart, can_restart);
  gtk_widget_set_sensitive(dsh_dlg_btn_stop, can_stop);
}

// ---- 关于弹框(GtkAboutDialog,run 阻塞式) ----
// 版本字符串可能含换行("harness X\n玲珑包 Y"):首行进版本号,其余行并入
// 说明区,避免版本号区域折行;icon_path 存在才加载图标,缺失时静默跳过。
static void dsh_show_about_dialog(GtkWindow *parent, const char *program,
                                  const char *version, const char *comments,
                                  const char *website, const char *author,
                                  const char *icon_path) {
  const char *authors[] = { author, NULL };
  GtkWidget *dlg = gtk_about_dialog_new();
  gtk_about_dialog_set_program_name(GTK_ABOUT_DIALOG(dlg), program);
  gtk_about_dialog_set_website(GTK_ABOUT_DIALOG(dlg), website);
  gtk_about_dialog_set_website_label(GTK_ABOUT_DIALOG(dlg), website);
  gtk_about_dialog_set_authors(GTK_ABOUT_DIALOG(dlg), authors);

  const char *nl = version != NULL ? strchr(version, '\n') : NULL;
  if (nl != NULL) {
    char *ver = g_strndup(version, (gsize)(nl - version));
    gtk_about_dialog_set_version(GTK_ABOUT_DIALOG(dlg), ver);
    char *extra = g_strconcat(comments, "\n\n", nl + 1, NULL);
    gtk_about_dialog_set_comments(GTK_ABOUT_DIALOG(dlg), extra);
    g_free(ver);
    g_free(extra);
  } else {
    gtk_about_dialog_set_version(GTK_ABOUT_DIALOG(dlg), version);
    gtk_about_dialog_set_comments(GTK_ABOUT_DIALOG(dlg), comments);
  }

  if (icon_path != NULL && g_file_test(icon_path, G_FILE_TEST_EXISTS)) {
    GError *err = NULL;
    GdkPixbuf *pb = gdk_pixbuf_new_from_file_at_scale(icon_path, 128, 128, TRUE, &err);
    if (pb != NULL) {
      gtk_about_dialog_set_logo(GTK_ABOUT_DIALOG(dlg), pb);
      g_object_unref(pb);
    } else {
      g_warning("dsh: 加载关于图标失败: %s", err != NULL ? err->message : "unknown");
      g_clear_error(&err);
    }
  }

  gtk_window_set_transient_for(GTK_WINDOW(dlg), parent);
  gtk_dialog_run(GTK_DIALOG(dlg));
  gtk_widget_destroy(dlg);
}
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

// 包级 UI 状态:单一窗口实例,由 installDesktopUI 初始化,GTK 回调使用。
var (
	activeSupervisor *Supervisor
	mainWindow       *C.GtkWindow
	statusLabel      *C.GtkWidget
	serverDialog     *C.GtkWidget
)

// installDesktopUI 挂载底部状态栏、应用自定义样式、注册 1s 状态轮询并居中窗口。
// 必须在 w.Run() 之前调用;win 来自 webview.WebView.Window()。
func installDesktopUI(win unsafe.Pointer, sup *Supervisor) {
	if win == nil {
		return
	}
	activeSupervisor = sup
	mainWindow = (*C.GtkWindow)(win)
	C.dsh_apply_style(mainWindow)
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

	// 状态文本仍来自 statusBarText,渲染交给 C 侧(给前导 ● 按状态着色)
	bar := C.CString(statusBarText(st))
	C.dsh_set_status_label((*C.GtkLabel)(unsafe.Pointer(statusLabel)), bar, C.int(st.State))
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
	icon := C.CString(aboutIconPath())
	C.dsh_show_about_dialog(mainWindow, prog, ver, comments, website, author, icon)
	C.free(unsafe.Pointer(prog))
	C.free(unsafe.Pointer(ver))
	C.free(unsafe.Pointer(comments))
	C.free(unsafe.Pointer(website))
	C.free(unsafe.Pointer(author))
	C.free(unsafe.Pointer(icon))
}

// fileExists 判断路径是否存在且为普通文件。
func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// aboutIconPath 返回关于弹框图标路径:打包态取 $PREFIX/share/icons 下的
// hicolor 图标(与 linglong.yaml 安装位置一致);开发态取可执行文件目录或
// 当前目录的 icons/dsh-desktop.png。两处都没有时返回空串(弹框不显示图标)。
func aboutIconPath() string {
	if p := harnessPrefix(); p != "" {
		cand := filepath.Join(p, "share", "icons", "hicolor", "256x256", "apps", "dsh-desktop.png")
		if fileExists(cand) {
			return cand
		}
	}
	// 开发态:可执行文件目录(go run 时为临时目录)或 cwd 下的 icons/
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "icons", "dsh-desktop.png")
		if fileExists(cand) {
			return cand
		}
	}
	cand := filepath.Join("icons", "dsh-desktop.png")
	if fileExists(cand) {
		return cand
	}
	return ""
}

func boolToGboolean(b bool) C.gboolean {
	if b {
		return 1
	}
	return 0
}

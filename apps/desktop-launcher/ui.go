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
extern void dshOnModeChanged(void);
extern void dshOnExternalConnect(void);
extern void dshOnExternalDisconnect(void);
extern void dshOnProbeResult(gpointer data);
extern void dshOnNavIdle(gpointer data);

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
    "}\n"
    // 外部连接区:错误标签红色,外部状态弱化;着色复用圆点同色系
    ".dsh-dialog-error {\n"
    "  color: #e5534b;\n"
    "  font-size: 12px;\n"
    "}\n"
    ".dsh-dialog-ext-state {\n"
    "  color: alpha(@theme_fg_color, 0.8);\n"
    "  font-size: 12px;\n"
    "}\n"
    // 复合选择器提高优先级,让状态色覆盖弱化基色
    ".dsh-dialog-ext-state.dsh-state-running { color: #2ea043; }\n"
    ".dsh-dialog-ext-state.dsh-state-stopped { color: #8b949e; }\n"
    // 服务地址行按钮:与容器按钮一致的最小宽度与内边距
    ".dsh-dialog-ext-row button {\n"
    "  min-width: 64px;\n"
    "  padding: 4px 14px;\n"
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
static GtkWidget *dsh_dlg_key1 = NULL, *dsh_dlg_val1 = NULL; // 详情第一行
static GtkWidget *dsh_dlg_key2 = NULL, *dsh_dlg_val2 = NULL; // 详情第二行
static GtkWidget *dsh_dlg_btn_start = NULL;
static GtkWidget *dsh_dlg_btn_restart = NULL;
static GtkWidget *dsh_dlg_btn_stop = NULL;

// ---- 外部连接:模式切换、URL 输入、连接/断开 ----
static GtkWidget *dsh_dlg_mode_container = NULL; // 模式单选:容器内
static GtkWidget *dsh_dlg_mode_external = NULL;  // 模式单选:本机/远端服务
static GtkWidget *dsh_dlg_url_entry = NULL;      // 外部服务地址输入
static GtkWidget *dsh_dlg_btn_connect = NULL;    // 连接按钮
static GtkWidget *dsh_dlg_btn_disconnect = NULL; // 断开按钮
static GtkWidget *dsh_dlg_error_label = NULL;    // 错误提示(红色)
static GtkWidget *dsh_dlg_ext_state = NULL;      // 外部状态区
static GtkWidget *dsh_dlg_container_buttons = NULL; // 启动/重启/停止 行
static GtkWidget *dsh_dlg_state_grid = NULL;     // 容器模式状态区(状态行)
static GtkWidget *dsh_dlg_detail_row1 = NULL;    // 详情第一行(地址/上次退出)
static GtkWidget *dsh_dlg_detail_row2 = NULL;    // 详情第二行(PID)
static GtkWidget *dsh_dlg_actions_sep = NULL;    // 外部连接区与容器按钮之间分隔线

static void dsh_mode_toggled(GtkToggleButton *b, gpointer d) { (void)b; (void)d; dshOnModeChanged(); }
static void dsh_external_connect_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnExternalConnect(); }
static void dsh_external_disconnect_clicked(GtkButton *b, gpointer d) { (void)b; (void)d; dshOnExternalDisconnect(); }

// 异步结果经 idle 回主线程。idle 回调必须保持 static(与 dsh_status_tick 同理),
// 而 static 函数不能被 Go 侧取地址,故由这两个 C 壳函数在 C 内注册 idle。
// 探测结果与待导航 URL 由 Go goroutine 以 C.CBytes 写入 C 内存,经 gpointer
// 交给 idle 回调;回调先调 Go 处理再 free,避免包级变量跨线程数据竞争。
static gboolean dsh_probe_idle(gpointer d) {
  dshOnProbeResult(d);
  free(d);
  return G_SOURCE_REMOVE;
}
static gboolean dsh_nav_idle(gpointer d) {
  dshOnNavIdle(d);
  free(d);
  return G_SOURCE_REMOVE;
}
static void dsh_schedule_probe_result(gpointer data) { g_idle_add(dsh_probe_idle, data); }
static void dsh_schedule_nav_idle(gpointer data) { g_idle_add(dsh_nav_idle, data); }

// Go 侧无法直接引用 C static 变量;弹框构建完成后把 Go 回调需要的子控件指针拷出。
static void dsh_get_dialog_ptrs(GtkWidget **mode_container, GtkWidget **mode_external,
                                GtkWidget **url_entry, GtkWidget **error_label) {
  *mode_container = dsh_dlg_mode_container;
  *mode_external = dsh_dlg_mode_external;
  *url_entry = dsh_dlg_url_entry;
  *error_label = dsh_dlg_error_label;
}

// 状态枚举 -> 圆点 CSS 类;与 Go 侧 HarnessState 对齐(0=启动中,1=运行中,其余=已停止)。
// 展示文案与着色分离:文案来自 ui_state.go,着色只依赖状态枚举,文案调整无需同步此处。
static const char *dsh_state_class(int state) {
  if (state == 1) {
    return "dsh-state-running";
  }
  if (state == 0) {
    return "dsh-state-starting";
  }
  return "dsh-state-stopped";
}

// 圆点样式类只保留当前状态,避免多个着色类叠加。
static void dsh_set_state_class(GtkWidget *dot, int state) {
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
  dsh_dlg_state = dsh_dlg_dot = NULL;
  dsh_dlg_key1 = dsh_dlg_val1 = dsh_dlg_key2 = dsh_dlg_val2 = NULL;
  dsh_dlg_btn_start = dsh_dlg_btn_restart = dsh_dlg_btn_stop = NULL;
  dsh_dlg_mode_container = dsh_dlg_mode_external = NULL;
  dsh_dlg_url_entry = dsh_dlg_btn_connect = dsh_dlg_btn_disconnect = NULL;
  dsh_dlg_error_label = dsh_dlg_ext_state = dsh_dlg_container_buttons = NULL;
  dsh_dlg_state_grid = dsh_dlg_detail_row1 = dsh_dlg_detail_row2 = NULL;
  dsh_dlg_actions_sep = NULL;
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
  gtk_widget_set_size_request(dlg, 440, -1);

  GtkWidget *content = gtk_dialog_get_content_area(GTK_DIALOG(dlg));
  GtkWidget *vbox = gtk_box_new(GTK_ORIENTATION_VERTICAL, 10);
  gtk_widget_set_margin_start(vbox, 18);
  gtk_widget_set_margin_end(vbox, 18);
  gtk_widget_set_margin_top(vbox, 18);
  gtk_widget_set_margin_bottom(vbox, 12);

  // 模式选择行:容器内 / 本机或远端服务(单选按钮组,互斥由 GTK 保证)。
  // 两种模式都显示,便于随时切换。
  GtkWidget *mode_row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 10);
  GtkWidget *mode_label = gtk_label_new("连接模式");
  gtk_style_context_add_class(gtk_widget_get_style_context(mode_label), "dsh-dialog-key");
  dsh_dlg_mode_container = gtk_radio_button_new_with_label(NULL, "容器内");
  dsh_dlg_mode_external = gtk_radio_button_new_with_label_from_widget(
      GTK_RADIO_BUTTON(dsh_dlg_mode_container), "本机/远端服务");
  gtk_toggle_button_set_active(GTK_TOGGLE_BUTTON(dsh_dlg_mode_container), TRUE);
  g_signal_connect(dsh_dlg_mode_container, "toggled", G_CALLBACK(dsh_mode_toggled), NULL);
  g_signal_connect(dsh_dlg_mode_external, "toggled", G_CALLBACK(dsh_mode_toggled), NULL);
  gtk_box_pack_start(GTK_BOX(mode_row), mode_label, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(mode_row), dsh_dlg_mode_container, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(mode_row), dsh_dlg_mode_external, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(vbox), mode_row, FALSE, FALSE, 0);

  // 状态区(容器模式):状态行 + 详情键值行,逐行 pack 进 vbox。
  // 注意:不用 GtkGrid 也不用中间容器——本环境实测 grid 首行会与上一行
  // 重叠错位(状态行丢失),平铺 hbox 最稳定。
  GtkWidget *key_state = gtk_label_new("状态");
  gtk_widget_set_size_request(key_state, 48, -1); // 固定键列宽,右对齐成列
  gtk_widget_set_halign(key_state, GTK_ALIGN_END);
  gtk_style_context_add_class(gtk_widget_get_style_context(key_state), "dsh-dialog-key");
  dsh_dlg_dot = gtk_label_new("●");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_dot), "dsh-state-dot");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_dot), "dsh-state-stopped");
  dsh_dlg_state = gtk_label_new("…");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_state), "dsh-dialog-state");
  gtk_widget_set_halign(dsh_dlg_state, GTK_ALIGN_START);
  GtkWidget *state_row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 6);
  gtk_box_pack_start(GTK_BOX(state_row), key_state, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(state_row), dsh_dlg_dot, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(state_row), dsh_dlg_state, FALSE, FALSE, 0);
  dsh_dlg_state_grid = state_row; // 状态区容器即状态行本身(供显隐切换)
  gtk_box_pack_start(GTK_BOX(vbox), state_row, FALSE, FALSE, 0);

  // 详情两行:地址/PID 或 上次退出,值可选中便于复制
  dsh_dlg_key1 = gtk_label_new("");
  dsh_dlg_val1 = gtk_label_new("");
  dsh_dlg_key2 = gtk_label_new("");
  dsh_dlg_val2 = gtk_label_new("");
  GtkWidget *detail_keys[] = {dsh_dlg_key1, dsh_dlg_key2};
  GtkWidget *detail_vals[] = {dsh_dlg_val1, dsh_dlg_val2};
  for (int i = 0; i < 2; i++) {
    gtk_widget_set_size_request(detail_keys[i], 48, -1); // 与状态键同宽对齐
    gtk_widget_set_halign(detail_keys[i], GTK_ALIGN_END);
    gtk_style_context_add_class(gtk_widget_get_style_context(detail_keys[i]), "dsh-dialog-key");
    gtk_widget_set_halign(detail_vals[i], GTK_ALIGN_START);
    gtk_label_set_selectable(GTK_LABEL(detail_vals[i]), TRUE);
    GtkWidget *row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 14);
    gtk_box_pack_start(GTK_BOX(row), detail_keys[i], FALSE, FALSE, 0);
    gtk_box_pack_start(GTK_BOX(row), detail_vals[i], FALSE, FALSE, 0);
    if (i == 0) {
      dsh_dlg_detail_row1 = row;
    } else {
      dsh_dlg_detail_row2 = row;
    }
    gtk_box_pack_start(GTK_BOX(vbox), row, FALSE, FALSE, 0);
  }

  // 分隔线:容器状态区与外部连接区之间(两种模式都保留)
  GtkWidget *ext_sep = gtk_separator_new(GTK_ORIENTATION_HORIZONTAL);
  gtk_box_pack_start(GTK_BOX(vbox), ext_sep, FALSE, FALSE, 0);

  // 外部模式:URL 输入 + 连接/断开 + 外部状态 + 错误标签
  GtkWidget *ext_row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
  gtk_style_context_add_class(gtk_widget_get_style_context(ext_row), "dsh-dialog-ext-row");
  GtkWidget *ext_label = gtk_label_new("服务地址");
  gtk_style_context_add_class(gtk_widget_get_style_context(ext_label), "dsh-dialog-key");
  dsh_dlg_url_entry = gtk_entry_new();
  gtk_entry_set_placeholder_text(GTK_ENTRY(dsh_dlg_url_entry), "http://127.0.0.1:3456");
  gtk_widget_set_hexpand(dsh_dlg_url_entry, TRUE);
  dsh_dlg_btn_connect = gtk_button_new_with_label("连接");
  dsh_dlg_btn_disconnect = gtk_button_new_with_label("断开");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_btn_connect), "suggested-action");
  g_signal_connect(dsh_dlg_btn_connect, "clicked", G_CALLBACK(dsh_external_connect_clicked), NULL);
  g_signal_connect(dsh_dlg_btn_disconnect, "clicked", G_CALLBACK(dsh_external_disconnect_clicked), NULL);
  gtk_box_pack_start(GTK_BOX(ext_row), ext_label, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(ext_row), dsh_dlg_url_entry, TRUE, TRUE, 0);
  gtk_box_pack_start(GTK_BOX(ext_row), dsh_dlg_btn_connect, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(ext_row), dsh_dlg_btn_disconnect, FALSE, FALSE, 0);
  dsh_dlg_ext_state = gtk_label_new("");
  gtk_widget_set_halign(dsh_dlg_ext_state, GTK_ALIGN_START);
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_ext_state), "dsh-dialog-ext-state");
  dsh_dlg_error_label = gtk_label_new("");
  gtk_widget_set_halign(dsh_dlg_error_label, GTK_ALIGN_START);
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_error_label), "dsh-dialog-error");
  // fill=TRUE 让外部队扩展满整行,URL 输入框(hexpand)随之撑开
  gtk_box_pack_start(GTK_BOX(vbox), ext_row, FALSE, TRUE, 0);
  gtk_box_pack_start(GTK_BOX(vbox), dsh_dlg_ext_state, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(vbox), dsh_dlg_error_label, FALSE, FALSE, 0);

  // 分隔线:外部连接区与容器按钮之间(仅容器模式显示)
  dsh_dlg_actions_sep = gtk_separator_new(GTK_ORIENTATION_HORIZONTAL);
  gtk_box_pack_start(GTK_BOX(vbox), dsh_dlg_actions_sep, FALSE, FALSE, 0);

  // 容器模式按钮行
  dsh_dlg_container_buttons = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
  gtk_widget_set_halign(dsh_dlg_container_buttons, GTK_ALIGN_END);
  dsh_dlg_btn_start = gtk_button_new_with_label("启动");
  dsh_dlg_btn_restart = gtk_button_new_with_label("重启");
  dsh_dlg_btn_stop = gtk_button_new_with_label("停止");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_btn_start), "suggested-action");
  gtk_style_context_add_class(gtk_widget_get_style_context(dsh_dlg_btn_stop), "destructive-action");
  g_signal_connect(dsh_dlg_btn_start, "clicked", G_CALLBACK(dsh_server_start_clicked), NULL);
  g_signal_connect(dsh_dlg_btn_restart, "clicked", G_CALLBACK(dsh_server_restart_clicked), NULL);
  g_signal_connect(dsh_dlg_btn_stop, "clicked", G_CALLBACK(dsh_server_stop_clicked), NULL);
  gtk_box_pack_start(GTK_BOX(dsh_dlg_container_buttons), dsh_dlg_btn_start, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(dsh_dlg_container_buttons), dsh_dlg_btn_restart, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(dsh_dlg_container_buttons), dsh_dlg_btn_stop, FALSE, FALSE, 0);
  gtk_box_pack_start(GTK_BOX(vbox), dsh_dlg_container_buttons, FALSE, FALSE, 0);

  gtk_container_add(GTK_CONTAINER(content), vbox);
  gtk_widget_show_all(dlg);
  return dlg;
}

// ---- 刷新服务器弹框(按模式分支) ----
// 容器模式:显示容器状态区与按钮;外部模式:隐藏两者,外部状态由
// dsh_update_external_dialog 呈现。模式判断以弹框单选按钮为准。
static void dsh_update_server_dialog(GtkWidget *dlg, const char *state_text, int state,
                                     const char *detail,
                                     gboolean can_start, gboolean can_restart, gboolean can_stop) {
  (void)dlg;
  gboolean external = gtk_toggle_button_get_active(GTK_TOGGLE_BUTTON(dsh_dlg_mode_external));
  gtk_widget_set_visible(dsh_dlg_container_buttons, !external);
  gtk_widget_set_visible(dsh_dlg_actions_sep, !external);
  gtk_widget_set_visible(dsh_dlg_state_grid, !external);
  gtk_widget_set_visible(dsh_dlg_detail_row1, !external);
  gtk_widget_set_visible(dsh_dlg_detail_row2, !external);
  if (!external) {
    gtk_label_set_text(GTK_LABEL(dsh_dlg_state), state_text);
    dsh_set_state_class(dsh_dlg_dot, state);
    dsh_update_detail(detail);
    gtk_widget_set_sensitive(dsh_dlg_btn_start, can_start);
    gtk_widget_set_sensitive(dsh_dlg_btn_restart, can_restart);
    gtk_widget_set_sensitive(dsh_dlg_btn_stop, can_stop);
  }
}

// ---- 刷新外部模式状态区与连接按钮(tick 调用) ----
// state_text 为空时隐藏外部状态区(容器模式下不占位);
// connected 用于状态文本着色(已连接绿/未连接灰,与状态栏圆点同色系)。
static void dsh_update_external_dialog(const char *state_text,
                                       gboolean can_connect, gboolean can_disconnect,
                                       gboolean connected) {
  gtk_widget_set_visible(dsh_dlg_ext_state, state_text != NULL && state_text[0] != '\0');
  gtk_label_set_text(GTK_LABEL(dsh_dlg_ext_state), state_text != NULL ? state_text : "");
  gtk_widget_set_sensitive(dsh_dlg_btn_connect, can_connect);
  gtk_widget_set_sensitive(dsh_dlg_btn_disconnect, can_disconnect);
  GtkStyleContext *ctx = gtk_widget_get_style_context(dsh_dlg_ext_state);
  gtk_style_context_remove_class(ctx, "dsh-state-running");
  gtk_style_context_remove_class(ctx, "dsh-state-stopped");
  gtk_style_context_add_class(ctx, connected ? "dsh-state-running" : "dsh-state-stopped");
}

// ---- 外部连接安全确认弹框 ----
// GtkMessageDialog 的正文/按钮都是变参调用,cgo 对非空变参支持有限
// (实测空变参可编译,带参即报 "unexpected type: ..."),故整体放 C 侧。
// URL 只作变参实参传入(经 %s 格式化),不能拼进格式串:URL 里的 % 会被
// 当作格式符解析而破坏文案。url 指向 Go 侧 C.CString,调用期间保持有效,
// 由 Go 侧 confirmExternal 在调用返回后释放,此处不重复分配。
static gboolean dsh_confirm_external(GtkWindow *parent, const char *url) {
  GtkWidget *dlg = gtk_message_dialog_new(
      parent, GTK_DIALOG_MODAL | GTK_DIALOG_DESTROY_WITH_PARENT,
      GTK_MESSAGE_QUESTION, GTK_BUTTONS_NONE, NULL);
  gtk_message_dialog_format_secondary_text(
      GTK_MESSAGE_DIALOG(dlg),
      "将连接远端 harness 服务 %s,其命令在远端机器上执行,API key 等配置将发往该机器。确认连接?",
      url);
  gtk_dialog_add_buttons(GTK_DIALOG(dlg), "_连接", GTK_RESPONSE_YES, "_取消", GTK_RESPONSE_NO, NULL);
  gint resp = gtk_dialog_run(GTK_DIALOG(dlg));
  gtk_widget_destroy(dlg);
  return resp == GTK_RESPONSE_YES;
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
	"time"
	"unsafe"
)

// 包级 UI 状态:单一窗口实例,由 installDesktopUI 初始化,GTK 回调使用。
var (
	activeSupervisor *Supervisor
	mainWindow       *C.GtkWindow
	statusLabel      *C.GtkWidget
	serverDialog     *C.GtkWidget
)

// 弹框子控件指针:Go 侧经 dsh_get_dialog_ptrs 从 C 取回
// (C static 变量无法被 Go 直接引用),弹框销毁时置空。
var (
	dsh_dlg_mode_container *C.GtkWidget
	dsh_dlg_mode_external  *C.GtkWidget
	dsh_dlg_url_entry      *C.GtkWidget
	dsh_dlg_error_label    *C.GtkWidget
)

// 外部连接状态与导航
var (
	connector  *Connector
	navigateFn func(string)
	configPath string
	// 异步探测结果与待导航 URL 不落包级变量:goroutine 用 C.CBytes 写入
	// C 内存,经 gpointer 交给 idle 回调,回调内处理并释放,避免跨线程数据竞争。
	externalBusy bool
)

// externalConfigFilePath 返回外部 URL 配置文件路径;HOME 不可用时回退
// 当前目录 .cache/dsh-desktop/config.json(随工作目录)。
func externalConfigFilePath() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "dsh-desktop", "config.json")
	}
	return filepath.Join(".cache", "dsh-desktop", "config.json")
}

// installDesktopUI 挂载底部状态栏、应用自定义样式、注册 1s 状态轮询、居中窗口
// 并初始化外部连接导航。必须在 w.Run() 之前调用;win 来自 webview.WebView.Window()。
func installDesktopUI(win unsafe.Pointer, sup *Supervisor, navigate func(string)) {
	if win == nil {
		return
	}
	activeSupervisor = sup
	navigateFn = navigate
	connector = NewConnector()
	configPath = externalConfigFilePath()
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

	// 状态栏:外部模式显示外部服务地址,容器模式显示容器状态。
	// 模式判断以 connector.Mode() 为准(唯一模式权威),与弹框单选按钮无关。
	var barText string
	if connector != nil && connector.Mode() == ModeExternal {
		barText = externalStatusBarText(connector)
	} else {
		barText = statusBarText(st)
	}
	bar := C.CString(barText)
	C.dsh_set_status_label((*C.GtkLabel)(unsafe.Pointer(statusLabel)), bar, C.int(st.State))
	C.free(unsafe.Pointer(bar))

	if serverDialog == nil {
		return
	}
	// 容器状态区:可见性与内容由弹框单选按钮决定(C 侧处理)
	d := serverDialogState(st)
	state := C.CString(d.State)
	detail := C.CString(d.Detail)
	C.dsh_update_server_dialog(serverDialog, state, C.int(st.State), detail,
		boolToGboolean(d.CanStart), boolToGboolean(d.CanRestart), boolToGboolean(d.CanStop))
	C.free(unsafe.Pointer(state))
	C.free(unsafe.Pointer(detail))

	// 外部状态区与连接/断开按钮:按模式与 busy 更新
	var extText string
	var canConnect, canDisconnect, connected bool
	if connector != nil && connector.Mode() == ModeExternal {
		ext := externalDialogState(connector, externalBusy)
		extText = ext.State + "\n" + ext.Detail
		canConnect, canDisconnect = ext.CanConnect, ext.CanDisconnect
		connected = true
	} else {
		// 容器模式:连接按钮随时可用(点击后自动切外部),busy 期间禁用;
		// 连接探测中(外部单选已选中)显示占位状态
		canConnect = !externalBusy
		if externalBusy && C.gtk_toggle_button_get_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_external))) != 0 {
			extText = "连接中…"
		}
	}
	cExt := C.CString(extText)
	C.dsh_update_external_dialog(cExt, boolToGboolean(canConnect), boolToGboolean(canDisconnect),
		boolToGboolean(connected))
	C.free(unsafe.Pointer(cExt))
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
	// 取回 Go 回调需要的子控件指针
	var modeContainer, modeExternal, urlEntry, errorLabel *C.GtkWidget
	C.dsh_get_dialog_ptrs(&modeContainer, &modeExternal, &urlEntry, &errorLabel)
	dsh_dlg_mode_container = modeContainer
	dsh_dlg_mode_external = modeExternal
	dsh_dlg_url_entry = urlEntry
	dsh_dlg_error_label = errorLabel
	// 外部模式已连接时,重开弹框把单选按钮同步到外部(connector.Mode() 是唯一权威)
	if connector != nil && connector.Mode() == ModeExternal {
		C.gtk_toggle_button_set_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_external)), 1)
	}
	// 填充上次连接的 URL:优先运行期记忆,其次配置文件(不自动重连)
	if connector != nil {
		u := connector.ExternalURL()
		if u == "" {
			u = loadExternalURL(configPath)
		}
		if u != "" {
			c := C.CString(u)
			C.gtk_entry_set_text((*C.GtkEntry)(unsafe.Pointer(dsh_dlg_url_entry)), c)
			C.free(unsafe.Pointer(c))
		}
	}
	dshRefreshStatus()
}

//export dshOnServerDialogDestroyed
func dshOnServerDialogDestroyed() {
	serverDialog = nil
	dsh_dlg_mode_container = nil
	dsh_dlg_mode_external = nil
	dsh_dlg_url_entry = nil
	dsh_dlg_error_label = nil
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

//export dshOnModeChanged
func dshOnModeChanged() {
	if dsh_dlg_mode_external == nil || dsh_dlg_mode_container == nil {
		return
	}
	// 单选按钮必须始终镜像 connector.Mode()(唯一模式依据):外部服务已连接时
	// 忽略用户切回容器内的操作,强制回外部,防止弹框显示容器模式而 webview
	// 仍指向外部服务(容器按钮会因此错误可用)。
	if connector != nil && connector.Mode() == ModeExternal {
		C.gtk_toggle_button_set_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_external)), 1)
		if serverDialog != nil {
			dshRefreshStatus()
		}
		return
	}
	// 容器模式:单选按钮组互斥由 GTK 保证,这里做防御性同步并刷新弹框
	external := C.gtk_toggle_button_get_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_external))) != 0
	if external {
		C.gtk_toggle_button_set_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_container)), 0)
	} else {
		C.gtk_toggle_button_set_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_external)), 0)
	}
	if serverDialog != nil {
		dshRefreshStatus()
	}
}

//export dshOnExternalConnect
func dshOnExternalConnect() {
	if connector == nil || navigateFn == nil || activeSupervisor == nil {
		return
	}
	raw := C.GoString(C.gtk_entry_get_text((*C.GtkEntry)(unsafe.Pointer(dsh_dlg_url_entry))))
	u, err := connector.ValidateURL(raw)
	if err != nil {
		setDialogError("地址无效: " + err.Error())
		return
	}
	if connector.NeedConfirmation(u) {
		if !confirmExternal(u) { // GtkMessageDialog 是/否
			return
		}
		connector.ConfirmHost(u)
	}
	// 连接前先把单选按钮切到外部模式,保证弹框 UI 与连接状态一致
	if dsh_dlg_mode_external != nil {
		C.gtk_toggle_button_set_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_external)), 1)
	}
	// 连接前先停容器 harness(释放端口、暂停自动重启),避免端口冲突;
	// BeginExternal 在 goroutine 执行(内含 ≤3s 探测,经 g_idle_add 回主线程),
	// 不阻塞 GTK 主线程,也不重复探测。
	activeSupervisor.StopHarness()
	externalBusy = true
	dshRefreshStatus()
	go func() {
		err := connector.BeginExternal(u)
		// 探测结果打包进 C 内存(url + NUL + errmsg + NUL)经 gpointer 交给
		// idle 回调:不写包级变量,避免 goroutine 与 GTK 主线程的数据竞争
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		buf := C.CBytes([]byte(u + "\x00" + errMsg + "\x00"))
		C.dsh_schedule_probe_result(C.gpointer(buf))
	}()
}

//export dshOnProbeResult
func dshOnProbeResult(data unsafe.Pointer) {
	// 载荷布局:url\0errmsg\0 两个连续 C 字符串;errmsg 为空表示探测成功。
	// 内存由 C 侧 idle 回调在处理后 free,此处只读。
	u := C.GoString((*C.char)(data))
	urlLen := C.strlen((*C.char)(data)) + 1
	errMsg := C.GoString((*C.char)(unsafe.Add(data, uintptr(urlLen))))
	externalBusy = false
	if errMsg != "" {
		// 探测失败:恢复容器模式(重启容器 harness),弹框内错误提示;
		// 单选按钮回容器内,与 connector.Mode()(ModeContainer)保持一致
		setDialogError("连接失败: " + errMsg)
		if serverDialog != nil && dsh_dlg_mode_container != nil {
			C.gtk_toggle_button_set_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_container)), 1)
		}
		activeSupervisor.Restart()
		dshRefreshStatus()
		return
	}
	// 探测成功:记忆 URL、清错误、导航到外部服务
	_ = saveExternalURL(configPath, u)
	setDialogError("")
	// 单选按钮强制回外部:探测期间用户可能已切回容器内,而连接已建立;
	// 单选必须始终镜像 connector.Mode()(唯一模式依据),禁止显示容器模式
	if serverDialog != nil && dsh_dlg_mode_external != nil {
		C.gtk_toggle_button_set_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_external)), 1)
	}
	if dsh_dlg_url_entry != nil {
		cu := C.CString(u)
		C.gtk_entry_set_text((*C.GtkEntry)(unsafe.Pointer(dsh_dlg_url_entry)), cu)
		C.free(unsafe.Pointer(cu))
	}
	navigateFn(u)
	dshRefreshStatus()
}

//export dshOnExternalDisconnect
func dshOnExternalDisconnect() {
	if connector == nil || activeSupervisor == nil || navigateFn == nil {
		return
	}
	connector.EndExternal()
	// 单选按钮回容器内,弹框 UI 立即回到容器模式
	if serverDialog != nil && dsh_dlg_mode_container != nil {
		C.gtk_toggle_button_set_active((*C.GtkToggleButton)(unsafe.Pointer(dsh_dlg_mode_container)), 1)
	}
	externalBusy = true
	dshRefreshStatus()
	// 重启容器 harness,等就绪后导航回(异步,有界 30s);
	// 待导航 URL 复制进 C 内存经 gpointer 交回 idle 回调,不落包级变量
	go func() {
		activeSupervisor.Restart()
		navURL := ""
		select {
		case u := <-activeSupervisor.Ready():
			navURL = u
		case <-time.After(30 * time.Second):
			navURL = ""
		}
		buf := C.CBytes([]byte(navURL + "\x00"))
		C.dsh_schedule_nav_idle(C.gpointer(buf))
	}()
}

//export dshOnNavIdle
func dshOnNavIdle(data unsafe.Pointer) {
	// 载荷为单个 NUL 结尾 C 字符串(可能为空串);内存由 C 侧 idle 回调释放。
	u := C.GoString((*C.char)(data))
	externalBusy = false
	if u != "" {
		navigateFn(u)
	}
	dshRefreshStatus()
}

// confirmExternal 弹确认框;返回用户是否确认。
// 变参 GTK 调用在 C 侧实现(见 dsh_confirm_external),Go 只传 URL 并判断结果。
func confirmExternal(u string) bool {
	cu := C.CString(u)
	defer C.free(unsafe.Pointer(cu))
	return C.dsh_confirm_external(mainWindow, cu) != 0
}

// setDialogError 设置弹框错误标签文本。
func setDialogError(text string) {
	if serverDialog == nil || dsh_dlg_error_label == nil {
		return
	}
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	C.gtk_label_set_text((*C.GtkLabel)(unsafe.Pointer(dsh_dlg_error_label)), c)
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

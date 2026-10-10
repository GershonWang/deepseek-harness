/*
 * dsh-about：随包的「关于」对话框助手。
 *
 * 为什么单独一个进程而不是让切换器自己画：切换器是 CGO_ENABLED=0 的纯 Go 进程，不带
 * 窗口工具链，而「关于」需要一个能正常显示中文的窗口。这里复用容器里本来就有的 GTK3
 * 运行时（薄壳客户端依赖它），由打包脚本在宿主机上编译成独立二进制。
 *
 * 本程序只负责画窗口：产品名、版本号、附加信息与图标路径全部由命令行传入，语言由切换器
 * （internal/i18n）决定，这里不内置任何面向用户的文案。
 *
 * 退出码：0 正常关闭；2 参数解析失败。
 */
#include <gtk/gtk.h>

/* 命令行参数；未提供时保持 NULL。 */
static gchar *option_program = NULL;
static gchar *option_version = NULL;
static gchar *option_comments = NULL;
static gchar *option_logo = NULL;

/* 把空指针收敛成空串：GTK 的属性是字符串，NULL 会让对话框显示 "(null)"。 */
static const gchar *text_or_empty(const gchar *value) {
    return value != NULL ? value : "";
}

/* 按给定尺寸把窗口摆到所在显示器工作区的正中。
   为什么不能只靠 gtk_window_set_position：位置提示只是给窗口管理器的建议，实测 DDE
   （kwin）按自己的策略摆放，弹框落在偏左上约 (175, 67) 的位置。 */
static void center_with_size(GtkWindow *window, gint width, gint height) {
    GdkDisplay *display = gtk_widget_get_display(GTK_WIDGET(window));
    GdkMonitor *monitor = gdk_display_get_primary_monitor(display);
    if (monitor == NULL) {
        monitor = gdk_display_get_monitor(display, 0);
    }
    if (monitor == NULL) {
        return;
    }
    GdkRectangle area;
    gdk_monitor_get_workarea(monitor, &area);
    gtk_window_move(window,
                    area.x + (area.width - width) / 2,
                    area.y + (area.height - height) / 2);
}

/* 尺寸分配完成后摆正。
   两个要点：一是不能只在显示后摆一次，窗口显示瞬间的尺寸还不是最终值；二是这里必须用
   分配到的尺寸而不是 gtk_window_get_size——回调里后者仍是旧值（实测比最终高度小 80
   像素），照它居中会往下溢出。gtk_window_move 只改位置、不触发本回调，因此不会自激。 */
static void on_size_allocate(GtkWidget *widget, GdkRectangle *allocation, gpointer data) {
    (void)data;
    center_with_size(GTK_WINDOW(widget), allocation->width, allocation->height);
}

int main(int argc, char **argv) {
    static GOptionEntry entries[] = {
        {"program-name", 0, 0, G_OPTION_ARG_STRING, &option_program, "Product name", NULL},
        {"version", 0, 0, G_OPTION_ARG_STRING, &option_version, "Package version", NULL},
        {"comments", 0, 0, G_OPTION_ARG_STRING, &option_comments, "Localized extra information", NULL},
        {"logo", 0, 0, G_OPTION_ARG_STRING, &option_logo, "Path to the product icon", NULL},
        {NULL}};

    GOptionContext *context = g_option_context_new(NULL);
    g_option_context_add_main_entries(context, entries, NULL);
    /* gtk_get_option_group(TRUE) 同时完成 gtk_init，因此后面不再单独调用它。 */
    g_option_context_add_group(context, gtk_get_option_group(TRUE));
    GError *error = NULL;
    if (!g_option_context_parse(context, &argc, &argv, &error)) {
        g_printerr("dsh-about: %s\n", error->message);
        g_error_free(error);
        g_option_context_free(context);
        return 2;
    }
    g_option_context_free(context);

    GtkWidget *dialog = gtk_about_dialog_new();
    gtk_window_set_title(GTK_WINDOW(dialog), text_or_empty(option_program));
    g_object_set(dialog,
                 "program-name", text_or_empty(option_program),
                 "version", text_or_empty(option_version),
                 "comments", text_or_empty(option_comments),
                 "modal", TRUE,
                 NULL);
    /* 图标按路径加载：随包图标不在容器的图标主题里，按名字查不到。文件缺失只少一张图，
       对话框本身照常显示，因此这里不把它当成失败。 */
    if (option_logo != NULL) {
        GError *logo_error = NULL;
        GdkPixbuf *logo = gdk_pixbuf_new_from_file(option_logo, &logo_error);
        if (logo != NULL) {
            gtk_about_dialog_set_logo(GTK_ABOUT_DIALOG(dialog), logo);
            g_object_unref(logo);
        } else {
            g_printerr("dsh-about: %s\n", logo_error->message);
            g_error_free(logo_error);
        }
    }
    /* 标准收尾：响应（关闭按钮、Esc）先销毁对话框，销毁再退出主循环。 */
    g_signal_connect_swapped(dialog, "response", G_CALLBACK(gtk_widget_destroy), dialog);
    g_signal_connect(dialog, "destroy", G_CALLBACK(gtk_main_quit), NULL);
    /* 位置提示先给一次，真正的居中在每次尺寸分配后按工作区摆。 */
    gtk_window_set_position(GTK_WINDOW(dialog), GTK_WIN_POS_CENTER);
    g_signal_connect(dialog, "size-allocate", G_CALLBACK(on_size_allocate), NULL);
    gtk_widget_show(dialog);
    gtk_main();
    return 0;
}

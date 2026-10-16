/*
 * dsh-about：随包的「关于」对话框助手。
 *
 * 为什么单独一个进程而不是让切换器自己画：切换器是 CGO_ENABLED=0 的纯 Go 进程，不带
 * 窗口工具链，而「关于」需要一个能正常显示中文的窗口。这里复用容器里本来就有的 GTK3
 * 运行时（薄壳客户端依赖它），由打包脚本在宿主机上编译成独立二进制。
 *
 * 为什么不用 GtkAboutDialog：它只有一个 website 字段，而「关于」要同时给出上游 DSH 与
 * 玲珑封装两个仓库地址，且都必须是可点击链接。自绘窗口用 GtkLinkButton 逐行摆放即可。
 *
 * 本程序只负责画窗口：窗口标题、产品名、版本号、居中状态行、事实行、按钮文案、图标与
 * 打开器路径全部由命令行传入，语言由切换器（internal/i18n）决定，这里不内置任何面向用户
 * 的文案。
 *
 * 两条内容约定，都由调用方在命令行上声明，助手不需要知道哪一行是什么：值以 http:// 或
 * https:// 开头时渲染成可点击链接；值出现在 --copy 列表里时，该行值右侧附一个一键复制
 * 按钮。其余值是普通文本，刻意不做成可选中文本——可选中标签获得焦点时会选中整行，用户
 * 打开弹框第一眼就是一段高亮（真机实测）；需要复制的值改用复制按钮，既不引入选中态，也
 * 不要求用户精确拖选。
 *
 * 退出码：0 正常关闭；2 参数解析失败。
 */
#include <atk/atk.h>
#include <gtk/gtk.h>

/* 复制按钮优先使用的主题图标名；容器里的图标主题不保证齐全，没有它时退回文案按钮。 */
#define COPY_ICON_NAME "edit-copy"
/* 复制成功后按钮显示反馈的时长（毫秒）：短到不打扰，长到看得见。 */
#define COPIED_FEEDBACK_MS 1500

/* 命令行参数；未提供时保持 NULL。 */
static gchar *option_title = NULL;
static gchar *option_program = NULL;
static gchar *option_version = NULL;
static gchar *option_close_label = NULL;
static gchar *option_logo = NULL;
static gchar *option_uri_opener = NULL;
static gchar **option_fields = NULL;
static gchar **option_center_fields = NULL;
/* --copy 点名的值（可重复）：这些行的值右侧附一个一键复制按钮。 */
static gchar **option_copy_values = NULL;
static gchar *option_copy_label = NULL;
static gchar *option_copied_label = NULL;

/* 链接点击需要的上下文：打开器路径与窗口（回退给 GTK 默认处理器时要它）。 */
typedef struct {
    const gchar *opener;
    GtkWindow *window;
} LinkContext;

/* 把空指针收敛成空串：GTK 的属性是字符串，NULL 会让标签显示 "(null)"。 */
static const gchar *text_or_empty(const gchar *value) {
    return value != NULL ? value : "";
}

/* 判断字段值是不是 http(s) 链接：与 Go 侧的约定一致，只认这两种 scheme。 */
static gboolean is_http_uri(const gchar *value) {
    return g_str_has_prefix(value, "http://") || g_str_has_prefix(value, "https://");
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

/* 链接点击：先走随包的真实 xdg-open（与壳内 GUI 外链同一条已在真机验证的路径），失败
   再退回 GTK/GIO 的默认处理器。返回 TRUE 表示这次点击已经处理完，不再走 GTK 默认实现，
   否则 GTK 会自己再打开一次。 */
static gboolean on_activate_link(GtkLinkButton *button, gpointer data) {
    LinkContext *context = (LinkContext *)data;
    const gchar *uri = gtk_link_button_get_uri(button);
    if (uri == NULL) {
        return TRUE;
    }
    if (context != NULL && context->opener != NULL && context->opener[0] != '\0') {
        gchar *argv[] = {(gchar *)context->opener, (gchar *)uri, NULL};
        GError *spawn_error = NULL;
        if (g_spawn_async(NULL, argv, NULL, G_SPAWN_SEARCH_PATH, NULL, NULL, NULL, &spawn_error)) {
            return TRUE;
        }
        g_printerr("dsh-about: %s\n", spawn_error->message);
        g_error_free(spawn_error);
    }
    if (context != NULL && context->window != NULL) {
        GError *uri_error = NULL;
        if (!gtk_show_uri_on_window(context->window, uri, GDK_CURRENT_TIME, &uri_error) && uri_error != NULL) {
            g_printerr("dsh-about: %s\n", uri_error->message);
            g_error_free(uri_error);
        }
    }
    return TRUE;
}

/* 判断值是否被 --copy 点名：精确比对即可，因为调用方标记的正是它自己传进来的同一个值。 */
static gboolean is_copy_marked(const gchar *value) {
    for (gint i = 0; option_copy_values != NULL && option_copy_values[i] != NULL; i++) {
        if (g_strcmp0(option_copy_values[i], value) == 0) {
            return TRUE;
        }
    }
    return FALSE;
}

/* 复制按钮的运行时状态：按钮、要复制的值、两种文案与反馈计时器。
   单独分配并由按钮的 destroy 释放，因为它被反馈计时器异步引用。 */
typedef struct {
    GtkWidget *button;
    /* value 指向命令行参数里的字符串，生命周期与进程相同，因此按借用指针保存。 */
    const gchar *value;
    const gchar *label;
    const gchar *copied_label;
    gboolean shows_icon;
    guint feedback_timer;
} CopyButton;

/* 反馈结束：回到普通状态。图标按钮清掉临时文案即回到图标，文案按钮换回原文案。 */
static gboolean restore_copy_button(gpointer data) {
    CopyButton *state = (CopyButton *)data;
    state->feedback_timer = 0;
    if (state->shows_icon) {
        gtk_button_set_label(GTK_BUTTON(state->button), NULL);
    } else {
        gtk_button_set_label(GTK_BUTTON(state->button), state->label);
    }
    return G_SOURCE_REMOVE;
}

/* 按钮销毁时先撤掉可能还在跑的反馈计时器，再释放状态：计时器回调持有这个指针，弹框在
   反馈期间被关掉时不能让它落到已释放的内存上。 */
static void on_copy_button_destroy(GtkWidget *widget, gpointer data) {
    (void)widget;
    CopyButton *state = (CopyButton *)data;
    if (state->feedback_timer != 0) {
        g_source_remove(state->feedback_timer);
        state->feedback_timer = 0;
    }
    g_free(state);
}

/* 点击复制：写入 CLIPBOARD 后交给剪贴板管理器保存。弹框进程退出后内容必须还在，否则
   用户关掉弹框再粘贴只会得到空——这正是「一键复制」要解决的场景。 */
static void on_copy_clicked(GtkButton *button, gpointer data) {
    (void)button;
    CopyButton *state = (CopyButton *)data;
    GtkClipboard *clipboard = gtk_clipboard_get(GDK_SELECTION_CLIPBOARD);
    gtk_clipboard_set_text(clipboard, state->value, -1);
    gtk_clipboard_store(clipboard);
    /* 反馈文案可能为空（调用方只想靠图标变化提示），此时不做任何视觉切换。 */
    if (state->copied_label == NULL || state->copied_label[0] == '\0') {
        return;
    }
    gtk_button_set_label(GTK_BUTTON(state->button), state->copied_label);
    if (state->feedback_timer != 0) {
        g_source_remove(state->feedback_timer);
    }
    state->feedback_timer = g_timeout_add(COPIED_FEEDBACK_MS, restore_copy_button, state);
}

/* 生成一键复制按钮：优先用主题图标（扁平按钮，视觉上不与事实文本争注意力），主题里没有
   这个图标时退回文案按钮——显示一个「复制」也远好过显示一个破图。图标按钮没有可见文字，
   因此提示与无障碍名都必须由调用方给出，否则这个按钮对读屏用户是空白。 */
static GtkWidget *make_copy_button(const gchar *value, const gchar *label, const gchar *copied_label) {
    GtkWidget *button = gtk_button_new();
    CopyButton *state = g_new0(CopyButton, 1);
    state->button = button;
    state->value = value;
    state->label = label;
    state->copied_label = copied_label;

    GtkIconTheme *theme = gtk_icon_theme_get_for_screen(gtk_widget_get_screen(button));
    if (theme != NULL && gtk_icon_theme_has_icon(theme, COPY_ICON_NAME)) {
        state->shows_icon = TRUE;
        gtk_button_set_image(GTK_BUTTON(button),
                             gtk_image_new_from_icon_name(COPY_ICON_NAME, GTK_ICON_SIZE_BUTTON));
    } else {
        gtk_button_set_label(GTK_BUTTON(button), text_or_empty(label));
    }

    gtk_widget_set_halign(button, GTK_ALIGN_CENTER);
    gtk_widget_set_valign(button, GTK_ALIGN_CENTER);
    gtk_style_context_add_class(gtk_widget_get_style_context(button), "flat");
    gtk_widget_set_tooltip_text(button, text_or_empty(label));
    atk_object_set_name(gtk_widget_get_accessible(button), text_or_empty(label));
    g_signal_connect(button, "clicked", G_CALLBACK(on_copy_clicked), state);
    g_signal_connect(button, "destroy", G_CALLBACK(on_copy_button_destroy), state);
    return button;
}

/* 字段行：标签左列弱化显示，值右列按约定给出链接或普通文本，被 --copy 点名的值再补一个
   一键复制按钮。值不做成可选中标签：可选中标签会抢初始焦点并选中整行。
   长 URL 用 xalign 0 + 换行兜底：容器里没有横向滚动，值必须能在窄窗口里断行。 */
static void add_field(GtkGrid *grid, gint row, const gchar *label, const gchar *value,
                      GtkWindow *window, const gchar *opener, const gchar *copy_label,
                      const gchar *copied_label) {
    GtkWidget *name = gtk_label_new(text_or_empty(label));
    gtk_widget_set_halign(name, GTK_ALIGN_START);
    gtk_widget_set_valign(name, GTK_ALIGN_START);
    gtk_style_context_add_class(gtk_widget_get_style_context(name), "dim-label");
    gtk_grid_attach(grid, name, 0, row, 1, 1);

    GtkWidget *entry;
    if (is_http_uri(value)) {
        LinkContext *context = g_new0(LinkContext, 1);
        context->opener = opener;
        context->window = window;
        entry = gtk_link_button_new_with_label(value, value);
        gtk_widget_set_halign(entry, GTK_ALIGN_START);
        g_signal_connect_data(GTK_LINK_BUTTON(entry), "activate-link",
                              G_CALLBACK(on_activate_link), context,
                              (GClosureNotify)g_free, 0);
    } else {
        entry = gtk_label_new(text_or_empty(value));
        gtk_label_set_line_wrap(GTK_LABEL(entry), TRUE);
        gtk_label_set_xalign(GTK_LABEL(entry), 0.0f);
        gtk_widget_set_halign(entry, GTK_ALIGN_START);
    }
    gtk_grid_attach(grid, entry, 1, row, 1, 1);

    /* 复制按钮占第三列；没有它的行留空，因此不会把网格拉宽。 */
    if (is_copy_marked(value)) {
        gtk_grid_attach(grid, make_copy_button(value, copy_label, copied_label), 2, row, 1, 1);
    }
}

/* 居中行：分隔线上方的单行状态文本（形如「当前形态: 薄壳版」），整行水平居中。
   为什么合成一行而不是复用两列网格：这两行是状态摘要，不是可对照的「字段—值」表；
   居中的整行读起来是一句话，也不会把下方事实网格的标签列拉宽。
   文本保持普通标签：可选中标签会抢初始焦点并让整行呈选中态，那是用户明确不要的。 */
static void add_centered_line(GtkBox *box, const gchar *label, const gchar *value,
                              const gchar *copy_label, const gchar *copied_label) {
    gboolean has_label = label != NULL && label[0] != '\0';
    /* 没有标签时只显示值，不留一个孤零零的分隔冒号。 */
    gchar *text = has_label ? g_strconcat(label, ": ", text_or_empty(value), NULL)
                            : g_strdup(text_or_empty(value));
    GtkWidget *line = gtk_label_new(text);
    g_free(text);
    gtk_widget_set_halign(line, GTK_ALIGN_CENTER);
    if (!is_copy_marked(value)) {
        gtk_box_pack_start(box, line, FALSE, FALSE, 0);
        return;
    }
    /* 被点名的值：文本与复制按钮组成一个水平居中组，行整体仍然居中。 */
    GtkWidget *row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 6);
    gtk_widget_set_halign(row, GTK_ALIGN_CENTER);
    gtk_box_pack_start(GTK_BOX(row), line, FALSE, FALSE, 0);
    gtk_box_pack_start(GTK_BOX(row), make_copy_button(value, copy_label, copied_label), FALSE, FALSE, 0);
    gtk_box_pack_start(box, row, FALSE, FALSE, 0);
}

/* Esc 关窗：与标题栏关闭按钮等价，避免用户只能去点标题栏。 */
static gboolean on_key_press(GtkWidget *widget, GdkEventKey *event, gpointer data) {
    (void)widget;
    (void)data;
    if (event->keyval == GDK_KEY_Escape) {
        gtk_main_quit();
        return TRUE;
    }
    return FALSE;
}

int main(int argc, char **argv) {
    static GOptionEntry entries[] = {
        {"title", 0, 0, G_OPTION_ARG_STRING, &option_title, "Window title", NULL},
        {"program-name", 0, 0, G_OPTION_ARG_STRING, &option_program, "Product name", NULL},
        {"version", 0, 0, G_OPTION_ARG_STRING, &option_version, "Package version", NULL},
        {"center-field", 0, 0, G_OPTION_ARG_STRING_ARRAY, &option_center_fields, "Centered line as LABEL=VALUE (repeatable)", NULL},
        {"field", 0, 0, G_OPTION_ARG_STRING_ARRAY, &option_fields, "Field as LABEL=VALUE (repeatable)", NULL},
        {"copy", 0, 0, G_OPTION_ARG_STRING_ARRAY, &option_copy_values, "Value that gets a one-click copy button (repeatable)", NULL},
        {"copy-label", 0, 0, G_OPTION_ARG_STRING, &option_copy_label, "Copy button label, tooltip and accessible name", NULL},
        {"copied-label", 0, 0, G_OPTION_ARG_STRING, &option_copied_label, "Label shown briefly after copying", NULL},
        {"close-label", 0, 0, G_OPTION_ARG_STRING, &option_close_label, "Close button label", NULL},
        {"logo", 0, 0, G_OPTION_ARG_STRING, &option_logo, "Path to the product icon", NULL},
        {"uri-opener", 0, 0, G_OPTION_ARG_STRING, &option_uri_opener, "Path to xdg-open", NULL},
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

    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    /* 窗口标题用调用方给的标题（切换器传本地化的「关于」）；没给时退回产品名。 */
    gtk_window_set_title(GTK_WINDOW(window),
                         option_title != NULL && option_title[0] != '\0'
                             ? option_title
                             : text_or_empty(option_program));
    gtk_window_set_resizable(GTK_WINDOW(window), FALSE);
    gtk_container_set_border_width(GTK_CONTAINER(window), 18);

    GtkWidget *box = gtk_box_new(GTK_ORIENTATION_VERTICAL, 10);
    gtk_container_add(GTK_CONTAINER(window), box);

    /* 图标按路径加载：随包图标不在容器的图标主题里，按名字查不到。文件缺失只少一张图，
       对话框本身照常显示，因此这里不把它当成失败。 */
    if (option_logo != NULL) {
        GError *logo_error = NULL;
        GdkPixbuf *pixbuf = gdk_pixbuf_new_from_file_at_scale(option_logo, 64, 64, TRUE, &logo_error);
        if (pixbuf != NULL) {
            GtkWidget *image = gtk_image_new_from_pixbuf(pixbuf);
            gtk_widget_set_halign(image, GTK_ALIGN_CENTER);
            gtk_box_pack_start(GTK_BOX(box), image, FALSE, FALSE, 0);
            g_object_unref(pixbuf);
        } else {
            g_printerr("dsh-about: %s\n", logo_error->message);
            g_error_free(logo_error);
        }
    }

    GtkWidget *title = gtk_label_new(text_or_empty(option_program));
    gtk_widget_set_halign(title, GTK_ALIGN_CENTER);
    gtk_style_context_add_class(gtk_widget_get_style_context(title), "title");
    gtk_box_pack_start(GTK_BOX(box), title, FALSE, FALSE, 0);

    /* 版本行：玲珑包版本（切换器侧的 --version）。DSH 版本是下面的字段行之一。 */
    if (option_version != NULL && option_version[0] != '\0') {
        GtkWidget *version = gtk_label_new(option_version);
        gtk_widget_set_halign(version, GTK_ALIGN_CENTER);
        gtk_style_context_add_class(gtk_widget_get_style_context(version), "dim-label");
        gtk_box_pack_start(GTK_BOX(box), version, FALSE, FALSE, 0);
    }

    /* 居中状态行（当前形态、包 ID）：在分隔线上方、整行水平居中。 */
    for (gint i = 0; option_center_fields != NULL && option_center_fields[i] != NULL; i++) {
        gchar *separator = g_strstr_len(option_center_fields[i], -1, "=");
        if (separator == NULL) {
            add_centered_line(GTK_BOX(box), "", option_center_fields[i],
                              option_copy_label, option_copied_label);
            continue;
        }
        gchar *label = g_strndup(option_center_fields[i], (gsize)(separator - option_center_fields[i]));
        add_centered_line(GTK_BOX(box), label, separator + 1, option_copy_label, option_copied_label);
        g_free(label);
    }

    gtk_box_pack_start(GTK_BOX(box), gtk_separator_new(GTK_ORIENTATION_HORIZONTAL), FALSE, FALSE, 4);

    GtkWidget *grid = gtk_grid_new();
    gtk_grid_set_row_spacing(GTK_GRID(grid), 6);
    gtk_grid_set_column_spacing(GTK_GRID(grid), 14);
    gtk_widget_set_halign(grid, GTK_ALIGN_FILL);
    gtk_box_pack_start(GTK_BOX(box), grid, FALSE, FALSE, 0);

    for (gint i = 0; option_fields != NULL && option_fields[i] != NULL; i++) {
        gchar *separator = g_strstr_len(option_fields[i], -1, "=");
        if (separator == NULL) {
            /* 没有分隔符的行无法拆成标签与值：整行当值，标签留空，绝不让它拖垮弹框。 */
            add_field(GTK_GRID(grid), i, "", option_fields[i], GTK_WINDOW(window), option_uri_opener,
                      option_copy_label, option_copied_label);
            continue;
        }
        gchar *label = g_strndup(option_fields[i], (gsize)(separator - option_fields[i]));
        add_field(GTK_GRID(grid), i, label, separator + 1, GTK_WINDOW(window), option_uri_opener,
                  option_copy_label, option_copied_label);
        g_free(label);
    }

    GtkWidget *close = NULL;
    if (option_close_label != NULL && option_close_label[0] != '\0') {
        close = gtk_button_new_with_label(option_close_label);
        gtk_widget_set_halign(close, GTK_ALIGN_CENTER);
        g_signal_connect_swapped(close, "clicked", G_CALLBACK(gtk_main_quit), NULL);
        gtk_box_pack_start(GTK_BOX(box), close, FALSE, FALSE, 4);
    }

    g_signal_connect(window, "destroy", G_CALLBACK(gtk_main_quit), NULL);
    g_signal_connect(window, "key-press-event", G_CALLBACK(on_key_press), NULL);
    /* 位置提示先给一次，真正的居中在每次尺寸分配后按工作区摆。 */
    gtk_window_set_position(GTK_WINDOW(window), GTK_WIN_POS_CENTER);
    g_signal_connect(window, "size-allocate", G_CALLBACK(on_size_allocate), NULL);
    /* 初始焦点显式指定给关闭按钮：窗口获得焦点时 GTK 会自己挑第一个可聚焦控件，弹框里
       除关闭按钮外还有复制按钮，不指定就会把焦点框画在复制按钮上。gtk_window_set_focus
       在窗口显示前记录目标控件，由 GTK 在 map 时应用；显示之后再 grab 一次是加固——
       未 map 时的 grab_focus 是否生效取决于 GTK 版本。 */
    if (close != NULL) {
        gtk_window_set_focus(GTK_WINDOW(window), close);
    }
    gtk_widget_show_all(window);
    if (close != NULL) {
        gtk_widget_grab_focus(close);
    }
    gtk_main();
    return 0;
}

/*
 * dsh-about：随包的「关于」对话框助手。
 *
 * 为什么单独一个进程而不是让切换器自己画：切换器是 CGO_ENABLED=0 的纯 Go 进程，不带
 * 窗口工具链，而「关于」需要一个能正常显示中文的窗口。这里复用容器里本来就有的 GTK3
 * 运行时（薄壳客户端依赖它），由打包脚本在宿主机上编译成独立二进制。
 *
 * 为什么不用 GtkAboutDialog：它只有一个 website 字段，而「关于」要同时给出上游 DSH 与
 * 玲珑封装两个仓库地址，且都必须是可点击链接。自绘窗口用带链接标记的 GtkLabel 逐行摆放即可。
 *
 * 本程序只负责画窗口：窗口标题、产品名、版本号、居中状态行、事实行、复制按钮文案、打开
 * 失败提示、图标与打开器路径全部由命令行传入，语言由切换器（internal/i18n）决定，这里不
 * 内置任何面向用户的文案。
 *
 * 两条内容约定，都由调用方在命令行上声明，助手不需要知道哪一行是什么：值以 http:// 或
 * https:// 开头时渲染成可点击链接；值出现在 --copy 列表里时，该行值右侧附一个一键复制
 * 按钮。其余值是普通文本，刻意不做成可选中文本——可选中标签获得焦点时会选中整行，用户
 * 打开弹框第一眼就是一段高亮（真机实测）；需要复制的值改用复制按钮，既不引入选中态，也
 * 不要求用户精确拖选。
 *
 * 打开链接按宿主 portal、随包 xdg-open、GTK 默认处理器的顺序尝试，理由见 open_uri 的注释；
 * 一旦成功就关闭弹框，三步都失败则保留弹框并就地写明原因，绝不静默。
 *
 * 退出码：0 正常关闭；2 参数解析失败。
 */
#include <atk/atk.h>
#include <gtk/gtk.h>
#include <string.h>
#include <sys/wait.h>

/* 复制按钮优先使用的主题图标名；容器里的图标主题不保证齐全，没有它时退回文案按钮。 */
#define COPY_ICON_NAME "edit-copy"
/* 复制成功后按钮显示反馈的时长（毫秒）：短到不打扰，长到看得见。 */
#define COPIED_FEEDBACK_MS 1500
/* 宿主 portal 的会话总线名字、对象路径与接口（xdg-desktop-portal 的标准位置）。 */
#define PORTAL_BUS_NAME "org.freedesktop.portal.Desktop"
#define PORTAL_OBJECT_PATH "/org/freedesktop/portal/desktop"
#define PORTAL_OPEN_URI_INTERFACE "org.freedesktop.portal.OpenURI"
/* portal 调用超时：它只负责收下请求并立刻回一个句柄（正常在毫秒级），超时只会在会话总线
   出问题时出现；给 3 秒是为了不让一次点击把界面卡住太久。 */
#define PORTAL_CALL_TIMEOUT_MS 3000

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
static gchar *option_open_failed_label = NULL;

/* 对话框的共享上下文：窗口、内容容器、由调用方传入的文案，以及打开链接失败时使用的
   那一行。这些值在窗口生命周期内不变，集中传递比逐个参数更不容易漂移。
   failure_line 是懒创建的：没有失败就不占一行。 */
typedef struct {
    GtkWindow *window;
    GtkBox *box;
    GtkBox *failure_slot;
    GtkWidget *failure_line;
    const gchar *opener;
    const gchar *copy_label;
    const gchar *copied_label;
    const gchar *open_failed_label;
} DialogContext;

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

/* 记录（并替换）最后一次失败原因：多步尝试里只有最后一条对用户有意义。 */
static void set_reason(gchar **reason, gchar *detail) {
    g_free(*reason);
    *reason = detail;
}

/* 取输出里的第一行非空内容：外部工具把真正的原因写在自己输出的第一行（例如 xdg-open 的
   "no method available"），转述不如原样带出来准确。 */
static gchar *first_nonempty_line(const gchar *text) {
    if (text == NULL) {
        return NULL;
    }
    const gchar *end = strchr(text, '\n');
    gchar *line = end != NULL ? g_strndup(text, (gsize)(end - text)) : g_strdup(text);
    g_strstrip(line);
    if (line[0] == '\0') {
        g_free(line);
        return NULL;
    }
    return line;
}

/* 拼出打开器失败的原因：退出状态 + 打开器自己输出的第一行。 */
static gchar *opener_failure_reason(const gchar *opener, gint wait_status, const gchar *stderr_text) {
    gchar *detail = first_nonempty_line(stderr_text);
    gchar *status_text = WIFEXITED(wait_status)
                             ? g_strdup_printf("%s exited with status %d", opener, WEXITSTATUS(wait_status))
                             : g_strdup_printf("%s was terminated abnormally", opener);
    gchar *reason = detail != NULL ? g_strdup_printf("%s: %s", status_text, detail)
                                   : g_strdup(status_text);
    g_free(status_text);
    g_free(detail);
    return reason;
}

/* 经宿主 portal 打开链接。
   为什么这是首选：本程序实际运行在玲珑容器里，那里没有任何本地 URI 处理器——随包
   xdg-open 因为拿不到 dde-open 与候选浏览器而以「no method available」退出 3，GIO 的默认
   处理器返回 Operation not supported（两者都在真机上量过）；而宿主 portal 就在会话总线上
   且实测能打开本机默认浏览器。parent_window 传空串：本程序不向 portal 声明父窗口，由宿主
   自行定位浏览器窗口。
   局限性：portal 收下请求即返回一个句柄，真正的打开结果经 org.freedesktop.portal.Request
   的 Response 信号异步送回，这里不订阅它——调用被接受即视为成功，只有调用本身失败（服务
   不存在、被拒绝、超时）才落到下一种手段。 */
static gboolean open_uri_via_portal(const gchar *uri, gchar **reason) {
    GError *error = NULL;
    GDBusConnection *connection = g_bus_get_sync(G_BUS_TYPE_SESSION, NULL, &error);
    if (connection == NULL) {
        *reason = g_strdup(error->message);
        g_error_free(error);
        return FALSE;
    }
    GVariant *reply = g_dbus_connection_call_sync(
        connection, PORTAL_BUS_NAME, PORTAL_OBJECT_PATH, PORTAL_OPEN_URI_INTERFACE, "OpenURI",
        g_variant_new("(ss@a{sv})", "", uri,
                      g_variant_new_array(G_VARIANT_TYPE("{sv}"), NULL, 0)),
        G_VARIANT_TYPE("(o)"), G_DBUS_CALL_FLAGS_NONE, PORTAL_CALL_TIMEOUT_MS, NULL, &error);
    g_object_unref(connection);
    if (reply == NULL) {
        *reason = g_strdup(error->message);
        g_error_free(error);
        return FALSE;
    }
    g_variant_unref(reply);
    return TRUE;
}

/* 随包 xdg-open：普通桌面（非玲珑容器）上的标准路径。
   同步等待并检查退出码，而不是只看「进程起来了」——上一版把启动成功当打开成功，容器里
   点击毫无反应却没有任何提示，根因就在这里。 */
static gboolean open_uri_with_opener(const gchar *opener, const gchar *uri, gchar **reason) {
    gchar *argv[] = {(gchar *)opener, (gchar *)uri, NULL};
    gchar *stderr_text = NULL;
    gint wait_status = 0;
    GError *error = NULL;
    if (!g_spawn_sync(NULL, argv, NULL, G_SPAWN_SEARCH_PATH, NULL, NULL, NULL, &stderr_text,
                      &wait_status, &error)) {
        *reason = g_strdup(error->message);
        g_error_free(error);
        g_free(stderr_text);
        return FALSE;
    }
    if (WIFEXITED(wait_status) && WEXITSTATUS(wait_status) == 0) {
        g_free(stderr_text);
        return TRUE;
    }
    *reason = opener_failure_reason(opener, wait_status, stderr_text);
    g_free(stderr_text);
    return FALSE;
}

/* 打开失败提示：整块弹框共用一行，第一次失败时创建，之后就地更新文本。
   用户看得到原因，才可能把「点了没反应」变成一条能报障的信息；而容器的 stderr 用户看不到，
   所以只打印是不够的。 */
static void show_open_failure(DialogContext *dialog, const gchar *reason) {
    gchar *text = g_strdup_printf("%s: %s", text_or_empty(dialog->open_failed_label),
                                  text_or_empty(reason));
    if (dialog->failure_line == NULL) {
        dialog->failure_line = gtk_label_new(text);
        gtk_label_set_line_wrap(GTK_LABEL(dialog->failure_line), TRUE);
        gtk_label_set_xalign(GTK_LABEL(dialog->failure_line), 0.0f);
        gtk_widget_set_halign(dialog->failure_line, GTK_ALIGN_FILL);
        gtk_box_pack_start(dialog->failure_slot, dialog->failure_line, FALSE, FALSE, 0);
        gtk_widget_show(dialog->failure_line);
    } else {
        gtk_label_set_text(GTK_LABEL(dialog->failure_line), text);
    }
    g_free(text);
}

/* 打开链接：按「在当前环境里最可能奏效」的顺序尝试，第一个成功的立即返回。
   顺序理由：宿主 portal 是容器里唯一被真机验证过能打开浏览器的路径，排第一；随包
   xdg-open 是普通桌面上的标准路径，排第二；GTK 的默认 URI 处理器最后兜底。三步都失败时
   返回 FALSE 并给出最后一条原因，由调用方显示在弹框里——静默失败等于这个入口不存在。 */
static gboolean open_uri(DialogContext *dialog, const gchar *uri, gchar **reason) {
    gchar *detail = NULL;
    if (open_uri_via_portal(uri, &detail)) {
        return TRUE;
    }
    set_reason(reason, detail);
    if (dialog->opener != NULL && dialog->opener[0] != '\0') {
        if (open_uri_with_opener(dialog->opener, uri, &detail)) {
            return TRUE;
        }
        set_reason(reason, detail);
    }
    if (dialog->window != NULL) {
        GError *error = NULL;
        if (gtk_show_uri_on_window(dialog->window, uri, GDK_CURRENT_TIME, &error)) {
            return TRUE;
        }
        if (error != NULL) {
            set_reason(reason, g_strdup(error->message));
            g_error_free(error);
        }
    }
    return FALSE;
}

/* 链接点击：交给 open_uri 按顺序尝试。
   成功后关闭弹框：用户已经随浏览器离开这一页，把对话框留在桌面上只是挡视线。关闭走
   gtk_main_quit 而不是 gtk_widget_destroy——GTK 在 activate-link 信号返回后仍会访问本标签
   的私有数据，在信号处理里销毁窗口会造成释放后使用。
   失败时保持弹框打开并就地写明原因：静默失败等于这个入口不存在。
   返回 TRUE 表示这次点击已经处理完，不再走 GTK 默认实现，否则 GTK 会自己再打开一次。 */
static gboolean on_activate_link(GtkLabel *label, const gchar *uri, gpointer data) {
    (void)label;
    DialogContext *dialog = (DialogContext *)data;
    gchar *reason = NULL;
    if (open_uri(dialog, uri, &reason)) {
        gtk_main_quit();
    } else {
        g_printerr("dsh-about: opening %s failed: %s\n", uri, text_or_empty(reason));
        show_open_failure(dialog, reason);
    }
    g_free(reason);
    return TRUE;
}

/* 链接行：用带 <a href> 标记的 GtkLabel 渲染，而不是 GtkLinkButton。
   两者的文本度量不同源：GtkLinkButton 是按钮，自带主题内边距与最小高度，把所在行撑高约
   5 像素，而它的文本在这个更高的行里垂直居中；标签列按行顶对齐，链接文本因此比同一行的
   普通值低约 4 像素（真机截图实测）。值列只保留 GtkLabel 一种部件，行高与文本位置就与
   普通值完全一致。
   点击、悬停、Tab 后按 Enter 激活、右键复制链接地址都由 GtkLabel 自己的链接机制提供，
   不需要按钮。 */
static GtkWidget *make_link_label(DialogContext *dialog, const gchar *uri) {
    /* URI 要进 Pango 标记：& 与 < 必须转义，标记解析失败会让整行文字消失。 */
    gchar *escaped = g_markup_escape_text(uri, -1);
    gchar *markup = g_strdup_printf("<a href=\"%s\">%s</a>", escaped, escaped);
    GtkWidget *label = gtk_label_new(NULL);
    gtk_label_set_markup(GTK_LABEL(label), markup);
    g_free(markup);
    g_free(escaped);
    gtk_label_set_line_wrap(GTK_LABEL(label), TRUE);
    gtk_label_set_xalign(GTK_LABEL(label), 0.0f);
    gtk_widget_set_halign(label, GTK_ALIGN_START);
    /* 上下文是 main 里那份 DialogContext，生命周期覆盖整个窗口，因此不需要复制或释放。 */
    g_signal_connect(label, "activate-link", G_CALLBACK(on_activate_link), dialog);
    return label;
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
static GtkWidget *make_copy_button(const DialogContext *dialog, const gchar *value) {
    GtkWidget *button = gtk_button_new();
    CopyButton *state = g_new0(CopyButton, 1);
    state->button = button;
    state->value = value;
    state->label = dialog->copy_label;
    state->copied_label = dialog->copied_label;

    GtkIconTheme *theme = gtk_icon_theme_get_for_screen(gtk_widget_get_screen(button));
    if (theme != NULL && gtk_icon_theme_has_icon(theme, COPY_ICON_NAME)) {
        state->shows_icon = TRUE;
        gtk_button_set_image(GTK_BUTTON(button),
                             gtk_image_new_from_icon_name(COPY_ICON_NAME, GTK_ICON_SIZE_BUTTON));
    } else {
        gtk_button_set_label(GTK_BUTTON(button), text_or_empty(state->label));
    }

    gtk_widget_set_halign(button, GTK_ALIGN_CENTER);
    gtk_widget_set_valign(button, GTK_ALIGN_CENTER);
    gtk_style_context_add_class(gtk_widget_get_style_context(button), "flat");
    gtk_widget_set_tooltip_text(button, text_or_empty(state->label));
    atk_object_set_name(gtk_widget_get_accessible(button), text_or_empty(state->label));
    g_signal_connect(button, "clicked", G_CALLBACK(on_copy_clicked), state);
    g_signal_connect(button, "destroy", G_CALLBACK(on_copy_button_destroy), state);
    return button;
}

/* 字段行：标签左列弱化显示，值右列按约定给出链接或普通文本，被 --copy 点名的值再补一个
   一键复制按钮。值不做成可选中标签：可选中标签会抢初始焦点并选中整行。
   长 URL 用 xalign 0 + 换行兜底：容器里没有横向滚动，值必须能在窄窗口里断行。 */
static void add_field(DialogContext *dialog, GtkGrid *grid, gint row, const gchar *label,
                      const gchar *value) {
    GtkWidget *name = gtk_label_new(text_or_empty(label));
    gtk_widget_set_halign(name, GTK_ALIGN_START);
    gtk_widget_set_valign(name, GTK_ALIGN_START);
    gtk_style_context_add_class(gtk_widget_get_style_context(name), "dim-label");
    gtk_grid_attach(grid, name, 0, row, 1, 1);

    GtkWidget *entry;
    if (is_http_uri(value)) {
        entry = make_link_label(dialog, value);
    } else {
        entry = gtk_label_new(text_or_empty(value));
        gtk_label_set_line_wrap(GTK_LABEL(entry), TRUE);
        gtk_label_set_xalign(GTK_LABEL(entry), 0.0f);
        gtk_widget_set_halign(entry, GTK_ALIGN_START);
    }
    gtk_grid_attach(grid, entry, 1, row, 1, 1);

    /* 复制按钮占第三列；没有它的行留空，因此不会把网格拉宽。 */
    if (is_copy_marked(value)) {
        gtk_grid_attach(grid, make_copy_button(dialog, value), 2, row, 1, 1);
    }
}

/* 居中行：分隔线上方的单行状态文本（形如「当前形态: 薄壳版」），整行水平居中。
   为什么合成一行而不是复用两列网格：这两行是状态摘要，不是可对照的「字段—值」表；
   居中的整行读起来是一句话，也不会把下方事实网格的标签列拉宽。
   文本保持普通标签：可选中标签会抢初始焦点并让整行呈选中态，那是用户明确不要的。 */
static void add_centered_line(DialogContext *dialog, const gchar *label, const gchar *value) {
    gboolean has_label = label != NULL && label[0] != '\0';
    /* 没有标签时只显示值，不留一个孤零零的分隔冒号。 */
    gchar *text = has_label ? g_strconcat(label, ": ", text_or_empty(value), NULL)
                            : g_strdup(text_or_empty(value));
    GtkWidget *line = gtk_label_new(text);
    g_free(text);
    gtk_widget_set_halign(line, GTK_ALIGN_CENTER);
    if (!is_copy_marked(value)) {
        gtk_box_pack_start(dialog->box, line, FALSE, FALSE, 0);
        return;
    }
    /* 被点名的值：文本与复制按钮组成一个水平居中组，行整体仍然居中。 */
    GtkWidget *row = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 6);
    gtk_widget_set_halign(row, GTK_ALIGN_CENTER);
    gtk_box_pack_start(GTK_BOX(row), line, FALSE, FALSE, 0);
    gtk_box_pack_start(GTK_BOX(row), make_copy_button(dialog, value), FALSE, FALSE, 0);
    gtk_box_pack_start(dialog->box, row, FALSE, FALSE, 0);
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
        {"open-failed", 0, 0, G_OPTION_ARG_STRING, &option_open_failed_label, "Prefix of the inline line reporting a failed link open", NULL},
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

    DialogContext dialog = {
        .window = GTK_WINDOW(window),
        .box = GTK_BOX(box),
        .failure_slot = NULL,
        .failure_line = NULL,
        .opener = option_uri_opener,
        .copy_label = option_copy_label,
        .copied_label = option_copied_label,
        .open_failed_label = option_open_failed_label,
    };

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
            add_centered_line(&dialog, "", option_center_fields[i]);
            continue;
        }
        gchar *label = g_strndup(option_center_fields[i], (gsize)(separator - option_center_fields[i]));
        add_centered_line(&dialog, label, separator + 1);
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
            add_field(&dialog, GTK_GRID(grid), i, "", option_fields[i]);
            continue;
        }
        gchar *label = g_strndup(option_fields[i], (gsize)(separator - option_fields[i]));
        add_field(&dialog, GTK_GRID(grid), i, label, separator + 1);
        g_free(label);
    }

    /* 失败提示的落点固定在「关闭」按钮上方：先建空容器占位，链接打不开时再往里填一行，
       提示因此出现在内容末尾而不是按钮下面。 */
    GtkWidget *failure_slot = gtk_box_new(GTK_ORIENTATION_VERTICAL, 0);
    gtk_box_pack_start(GTK_BOX(box), failure_slot, FALSE, FALSE, 0);
    dialog.failure_slot = GTK_BOX(failure_slot);

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

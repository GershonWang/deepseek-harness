// Package about 用随包的 GTK3 助手弹出「关于」对话框。
//
// 为什么不让切换器自己画窗口：切换器是 CGO_ENABLED=0 的纯 Go 进程，不带任何窗口工具链，
// 而玲珑容器里没有 zenity/kdialog/yad，也没有 Qt/DTK；容器内现成的只有 xmessage（Motif
// 外观，中文很可能缺字形）。GTK3 运行时本来就在容器里——薄壳客户端就靠它——宿主机也有
// GTK3 开发文件与 gcc，因此打包时编译一个 GTK3 助手，是外观、中文与依赖三者之间代价最小
// 的选择。
//
// 助手只负责画窗口：所有面向用户的文案由调用方传入，语言仍由 internal/i18n 决定。
package about

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

const (
	// PackageID 是本玲珑包的 ID，作为「关于」里的诊断信息展示。它与薄壳版相同——本包
	// 就是薄壳版的升级款，用户报障时给出的包 ID 必须能对回货架上的那一个应用。
	PackageID = "com.deepseek.dsh-desktop"
	// Packager 是玲珑封装作者署名。它标识的是「谁把这个 fork 打成玲珑包」，与上游作者
	// 无关，故不随上游同步变动。
	//
	// 薄壳版曾有一份同名常量供它的关于弹框使用；那个弹框已移除，薄壳版不再保留仓库与
	// 署名常量，这里是切换器侧的唯一一份。
	Packager = "Jokul"
	// GithubRepo 是玲珑封装（fork）仓库地址：玲珑包特有的改动都落在这里，而不是上游。
	GithubRepo = "https://github.com/GershonWang/deepseek-harness"
	// UpstreamRepo 是上游 DSH 官方仓库地址。「关于」必须同时给出两者：本包是在上游之上
	// 做的封装，只留 fork 地址会让用户把上游的问题报到这里。
	UpstreamRepo = "https://github.com/deepseek-ai/deepseek-harness"
	// productName 是产品名，两种语言下一致。
	productName = "DeepSeek Harness"
	// helperName 是打包时编译出的对话框助手文件名。
	helperName = "dsh-desktop-about"
	// logoSize 是「关于」里使用的随包图标尺寸：对话框按需缩放，取最大的一张最清晰。
	logoSize = "256x256"
	// logoFile 是随包图标的文件名，与 linglong.yaml 的安装名、.desktop 的 Icon= 以及托盘
	// 图标读取的是同一份文件。图标缺失只少一张图，因此路径写错不会报错——用名字对齐来防它。
	logoFile = "dsh-desktop.png"
	// openerName 是随包安装的真实 xdg-open（薄壳包与本包都经 buildext 带入 xdg-utils，
	// 落在 ${PREFIX}/bin，容器 PATH 首位）。链接点击显式走它，而不是依赖容器内残缺的
	// 默认 URI 处理器：这条路径与壳内 GUI 外链已经在真机上验证过。
	openerName = "xdg-open"
	// unknownVersion 是 harness 版本解析不出来时的取值。空串会在对话框里留下一行看不出
	// 所以然的空白，显式写 unknown 才能区分「没解析到」与「这个包没带运行时」。
	unknownVersion = "unknown"
)

// Field 是「关于」里的一行：本地化的标签 + 值。
//
// 值以 http:// 或 https:// 开头时由助手渲染成可点击链接，Copy 为 true 时助手在值右侧补一
// 个一键复制按钮——这是调用方与助手之间的两条约定，助手因此不需要知道哪一行是什么（见
// tools/dsh-about.c）。值本身刻意不做成可选中文本：选中态会让用户打开弹框第一眼看到一段
// 高亮，需要复制的值改用复制按钮。
type Field struct {
	// Label 是本地化的字段名。
	Label string
	// Value 是值；URL 会被渲染成可点击链接，Copy 标记的值会附带复制按钮。
	Value string
	// Copy 表示这一行的值需要一键复制入口（例如用户报障时要原样给出的玲珑包 ID）。
	Copy bool
}

// Dialog 是「关于」对话框的全部内容。
type Dialog struct {
	// Title 是窗口标题（本地化，例如「关于」）；为空时助手回落到产品名。
	Title string
	// CenterFields 渲染在分隔线上方、整行水平居中，用于「当前形态」「包 ID」这类状态摘要。
	CenterFields []Field
	// Fields 渲染在分隔线下方的事实网格里，按显示顺序排列；顺序即命令行参数的顺序——
	// 助手不做任何排序。
	Fields []Field
	// CloseLabel 是关闭按钮的文案（本地化）；为空时助手只留标题栏关闭与 Esc。
	CloseLabel string
	// CopyLabel 是复制按钮的文案（本地化），同时用作它的提示与无障碍名；为空时按钮只是没有
	// 文字提示，复制本身仍然可用。
	CopyLabel string
	// CopiedLabel 是复制成功后短暂显示的反馈文案（本地化）；为空时不做视觉反馈。
	CopiedLabel string
	// OpenFailedLabel 是链接打不开时，弹框里那一行提示的前缀（本地化）；助手会在它后面接上
	// 具体原因。为空时只显示原因，仍然可见。
	OpenFailedLabel string
}

// Presenter 弹出「关于」对话框。同一个实例同时只允许一个对话框，避免连点叠窗。
type Presenter struct {
	prefix  string
	version string

	mu      sync.Mutex
	running bool
}

// New 构造调用器。
// @param prefix - 安装前缀（<PREFIX>），助手、图标与 xdg-open 都按它定位；为空时 Show 会明确报错。
// @param version - 玲珑包版本，与切换器启动日志里的是同一个值。
// @returns 调用器。
func New(prefix, version string) *Presenter {
	return &Presenter{prefix: prefix, version: version}
}

// packageManifest 只取 package.json 的版本字段；其余内容与本对话框无关。
type packageManifest struct {
	Version string `json:"version"`
}

// HarnessVersion 读取 <PREFIX>/harness/package.json 的版本号。
//
// 为什么从文件读而不是让打包脚本注入：本包复用薄壳版的 harness 树（install-shell-runtime.sh
// 铺到同一个 ${PREFIX}/harness），读同一份文件才能保证托盘显示的 DSH 版本与薄壳版当时
// 显示的完全一致，也不会因为多一处 ldflags 注入而漂移。
// @param prefix - 安装前缀（<PREFIX>）；为空表示开发态，直接返回 unknown。
// @returns 版本号；文件缺失、内容损坏或版本为空时返回 "unknown"。
func HarnessVersion(prefix string) string {
	if prefix == "" {
		return unknownVersion
	}
	data, err := os.ReadFile(filepath.Join(prefix, "harness", "package.json"))
	if err != nil {
		return unknownVersion
	}
	var manifest packageManifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version == "" {
		return unknownVersion
	}
	return manifest.Version
}

// Show 弹出对话框。已有对话框在显示时直接返回，不叠第二个窗口。
// @param dialog - 字段行与按钮文案。
// @returns 助手缺失或启动失败的原因。
func (p *Presenter) Show(dialog Dialog) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	helper := filepath.Join(p.prefix, "bin", helperName)
	if _, err := os.Stat(helper); err != nil {
		return fmt.Errorf("about: dialog helper unavailable (%s): %w", helper, err)
	}
	args := []string{"--program-name", productName, "--version", p.version}
	if dialog.Title != "" {
		args = append(args, "--title", dialog.Title)
	}
	if dialog.CopyLabel != "" {
		args = append(args, "--copy-label", dialog.CopyLabel)
	}
	if dialog.CopiedLabel != "" {
		args = append(args, "--copied-label", dialog.CopiedLabel)
	}
	if dialog.OpenFailedLabel != "" {
		args = append(args, "--open-failed", dialog.OpenFailedLabel)
	}
	// 复制标记与行分开传：助手按「值出现在 --copy 列表里」给该行补按钮，因此同一区域内
	// 可复制行与普通行的相对顺序仍然由下面的参数顺序决定，不需要为它另开一种行类型。
	for _, field := range dialog.CenterFields {
		if field.Copy {
			args = append(args, "--copy", field.Value)
		}
	}
	for _, field := range dialog.Fields {
		if field.Copy {
			args = append(args, "--copy", field.Value)
		}
	}
	for _, field := range dialog.CenterFields {
		args = append(args, "--center-field", field.Label+"="+field.Value)
	}
	for _, field := range dialog.Fields {
		args = append(args, "--field", field.Label+"="+field.Value)
	}
	if dialog.CloseLabel != "" {
		args = append(args, "--close-label", dialog.CloseLabel)
	}
	// 图标缺失不算失败：没有它只是少一张图，对话框本身照常显示。
	logo := filepath.Join(p.prefix, "share", "icons", "hicolor", logoSize, "apps", logoFile)
	if info, err := os.Stat(logo); err == nil && info.Mode().IsRegular() {
		args = append(args, "--logo", logo)
	}
	// 打开器缺失也不拦弹框：助手会退回 GTK 的默认 URI 处理器，链接点击仍有兜底。
	opener := filepath.Join(p.prefix, "bin", openerName)
	if info, err := os.Stat(opener); err == nil && info.Mode().IsRegular() {
		args = append(args, "--uri-opener", opener)
	}
	cmd := exec.Command(helper, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("about: starting the dialog: %w", err)
	}
	p.running = true
	// 对话框进程在用户关闭窗口后退出；这里只回收，避免僵尸，并让下一次点击能再弹。
	go func() {
		_ = cmd.Wait()
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
	}()
	return nil
}

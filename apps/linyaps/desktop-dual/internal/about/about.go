// Package about 用随包的 GTK3 助手弹出「关于」对话框。
//
// 为什么不让切换器自己画窗口：切换器是 CGO_ENABLED=0 的纯 Go 进程，不带任何窗口工具链，
// 而玲珑容器里没有 zenity/kdialog/yad，也没有 Qt/DTK；容器内现成的只有 xmessage（Motif
// 外观，中文很可能缺字形）。GTK3 运行时本来就在容器里——薄壳客户端就靠它——宿主机也有
// GTK3 开发文件与 gcc，因此打包时编译一个几十行的 GTK3 助手，是外观、中文与依赖三者之间
// 代价最小的选择。
//
// 助手只负责画窗口：所有面向用户的文案由调用方传入，语言仍由 internal/i18n 决定。
package about

import (
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
	// productName 是产品名，两种语言下一致。
	productName = "DeepSeek Harness"
	// helperName 是打包时编译出的对话框助手文件名。
	helperName = "dsh-desktop-about"
	// logoSize 是「关于」里使用的随包图标尺寸：对话框按需缩放，取最大的一张最清晰。
	logoSize = "256x256"
	// logoFile 是随包图标的文件名，与 linglong.yaml 的安装名、.desktop 的 Icon= 以及托盘
	// 图标读取的是同一份文件。图标缺失只少一张图，因此路径写错不会报错——用名字对齐来防它。
	logoFile = "dsh-desktop.png"
)

// Presenter 弹出「关于」对话框。同一个实例同时只允许一个对话框，避免连点叠窗。
type Presenter struct {
	prefix  string
	version string

	mu      sync.Mutex
	running bool
}

// New 构造调用器。
// @param prefix - 安装前缀（<PREFIX>），助手与图标都按它定位；为空时 Show 会明确报错。
// @param version - 玲珑包版本，与切换器启动日志里的是同一个值。
// @returns 调用器。
func New(prefix, version string) *Presenter {
	return &Presenter{prefix: prefix, version: version}
}

// Show 弹出对话框。已有对话框在显示时直接返回，不叠第二个窗口。
// @param comments - 本地化的附加信息（当前形态、包 ID）。
// @returns 助手缺失或启动失败的原因。
func (p *Presenter) Show(comments string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	helper := filepath.Join(p.prefix, "bin", helperName)
	if _, err := os.Stat(helper); err != nil {
		return fmt.Errorf("about: dialog helper unavailable (%s): %w", helper, err)
	}
	args := []string{
		"--program-name", productName,
		"--version", p.version,
		"--comments", comments,
	}
	// 图标缺失不算失败：没有它只是少一张图，对话框本身照常显示。
	logo := filepath.Join(p.prefix, "share", "icons", "hicolor", logoSize, "apps", logoFile)
	if info, err := os.Stat(logo); err == nil && info.Mode().IsRegular() {
		args = append(args, "--logo", logo)
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

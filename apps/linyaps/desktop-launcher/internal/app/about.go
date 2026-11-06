// 本文件组装「关于」对话框的内容，并用 v3 的 Info 对话框展示。
//
// 为什么走 v3 的 MessageDialog 而不是像 dual 那样编译一个 GTK3 助手：薄壳客户端本身
// 就是 GTK4 窗口进程，框架已提供对话框 API；再带一个 GTK3 助手等于为一个弹框多维护
// 一套跨版本依赖。代价是 v3 的对话框不支持富文本链接——所以链接以纯文本展示，另外提供
// 「打开项目主页」按钮走系统浏览器。
package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/packaging"
)

const (
	// PackageID 是本玲珑包的 ID，作为「关于」里的诊断信息展示。
	PackageID = "com.deepseek.dsh-desktop"
	// Packager 是玲珑封装作者署名：标识「谁把这个 fork 打成玲珑包」，与上游作者无关。
	Packager = "Jokul"
	// GithubRepo 是玲珑封装（fork）仓库地址：玲珑包特有的改动都落在这里。
	GithubRepo = "https://github.com/GershonWang/deepseek-harness"
	// UpstreamRepo 是上游 DSH 官方仓库地址。两个地址都要给出：只留 fork 会让用户把
	// 上游的问题报到封装仓库。
	UpstreamRepo = "https://github.com/deepseek-ai/deepseek-harness"
	// ProductName 是产品名。品牌在两种语言下同形，因此不进字典，随载荷下发。
	ProductName = "DeepSeek Harness"
	// devVersion 是未经 -ldflags 注入时的版本（开发态）。
	devVersion = "dev"
)

// Version 是桌面客户端版本，构建时由 -ldflags 注入，取值来自 linglong/linglong.yaml
// 的 version（见 Makefile 与 linglong/prepare-offline.sh 的构建行）。
var Version = devVersion

// HarnessVersion 读取 <PREFIX>/harness/package.json 的版本号。
//
// 从文件读而不是再注入一次 ldflags：托盘显示的 DSH 版本必须与包内实际运行的那棵树
// 一致，多一处注入就多一处漂移面。
// @returns 版本号；开发态、文件缺失或内容损坏时返回 "dev"。
func HarnessVersion() string {
	prefix := packaging.HarnessPrefix()
	if prefix == "" {
		return devVersion
	}
	raw, err := os.ReadFile(filepath.Join(prefix, "harness", "package.json"))
	if err != nil {
		return devVersion
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || strings.TrimSpace(manifest.Version) == "" {
		return devVersion
	}
	return manifest.Version
}

// AboutEvent 是「关于」弹框的数据事件。
//
// 布局由壳前端渲染（与 desktop-dual 的 GTK 助手对齐：logo、居中版本、形态徽标、
// 包 ID + 复制、字段网格 + 可点仓库）；Go 侧只负责提供数据，不再自绘纯文本对话框。
const AboutEvent = "about:open"

// AboutPayload 是发给壳前端的「关于」数据。
// JSON 字段名与 frontend/app.js 的填充逻辑一一对应，改名要同步两侧。
type AboutPayload struct {
	// Product 是产品名（品牌，语言无关）。
	Product string `json:"product"`
	// Version 是桌面客户端版本（构建期注入）。
	Version string `json:"version"`
	// HarnessVersion 是包内 DSH 运行时版本。
	HarnessVersion string `json:"harnessVersion"`
	// PackageID 是玲珑包 ID，用户报障时要能原样给出。
	PackageID string `json:"packageId"`
	// Packager 是玲珑封装作者署名。
	Packager string `json:"packager"`
	// UpstreamRepo 是上游仓库地址。
	UpstreamRepo string `json:"upstreamRepo"`
	// ForkRepo 是玲珑封装仓库地址。
	ForkRepo string `json:"forkRepo"`
}

// AboutInfo 汇总「关于」要展示的全部数据。
// @returns 数据载荷；各字段都是静态常量或只读文件解析结果，不会失败。
func AboutInfo() AboutPayload {
	return AboutPayload{
		Product:        ProductName,
		Version:        Version,
		HarnessVersion: HarnessVersion(),
		PackageID:      PackageID,
		Packager:       Packager,
		UpstreamRepo:   UpstreamRepo,
		ForkRepo:       GithubRepo,
	}
}

// AboutText 返回「关于」正文（按当前壳语言渲染）。
//
// 每行一条事实，用换行分隔：v3 的 MessageDialog 是纯文本，没有字段网格。
// @returns 本地化的多行正文。
func (a *App) AboutText() string {
	lines := []string{
		ProductName,
		a.t("about.clientVersion", Version),
		a.t("about.harnessVersion", HarnessVersion()),
		a.t("about.packageId", PackageID),
		a.t("about.packager", Packager),
		a.t("about.homepage", UpstreamRepo),
		a.t("about.fork", GithubRepo),
	}
	return strings.Join(lines, "\n")
}

// ShowAbout 展示「关于」对话框。
//
// 关闭按钮同时设为默认与取消按钮：Esc 与点标题栏关闭都落到同一个回调，不会留下
// 一个无人处理的对话框。链接按钮走系统浏览器（与壳内 GUI 的外链同一路径）。
func (a *App) ShowAbout() {
	if a.app == nil {
		return
	}
	// 壳前端接管渲染（布局与 dual 的 GTK 助手一致）；运行时尚未绑定时退回 v3 自带的
	// Info 对话框——那种情况下前端一定还没加载，发事件不会有人接。
	if !a.bound {
		a.showAboutDialog()
		return
	}
	if a.windowHidden.Load() {
		a.PresentWindow()
	}
	a.emit(AboutEvent, AboutInfo())
}

// showAboutDialog 用 v3 的 Info 对话框展示「关于」，作为前端未就绪时的兜底。
func (a *App) showAboutDialog() {
	dialog := a.app.Dialog.Info().
		SetTitle(a.t("about.title")).
		SetMessage(a.AboutText())

	openButton := dialog.AddButton(a.t("about.openHomepage"))
	openButton.OnClick(func() {
		if err := a.app.Browser.OpenURL(UpstreamRepo); err != nil {
			a.logf("打开项目主页失败: %v", err)
		}
	})
	closeButton := dialog.AddButton(a.t("about.close"))
	dialog.SetDefaultButton(closeButton)
	dialog.SetCancelButton(closeButton)
	dialog.Show()
}

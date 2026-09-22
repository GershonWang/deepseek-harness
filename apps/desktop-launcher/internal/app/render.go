// 本文件把「领域包报的事实」渲染成用户可见文案。
//
// 为什么单独一个文件：领域包（toolchain/connector/hosttools/preflight）只报枚举与原始
// 技术细节，措辞全部收在这里。它们都是同一种转换——errors.As 认出归类 → 查字典 → 接上
// 技术细节，集中在一处才能保证「同一事实各处的说法一致」，也才有一个地方能一眼看全
// Go 侧到底把哪些事实变成了文案（见 docs/i18n.md 第六节）。
//
// 技术细节（原始错误、路径、stderr 片段）一律原样拼接、不翻译：它们是排障线索，翻译后
// 反而无法搜索与比对。
package app

import (
	"errors"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/connector"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/hosttools"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/preflight"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/toolchain"
)

// connectorErrorText 渲染外部地址校验失败的原因。
//
// 归类走字典；url.Parse 自身的错误没有归类，直接显示原文（英文技术细节）。
//
// @param err ValidateURL 返回的错误。
// @returns 当前语言下的原因文本。
func (a *App) connectorErrorText(err error) string {
	var validationErr *connector.ValidationError
	if !errors.As(err, &validationErr) {
		return err.Error()
	}
	return a.t("connector.error." + string(validationErr.Kind))
}

// hostToolErrorText 渲染宿主挂载失败的原因。
//
// @param err hosttools.Add/Remove 返回的错误。
// @returns 当前语言下的原因文本。
func (a *App) hostToolErrorText(err error) string {
	var mountErr *hosttools.Error
	if !errors.As(err, &mountErr) {
		return err.Error()
	}
	return a.t("hosttool.error."+string(mountErr.Kind), mountErr.Detail)
}

// hostToolWarningText 渲染宿主挂载的提醒。
//
// @param kind 领域包给出的提醒归类；空串表示无需提醒。
// @returns 当前语言下的提醒文本；kind 为空时为空串。
func (a *App) hostToolWarningText(kind hosttools.WarningKind) string {
	if kind == "" {
		return ""
	}
	return a.t("hosttool.warning." + string(kind))
}

// preflightErrorText 渲染预检里 doctor 调用失败的原因。
//
// 无输出且 stderr 也为空时用不带细节的键：界面上多一个空冒号只会让人以为漏了信息。
//
// @param err preflight.Diagnose 返回的错误。
// @returns 当前语言下的原因文本。
func (a *App) preflightErrorText(err error) string {
	var doctorErr *preflight.DoctorError
	if !errors.As(err, &doctorErr) {
		return err.Error()
	}
	key := "preflight.doctor." + string(doctorErr.Kind)
	if doctorErr.Detail == "" {
		return a.t(key)
	}
	return a.t(key+"Detail", doctorErr.Detail)
}

// installErrorText 渲染安装失败原因。
//
// 领域包给出归类（toolchain.InstallError）时按 kind 查字典，再接上原始错误；其余错误
// （激活失败、解包 IO 等）没有可归类的事实，直接显示原文——硬塞一句「安装失败」反而
// 丢掉线索。
//
// @param err 安装失败原因。
// @returns 当前语言下的原因文本。
func (a *App) installErrorText(err error) string {
	var installErr *toolchain.InstallError
	if !errors.As(err, &installErr) {
		return err.Error()
	}
	text := a.t("toolchain.error." + string(installErr.Kind))
	if installErr.Err != nil {
		text += a.t("common.detailSeparator") + installErr.Err.Error()
	}
	return text
}

// 前端界面事件落盘：壳自己的界面行为（舞台切换、iframe 重载、Target 换代、挂载
// 看门狗）原本只存在于前端内存里，出现"闪屏/白屏"这类偶发问题时无法与 harness.log
// 的时间线对齐，只能靠猜。本文件提供一条最小诊断通道——一个 Wails 绑定方法，把前端
// 事件按统一格式追加到 launcher.log（与 harness.log 同目录、同时间戳格式）。
//
// 设计边界：
//   - 只做诊断，不改变界面行为；写入失败只返回错误由前端忽略，绝不打扰用户。
//   - 绝不落盘 token 与会话内容：iframe 地址带 ?token=…，写进日志等于把凭据落盘。
//     写入前强制脱敏，不依赖前端是否记得。
//   - 单文件上限 + 轮转：诊断日志不允许无限增长。
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/appenv"
)

const (
	// uiLogFileName 与 harness.log 同目录：两者按时间戳可直接对齐。
	uiLogFileName = "launcher.log"
	// uiLogMaxBytes 超过即轮转为 .1（只保留一份历史），避免诊断日志无限增长。
	uiLogMaxBytes = 2 << 20
	// uiLogMaxDetail 是单条 detail 的字符上限：前端误传整段 DOM 时也不至于写爆。
	uiLogMaxDetail = 400
)

// uiLogTokenPattern 匹配地址里的 token 查询串（大小写不敏感），写入前整体替换。
var uiLogTokenPattern = regexp.MustCompile(`(?i)token=[^\s&"']*`)

// uiLogMu 串行化写入：前端可能并发上报，轮转与追加需要互斥。
var uiLogMu sync.Mutex

// LogUiEvent 把一条前端界面事件追加到 launcher.log，用于把界面侧现象（重载、舞台
// 切换、换 target）与 harness.log 的时间线对齐。
//
// kind 是稳定的事件名（如 "reload"、"stage"），detail 是简短说明，可为空。本方法只
// 用于诊断：写入失败返回错误，界面侧忽略即可。
// @param kind - 事件名。
// @param detail - 事件细节（会脱敏并压成一行）。
// @returns 写入失败的原因；没有可写位置时返回 nil。
func (a *App) LogUiEvent(kind, detail string) error {
	return appendUiEventLog(appenv.RuntimeDir(), kind, detail)
}

// appendUiEventLog 追加一条事件到 dir/launcher.log；dir 为空表示没有可写位置。
//
// 轮转在写入前按"当前大小 + 本行大小"判断，因此单条超长事件也会触发轮转，不会把
// 文件撑过上限太多。
// @param dir - 日志目录（调用方负责解析，测试可直接指向临时目录）。
// @param kind - 事件名。
// @param detail - 事件细节。
// @returns 写入失败的原因；dir 为空时返回 nil。
func appendUiEventLog(dir, kind, detail string) error {
	if dir == "" {
		return nil
	}
	kind = redactUiEvent(kind)
	detail = redactUiEvent(detail)
	line := fmt.Sprintf("[%s] [ui] %s", time.Now().Format("2006-01-02 15:04:05.000"), kind)
	if detail != "" {
		line += " " + detail
	}
	line += "\n"

	uiLogMu.Lock()
	defer uiLogMu.Unlock()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, uiLogFileName)
	if info, err := os.Stat(path); err == nil && info.Size()+int64(len(line)) > uiLogMaxBytes {
		// 只保留一份历史：诊断日志的价值在最近一段，不在完整归档。
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

// redactUiEvent 抹掉凭据类片段并把内容压成一行，避免泄露 token 或让多行内容破坏
// "一行一事件"的解析约定。
// @param s - 原始事件名或细节。
// @returns 可安全落盘的文本。
func redactUiEvent(s string) string {
	s = uiLogTokenPattern.ReplaceAllString(s, "token=<redacted>")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > uiLogMaxDetail {
		s = string(r[:uiLogMaxDetail]) + "…"
	}
	return s
}

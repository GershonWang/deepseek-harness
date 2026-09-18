// 自动禁用留痕的读取与确认：doctor 写、壳读的跨进程交接。
//
// 链路：doctor 判定插件与当前版本不兼容 → 把它从 profile 的 bundle 层禁用并把
// 留痕追加到 <dshHome>/doctor/auto-disabled.json → 壳在 harness 启动成功后提示
// 用户"哪些插件被禁用、为什么" → 用户据此决定去插件页重新启用还是自行卸载。
//
// 两个文件分属两个进程，各自拥有自己的状态：留痕由 doctor 只追加，壳只读；
// "已提示"标记只有壳读写，因此 doctor 运行时也不会与壳争抢同一个文件。
//
// 留痕只用于提示：读失败、内容被改坏都按"没有留痕"处理，最坏结果是少提示一次，
// 绝不阻断启动。
package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/appenv"
)

// autoDisabledAckFile 是壳记录已提示留痕的文件名，位于启动器运行时目录下。
const autoDisabledAckFile = "auto-disabled-ack.json"

// AutoDisabledNotice 是给前端的单条自动禁用提示。
type AutoDisabledNotice struct {
	// Bundle 是被禁用的插件包名。
	Bundle string
	// Reason 是禁用原因（doctor 写入的面向用户文案）。
	Reason string
	// At 是禁用时刻（ISO 8601）。
	At string
}

// autoDisabledRecord 对齐 doctor 留痕文件的记录字段
// （packages/support/doctor/src/auto-disabled.ts 的 AutoDisabledRecord）。
type autoDisabledRecord struct {
	At        string `json:"at"`
	Bundle    string `json:"bundle"`
	Reason    string `json:"reason"`
	BackupDir string `json:"backupDir"`
}

// autoDisabledPath 返回 doctor 写下的留痕文件路径。
// @param dshHome - 真实 harness home（预检与 doctor 面板指向的目录）。
// @returns 留痕文件的绝对路径。
func autoDisabledPath(dshHome string) string {
	return filepath.Join(dshHome, "doctor", "auto-disabled.json")
}

// readAutoDisabled 读取留痕文件。
// @param dshHome - 真实 harness home。
// @returns 留痕记录，按 doctor 追加顺序（旧→新）；文件缺失或内容不可解析时为空。
func readAutoDisabled(dshHome string) []autoDisabledRecord {
	raw, err := os.ReadFile(autoDisabledPath(dshHome))
	if err != nil {
		return nil // 从未自动禁用过任何插件：最常见的路径。
	}
	var records []autoDisabledRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil // 留痕被外部改坏：放弃提示，不阻断启动。
	}
	return records
}

// autoDisabledKey 生成一条留痕的稳定标识。
//
// 时刻参与标识，是因为同一次修复禁用的多个插件共享时刻：只按时刻去重会把同轮的
// 其它插件当成已提示，只按包名去重又分不清同一插件被禁用过几轮。
//
// @param record - 一条留痕记录。
// @returns 该记录在"已提示"集合中的键。
func autoDisabledKey(record autoDisabledRecord) string {
	return record.At + "|" + record.Bundle
}

// ackFilePath 返回壳记录已提示留痕的文件路径。
// @returns 启动器运行时目录下的标记文件路径。
func ackFilePath() string {
	return filepath.Join(appenv.RuntimeDir(), autoDisabledAckFile)
}

// readAcknowledged 读取已提示集合。
// @returns 已提示留痕的键集合；文件缺失或内容不可解析时为空集合（最坏是重复提示一次）。
func readAcknowledged() map[string]bool {
	raw, err := os.ReadFile(ackFilePath())
	if err != nil {
		return map[string]bool{}
	}
	var keys []string
	if err := json.Unmarshal(raw, &keys); err != nil {
		return map[string]bool{}
	}
	set := make(map[string]bool, len(keys))
	for _, key := range keys {
		set[key] = true
	}
	return set
}

// PendingAutoDisabled 返回尚未提示过的自动禁用留痕（旧→新）。
//
// 前端在 harness 首次进入运行态时调用一次。同一插件被禁用过多轮时只保留最新一条：
// 提示要回答的是"现在有哪些插件被自动禁用了"，而不是每次禁用事件；确认时按包名
// 覆盖，因此被折叠掉的旧记录不会在下一轮又冒出来。
//
// @returns 待提示的留痕（可为空）；调用本身不写入任何状态。
func (a *App) PendingAutoDisabled() []AutoDisabledNotice {
	acknowledged := readAcknowledged()
	latest := make(map[string]autoDisabledRecord)
	order := make([]string, 0)
	for _, record := range readAutoDisabled(preflightHomePath(a.home)) {
		if acknowledged[autoDisabledKey(record)] {
			continue
		}
		if _, seen := latest[record.Bundle]; !seen {
			order = append(order, record.Bundle)
		}
		latest[record.Bundle] = record
	}
	notices := make([]AutoDisabledNotice, 0, len(order))
	for _, bundle := range order {
		record := latest[bundle]
		notices = append(notices, AutoDisabledNotice{Bundle: record.Bundle, Reason: record.Reason, At: record.At})
	}
	return notices
}

// AckAutoDisabled 把前端已展示的插件标记为已提示。
//
// 由前端在提示真正展示给用户之后调用，并回传展示过的包名：提示没被看到就不算提示过，
// 而按包名确认也让"展示期间 doctor 又禁用了别的插件"这种情况仍能在下次启动时提示。
//
// 集合里只保存留痕文件当前的键，因此不会无限增长；留痕被 doctor 裁剪掉之后，
// 对应的旧键也会在下次确认时消失。
//
// @param bundles - 前端已展示的插件包名。
// @returns 空串表示已记录；否则为写入失败的原因（前端可忽略，最坏是下次再提示一次）。
func (a *App) AckAutoDisabled(bundles []string) string {
	shown := make(map[string]bool, len(bundles))
	for _, bundle := range bundles {
		shown[bundle] = true
	}
	records := readAutoDisabled(preflightHomePath(a.home))
	acknowledged := readAcknowledged()
	// 按留痕顺序重建集合：既保留此前确认过的键，也纳入本次展示的包名。集合只镜像
	// 留痕里仍存在的键——留痕本身按 20 条封顶（doctor 侧），壳这边再设一个上限只会
	// 在两边不一致时把尚未展示的记录重新算成未提示；留痕被裁剪后旧键自然消失。
	ordered := make([]string, 0, len(records))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		key := autoDisabledKey(record)
		if seen[key] || !(acknowledged[key] || shown[record.Bundle]) {
			continue
		}
		seen[key] = true
		ordered = append(ordered, key)
	}
	raw, err := json.Marshal(ordered)
	if err != nil {
		return "无法序列化已提示记录: " + err.Error()
	}
	if err := os.MkdirAll(filepath.Dir(ackFilePath()), 0o700); err != nil {
		return "无法创建运行时目录: " + err.Error()
	}
	if err := os.WriteFile(ackFilePath(), raw, 0o600); err != nil {
		return "无法写入已提示记录: " + err.Error()
	}
	return ""
}

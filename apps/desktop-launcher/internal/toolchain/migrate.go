// 工具 ID 改名后的安装迁移。
//
// 工具 ID 是安装目录名（`<dir>/<id>-<version>`）与激活软链名（`current/<id>`）的
// 组成部分，因此改名不能只改清单：老用户磁盘上按旧 ID 铺开的目录与软链必须一起
// 搬过去，否则那份安装会在新清单下变成查不到工具定义的孤儿——卡片显示未安装，
// 而文件仍占着空间，用户既用不到也删不掉。
//
// 迁移在启动自愈（ReconcileBinLinks）之前执行：先把目录与 current 软链搬到新 ID，
// 再由自愈统一重建 bin/ 下的命令软链。
package toolchain

import (
	"fmt"
	"os"
	"path/filepath"
)

// legacyToolIDs 是历史工具 ID 到当前 ID 的映射。
//
// 之所以用表而不是散落的 if：改名可能再发生，而每次改名的迁移步骤完全相同；
// 表也让「哪些 ID 曾经存在」在代码里有一处可查。
var legacyToolIDs = map[string]string{
	// jdk21 在清单里同时承载 JDK 8/11/17/21/25 多条大版本线，名实不符（AUDIT N23）。
	"jdk21": "jdk",
}

// MigrateLegacyToolIDs 把已改名工具的历史安装搬到新 ID，返回逐项结果供启动日志留痕。
//
// 幂等：目标 ID 下已存在同名版本时保留既有那份并跳过该版本，因此重复启动只会重复
// 报告「目标已存在」，不会覆盖或丢数据。单项失败只留痕不中断——迁移问题不该让
// harness 起不来，这与 ReconcileBinLinks 的取舍一致。
//
// 调用方需在之后执行 ReconcileBinLinks：旧 ID 名下的 bin/ 命令软链此时已指向不存在
// 的路径，由自愈清理并按新 ID 重建。
func MigrateLegacyToolIDs(dir string) []string {
	var done []string
	for oldID, newID := range legacyToolIDs {
		versions := ListVersions(dir, oldID)
		// 激活版本记录先读出来：版本目录一旦改名，旧软链的解析结果就失效了。
		active := ActiveVersion(dir, oldID)
		if len(versions) == 0 && active == "" {
			continue
		}
		for _, v := range versions {
			dst := versionDir(dir, newID, v)
			if _, err := os.Stat(dst); err == nil {
				done = append(done, fmt.Sprintf("%s %s: 目标已存在，保留现有", newID, v))
				continue
			}
			if err := os.Rename(versionDir(dir, oldID, v), dst); err != nil {
				done = append(done, fmt.Sprintf("%s %s: 迁移失败 %v", newID, v, err))
				continue
			}
			done = append(done, fmt.Sprintf("%s %s: 已迁移", newID, v))
		}
		oldLink := currentLink(dir, oldID)
		if _, err := os.Lstat(oldLink); err == nil {
			if active != "" && IsInstalled(dir, newID, active) {
				newLink := currentLink(dir, newID)
				if _, err := os.Lstat(newLink); os.IsNotExist(err) {
					if err := os.MkdirAll(filepath.Dir(newLink), 0o755); err == nil {
						_ = os.Symlink(versionDir(dir, newID, active), newLink)
					}
				}
			}
			_ = os.Remove(oldLink)
		}
	}
	return done
}

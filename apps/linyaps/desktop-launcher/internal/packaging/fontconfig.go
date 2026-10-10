// 打包态字体配置注入：让运行时 fontconfig 读到包内字体目录。
//
// 背景：玲珑基础镜像清空了 /usr/share/fonts，运行容器里该目录又被宿主目录
// （/share/fonts → /usr/share/fonts, ro）整体挂载覆盖；实测把字体放进层内的
// usr/share/fonts、或由层内 etc/fonts/conf.d 提供规则都无效——层里的 usr/ 与
// etc/ 都不进容器命名空间，容器只读 base 层那份 /etc/fonts/conf.d。
// 但层内 ${PREFIX}/share/dsh-fonts 可读，fontconfig 也能解析其中的字体（实测
// 以 FONTCONFIG_FILE 指向一份含该 <dir> 的配置时，fc-list/fc-match 立即命中
// 包内文件、且不触发任何 family 解析变化）。因此这里在 WebKit 初始化前设置该变量。
//
// 未打包态或配置缺失时不设置任何变量，fontconfig 保持默认行为，开发态与宿主
// 既有的字体解析不受影响。
package packaging

import (
	"os"
	"path/filepath"
)

// fontConfigEnvName 是交给 fontconfig 的环境变量名；值为配置文件路径。
const fontConfigEnvName = "FONTCONFIG_FILE"

// fontConfigRelPath 是包内字体配置相对 prefix 的位置，由
// install-container-fonts.sh 在构建期生成。
func fontConfigRelPath() string {
	return filepath.Join("etc", "fonts", "dsh-fonts.conf")
}

// fontConfigPath 返回包内字体配置的绝对路径；未打包态返回空串。
func fontConfigPath(prefix string) string {
	if prefix == "" {
		return ""
	}
	return filepath.Join(prefix, fontConfigRelPath())
}

// fontConfigEnv 决定是否为该 prefix 注入 FONTCONFIG_FILE。
//
// 返回空串表示不注入：未打包态没有 prefix，而配置缺失时必须保持默认行为——
// 把 FONTCONFIG_FILE 指向缺失路径会让 fontconfig 退回内置默认，反而丢掉宿主
// 既有的字体解析与中文回退。
func fontConfigEnv(prefix string) string {
	conf := fontConfigPath(prefix)
	if conf == "" {
		return ""
	}
	if _, err := os.Stat(conf); err != nil {
		return ""
	}
	return conf
}

// ConfigureFontConfig 在 GTK/WebKit 初始化之前把包内字体配置交给 fontconfig。
//
// GTK/WebKit 在首次初始化时读取 FONTCONFIG_FILE，因此必须在 wails.Run 之前调用。
func ConfigureFontConfig() {
	if conf := fontConfigEnv(HarnessPrefix()); conf != "" {
		_ = os.Setenv(fontConfigEnvName, conf)
	}
}

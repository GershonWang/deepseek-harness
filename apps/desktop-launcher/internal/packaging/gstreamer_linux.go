//go:build linux

package packaging

import (
	"os"
	"path/filepath"
)

// ConfigureGStreamerPlugins 让 webkit2gtk 的 WebProcess 找到包内的 GStreamer 插件。
//
// 背景：语音输入经由 getUserMedia 采集，而 WebKitGTK 用 GStreamer 枚举音频设备。
// 玲珑包内的插件目录（<PREFIX>/lib/x86_64-linux-gnu/gstreamer-1.0，259 个插件）不在
// GStreamer 的运行期搜索路径上，容器内系统目录只有 coreelements 与 coretracers 两个，
// 于是 appsink 加载失败、枚举到 0 个音频输入，getUserMedia 以 OverconstrainedError
// 失败——用户看到的是「麦克风无法使用」，与权限被拒完全是两回事。
//
// 为什么必须在 wails.Run 之前：真正做枚举的是 WebKit 的 WebProcess（独立进程），它
// 继承本进程的环境；启动后再设置对已经起来的 WebProcess 无效。
//
// 用 GST_PLUGIN_PATH 而不是 GST_PLUGIN_SYSTEM_PATH：前者是附加语义，不覆盖系统默认
// 搜索路径，环境里已有的取值也照旧生效（见 gstreamerPluginPath）。
func ConfigureGStreamerPlugins() {
	prefix := HarnessPrefix()
	if prefix == "" {
		return
	}
	dir := filepath.Join(prefix, "lib", "x86_64-linux-gnu", "gstreamer-1.0")
	if _, statErr := os.Stat(dir); statErr != nil {
		return // 开发态：插件在系统标准路径，默认即正确
	}
	_ = os.Setenv("GST_PLUGIN_PATH", gstreamerPluginPath(os.Getenv("GST_PLUGIN_PATH"), dir))
}

// gstreamerPluginPath 计算 GST_PLUGIN_PATH 的最终取值。
//
// 包内目录排在前面，保证应用自带的插件优先于外部同名插件；existing 非空时追加在
// 后面而不是丢弃，避免覆盖用户或宿主环境已经配置好的路径。
func gstreamerPluginPath(existing, dir string) string {
	if existing == "" {
		return dir
	}
	return dir + string(os.PathListSeparator) + existing
}

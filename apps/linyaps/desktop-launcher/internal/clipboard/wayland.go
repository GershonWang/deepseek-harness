package clipboard

// 本文件实现 Wayland 通道：把读取工作交给 wl-paste（wl-clipboard）。
// 走外部命令而不是自实现 wl_data_device/wlr-data-control，是因为剪贴板
// 传输要跨进程传 fd 并驱动完整事件循环，自实现的出错面远大于一次调用。
//
// 两条策略与 X11 侧对称：位图（image/*）与文件（text/uri-list）。少了后者时，
// 在文件管理器里复制一张图片会粘不出来——实测 DDE 文管此时只给 text/uri-list 与
// x-special/gnome-copied-files，一个 image/* 都没有，而 XWayland 也不把 Wayland
// 侧的 selection 桥接到 X11，两边都读不到。

import (
	"os"
	"os/exec"
)

// waylandWlPaste 返回可用的 wl-paste 路径；不在 Wayland 会话、或候选位置都找不到
// 命令时返回空串。两条策略都先问它：既免去各自重复判断会话，也让「命令缺失」只有
// 一个判断点——那时整个 Wayland 通道都不可用，而不是某一条策略悄悄返回空。
//
// 先查 PATH 再退固定位置：随包安装后 wl-paste 落在 ${PREFIX}/bin，该目录在容器
// PATH 上；固定位置是给依赖宿主自带命令的旧产物留的兜底。
func waylandWlPaste() string {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return ""
	}
	if p, err := exec.LookPath("wl-paste"); err == nil {
		return p
	}
	for _, p := range []string{
		"/run/host/usr/bin/wl-paste",
		"/usr/bin/wl-paste",
		"/bin/wl-paste",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// wlPasteData 读出剪贴板上指定 MIME 类型的内容，取不到时返回 nil。
//
// 取不到是常态而不是异常：类型是逐个探测的，同一条 selection 上多数类型本来就不
// 存在，因此这里既不返回错误也不记日志。超过 maxImageBytes 的输出直接丢弃——同一
// 上限也用于位图路径，避免一个病态 owner 让壳进程按声明分配内存。
func wlPasteData(wlPaste, mime string) []byte {
	out, err := exec.Command(wlPaste, "--type", mime, "--no-newline").Output()
	if err != nil || len(out) == 0 || len(out) > maxImageBytes {
		return nil
	}
	return out
}

// readWaylandImage 读 Wayland 侧的位图剪贴板：按常见图片 MIME 逐个探测，并校验字节
// 确实匹配所请求的格式——类型不存在时 wl-paste 退出非零，但拿到输出并不等于拿到了
// 所请求的格式，格式判定仍以魔数为准。
//
// 逐个探测而不是先问 wl-paste --list-types：后者要 wl-clipboard 2.0+，而候选类型
// 只有六个，多起几个短进程换掉一个版本依赖更划算。
func readWaylandImage() []byte {
	wlPaste := waylandWlPaste()
	if wlPaste == "" {
		return nil
	}
	candidates := []struct {
		mime  string
		valid func([]byte) bool
	}{
		{"image/png", isValidPNG},
		{"image/jpeg", isValidJPEG},
		{"image/webp", isValidWebP},
		{"image/bmp", isValidBMP},
		{"image/tiff", isValidTIFF},
		{"image/gif", isValidGIF},
	}
	for _, cand := range candidates {
		if out := wlPasteData(wlPaste, cand.mime); out != nil && cand.valid(out) {
			return out
		}
	}
	return nil
}

// readWaylandUriListImage 读 Wayland 侧的「文件」剪贴板：复制图片文件时剪贴板上是
// URI 列表而不是位图。DDE 文管同时给 text/uri-list（标准格式，路径百分号编码）与
// x-special/gnome-copied-files（GNOME 约定，首行为 copy/cut），两者行格式一致，因此
// 共用 readImageFileFromURIList 解析；先试标准的那个。
//
// 路径在容器内可直接读：Linglong 把 /home/<user>、/media、/mnt 绑定挂载进来，容器内
// 路径与宿主一致，无需再做一次路径映射。
func readWaylandUriListImage() []byte {
	wlPaste := waylandWlPaste()
	if wlPaste == "" {
		return nil
	}
	for _, mime := range []string{"text/uri-list", "x-special/gnome-copied-files"} {
		if data := wlPasteData(wlPaste, mime); data != nil {
			if img := readImageFileFromURIList(data); img != nil {
				return img
			}
		}
	}
	return nil
}

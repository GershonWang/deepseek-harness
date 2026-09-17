package clipboard

// 本文件实现 Wayland 通道：把读取工作交给 wl-paste（wl-clipboard）。
// 走外部命令而不是自实现 wl_data_device/wlr-data-control，是因为剪贴板
// 传输要跨进程传 fd 并驱动完整事件循环，自实现的出错面远大于一次调用。

import (
	"os"
	"os/exec"
)

// readWaylandImage tries to read an image from the Wayland compositor's
// clipboard using wl-paste. It returns nil when wl-paste is unavailable,
// produces no image, or the output is not a supported raster format.
//
// This is a fallback for Wayland-native desktops (e.g. deepin with Wayland
// compositing) where XWayland's clipboard bridge does not carry image
// formats across the protocol boundary.
func readWaylandImage() []byte {
	// Only try when we appear to be on a Wayland session.
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return nil
	}
	// Find wl-paste: it might be on PATH in the host mount, or in the
	// container's own /usr/bin. Prefer host paths when they exist.
	wlPaste, err := exec.LookPath("wl-paste")
	if err != nil {
		// Common locations in a Linglong host mount.
		for _, p := range []string{
			"/run/host/usr/bin/wl-paste",
			"/usr/bin/wl-paste",
			"/bin/wl-paste",
		} {
			if _, err := os.Stat(p); err == nil {
				wlPaste = p
				break
			}
		}
		if wlPaste == "" {
			return nil
		}
	}

	// Try each common image MIME type; wl-paste --list-types is available in
	// wl-clipboard 2.0+, but iterating the common set works everywhere and
	// is only a handful of short processes.
	mimeTypes := []string{
		"image/png",
		"image/jpeg",
		"image/webp",
		"image/bmp",
		"image/tiff",
		"image/gif",
	}
	for _, mime := range mimeTypes {
		cmd := exec.Command(wlPaste, "--type", mime, "--no-newline")
		cmd.Env = append(os.Environ(), "WAYLAND_DISPLAY="+os.Getenv("WAYLAND_DISPLAY"))
		out, err := cmd.Output()
		if err != nil || len(out) == 0 {
			continue
		}
		if len(out) > maxImageBytes {
			continue
		}
		// Validate the bytes match the requested MIME type.
		valid := false
		switch mime {
		case "image/png":
			valid = isValidPNG(out)
		case "image/jpeg":
			valid = isValidJPEG(out)
		case "image/webp":
			valid = isValidWebP(out)
		case "image/bmp":
			valid = isValidBMP(out)
		case "image/tiff":
			valid = isValidTIFF(out)
		case "image/gif":
			valid = isValidGIF(out)
		}
		if valid {
			return out
		}
	}
	return nil
}

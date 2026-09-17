// Package clipboard reads the current desktop clipboard image. It tries
// several strategies in order because modern desktops split clipboard ownership
// between X11 and Wayland and different apps advertise different target atoms:
//
//  1. X11 CLIPBOARD selection, TARGETS-aware: query what formats the owner
//     offers and pick the first raster one we can decode.
//  2. X11 PRIMARY selection, same TARGETS-aware path (some apps only put
//     screenshots on PRIMARY, or the user has "copy on select" behaviour).
//  3. wl-paste (Wayland clipboard) when the host compositor is Wayland and
//     X11 reading produced no image — XWayland clipboard bridges do not
//     always carry image formats across the protocol boundary.
//
// The X11 path uses a raw wire connection (no cgo, no external tools). It
// exists because the packaged WebKitGTK renderer never surfaces clipboard
// images to the page, while the shell process itself can reach the host
// display server.
//
// 文件分工：本文件是包入口（ReadImage 策略编排）与后端无关的图片格式校验；
// X11 通道见 x11.go，Wayland 通道见 wayland.go。
package clipboard

import (
	"encoding/binary"
	"errors"
)

// maxImageBytes 限制单次读取接受的图片体积。截图远低于这个上限，它的作用是挡住
// 病态 owner：对端可声明约 17 GB 的长度，按声明分配会一次应答就把壳进程打死。
const maxImageBytes = 20 << 20 // 20 MiB

var errSelectionEmpty = errors.New("clipboard has no supported image content")

// ReadImage returns the current clipboard image payload, or nil when no
// supported image is available. It tries X11 CLIPBOARD, X11 PRIMARY, and
// Wayland (wl-paste) in order, so screenshots from apps that only offer one
// path still work. Errors are returned only for unreachable displays or
// protocol failures — an empty clipboard is not an error.
//
// The search also follows text/uri-list entries: many screenshot tools save
// the capture to a file and put only the file URI on the clipboard, in which
// case we read the file from disk and return its bytes (provided the file
// extension and magic bytes both match a supported raster format).
func ReadImage() ([]byte, error) {
	// Strategy 1 & 2: X11 CLIPBOARD, then PRIMARY (direct bitmap).
	if data, err := readX11Images(); err == nil && data != nil {
		if isPlausibleImage(data) {
			return data, nil
		}
	}
	// Strategy 3: X11 text/uri-list → read the image file from disk.
	//    Many screenshot tools only put a file URI on CLIPBOARD after saving
	//    the capture, especially when the "save to file" workflow is used.
	if data := readX11UriListImage(); data != nil && isPlausibleImage(data) {
		return data, nil
	}
	// Strategy 4: Wayland clipboard via wl-paste (when available).
	if data := readWaylandImage(); data != nil && isPlausibleImage(data) {
		return data, nil
	}
	return nil, errSelectionEmpty
}

// isValidImage returns true if the byte slice starts with any supported
// raster format's magic number. Used for the uri-list file fallback.
func isValidImage(data []byte) bool {
	return isValidPNG(data) || isValidJPEG(data) || isValidWebP(data) ||
		isValidBMP(data) || isValidTIFF(data) || isValidGIF(data)
}

// minPlausibleImageBytes is the smallest image payload we accept as a real
// screenshot or copied picture. Tiny payloads (sub-kilobyte) are almost always
// 1×1 placeholders from chat apps, drag-and-drop drag-image ghosts, or
// broken X11 selection transfers, all of which would produce a useless blank
// image in the composer.
const minPlausibleImageBytes = 1 << 10 // 1 KiB

// minPlausibleDimension is the smallest width/height we accept in pixels.
// 1×1 to 4×4 images are always placeholders or corrupt.
const minPlausibleDimension = 5

// isPlausibleImage returns true when data looks like a real, usable image —
// not a 1×1 placeholder, a drag ghost, or a truncated transfer. It checks
// minimum byte size and, for formats whose header contains dimensions, a
// minimum width/height.
func isPlausibleImage(data []byte) bool {
	if len(data) < minPlausibleImageBytes {
		return false
	}
	if w, h, ok := pngDimensions(data); ok {
		return w >= minPlausibleDimension && h >= minPlausibleDimension
	}
	if w, h, ok := bmpDimensions(data); ok {
		return w >= minPlausibleDimension && h >= minPlausibleDimension
	}
	// For formats where we don't parse the header (JPEG, WebP, GIF, TIFF),
	// the byte-size floor is the only filter.
	return true
}

// pngDimensions returns the width/height from a PNG's IHDR chunk, or (0,0,false)
// when the header is too short or malformed.
func pngDimensions(data []byte) (uint32, uint32, bool) {
	if !isValidPNG(data) || len(data) < 24 {
		return 0, 0, false
	}
	// PNG structure: 8-byte signature | 4-byte length | 'IHDR' (4) | width (4) | height (4) | ...
	// IHDR starts at offset 8, width at 16, height at 20 (all big-endian).
	width := binary.BigEndian.Uint32(data[16:20])
	height := binary.BigEndian.Uint32(data[20:24])
	return width, height, true
}

// bmpDimensions returns the width/height from a BMP info header, or (0,0,false)
// when the header is too short.
func bmpDimensions(data []byte) (uint32, uint32, bool) {
	if !isValidBMP(data) || len(data) < 26 {
		return 0, 0, false
	}
	// BITMAPFILEHEADER: 14 bytes (signature + size + reserved + offset)
	// BITMAPINFOHEADER starts at offset 14; width at 18, height at 22 (little-endian int32).
	width := binary.LittleEndian.Uint32(data[18:22])
	height := binary.LittleEndian.Uint32(data[22:26])
	// Height can be negative (top-down DIB); take absolute value.
	if int32(height) < 0 {
		height = uint32(-int32(height))
	}
	return width, height, true
}

func isValidPNG(data []byte) bool {
	// 8-byte PNG signature: 89 50 4E 47 0D 0A 1A 0A
	return len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' &&
		data[4] == 0x0d && data[5] == 0x0a && data[6] == 0x1a && data[7] == 0x0a
}

func isValidJPEG(data []byte) bool {
	// SOI marker FF D8 followed by at least one FF marker
	return len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff
}

func isValidBMP(data []byte) bool {
	return len(data) >= 2 && data[0] == 'B' && data[1] == 'M'
}

func isValidTIFF(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// Little-endian (II) or big-endian (MM) + magic number 42
	return (data[0] == 'I' && data[1] == 'I' && data[2] == 0x2a && data[3] == 0x00) ||
		(data[0] == 'M' && data[1] == 'M' && data[2] == 0x00 && data[3] == 0x2a)
}

func isValidWebP(data []byte) bool {
	// RIFF....WEBP
	return len(data) >= 12 &&
		data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P'
}

func isValidGIF(data []byte) bool {
	return len(data) >= 6 &&
		data[0] == 'G' && data[1] == 'I' && data[2] == 'F' && data[3] == '8' &&
		(data[4] == '7' || data[4] == '9') && data[5] == 'a'
}

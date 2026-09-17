// Package clipboard reads the current desktop clipboard image. It tries
// several strategies in order because modern desktops split clipboard ownership
// between X11 and Wayland and different apps advertise different target atoms:
//
//  1. X11 CLIPBOARD selection, TARGETS-aware: query what formats the owner
//     offers and pick the first raster one we can decode.
//  2. X11 PRIMARY selection, same TARGETS-aware path (some apps only put
//     screenshots on PRIMARY, or the user has "copy on select" behaviour).
//  3. X11 CLIPBOARD text/uri-list: many screenshot tools save the capture to a
//     file and put only the file URI on the clipboard.
//  4. wl-paste (Wayland clipboard) image/* when the host compositor is Wayland
//     and X11 reading produced no image — XWayland clipboard bridges do not
//     always carry image formats across the protocol boundary.
//  5. wl-paste (Wayland clipboard) text/uri-list: copying an image file in a
//     Wayland file manager advertises no image/* type at all and is not bridged
//     to X11 either.
//
// The X11 path uses a raw wire connection (no cgo, no external tools). It
// exists because the packaged WebKitGTK renderer never surfaces clipboard
// images to the page, while the shell process itself can reach the host
// display server.
//
// 文件分工：本文件是包入口（ReadImage 策略编排）、后端无关的图片格式校验，以及
// 「URI 列表 → 本地图片文件」的共享解析——X11 与 Wayland 两侧的 text/uri-list 都走
// 它，放在任一后端文件里都会让另一侧依赖对方。X11 通道见 x11.go，Wayland 通道见
// wayland.go。
package clipboard

import (
	"encoding/binary"
	"errors"
	"os"
	"strings"
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
//
// 没有 DISPLAY 就跳过前三条 X11 路径：纯 Wayland 会话（合成器不带 XWayland）下
// 它们必然落空，先试只会白等几轮 socket 连接。
func ReadImage() ([]byte, error) {
	if os.Getenv("DISPLAY") != "" {
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
	}
	// Strategy 4: Wayland clipboard bitmap via wl-paste (when available).
	if data := readWaylandImage(); data != nil && isPlausibleImage(data) {
		return data, nil
	}
	// Strategy 5: Wayland clipboard text/uri-list → read the image file from
	//    disk. Copying an image file in a Wayland file manager puts only a URI
	//    list on the clipboard and is reachable no other way.
	if data := readWaylandUriListImage(); data != nil && isPlausibleImage(data) {
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

// uriToPath converts a file:// URI to a local filesystem path. Returns an
// empty string for non-file URIs or unparseable input.
//
// The path portion is percent-decoded: file managers (e.g. DDE) encode spaces
// and other reserved characters in file names as %XX (a space becomes %20), so
// a URI like file:///home/user/My%20Image%20(1).png must decode before the
// filesystem can be asked. Only %XX sequences are decoded; a literal '+' stays
// a '+', because file paths are not application/x-www-form-urlencoded.
func uriToPath(uri string) string {
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(uri, "file://") {
		return ""
	}
	path := strings.TrimPrefix(uri, "file://")
	// Strip or reject the authority part: file:///path has an empty host and
	// the path already starts with /; file://localhost/path carries the local
	// host explicitly; any other host is a remote file and is rejected.
	switch {
	case strings.HasPrefix(path, "/"):
		// empty host, local path — keep as-is
	case strings.HasPrefix(path, "localhost/"):
		path = strings.TrimPrefix(path, "localhost") // → /path
	default:
		return "" // non-local host (e.g. file://otherhost/path)
	}
	return percentDecode(path)
}

// percentDecode decodes RFC 3986 %XX escapes in s in place of writing them
// back. It leaves every other byte untouched, including '+' (which only means
// space in form encoding, not in file paths). Returns s unchanged when no
// escapes are present.
func percentDecode(s string) string {
	if !strings.ContainsRune(s, '%') {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, ok1 := hexVal(s[i+1])
			lo, ok2 := hexVal(s[i+2])
			if ok1 && ok2 {
				out = append(out, hi<<4|lo)
				i += 2
				continue
			}
		}
		out = append(out, s[i])
	}
	return string(out)
}

// hexVal decodes one hexadecimal digit.
func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// imageExtensions lists the file extensions we consider valid image inputs
// for the text/uri-list fallback.
var imageExtensions = []string{
	".png", ".jpg", ".jpeg", ".webp", ".bmp", ".tiff", ".tif", ".gif",
}

func isImageExtension(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range imageExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// readImageFileFromURIList 从一份 URI 列表文本里取出第一个可读图片文件的内容，没有
// 可用文件时返回 nil。X11 的 text/uri-list 属性与 Wayland 侧 wl-paste 取到的
// text/uri-list / x-special/gnome-copied-files 共用它：三者行格式一致——每行一个
// URI，空行与 # 开头的注释行跳过。gnome-copied-files 的首行是 copy/cut，uriToPath
// 判定它不是 file:// 后自然跳过，无需特判。
//
// 扩展名是文件管理器给的第一道筛子，不是可信输入：读进来的字节还要过 isValidImage
// 的魔数校验，否则一个改了后缀的文本文件会被当成图片交给渲染层。单个文件超过
// maxImageBytes 直接跳过，避免把整张超大图读进内存。
func readImageFileFromURIList(data []byte) []byte {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		path := uriToPath(line)
		if path == "" || !isImageExtension(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 || info.Size() > int64(maxImageBytes) {
			continue
		}
		fileData, err := os.ReadFile(path)
		if err != nil || len(fileData) == 0 {
			continue
		}
		if isValidImage(fileData) {
			return fileData
		}
	}
	return nil
}

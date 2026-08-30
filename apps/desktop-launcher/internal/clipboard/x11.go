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
package clipboard

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Limits applied to one image read. A screenshot is far below these; the caps
// protect the shell from a pathological owner.
const (
	maxImageBytes = 20 << 20 // 20 MiB
	readTimeout   = 6 * time.Second
)

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

// readX11Images tries CLIPBOARD then PRIMARY over one X connection and
// returns the first image found, or (nil, nil) when none is available.
func readX11Images() ([]byte, error) {
	x, err := dial()
	if err != nil {
		return nil, err
	}
	defer x.c.Close()
	selections := []string{"CLIPBOARD", "PRIMARY"}
	for _, selName := range selections {
		sel := x.mustAtom(selName)
		if sel == 0 {
			continue
		}
		owner, err := x.getSelectionOwner(sel)
		if err != nil || owner == 0 {
			continue
		}
		if data, err := x.readImageFromSelection(sel); err == nil && data != nil {
			return data, nil
		}
	}
	return nil, nil
}

// readX11UriListImage checks the CLIPBOARD selection for a text/uri-list
// payload and, when it contains a local file path whose extension and magic
// bytes match a supported image format, returns the file's bytes.
//
// Many screenshot tools (including deepin-screenshot in the "save to file"
// workflow) put only a file:// URI on the clipboard rather than the bitmap
// itself. Since the shell process runs inside the Linglong container, the
// target file must be under a path that the container can read (typically
// the user's home directory when mounted).
func readX11UriListImage() []byte {
	x, err := dial()
	if err != nil {
		return nil
	}
	defer x.c.Close()

	clip := x.mustAtom("CLIPBOARD")
	prop := x.mustAtom("_DSH_CLIP_URI")
	incr := x.mustAtom("INCR")
	uriList := x.mustAtom("text/uri-list")
	if clip == 0 || prop == 0 || incr == 0 || uriList == 0 {
		return nil
	}
	owner, err := x.getSelectionOwner(clip)
	if err != nil || owner == 0 {
		return nil
	}
	got, err := x.convert(clip, uriList, prop)
	if err != nil || got == 0 {
		return nil
	}
	_, data, err := x.getProperty(got, 0, incr)
	if err != nil || len(data) == 0 {
		return nil
	}

	// text/uri-list: one URI per line, lines starting with # are comments.
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		path := uriToPath(line)
		if path == "" {
			continue
		}
		if !isImageExtension(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > int64(maxImageBytes) || info.Size() == 0 {
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

// uriToPath converts a file:// URI to a local filesystem path. Returns an
// empty string for non-file URIs or unparseable input.
func uriToPath(uri string) string {
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(uri, "file://") {
		return ""
	}
	path := strings.TrimPrefix(uri, "file://")
	// file:///path → /path (three slashes for local files with no host)
	// file://localhost/path → /path (localhost is special-cased)
	if strings.HasPrefix(path, "//localhost/") {
		path = strings.TrimPrefix(path, "//localhost")
	} else if strings.HasPrefix(path, "//") {
		// Non-localhost host — not a local file.
		return ""
	}
	return path
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

// xconn is one X11 wire connection. Methods are strictly serial: each call
// sends one request and reads its reply (or the next event when the request
// has no reply).
type xconn struct {
	c   net.Conn
	seq uint16
	// root is the screen-0 root window; resourceBase mints requestor windows.
	root            uint32
	resourceBase    uint32
	requesterWindow uint32
	// atoms interned per connection (atom ids are connection-global in X11,
	// but re-interning keeps the code self-contained).
	atoms map[string]uint32
}

func dial() (*xconn, error) {
	conn, err := connectSocket()
	if err != nil {
		return nil, err
	}
	x := &xconn{c: conn}
	if err := x.setup(); err != nil {
		conn.Close()
		return nil, err
	}
	x.atoms = map[string]uint32{}
	if err := x.installWindow(); err != nil {
		conn.Close()
		return nil, err
	}
	return x, nil
}

var socketDial = func(network, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}

// connectSocket tries the abstract unix socket first (how the Linglong
// container reaches the shared X server), then the classic path, then TCP.
var connectSocket = func() (net.Conn, error) {
	paths := []string{"\x00/tmp/.X11-unix/X0", "/tmp/.X11-unix/X0"}
	for _, p := range paths {
		if c, err := socketDial("unix", p, 2*time.Second); err == nil {
			return c, nil
		}
	}
	if c, err := socketDial("tcp", "127.0.0.1:6000", 2*time.Second); err == nil {
		return c, nil
	}
	return nil, errors.New("clipboard: no X11 transport for DISPLAY=:0")
}

// setup performs the connection handshake. The server replies with the same
// byte order the client declared (LSBFirst here), which this code hard-codes.
func (x *xconn) setup() error {
	cookie := loadXauthCookie()
	// No-auth first (this host grants host access); fall back to the cookie.
	for _, auth := range [][]byte{nil, cookie} {
		var req []byte
		if auth == nil {
			req = append([]byte{'l', 0, 0x0b, 0, 0, 0}, 0, 0, 0, 0, 0, 0)
		} else {
			name := []byte("MIT-MAGIC-COOKIE-1")
			hdr := []byte{'l', 0, 0x0b, 0, 0, 0, 0, 0, 0, 0, 0, 0}
			binary.BigEndian.PutUint16(hdr[6:8], uint16(len(name)))
			binary.BigEndian.PutUint16(hdr[8:10], uint16(len(auth)))
			req = append(hdr, name...)
			req = append(req, auth...)
		}
		if _, err := x.c.Write(req); err != nil {
			return err
		}
		hdr := make([]byte, 8)
		if _, err := readFull(x.c, hdr); err != nil {
			continue
		}
		switch hdr[0] {
		case 0: // failed
			rl := int(binary.LittleEndian.Uint16(hdr[6:8]))
			reason := make([]byte, rl)
			_, _ = readFull(x.c, reason)
			continue
		case 1: // success
			length := int(binary.LittleEndian.Uint16(hdr[6:8]))
			body := make([]byte, length*4)
			if _, err := readFull(x.c, body); err != nil {
				return err
			}
			// Field offsets are LSBFirst like the reply header; root window is
			// the first field of screen 0.
			vendorLen := int(binary.LittleEndian.Uint16(body[16:18]))
			nFormats := int(body[21])
			off := 32 + ((vendorLen+3)/4)*4 + nFormats*8
			x.root = binary.LittleEndian.Uint32(body[off : off+4])
			x.resourceBase = binary.LittleEndian.Uint32(body[4:8])
			return nil
		}
	}
	return errors.New("clipboard: X setup failed for all auth attempts")
}

func readFull(c net.Conn, buf []byte) (int, error) {
	n, err := readFullUntil(c, buf)
	return n, err
}

// recv reads exactly len(buf) bytes, applying the connection deadline.
func readFullUntil(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// send writes one request: 4-byte header (opcode, pad, length in words) then
// the 4-byte-aligned payload. length counts the whole request including the
// header.
func (x *xconn) send(opcode byte, payload []byte, pad ...byte) error {
	padded := payload
	if r := len(payload) % 4; r != 0 {
		padded = append(append([]byte{}, payload...), make([]byte, 4-r)...)
	}
	words := uint16(1 + len(padded)/4)
	hdr := []byte{opcode, 0, 0, 0}
	if len(pad) > 0 {
		hdr[1] = pad[0]
	}
	binary.LittleEndian.PutUint16(hdr[2:4], words)
	if _, err := x.c.Write(hdr); err != nil {
		return err
	}
	_, err := x.c.Write(padded)
	return err
}

// readReply reads one 32-byte reply/event/error packet, then the payload for
// replies. X errors are returned as errors.
func (x *xconn) readReply() ([]byte, []byte, error) {
	hdr := make([]byte, 32)
	if _, err := readFull(x.c, hdr); err != nil {
		return nil, nil, err
	}
	switch hdr[0] {
	case 0:
		return hdr, nil, fmt.Errorf("clipboard: X error code=%d major=%d minor=%d bad=0x%x seq=%d",
			hdr[1], binary.LittleEndian.Uint16(hdr[10:12]), binary.LittleEndian.Uint16(hdr[8:10]),
			binary.LittleEndian.Uint32(hdr[4:8]), binary.LittleEndian.Uint16(hdr[2:4]))
	case 1:
		// The reply header's length field is a CARD32 (bytes 4-7) counting
		// 4-byte words. Reading only the low 16 bits truncated every reply
		// larger than 65535 words (262,140 bytes): large clipboard images
		// (e.g. pasted screenshots) were silently cut, yielding a corrupt
		// image in the composer.
		length := int(binary.LittleEndian.Uint32(hdr[4:8]))
		if length == 0 {
			return hdr, nil, nil
		}
		extra := make([]byte, length*4)
		if _, err := readFull(x.c, extra); err != nil {
			return nil, nil, err
		}
		return hdr, extra, nil
	default:
		// events carry no extra payload
		return hdr, nil, nil
	}
}

// readReplySkipEvents reads the next reply or error, discarding any events
// that arrive first (replies are guaranteed to be ordered after the request
// that produced them, but unrelated events may precede them in the stream).
func (x *xconn) readReplySkipEvents() ([]byte, []byte, error) {
	for {
		hdr, extra, err := x.readReply()
		if err != nil {
			return nil, nil, err
		}
		if hdr[0] == 0 || hdr[0] == 1 {
			return hdr, extra, nil
		}
	}
}

func (x *xconn) intern(name string) (uint32, error) {
	if a, ok := x.atoms[name]; ok {
		return a, nil
	}
	payload := make([]byte, 4+len(name))
	binary.LittleEndian.PutUint16(payload[0:2], uint16(len(name)))
	copy(payload[4:], name)
	if err := x.send(16, payload); err != nil { // InternAtom
		return 0, err
	}
	hdr, _, err := x.readReplySkipEvents()
	if err != nil {
		return 0, err
	}
	a := binary.LittleEndian.Uint32(hdr[8:12])
	x.atoms[name] = a
	return a, nil
}

// getSelectionOwner returns the owner window for a selection (0 = none).
func (x *xconn) getSelectionOwner(sel uint32) (uint32, error) {
	// The request body is a single 4-byte selection atom (no window field).
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, sel)
	if err := x.send(23, payload); err != nil { // GetSelectionOwner
		return 0, err
	}
	hdr, _, err := x.readReplySkipEvents()
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(hdr[8:12]), nil
}

// convert starts an XConvertSelection for target into prop and waits for the
// SelectionNotify event, returning the resulting property id (0 = refused).
func (x *xconn) convert(sel, target, prop uint32) (uint32, error) {
	payload := make([]byte, 20)
	binary.LittleEndian.PutUint32(payload[0:4], x.nextWindow())
	binary.LittleEndian.PutUint32(payload[4:8], sel)
	binary.LittleEndian.PutUint32(payload[8:12], target)
	binary.LittleEndian.PutUint32(payload[12:16], prop)
	if err := x.send(24, payload); err != nil { // ConvertSelection
		return 0, err
	}
	if err := x.setDeadline(readTimeout); err != nil {
		return 0, err
	}
	for {
		hdr, _, err := x.readReply()
		if err != nil {
			return 0, err
		}
		switch hdr[0] {
		case 31: // SelectionNotify (standard)
			return binary.LittleEndian.Uint32(hdr[24:28]), nil
		case 159: // Linglong X bridge SelectionNotify (property at offset 20)
			return binary.LittleEndian.Uint32(hdr[20:24]), nil
		}
		// Ignore unrelated events (e.g. property changes) until the notify.
	}
}

// getProperty reads the whole value of one property; INCR transfers are
// followed to completion. incr must be the pre-interned INCR atom so the
// incremental loop never issues a request of its own (replies stay aligned).
func (x *xconn) getProperty(prop, expectedType, incr uint32) (uint32, []byte, error) {
	for {
		ptype, after, data, err := x.getPropertyOnce(prop, expectedType)
		if err != nil {
			return 0, nil, err
		}
		if ptype != incr || after == 0 {
			return ptype, data, nil
		}
		// INCR: wait for PropertyNotify, then re-read the growing property.
		if err := x.setDeadline(readTimeout); err != nil {
			return 0, nil, err
		}
		chunks := [][]byte{data}
		for {
			hdr, _, err := x.readReply()
			if err != nil {
				return 0, nil, err
			}
			if hdr[0] != 28 { // PropertyNotify
				continue
			}
			// PropertyNotify: window at bytes 8-11, property atom at 12-15.
			if binary.LittleEndian.Uint32(hdr[12:16]) != prop {
				continue
			}
			_, after, chunk, err := x.getPropertyOnce(prop, expectedType)
			if err != nil {
				return 0, nil, err
			}
			if len(chunk) > 0 {
				chunks = append(chunks, chunk)
			}
			if after == 0 {
				return expectedType, bytes.Join(chunks, nil), nil
			}
		}
	}
}

func (x *xconn) getPropertyOnce(prop, expectedType uint32) (uint32, uint32, []byte, error) {
	payload := make([]byte, 20)
	binary.LittleEndian.PutUint32(payload[0:4], x.requesterWindow)
	binary.LittleEndian.PutUint32(payload[4:8], prop)
	binary.LittleEndian.PutUint32(payload[8:12], expectedType) // 0 = any
	binary.LittleEndian.PutUint32(payload[12:16], 0)
	binary.LittleEndian.PutUint32(payload[16:20], 1<<20)
	// GetProperty: the request header's second byte is the delete flag. X11
	// INCR protocol requires the client to delete each segment it reads;
	// without it, later reads return the accumulated property and the INCR
	// assembly below duplicates every chunk, corrupting the image bytes.
	if err := x.send(20, payload, 1); err != nil { // GetProperty (delete = True)
		return 0, 0, nil, err
	}
	hdr, extra, err := x.readReplySkipEvents()
	if err != nil {
		return 0, 0, nil, err
	}
	format := int(hdr[1])
	ptype := binary.LittleEndian.Uint32(hdr[8:12])
	after := binary.LittleEndian.Uint32(hdr[12:16])
	nitems := binary.LittleEndian.Uint32(hdr[16:20])
	data := extra
	if format > 0 {
		n := int(nitems) * (format / 8)
		if n < len(data) {
			data = data[:n]
		}
	}
	return ptype, after, data, nil
}

func (x *xconn) mustAtom(name string) uint32 {
	a, err := x.intern(name)
	if err != nil {
		return 0
	}
	return a
}

func (x *xconn) setDeadline(d time.Duration) error { return x.c.SetReadDeadline(time.Now().Add(d)) }

// installWindow creates one tiny requestor window used as the ConvertSelection
// destination (the requestor must exist server-side). The window id derives
// from the resource base reported during setup.
//
// The window is intentionally left unmapped: X11 selection transfer only
// requires the requestor to exist as a resource, not to be visible. Mapping it
// (a mapped InputOutput window at screen origin (0,0)) made a small window
// flash in the top-left corner of the screen on every clipboard read.
func (x *xconn) installWindow() error {
	wid := x.resourceBase + 1
	payload := make([]byte, 28)
	binary.LittleEndian.PutUint32(payload[0:4], wid)
	binary.LittleEndian.PutUint32(payload[4:8], x.root)
	// CreateWindow body: window(4) parent(4) x(2) y(2) width(2) height(2)
	// border(2) class(2) visual(4) mask(4); depth lives in the request header.
	binary.LittleEndian.PutUint16(payload[12:14], 8) // width
	binary.LittleEndian.PutUint16(payload[14:16], 8) // height
	binary.LittleEndian.PutUint16(payload[18:20], 1) // class = InputOutput
	if err := x.send(1, payload); err != nil {       // CreateWindow (depth 0)
		return err
	}
	// Deliberately no MapWindow: the requestor stays unmapped (see above).
	x.requesterWindow = wid
	return nil
}

// nextWindow returns the installed requestor window.
func (x *xconn) nextWindow() uint32 { return x.requesterWindow }

// imageFormats lists every X11 selection target we can decode, in priority
// order. Some apps advertise non-standard names (Qt/GIMP/KDE flavours); we
// match them all and validate the bytes independently.
var imageFormats = []struct {
	atom  string
	valid func([]byte) bool
}{
	{"image/png", isValidPNG},
	{"image/jpeg", isValidJPEG},
	{"image/webp", isValidWebP},
	{"image/bmp", isValidBMP},
	{"image/tiff", isValidTIFF},
	{"image/gif", isValidGIF},
	// Non-standard but common target names advertised by Qt, GTK, and image
	// viewers. Some apps use the x- prefix, others use the vendor prefix.
	{"image/x-png", isValidPNG},
	{"image/x-jpeg", isValidJPEG},
	{"image/x-bmp", isValidBMP},
	{"image/x-tiff", isValidTIFF},
	{"image/x-webp", isValidWebP},
	{"application/x-qt-image", isValidPNG}, // Qt sometimes wraps PNG here
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

// readImageFromSelection queries the selection owner's TARGETS first, then
// tries each image format we recognise in priority order, returning the
// first one whose bytes pass a magic-number check.
func (x *xconn) readImageFromSelection(sel uint32) ([]byte, error) {
	targets := x.mustAtom("TARGETS")
	prop := x.mustAtom("_DSH_CLIP")
	incr := x.mustAtom("INCR")
	if prop == 0 || incr == 0 {
		return nil, errors.New("clipboard: X11 intern failed")
	}

	// Phase 1: ask the owner what targets it offers. If we can list them we
	// can short-circuit to a supported format instead of trying every atom.
	var available []uint32
	if targets != 0 {
		got, err := x.convert(sel, targets, prop)
		if err == nil && got != 0 {
			_, data, err := x.getProperty(got, 0, incr)
			if err == nil && len(data) > 0 {
				available = parseAtomList(data)
			}
		}
	}

	// Phase 2: try each known image format. When we have a TARGETS list we
	// only attempt targets the owner actually advertises; otherwise we try
	// every known format as a best-effort fallback.
	for _, f := range imageFormats {
		target := x.mustAtom(f.atom)
		if target == 0 {
			continue
		}
		if len(available) > 0 && !containsAtom(available, target) {
			continue
		}
		got, err := x.convert(sel, target, prop)
		if err != nil {
			continue
		}
		if got == 0 {
			continue
		}
		_, data, err := x.getProperty(got, 0, incr)
		if err != nil {
			continue
		}
		if len(data) == 0 || !f.valid(data) {
			continue
		}
		if len(data) > maxImageBytes {
			return nil, fmt.Errorf("clipboard: image exceeds %d bytes", maxImageBytes)
		}
		return data, nil
	}
	return nil, nil
}

// parseAtomList decodes an array-of-atoms property payload (format 32).
func parseAtomList(data []byte) []uint32 {
	if len(data) < 4 || len(data)%4 != 0 {
		return nil
	}
	atoms := make([]uint32, 0, len(data)/4)
	for i := 0; i+4 <= len(data); i += 4 {
		atoms = append(atoms, binary.LittleEndian.Uint32(data[i:i+4]))
	}
	return atoms
}

func containsAtom(list []uint32, target uint32) bool {
	for _, a := range list {
		if a == target {
			return true
		}
	}
	return false
}

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

// loadXauthCookie parses the MIT-MAGIC-COOKIE-1 entry from $XAUTHORITY or
// ~/.Xauthority. A missing cookie file is not an error (host access may be
// granted without one).
func loadXauthCookie() []byte {
	path := os.Getenv("XAUTHORITY")
	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, ".Xauthority")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	i := 0
	for i < len(data) {
		need := func(n int) bool { return i+n <= len(data) }
		if !need(2) {
			break
		}
		family := int(binary.BigEndian.Uint16(data[i:]))
		i += 2
		if !need(2) {
			break
		}
		alen := int(binary.BigEndian.Uint16(data[i:]))
		i += 2 + alen
		if !need(2) {
			break
		}
		numlen := int(binary.BigEndian.Uint16(data[i:]))
		i += 2 + numlen
		if !need(2) {
			break
		}
		namelen := int(binary.BigEndian.Uint16(data[i:]))
		i += 2
		if !need(namelen) {
			break
		}
		name := data[i : i+namelen]
		i += namelen
		if !need(2) {
			break
		}
		datalen := int(binary.BigEndian.Uint16(data[i:]))
		i += 2
		if !need(datalen) {
			break
		}
		dd := data[i : i+datalen]
		i += datalen
		if family == 0 || family == 256 { // Local or Wild
			if bytes.Equal(name, []byte("MIT-MAGIC-COOKIE-1")) && len(dd) > 0 {
				return dd
			}
		}
	}
	return nil
}

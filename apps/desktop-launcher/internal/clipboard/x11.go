package clipboard

// 本文件实现 X11 通道：一条裸协议连接（无 cgo、无外部工具），覆盖
// CLIPBOARD/PRIMARY 选区取图与 text/uri-list 指向的本地文件。

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// readTimeout 是单次 X11 请求的读超时；INCR 传输的每一轮重新计时。
const readTimeout = 6 * time.Second

const (
	// setupFixedLen 是 setup 成功回复固定部分的字节数（8 字节回复头 + 24 字节
	// 字段）；vendor 名与 format 列表紧随其后，长度都由对端给出。
	setupFixedLen = 32
	// maxReplyBytes 限制单次回复的附加数据。线上长度是 CARD32，对端可声明约
	// 17 GB，按它分配会让一次应答就把进程打死；上限给 maxImageBytes 留三倍余量，
	// 够装整屏位图，又不至于被恶意长度撑着。
	maxReplyBytes = 3 * maxImageBytes
)

// CreateWindow 的 CW 属性位与事件掩码位是两套独立编号，混用会让服务端回 BadValue：
// value-mask 里要置的是 cwEventMask，value-list 里放的才是事件掩码本身。
const (
	cwEventMask        = 1 << 11 // CreateWindow value-mask 的 CWEventMask 位
	propertyChangeMask = 1 << 22 // 事件掩码：PropertyChangeMask
)

// maxINCRChunks 限制一次 INCR 传输的分块轮数。每轮重新计时 readTimeout，
// 所以轮数就是这次传输总时长的上界：没有它，一个每次只写 1 字节的 owner 能让我们
// 在 maxImageBytes 之内不断续时，把一次粘贴拖成无限等待。
const maxINCRChunks = 1 << 16

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
	data, err := x.getProperty(got, 0, incr)
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
		// setup 的认证重试可能已经换过连接，关掉当前那条而不是最初那条。
		x.close()
		return nil, err
	}
	x.atoms = map[string]uint32{}
	if err := x.installWindow(); err != nil {
		x.close()
		return nil, err
	}
	return x, nil
}

// reconnect 关闭当前连接并新建一条；setup 的认证重试与关闭路径共用它，
// 使"当前连接是哪一条"只有一处维护点。
func (x *xconn) reconnect() error {
	x.close()
	conn, err := connectSocket()
	if err != nil {
		return err
	}
	x.c = conn
	return nil
}

// close 关闭当前连接；已关闭或尚未建立时为空操作。
func (x *xconn) close() {
	if x.c == nil {
		return
	}
	_ = x.c.Close()
	x.c = nil
}

var socketDial = func(network, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}

// x11Transport 描述一条通往 X server 的候选传输路径。
type x11Transport struct {
	network string // "unix" 或 "tcp"
	addr    string // unix socket 路径（抽象 socket 以 \x00 开头）或 host:port
}

// x11Transports 按 DISPLAY 推导候选路径，返回值顺序即尝试顺序。
//
// display number 必须来自环境，不能写死：X11 会话通常是 :0，Wayland 会话经
// XWayland 通常是 :1。写死 X0 会让 Wayland 会话连到无关的 X server——轻则
// 认证被拒，重则读到另一个 server 的空剪贴板而看不出错。
//
// 容器里抽象 socket 优先：Linglong 与宿主共享 network namespace，抽象 socket
// 不经文件系统挂载即可达；文件路径是 linyaps 按 DISPLAY 显式 bind 的那份，作
// 次选；TCP 仅作兜底。
func x11Transports(display string) []x11Transport {
	if display == "" {
		return nil
	}
	// DISPLAY 本身就是 unix socket 路径时按该路径连，不再拼 /tmp/.X11-unix。
	if strings.HasPrefix(display, "/") {
		return []x11Transport{{"unix", "\x00" + display}, {"unix", display}}
	}
	host, displayNo, ok := splitDisplay(display)
	if !ok {
		return nil
	}
	if host != "" {
		return []x11Transport{{"tcp", net.JoinHostPort(host, strconv.Itoa(6000+displayNo))}}
	}
	return []x11Transport{
		{"unix", "\x00/tmp/.X11-unix/X" + strconv.Itoa(displayNo)},
		{"unix", "/tmp/.X11-unix/X" + strconv.Itoa(displayNo)},
		{"tcp", "127.0.0.1:" + strconv.Itoa(6000+displayNo)},
	}
}

// splitDisplay 解析 X11 的 [protocol/][host]:display[.screen]，返回主机名
// （本地为空）与 display number。
//
// 只认这一个语法：解析失败即返回 ok=false，调用方据此放弃 X11 通道，而不是
// 退回"猜一个 display"。容器内该值由 linyaps 从宿主透传，读不准时宁可走
// Wayland 通道，也不能连到另一个 X server 上。
func splitDisplay(display string) (host string, displayNo int, ok bool) {
	rest := display
	proto := ""
	if i := strings.Index(rest, "/"); i >= 0 {
		proto, rest = rest[:i], rest[i+1:]
	}
	i := strings.LastIndex(rest, ":")
	if i < 0 {
		return "", 0, false
	}
	host, rest = rest[:i], rest[i+1:]
	if j := strings.Index(rest, "."); j >= 0 {
		rest = rest[:j] // 丢掉 .screen：同一 server 的所有 screen 共用一个 socket
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return "", 0, false
	}
	if host != "" && (proto == "unix" || proto == "local") {
		host = ""
	}
	// "unix" 与 "local" 出现在主机位（unix:0）时同样表示本地 unix socket；
	// localhost 是普通主机名，仍走 TCP。
	if host == "unix" || host == "local" {
		host = ""
	}
	return host, n, true
}

// connectSocket 按 DISPLAY 逐个尝试候选路径，返回第一个连上的连接。
// 认证在 setup 里做；这里只负责选到与当前会话匹配的 X server，避免连上另一个
// display 之后才在认证上失败。
var connectSocket = func() (net.Conn, error) {
	display := os.Getenv("DISPLAY")
	for _, t := range x11Transports(display) {
		if c, err := socketDial(t.network, t.addr, 2*time.Second); err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("clipboard: no X11 transport for DISPLAY=%q", display)
}

// pad4 把 b 补齐到 4 字节边界。X11 的 connection setup 请求里，auth name 与
// auth data 之后各带 p = pad(n) 个未使用字节，服务端按补齐后的长度读取请求。
// 已对齐时原样返回，不做多余分配。
func pad4(b []byte) []byte {
	if r := len(b) % 4; r != 0 {
		out := make([]byte, len(b)+4-r)
		copy(out, b)
		return out
	}
	return b
}

// setup performs the connection handshake. The server replies with the same
// byte order the client declared (LSBFirst here), which this code hard-codes:
// every length field written below is little-endian.
func (x *xconn) setup() error {
	cookie := loadXauthCookie()
	// No-auth first (this host grants host access); fall back to the cookie.
	// 没有 cookie 时不排第二次尝试：重发一份一模一样的无认证请求不可能有不同的结果。
	attempts := [][]byte{nil}
	if len(cookie) > 0 {
		attempts = append(attempts, cookie)
	}
	for i, auth := range attempts {
		// X 服务端在 Failed 回复之后关闭连接，所以重试必须换一条新连接：复用同一条
		// 已关闭的连接时，带 cookie 的这次尝试连请求都发不出去，认证永远失败——
		// 这正是需要 Xauthority 的主机上"粘贴截图毫无反应"的直接原因。
		if i > 0 {
			if err := x.reconnect(); err != nil {
				return err
			}
		}
		var req []byte
		if auth == nil {
			req = append([]byte{'l', 0, 0x0b, 0, 0, 0}, 0, 0, 0, 0, 0, 0)
		} else {
			name := []byte("MIT-MAGIC-COOKIE-1")
			hdr := []byte{'l', 0, 0x0b, 0, 0, 0, 0, 0, 0, 0, 0, 0}
			// 请求头声明 'l'（LSBFirst），协议要求 auth name/data 长度按客户端
			// 字节序编码。写成大端时服务端读到的是 0x1200 / 0x2000 这类长度，
			// 认证必然被拒。
			binary.LittleEndian.PutUint16(hdr[6:8], uint16(len(name)))
			binary.LittleEndian.PutUint16(hdr[8:10], uint16(len(auth)))
			// name 与 data 各自补齐到 4 字节边界（协议记作 p = pad(n)）。漏掉补齐
			// 时请求比服务端期望的短，它不会回 Failed 而是继续等那几个字节，于是
			// 下面的读取永久阻塞——表现为粘贴后毫无反应且永不返回。
			req = append(hdr, pad4(name)...)
			req = append(req, pad4(auth)...)
		}
		if _, err := x.c.Write(req); err != nil {
			return err
		}
		// setup 是唯一不带自身超时的收发：服务端在请求不完整时保持沉默，
		// 没有超时就会挂死整个剪贴板读取。
		if err := x.setDeadline(readTimeout); err != nil {
			return err
		}
		hdr := make([]byte, 8)
		if _, err := readFull(x.c, hdr); err != nil {
			continue
		}
		switch hdr[0] {
		case 0: // failed
			// 附加数据长度在线上以 4 字节为单位，不是字节数：按字节读会少读四分之三，
			// 把剩余数据留在连接里。这里只是丢弃内容，但要读干净才不至于让残留
			// 数据影响对后续回复的判断。
			rl := int(binary.LittleEndian.Uint16(hdr[6:8])) * 4
			if rl > 0 {
				reason := make([]byte, rl)
				_, _ = readFull(x.c, reason)
			}
			continue
		case 1: // success
			length := int(binary.LittleEndian.Uint16(hdr[6:8]))
			body := make([]byte, length*4)
			if _, err := readFull(x.c, body); err != nil {
				return err
			}
			// Field offsets are LSBFirst like the reply header; root window is
			// the first field of screen 0.
			//
			// 固定部分是 32 字节，vendor 名与 format 列表的长度都由对端给出：
			// 必须先按最小长度与算出的偏移校验，否则 body[off:off+4] 会因越界
			// 直接 panic（审计 S3）。这里的 length 来自 uint16，分配本身有界。
			if len(body) < setupFixedLen {
				return fmt.Errorf("clipboard: X setup reply too short: %d bytes", len(body))
			}
			vendorLen := int(binary.LittleEndian.Uint16(body[16:18]))
			nFormats := int(body[21])
			off := setupFixedLen + ((vendorLen+3)/4)*4 + nFormats*8
			if off+4 > len(body) {
				return fmt.Errorf("clipboard: X setup reply truncated: vendor=%d formats=%d bytes=%d",
					vendorLen, nFormats, len(body))
			}
			x.root = binary.LittleEndian.Uint32(body[off : off+4])
			x.resourceBase = binary.LittleEndian.Uint32(body[4:8])
			// 撤掉 setup 的读超时：后续每个请求各自计时，留着这个已经开始的
			// deadline 会让紧随其后的读取提前超时。
			return x.c.SetReadDeadline(time.Time{})
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
		// 长度直接来自对端：CARD32 可声明约 17 GB，按它 make 会让一次应答打死
		// 进程；先与上限比较再乘 4，顺带避开 32 位平台上 length*4 溢出成负数
		// 导致的 makeslice panic（审计 S3）。
		if length > maxReplyBytes/4 {
			return nil, nil, fmt.Errorf("clipboard: X reply too large: %d words", length)
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
		// SelectionNotify 的线上布局是 type(1) pad(1) sequence(2) time(4)
		// requestor(4) selection(4) target(4) property(4)，也就是 property 落在
		// 偏移 20。标准事件码是 31，玲珑 X 桥把它重写成 159，property 偏移不变；
		// 若按偏移 24 读取，拿到的是保留字段的全零，于是每一次转换都被判成
		// “owner 拒绝”，整条 X11 读取路径静默失效。
		switch hdr[0] {
		case 31, 159: // SelectionNotify（标准 / 玲珑 X 桥）
			return binary.LittleEndian.Uint32(hdr[20:24]), nil
		}
		// Ignore unrelated events (e.g. property changes) until the notify.
	}
}

// getProperty 读取一个属性的完整值，INCR 分块传输会被跟到底。
// incr 必须是调用方预先 intern 好的 INCR 原子：分块循环内部不能再发 InternAtom，
// 否则自己插入的请求会打乱请求与回复的对应关系。
//
// @param prop - 属性原子，即 ConvertSelection 指定的落点。
// @param expectedType - 期望的属性类型，0 表示任意。
// @param incr - 预先 intern 的 INCR 原子，用于识别分块传输的起始回复。
// @returns 属性的完整字节值；协议失败、超出体积上限或分块轮数用尽时返回错误。
func (x *xconn) getProperty(prop, expectedType, incr uint32) ([]byte, error) {
	ptype, _, data, err := x.getPropertyOnce(prop, expectedType)
	if err != nil {
		return nil, err
	}
	if ptype != incr {
		return data, nil
	}
	// 回复类型是 INCR 就进入分块传输：这次读到的 4 字节是数据总量而不是数据本身，
	// 载荷随后由 owner 逐块写入属性，每块都伴随一个 PropertyNotify 事件，
	// 读到 0 字节的属性即传输结束。
	//
	// 判定只看类型，不能看 bytes-after：INCR 标记本身只有 4 字节，一次读尽后
	// bytes-after 就是 0，把 0 当作“已经读完”会把这个标记当成图像数据返回，
	// 于是所有走 INCR 的剪贴板内容（截图工具的位图、剪贴板管理器转存的位图）
	// 都变成 4 字节垃圾，PNG 魔数校验失败，粘贴表现为毫无反应。
	if len(data) < 4 {
		return nil, errors.New("clipboard: INCR marker without a size")
	}
	if total := binary.LittleEndian.Uint32(data[:4]); total > maxImageBytes {
		return nil, fmt.Errorf("clipboard: selection exceeds %d bytes", maxImageBytes)
	}
	chunks := make([][]byte, 0, 8)
	size := 0
	for round := 0; round < maxINCRChunks; round++ {
		// 每一块都重新计时：慢但确实在推进的 owner 应该传完，卡住的 owner 也必须在
		// 有限的轮数内被放弃。
		if err := x.setDeadline(readTimeout); err != nil {
			return nil, err
		}
		if err := x.waitPropertyNotify(prop); err != nil {
			return nil, err
		}
		_, _, chunk, err := x.getPropertyOnce(prop, expectedType)
		if err != nil {
			return nil, err
		}
		if len(chunk) == 0 {
			return bytes.Join(chunks, nil), nil
		}
		if size += len(chunk); size > maxImageBytes {
			return nil, fmt.Errorf("clipboard: selection exceeds %d bytes", maxImageBytes)
		}
		chunks = append(chunks, chunk)
	}
	return nil, errors.New("clipboard: INCR transfer did not finish")
}

// waitPropertyNotify 读到属于 prop 的 PropertyNotify 为止，丢弃其间无关事件。
//
// 事件码 28 的线上布局是 type(1) pad(1) sequence(2) window(4) atom(4) time(4)
// state(1)：window 在偏移 4、atom 在偏移 8。若按 window 8 / atom 12 读取，
// 比对的是 time 字段，永远匹配不上 prop，INCR 会在每一块之前空等到超时。
func (x *xconn) waitPropertyNotify(prop uint32) error {
	for {
		hdr, _, err := x.readReply()
		if err != nil {
			return err
		}
		if hdr[0] != 28 { // PropertyNotify
			continue
		}
		if binary.LittleEndian.Uint32(hdr[8:12]) != prop {
			continue
		}
		return nil
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

// installWindow 创建一个 8×8 的请求方窗口，作为 ConvertSelection 的属性落点
// （请求方必须是服务端真实存在的资源）。窗口 id 取自 setup 汇报的资源基址。
//
// CreateWindow 的 value-list 只放一项：cwEventMask → propertyChangeMask。
// INCR 分块传输靠服务端的 PropertyNotify 事件驱动，未选该掩码时事件不会投递，
// 分块读取只能空等到超时（见 getProperty / waitPropertyNotify）。
//
// The window is intentionally left unmapped: X11 selection transfer only
// requires the requestor to exist as a resource, not to be visible. Mapping it
// (a mapped InputOutput window at screen origin (0,0)) made a small window
// flash in the top-left corner of the screen on every clipboard read.
func (x *xconn) installWindow() error {
	wid := x.resourceBase + 1
	payload := make([]byte, 32)
	binary.LittleEndian.PutUint32(payload[0:4], wid)
	binary.LittleEndian.PutUint32(payload[4:8], x.root)
	// CreateWindow body: window(4) parent(4) x(2) y(2) width(2) height(2)
	// border(2) class(2) visual(4) mask(4) value-list; depth lives in the
	// request header.
	binary.LittleEndian.PutUint16(payload[12:14], 8) // width
	binary.LittleEndian.PutUint16(payload[14:16], 8) // height
	binary.LittleEndian.PutUint16(payload[18:20], 1) // class = InputOutput
	binary.LittleEndian.PutUint32(payload[24:28], cwEventMask)
	binary.LittleEndian.PutUint32(payload[28:32], propertyChangeMask)
	if err := x.send(1, payload); err != nil { // CreateWindow (depth 0)
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
			data, err := x.getProperty(got, 0, incr)
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
		data, err := x.getProperty(got, 0, incr)
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

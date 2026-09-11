package clipboard

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestReadImageSimple 覆盖标准事件码（31）下的一次性读取：owner 直接把整张 PNG
// 放进属性，不走 INCR。
func TestReadImageSimple(t *testing.T) {
	png := makeTestPNG(64, 64)
	server := newFakeXServer(t, fakeServerOptions{png: png, notifyCode: 31, advertise: true})
	data, err := ReadImage()
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	server.wait()
	if !bytes.Equal(data, png) {
		t.Fatalf("got %d bytes, want %d", len(data), len(png))
	}
}

// TestReadImageIncremental 是这次修复的核心回归：owner 按 ICCCM 用 INCR 分块投递位图。
//
// 修复前三条缺陷叠加会让它读成 4 字节垃圾：INCR 标记（4 字节总长度、bytes-after 为 0）
// 被当成图像数据；请求方窗口没选 PropertyChangeMask 收不到 PropertyNotify；
// 即便收到通知，property atom 也读成了 time 字段。表现就是截图粘贴毫无反应。
func TestReadImageIncremental(t *testing.T) {
	png := makeTestPNG(64, 64)
	server := newFakeXServer(t, fakeServerOptions{
		png: png, incr: true, incrChunks: 3, notifyCode: 31, advertise: true,
	})
	data, err := ReadImage()
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	server.wait()
	if !bytes.Equal(data, png) {
		t.Fatalf("INCR 拼装得到 %d 字节，期望 %d 字节", len(data), len(png))
	}
	if want := 1 + 3 + 1; server.imageReads != want {
		t.Fatalf("image/png 上的读取次数 = %d，期望 %d（标记 + 分块 + 结束块）", server.imageReads, want)
	}
}

// TestReadImageIncrementalBridgeNotifyCode 用玲珑 X 桥重写的 159 事件码重复分块读取。
// 桥只改事件码，property 偏移与标准事件相同，两条路径都必须可用。
func TestReadImageIncrementalBridgeNotifyCode(t *testing.T) {
	png := makeTestPNG(48, 48)
	server := newFakeXServer(t, fakeServerOptions{
		png: png, incr: true, incrChunks: 2, notifyCode: 159, advertise: true,
	})
	data, err := ReadImage()
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	server.wait()
	if !bytes.Equal(data, png) {
		t.Fatalf("桥事件码下 INCR 拼装得到 %d 字节，期望 %d 字节", len(data), len(png))
	}
}

// TestReadImageUnadvertisedTargets 保留 TARGETS 列表为空时的退化路径：
// 客户端逐个尝试所有已知图片格式，而不是直接放弃。
func TestReadImageUnadvertisedTargets(t *testing.T) {
	png := makeTestPNG(64, 64)
	server := newFakeXServer(t, fakeServerOptions{png: png, notifyCode: 31})
	data, err := ReadImage()
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	server.wait()
	if !bytes.Equal(data, png) {
		t.Fatalf("got %d bytes, want %d", len(data), len(png))
	}
}

// TestReadImageIncrOversizeMarker 验证 INCR 标记声明的总量超限时立即放弃：
// 只在标记那一次读取上花代价，不进入分块循环。
func TestReadImageIncrOversizeMarker(t *testing.T) {
	server := newFakeXServer(t, fakeServerOptions{
		png: makeTestPNG(64, 64), incr: true, incrChunks: 3, notifyCode: 31,
		advertise: true, markerTotal: maxImageBytes + 1,
	})
	if _, err := ReadImage(); err != errSelectionEmpty {
		t.Fatalf("want errSelectionEmpty, got %v", err)
	}
	server.wait()
	if server.imageReads != 1 {
		t.Fatalf("超限时 image/png 读取次数 = %d，期望只读标记一次", server.imageReads)
	}
}

// TestReadImageEmpty reports an empty selection without an error.
func TestReadImageEmpty(t *testing.T) {
	newFakeXServer(t, fakeServerOptions{notifyCode: 31})
	if _, err := ReadImage(); err != errSelectionEmpty {
		t.Fatalf("want errSelectionEmpty, got %v", err)
	}
}

// TestGetPropertyDeleteFlag pins the X11 INCR requirement: every GetProperty
// the client sends carries the delete flag set in the request header's second
// byte. Without it, an incremental selection transfer accumulates property
// bytes and the assembled image is corrupted (blank preview / bad format).
func TestGetPropertyDeleteFlag(t *testing.T) {
	var got []byte
	rec := &recordingConn{write: func(p []byte) { got = append(got, p...) }}
	x := &xconn{c: rec, requesterWindow: 0x100001}
	payload := make([]byte, 20)
	binary.LittleEndian.PutUint32(payload[4:8], 0x300)
	if err := x.send(20, payload, 1); err != nil {
		t.Fatalf("send: %v", err)
	}
	// Header layout: opcode(1) delete(1) length(2).
	if len(got) < 4 || got[0] != 20 || got[1] != 1 {
		t.Fatalf("GetProperty header = %x, want opcode 20 with delete=1", got[:4])
	}
}

// recordingConn records every write for wire-level assertions.
type recordingConn struct {
	write func(p []byte)
}

func (r *recordingConn) Write(p []byte) (int, error) { r.write(p); return len(p), nil }
func (r *recordingConn) Read(p []byte) (int, error)  { return 0, io.EOF }
func (r *recordingConn) Close() error                { return nil }
func (r *recordingConn) LocalAddr() net.Addr         { return recAddr("rec") }
func (r *recordingConn) RemoteAddr() net.Addr        { return recAddr("rec") }
func (r *recordingConn) SetDeadline(time.Time) error { return nil }
func (r *recordingConn) SetReadDeadline(time.Time) error {
	return nil
}
func (r *recordingConn) SetWriteDeadline(time.Time) error { return nil }

type recAddr string

func (a recAddr) Network() string { return "rec" }
func (a recAddr) String() string  { return string(a) }

// TestLoadXauthCookie parses a real .Xauthority-format cookie file.
func TestLoadXauthCookie(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Xauthority")
	cookie := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	var b bytes.Buffer
	writeU16 := func(v int) { _ = binary.Write(&b, binary.BigEndian, uint16(v)) }
	writeU16(256) // FamilyWild
	writeU16(0)   // address
	writeU16(0)   // number
	name := []byte("MIT-MAGIC-COOKIE-1")
	writeU16(len(name))
	b.Write(name)
	writeU16(len(cookie))
	b.Write(cookie)
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Setenv("XAUTHORITY", path)
	t.Cleanup(func() { os.Unsetenv("XAUTHORITY") })
	got := loadXauthCookie()
	if !bytes.Equal(got, cookie) {
		t.Fatalf("cookie = %x, want %x", got, cookie)
	}
}

// ---------- 假 X 服务端 ----------

// fakeServerOptions 描述假服务端这次要模拟的剪贴板形态。
type fakeServerOptions struct {
	// png 是 owner 提供的内容；nil 表示剪贴板里没有图片。
	png []byte
	// incr 为真时按 ICCCM 分块投递 png，而不是一次给全。
	incr bool
	// incrChunks 是分块数量，仅在 incr 为真时有意义。
	incrChunks int
	// notifyCode 是 SelectionNotify 的事件码：31 是标准码，159 是玲珑 X 桥重写码。
	notifyCode byte
	// advertise 为真时 TARGETS 里写出 image/png；为假时写出空列表，
	// 客户端据此退化成逐个尝试所有已知格式。
	advertise bool
	// markerTotal 非零时替换 INCR 标记里的总字节数，用于验证超限提前拒绝。
	markerTotal uint32
}

// queueWriter 把服务端的写出排队，由一个 goroutine 顺序落到连接上。
//
// net.Pipe 没有缓冲：服务端一旦写在客户端还没读的字节上就会阻塞，而客户端此时可能
// 正在写下一个请求，双方互等 —— 真实 X 连接有内核缓冲，不会出现这种死锁。
// 队列让假服务端具备同样的“写不阻塞逻辑”的行为；客户端长时间不再读取时丢弃事件，
// 避免测试被拖死。
type queueWriter struct {
	ch chan []byte
}

func newQueueWriter(c net.Conn) *queueWriter {
	q := &queueWriter{ch: make(chan []byte, 256)}
	go func() {
		for b := range q.ch {
			if _, err := c.Write(b); err != nil {
				return
			}
		}
	}()
	return q
}

func (q *queueWriter) write(b []byte) {
	select {
	case q.ch <- append([]byte(nil), b...):
	case <-time.After(2 * time.Second):
	}
}

// fakeXServer 只实现剪贴板读取会用到的那几个请求，并按真实时序回放事件，
// 因此客户端在事件码、字段偏移或 INCR 时序上的任何偏差都会在这里暴露。
type fakeXServer struct {
	t          *testing.T
	opts       fakeServerOptions
	done       chan struct{}
	out        *queueWriter
	atoms      map[string]uint32
	nextAtom   uint32
	requestor  uint32
	prop       uint32
	lastTarget uint32
	// incrPhase：0 表示还没回 INCR 标记，1..n 表示正在回第 n 块，n+1 表示回结束块。
	incrPhase int
	// imageReads 统计 image/png 上的 GetProperty 次数（标记、分块与结束块都算）。
	imageReads int
}

func newFakeXServer(t *testing.T, opts fakeServerOptions) *fakeXServer {
	t.Helper()
	// ReadImage dials the real sockets; we override connectSocket so the test
	// drives the fake through a pipe without touching the display.
	srvConn, cliConn := net.Pipe()
	origConn := connectSocket
	connectSocket = func() (net.Conn, error) { return cliConn, nil }
	t.Cleanup(func() { connectSocket = origConn })
	fs := &fakeXServer{t: t, opts: opts, done: make(chan struct{}),
		atoms: map[string]uint32{}, nextAtom: 0x200}
	// 服务端自己要引用的原子先占好 id：客户端的 InternAtom 会拿到同一个 id，
	// 于是 TARGETS 里写出的 image/png 与客户端后续 intern 的结果一致。
	for _, name := range []string{"ATOM", "INCR", "TARGETS", "image/png"} {
		fs.nextAtom++
		fs.atoms[name] = fs.nextAtom
	}
	go fs.serve(srvConn)
	return fs
}

func (f *fakeXServer) wait() {
	select {
	case <-f.done:
	case <-time.After(2 * time.Second):
		f.t.Fatal("fake server did not finish")
	}
}

func (f *fakeXServer) serve(c net.Conn) {
	defer close(f.done)
	defer c.Close()
	f.out = newQueueWriter(c)
	// setup handshake
	req := make([]byte, 12)
	if _, err := io.ReadFull(c, req); err != nil {
		f.t.Errorf("setup read: %v", err)
		return
	}
	// Setup body (LSBFirst): resource base at [4:8], screen-0 root at [32:36].
	body := make([]byte, 40)
	binary.LittleEndian.PutUint32(body[4:8], 0x100000)  // resource id base
	binary.LittleEndian.PutUint32(body[8:12], 0x1fffff) // mask
	binary.LittleEndian.PutUint16(body[16:18], 0)       // vendor length 0
	binary.LittleEndian.PutUint16(body[18:20], 0)       // max request length
	body[20] = 1                                        // screens 1
	body[21] = 0                                        // formats 0
	binary.LittleEndian.PutUint32(body[32:36], 0x1234)  // root window
	hdr := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(hdr[2:4], 0)
	binary.LittleEndian.PutUint16(hdr[6:8], uint16(len(body)/4))
	f.out.write(append(hdr, body...))
	// request loop
	for {
		h := make([]byte, 4)
		if _, err := io.ReadFull(c, h); err != nil {
			return
		}
		op := h[0]
		words := int(binary.LittleEndian.Uint16(h[2:4]))
		payload := make([]byte, words*4-4)
		if _, err := io.ReadFull(c, payload); err != nil {
			return
		}
		switch op {
		case 1: // CreateWindow：请求方窗口必须选中 PropertyChangeMask
			if len(payload) < 32 {
				f.t.Errorf("CreateWindow payload = %d bytes, want value-mask + value-list", len(payload))
				return
			}
			f.requestor = binary.LittleEndian.Uint32(payload[0:4])
			if mask := binary.LittleEndian.Uint32(payload[24:28]); mask&cwEventMask == 0 {
				f.t.Errorf("CreateWindow value-mask = %#x, want CWEventMask", mask)
				return
			}
			if value := binary.LittleEndian.Uint32(payload[28:32]); value&propertyChangeMask == 0 {
				f.t.Errorf("CreateWindow event mask = %#x, want PropertyChangeMask", value)
				return
			}
		case 8: // MapWindow：无需应答
		case 16: // InternAtom
			nlen := int(binary.LittleEndian.Uint16(payload[0:2]))
			name := string(payload[4 : 4+nlen])
			if a, ok := f.atoms[name]; ok {
				f.reply(a)
				continue
			}
			f.nextAtom++
			f.atoms[name] = f.nextAtom
			f.reply(f.nextAtom)
		case 23: // GetSelectionOwner
			owner := uint32(0)
			if f.opts.png != nil {
				owner = 0x500
			}
			f.reply(owner)
		case 24: // ConvertSelection: window(0-3) sel(4-7) target(8-11) prop(12-15) time(16-19)
			f.lastTarget = binary.LittleEndian.Uint32(payload[8:12])
			f.prop = binary.LittleEndian.Uint32(payload[12:16])
			f.incrPhase = 0
			f.imageReads = 0
			// SelectionNotify 线上布局：type(1) pad(1) seq(2) time(4) requestor(4)
			// selection(4) target(4) property(4)——property 在偏移 20。
			ev := make([]byte, 32)
			ev[0] = f.opts.notifyCode
			binary.LittleEndian.PutUint32(ev[4:8], 1) // time
			binary.LittleEndian.PutUint32(ev[8:12], f.requestor)
			binary.LittleEndian.PutUint32(ev[20:24], f.prop)
			f.out.write(ev)
		case 20: // GetProperty
			// X11 INCR requires the client to delete each segment it reads.
			if h[1] != 1 {
				f.t.Errorf("GetProperty delete flag = %d, want 1", h[1])
				return
			}
			f.replyProperty()
		default:
			f.t.Errorf("unexpected opcode %d", op)
			return
		}
	}
}

// replyProperty 按 lastTarget 回一个属性值。TARGETS 回广告列表；image/png 回图像
// （直给或 INCR 分块）；其余回空，让客户端继续尝试下一个格式。
func (f *fakeXServer) replyProperty() {
	switch f.lastTarget {
	case f.atoms["TARGETS"]:
		var data []byte
		if f.opts.advertise {
			data = atomBytes(f.atoms["image/png"])
		}
		f.writePropReply(f.atoms["ATOM"], 32, data, 0)
	case f.atoms["image/png"]:
		f.imageReads++
		if !f.opts.incr {
			f.writePropReply(f.atoms["image/png"], 8, f.opts.png, 0)
			return
		}
		f.replyIncremental()
	default:
		f.writePropReply(f.atoms["ATOM"], 32, nil, 0)
	}
}

// replyIncremental 按 ICCCM 投递一次 INCR 传输，时序与真实 owner 一致：
// 第一次请求回 4 字节 INCR 标记（format 32 的总字节数，bytes-after 为 0），
// 之后每次请求回一块、并在应答后立刻发一个 PropertyNotify 表示「下一块已就绪」，
// 最后一块之后再回 0 字节的结束块。
func (f *fakeXServer) replyIncremental() {
	chunks := f.chunks()
	switch {
	case f.incrPhase == 0:
		total := f.opts.markerTotal
		if total == 0 {
			total = uint32(len(f.opts.png))
		}
		marker := make([]byte, 4)
		binary.LittleEndian.PutUint32(marker, total)
		f.writePropReply(f.atoms["INCR"], 32, marker, 0)
		f.incrPhase = 1
		f.notifyProperty()
	case f.incrPhase <= len(chunks):
		f.writePropReply(f.atoms["image/png"], 8, chunks[f.incrPhase-1], 0)
		f.incrPhase++
		f.notifyProperty()
	default:
		f.writePropReply(f.atoms["image/png"], 8, nil, 0)
	}
}

// chunks 把 png 均分成 opts.incrChunks 块，至少一块。
func (f *fakeXServer) chunks() [][]byte {
	count := f.opts.incrChunks
	if count < 1 {
		count = 1
	}
	size := (len(f.opts.png) + count - 1) / count
	var out [][]byte
	for off := 0; off < len(f.opts.png); off += size {
		end := off + size
		if end > len(f.opts.png) {
			end = len(f.opts.png)
		}
		out = append(out, f.opts.png[off:end])
	}
	return out
}

// notifyProperty 发一个 PropertyNotify（事件码 28）。
// 线上布局：type(1) pad(1) sequence(2) window(4) atom(4) time(4) state(1)，
// 因此 window 在偏移 4、atom 在偏移 8。
func (f *fakeXServer) notifyProperty() {
	ev := make([]byte, 32)
	ev[0] = 28
	binary.LittleEndian.PutUint32(ev[4:8], f.requestor)
	binary.LittleEndian.PutUint32(ev[8:12], f.prop)
	f.out.write(ev)
}

// reply 回一个只带返回值的 32 字节应答（InternAtom / GetSelectionOwner）。
func (f *fakeXServer) reply(value uint32) {
	hdr := make([]byte, 32)
	hdr[0] = 1
	binary.LittleEndian.PutUint32(hdr[8:12], value)
	f.out.write(hdr)
}

// writePropReply 写一个 GetProperty 回复。format 必须显式给出：普通载荷是
// format 8 的字节流，INCR 标记是 format 32 的单个 CARD32，nitems 按 format 换算。
func (f *fakeXServer) writePropReply(ptype uint32, format byte, data []byte, after uint32) {
	hdr := make([]byte, 32)
	hdr[0] = 1
	hdr[1] = format
	binary.LittleEndian.PutUint16(hdr[4:6], uint16((len(data)+3)/4))
	binary.LittleEndian.PutUint32(hdr[8:12], ptype)
	binary.LittleEndian.PutUint32(hdr[12:16], after)
	items := len(data)
	if format > 0 {
		items = len(data) / int(format/8)
	}
	binary.LittleEndian.PutUint32(hdr[16:20], uint32(items))
	payload := make([]byte, (len(data)+3)&^3)
	copy(payload, data)
	f.out.write(append(hdr, payload...))
}

// atomBytes 把一个或多个原子编码成 TARGETS 属性的 format 32 载荷。
func atomBytes(atoms ...uint32) []byte {
	out := make([]byte, 0, 4*len(atoms))
	for _, a := range atoms {
		out = binary.LittleEndian.AppendUint32(out, a)
	}
	return out
}

// makeTestPNG builds a real, valid PNG (gradient RGB, no alpha) of the given
// dimensions so the plausibility filter and pixel decoding accept it.
func makeTestPNG(w, h int) []byte {
	var raw bytes.Buffer
	for y := 0; y < h; y++ {
		raw.WriteByte(0) // filter: none
		for x := 0; x < w; x++ {
			raw.WriteByte(byte(x * 255 / w))
			raw.WriteByte(byte(y * 255 / h))
			raw.WriteByte(byte((x + y) * 255 / (w + h)))
		}
	}
	var comp bytes.Buffer
	zw := zlib.NewWriter(&comp)
	_, _ = zw.Write(raw.Bytes())
	_ = zw.Close()
	out := new(bytes.Buffer)
	chunk := func(tag string, data []byte) {
		_ = binary.Write(out, binary.BigEndian, uint32(len(data)))
		out.WriteString(tag)
		out.Write(data)
		_ = binary.Write(out, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(tag), data...)))
	}
	var ihdr bytes.Buffer
	_ = binary.Write(&ihdr, binary.BigEndian, uint32(w))
	_ = binary.Write(&ihdr, binary.BigEndian, uint32(h))
	ihdr.Write([]byte{8, 2, 0, 0, 0})
	out.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	chunk("IHDR", ihdr.Bytes())
	chunk("IDAT", comp.Bytes())
	chunk("IEND", nil)
	return out.Bytes()
}

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

// TestReadImageSimple drives ReadImage through a fake X server that hands
// back a small PNG immediately (no INCR).
func TestReadImageSimple(t *testing.T) {
	png := makeTestPNG(64, 64)
	server := newFakeXServer(t, png, false)
	data, err := ReadImage()
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	server.wait()
	if !bytes.Equal(data, png) {
		t.Fatalf("got %x, want %x", data, png)
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

// TestReadImageEmpty reports an empty selection without an error.
func TestReadImageEmpty(t *testing.T) {
	newFakeXServer(t, nil, false)
	if _, err := ReadImage(); err != errSelectionEmpty {
		t.Fatalf("want errSelectionEmpty, got %v", err)
	}
}

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

// ---------- fake X server ----------

type fakeXServer struct {
	t          *testing.T
	png        []byte
	incr       bool
	done       chan struct{}
	incrSent   bool
	atoms      map[string]uint32
	lastTarget uint32
	nextAtom   uint32
}

func newFakeXServer(t *testing.T, png []byte, incr bool) *fakeXServer {
	t.Helper()
	// ReadImage dials the real sockets; we override connectSocket so the test
	// drives the fake through a pipe without touching the display.
	srvConn, cliConn := net.Pipe()
	origConn := connectSocket
	connectSocket = func() (net.Conn, error) { return cliConn, nil }
	t.Cleanup(func() { connectSocket = origConn })
	fs := &fakeXServer{t: t, png: png, incr: incr, done: make(chan struct{}),
		atoms: map[string]uint32{}, nextAtom: 0x200}
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
	if _, err := c.Write(append(hdr, body...)); err != nil {
		f.t.Errorf("setup reply: %v", err)
		return
	}
	prop := uint32(0x300)
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
		case 1, 8: // CreateWindow/MapWindow: acknowledge without a reply
			continue
		case 16: // InternAtom
			nlen := int(binary.LittleEndian.Uint16(payload[0:2]))
			name := string(payload[4 : 4+nlen])
			if a, ok := f.atoms[name]; ok {
				reply(c, a)
				continue
			}
			f.nextAtom++
			f.atoms[name] = f.nextAtom
			reply(c, f.nextAtom)
		case 23: // GetSelectionOwner
			owner := uint32(0)
			if f.png != nil {
				owner = 0x500
			}
			reply(c, owner)
		case 24: // ConvertSelection: window(0-3) sel(4-7) target(8-11) prop(12-15)
			f.lastTarget = binary.LittleEndian.Uint32(payload[8:12])
			// SelectionNotify event: type 31, prop at offset 24
			ev := make([]byte, 32)
			ev[0] = 31
			binary.LittleEndian.PutUint16(ev[4:6], 2)
			binary.LittleEndian.PutUint32(ev[12:16], 0x10001) // requestor
			binary.LittleEndian.PutUint32(ev[24:28], prop)
			if _, err := c.Write(ev); err != nil {
				return
			}
		case 20: // GetProperty
			// X11 INCR requires the client to delete each segment it reads.
			if h[1] != 1 {
				f.t.Errorf("GetProperty delete flag = %d, want 1", h[1])
				return
			}
			switch f.lastTarget {
			case f.atoms["TARGETS"]:
				// Advertise nothing: the client then tries every format.
				writePropReplyAfter(c, f.atoms["ATOM"], nil, 0)
			case f.atoms["image/png"]:
				writePropReplyAfter(c, f.atoms["image/png"], f.png, 0)
			default:
				writePropReplyAfter(c, f.atoms["ATOM"], nil, 0)
			}
		default:
			f.t.Errorf("unexpected opcode %d", op)
			return
		}
	}
}

func reply(c net.Conn, value uint32) {
	hdr := make([]byte, 32)
	hdr[0] = 1
	binary.LittleEndian.PutUint32(hdr[8:12], value)
	_, _ = c.Write(hdr)
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

func writePropReplyAfter(c net.Conn, ptype uint32, data []byte, after uint32) {
	hdr := make([]byte, 32)
	hdr[0] = 1
	hdr[1] = 8 // format 8
	binary.LittleEndian.PutUint16(hdr[4:6], uint16((len(data)+3)/4))
	binary.LittleEndian.PutUint32(hdr[8:12], ptype)
	binary.LittleEndian.PutUint32(hdr[12:16], after)
	binary.LittleEndian.PutUint32(hdr[16:20], uint32(len(data)))
	payload := make([]byte, (len(data)+3)&^3)
	copy(payload, data)
	_, _ = c.Write(append(hdr, payload...))
}

package clipboard

import (
	"bytes"
	"encoding/binary"
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
	png := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
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

// TestReadImageINCR covers the incremental transfer path used for big payloads.
func TestReadImageINCR(t *testing.T) {
	png := bytes.Repeat([]byte{0x89, 'P', 'N', 'G', 3, 4}, 3000) // > property chunk
	server := newFakeXServer(t, png, true)
	data, err := ReadImage()
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	server.wait()
	if !bytes.Equal(data, png) {
		t.Fatalf("got %d bytes, want %d", len(data), len(png))
	}
}

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
	t    *testing.T
	png  []byte
	incr bool
	done chan struct{}
}

func newFakeXServer(t *testing.T, png []byte, incr bool) *fakeXServer {
	t.Helper()
	// ReadImage dials the real sockets; we override connectSocket so the test
	// drives the fake through a pipe without touching the display.
	srvConn, cliConn := net.Pipe()
	origConn := connectSocket
	connectSocket = func() (net.Conn, error) { return cliConn, nil }
	t.Cleanup(func() { connectSocket = origConn })
	fs := &fakeXServer{t: t, png: png, incr: incr, done: make(chan struct{})}
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
	nextAtom := uint32(0x200)
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
			nextAtom++
			reply(c, nextAtom)
		case 23: // GetSelectionOwner
			owner := uint32(0)
			if f.png != nil {
				owner = 0x500
			}
			reply(c, owner)
		case 24: // ConvertSelection
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
			if f.incr {
				// First read: type INCR, bytes-after huge, one chunk.
				writePropReplyAfter(c, 0x201, f.png, uint32(len(f.png)))
				// Then a PropertyNotify and an empty read with after=0.
				pn := make([]byte, 32)
				pn[0] = 28
				pn[1] = 0
				binary.LittleEndian.PutUint32(pn[8:12], prop)
				if _, err := c.Write(pn); err != nil {
					return
				}
				writePropReplyAfter(c, 0x201, nil, 0)
				return
			}
			writePropReplyAfter(c, 0x200, f.png, 0)
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

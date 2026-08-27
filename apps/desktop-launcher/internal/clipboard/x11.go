// Package clipboard reads the X11 CLIPBOARD selection over a raw wire
// connection (no cgo, no external tools). It exists because the packaged
// WebKitGTK renderer never surfaces clipboard images to the page, while the
// shell process itself can read them from the host X server. Only image/png
// is supported today.
package clipboard

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Limits applied to one image read. A screenshot is far below these; the caps
// protect the shell from a pathological owner.
const (
	maxImageBytes = 20 << 20 // 20 MiB
	readTimeout   = 6 * time.Second
)

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

var errSelectionEmpty = errors.New("CLIPBOARD has no image/png content")

// ReadImage returns the current CLIPBOARD image/png payload, or nil when the
// selection carries no image. Errors are returned for unreachable displays,
// protocol failures and oversized payloads.
func ReadImage() ([]byte, error) {
	x, err := dial()
	if err != nil {
		return nil, err
	}
	defer x.c.Close()
	return x.readClipboardPNG()
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
func (x *xconn) send(opcode byte, payload []byte) error {
	padded := payload
	if r := len(payload) % 4; r != 0 {
		padded = append(append([]byte{}, payload...), make([]byte, 4-r)...)
	}
	words := uint16(1 + len(padded)/4)
	hdr := []byte{opcode, 0, 0, 0}
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
		length := int(binary.LittleEndian.Uint16(hdr[4:6]))
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
			if binary.LittleEndian.Uint32(hdr[8:12]) != prop {
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
	if err := x.send(20, payload); err != nil { // GetProperty
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
	payload = make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, wid)
	if err := x.send(8, payload); err != nil { // MapWindow
		return err
	}
	x.requesterWindow = wid
	return nil
}

// nextWindow returns the installed requestor window.
func (x *xconn) nextWindow() uint32 { return x.requesterWindow }

// readClipboardPNG implements the selection read for image/png.
func (x *xconn) readClipboardPNG() ([]byte, error) {
	clip := x.mustAtom("CLIPBOARD")
	if clip == 0 {
		return nil, errors.New("clipboard: X11 intern failed")
	}
	owner, err := x.getSelectionOwner(clip)
	if err != nil {
		return nil, err
	}
	if owner == 0 {
		return nil, errSelectionEmpty
	}
	png := x.mustAtom("image/png")
	prop := x.mustAtom("_DSH_CLIP")
	incr := x.mustAtom("INCR")
	if png == 0 || prop == 0 || incr == 0 {
		return nil, errors.New("clipboard: X11 intern failed")
	}
	got, err := x.convert(clip, png, prop)
	if err != nil {
		return nil, err
	}
	if got == 0 {
		return nil, errSelectionEmpty
	}
	_, data, err := x.getProperty(got, 0, incr)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || data[0] != 0x89 { // PNG magic
		return nil, errSelectionEmpty
	}
	if len(data) > maxImageBytes {
		return nil, fmt.Errorf("clipboard: image exceeds %d bytes", maxImageBytes)
	}
	return data, nil
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

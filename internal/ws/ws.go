// Package ws is a minimal RFC 6455 WebSocket transport: just enough to carry
// a byte stream through an HTTP-only proxy such as a Cloudflare tunnel.
//
// Both Dial and Accept return a net.Conn whose Read and Write behave like a
// stream, so the framing in package comm stays unchanged. Each Write becomes
// one binary frame; Read stitches frames back into a stream.
package ws

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// magic is the GUID RFC 6455 mixes into the handshake accept token.
const magic = "258EAFA5-E914-47DA-95CA-5AB0DC85B31D"

// Opcodes we care about. Text frames are never produced and rejected on read.
const (
	opContinuation = 0x0
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// maxFrame bounds an incoming frame so a hostile peer cannot make us allocate
// unbounded memory; comm's own limit is smaller still.
const maxFrame = 16 << 20

// Conn is a WebSocket connection presented as a stream-oriented net.Conn.
type Conn struct {
	net.Conn
	br      *bufio.Reader
	maskOut bool // clients must mask every frame they send
	rmask   bool // the frame being read was masked
	rest    int  // bytes left in the frame being read
	key     [4]byte
	keyPos  int
	wmu     sync.Mutex
}

// Dial opens a WebSocket connection to rawURL. http/https URLs are accepted
// and treated as ws/wss, since that is how tunnel hostnames are handed out.
func Dial(rawURL string, timeout time.Duration) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	secure := false
	switch u.Scheme {
	case "ws", "http":
	case "wss", "https":
		secure = true
	default:
		return nil, fmt.Errorf("ws: unsupported scheme %q", u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = DefaultPath
	}

	host := u.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		if secure {
			host = net.JoinHostPort(host, "443")
		} else {
			host = net.JoinHostPort(host, "80")
		}
	}

	raw, err := dialTCP(host, u.Hostname(), secure, timeout)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		raw.Close()
		return nil, err
	}
	clientKey := base64.StdEncoding.EncodeToString(nonce)

	req := "GET " + u.RequestURI() + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + clientKey + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	raw.SetDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(raw, req); err != nil {
		raw.Close()
		return nil, err
	}

	br := bufio.NewReader(raw)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		raw.Close()
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		raw.Close()
		return nil, fmt.Errorf("ws: server answered %s", resp.Status)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != acceptToken(clientKey) {
		raw.Close()
		return nil, errors.New("ws: server returned a bad accept token")
	}
	raw.SetDeadline(time.Time{})
	return &Conn{Conn: raw, br: br, maskOut: true}, nil
}

// Accept upgrades an inbound HTTP request. On success the returned Conn owns
// the underlying connection and the handler must not touch w again.
func Accept(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "expected a websocket upgrade", http.StatusBadRequest)
		return nil, errors.New("ws: not an upgrade request")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return nil, errors.New("ws: missing key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "cannot upgrade", http.StatusInternalServerError)
		return nil, errors.New("ws: response writer cannot be hijacked")
	}
	raw, buf, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptToken(key) + "\r\n\r\n"
	if _, err := io.WriteString(raw, resp); err != nil {
		raw.Close()
		return nil, err
	}
	if err := buf.Writer.Flush(); err != nil {
		raw.Close()
		return nil, err
	}
	return &Conn{Conn: raw, br: buf.Reader}, nil
}

func acceptToken(clientKey string) string {
	sum := sha1.Sum([]byte(clientKey + magic))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// Read fills p from the current frame, advancing to the next data frame when
// the current one runs out. Control frames are handled transparently.
func (c *Conn) Read(p []byte) (int, error) {
	for c.rest == 0 {
		if err := c.nextFrame(); err != nil {
			return 0, err
		}
	}
	if len(p) > c.rest {
		p = p[:c.rest]
	}
	n, err := c.br.Read(p)
	if c.rmask {
		c.unmask(p[:n])
	}
	c.rest -= n
	return n, err
}

// nextFrame reads a frame header, answering pings and treating close as EOF,
// and leaves c.rest set to the payload length of a data frame.
func (c *Conn) nextFrame() error {
	var hdr [2]byte
	if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
		return err
	}
	opcode := hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	length := int(hdr[1] & 0x7F)

	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return err
		}
		length = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return err
		}
		n := binary.BigEndian.Uint64(ext[:])
		if n > maxFrame {
			return fmt.Errorf("ws: frame of %d bytes exceeds the %d byte limit", n, maxFrame)
		}
		length = int(n)
	}
	if length > maxFrame {
		return fmt.Errorf("ws: frame of %d bytes exceeds the %d byte limit", length, maxFrame)
	}

	if masked {
		if _, err := io.ReadFull(c.br, c.key[:]); err != nil {
			return err
		}
		c.keyPos = 0
	}
	c.rmask = masked

	switch opcode {
	case opBinary, opContinuation:
		c.rest = length
		return nil
	case opClose:
		c.discard(length)
		return io.EOF
	case opPing:
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return err
		}
		if masked {
			c.unmask(payload)
		}
		return c.writeFrame(opPong, payload)
	case opPong:
		c.discard(length)
		return nil
	default:
		return fmt.Errorf("ws: unexpected opcode %d", opcode)
	}
}

func (c *Conn) discard(n int) {
	if n > 0 {
		io.CopyN(io.Discard, c.br, int64(n))
	}
}

// unmask applies the running XOR key in place.
func (c *Conn) unmask(p []byte) {
	for i := range p {
		p[i] ^= c.key[c.keyPos&3]
		c.keyPos++
	}
}

// Write sends p as a single binary frame.
func (c *Conn) Write(p []byte) (int, error) {
	if err := c.writeFrame(opBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *Conn) writeFrame(opcode byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()

	hdr := make([]byte, 0, 14)
	hdr = append(hdr, 0x80|opcode) // every frame we send is final
	n := len(payload)
	var maskBit byte
	if c.maskOut {
		maskBit = 0x80
	}
	switch {
	case n < 126:
		hdr = append(hdr, maskBit|byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, maskBit|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, maskBit|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		hdr = append(hdr, ext[:]...)
	}

	body := payload
	if c.maskOut {
		var key [4]byte
		if _, err := rand.Read(key[:]); err != nil {
			return err
		}
		hdr = append(hdr, key[:]...)
		body = make([]byte, n)
		for i := range payload {
			body[i] = payload[i] ^ key[i&3]
		}
	}
	if _, err := c.Conn.Write(append(hdr, body...)); err != nil {
		return err
	}
	return nil
}

// Close sends a close frame before shutting the socket down, so the peer sees
// a clean end of stream rather than a reset.
func (c *Conn) Close() error {
	c.writeFrame(opClose, nil)
	return c.Conn.Close()
}

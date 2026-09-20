// Package comm frames a TCP connection into discrete length-prefixed
// messages, so both peers and the relay agree on message boundaries.
package comm

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/scotthellings/croc-go/internal/ws"
)

// MaxMessage bounds a single frame, keeping a hostile peer from making us
// allocate unbounded memory.
const MaxMessage = 8 << 20

// Conn is a framed connection.
type Conn struct {
	net.Conn
	hdr [4]byte
}

func New(c net.Conn) *Conn { return &Conn{Conn: c} }

// Dial connects to addr and wraps the connection. A bare host:port is dialed
// as raw TCP; a ws/wss/http/https URL is dialed as a WebSocket, which is what
// lets a transfer pass through an HTTP-only tunnel.
func Dial(addr string, timeout time.Duration) (*Conn, error) {
	if isURL(addr) {
		c, err := ws.Dial(addr, timeout)
		if err != nil {
			return nil, err
		}
		return New(c), nil
	}
	c, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	return New(c), nil
}

// isURL reports whether addr names a WebSocket endpoint rather than a TCP
// host:port.
func isURL(addr string) bool {
	for _, scheme := range []string{"ws://", "wss://", "http://", "https://"} {
		if strings.HasPrefix(addr, scheme) {
			return true
		}
	}
	return false
}

// Send writes one frame.
func (c *Conn) Send(b []byte) error {
	if len(b) > MaxMessage {
		return fmt.Errorf("comm: message of %d bytes exceeds the %d byte limit", len(b), MaxMessage)
	}
	binary.BigEndian.PutUint32(c.hdr[:], uint32(len(b)))
	if _, err := c.Write(c.hdr[:]); err != nil {
		return err
	}
	_, err := c.Write(b)
	return err
}

// Receive reads one frame.
func (c *Conn) Receive() ([]byte, error) {
	if _, err := io.ReadFull(c.Conn, c.hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(c.hdr[:])
	if n > MaxMessage {
		return nil, fmt.Errorf("comm: peer announced %d bytes, over the %d byte limit", n, MaxMessage)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.Conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// SendString and ReceiveString are conveniences for the handshake, which is
// all short text messages.
func (c *Conn) SendString(s string) error { return c.Send([]byte(s)) }

func (c *Conn) ReceiveString() (string, error) {
	b, err := c.Receive()
	return string(b), err
}

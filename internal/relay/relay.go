// Package relay pairs two peers that present the same room name and then
// pipes bytes between them. The relay sees only ciphertext: the room name is
// derived from the code, but the secret words that seed the PAKE never leave
// the peers.
package relay

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/scotthellings/croc-go/internal/comm"
)

// Protocol constants shared with clients.
const (
	Greeting    = "croc-go/1"
	Paired      = "paired"
	WaitTimeout = 10 * time.Minute
)

// Server accepts peers and matches them by room.
type Server struct {
	mu      sync.Mutex
	waiting map[string]*comm.Conn
	log     *log.Logger
}

func NewServer(logger *log.Logger) *Server {
	return &Server{waiting: map[string]*comm.Conn{}, log: logger}
}

// ListenAndServe runs until the listener fails.
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	s.log.Printf("relay listening on %s", ln.Addr())
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(comm.New(c))
	}
}

func (s *Server) handle(c *comm.Conn) {
	room, err := s.handshake(c)
	if err != nil {
		s.log.Printf("rejected %s: %v", c.RemoteAddr(), err)
		c.Close()
		return
	}

	s.mu.Lock()
	partner, found := s.waiting[room]
	if found {
		delete(s.waiting, room)
	} else {
		s.waiting[room] = c
	}
	s.mu.Unlock()

	if !found {
		// We are first; the partner's goroutine takes over once it arrives.
		// Drop the connection if nobody shows up in time.
		s.expireAfter(room, c)
		return
	}

	s.log.Printf("pairing room %s", room)
	if err := partner.SendString(Paired); err != nil {
		partner.Close()
		c.Close()
		return
	}
	if err := c.SendString(Paired); err != nil {
		partner.Close()
		c.Close()
		return
	}
	pipe(partner, c)
	s.log.Printf("closed room %s", room)
}

// handshake reads the client greeting and room name.
func (s *Server) handshake(c *comm.Conn) (string, error) {
	c.SetReadDeadline(time.Now().Add(30 * time.Second))
	defer c.SetReadDeadline(time.Time{})

	greeting, err := c.ReceiveString()
	if err != nil {
		return "", err
	}
	if greeting != Greeting {
		return "", errors.New("unexpected greeting " + greeting)
	}
	room, err := c.ReceiveString()
	if err != nil {
		return "", err
	}
	if room == "" {
		return "", errors.New("empty room")
	}
	return room, nil
}

// expireAfter closes a lone peer that was never paired.
func (s *Server) expireAfter(room string, c *comm.Conn) {
	time.AfterFunc(WaitTimeout, func() {
		s.mu.Lock()
		still := s.waiting[room] == c
		if still {
			delete(s.waiting, room)
		}
		s.mu.Unlock()
		if still {
			s.log.Printf("room %s timed out waiting for a partner", room)
			c.Close()
		}
	})
}

// pipe copies in both directions until either side hangs up.
func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	copyOnce := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		// Unblock the other direction rather than waiting on a half-open
		// connection that will never carry more data.
		dst.Close()
		src.Close()
	}
	go copyOnce(a, b)
	go copyOnce(b, a)
	wg.Wait()
}

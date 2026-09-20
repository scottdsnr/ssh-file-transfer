package ws_test

import (
	"bytes"
	"crypto/rand"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scotthellings/croc-go/internal/ws"
)

// echoServer upgrades every request on DefaultPath and copies bytes back.
func echoServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Accept(w, r)
		if err != nil {
			return
		}
		defer c.Close()
		io.Copy(c, c)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + ws.DefaultPath
}

func TestRoundTripSmall(t *testing.T) {
	c, err := ws.Dial(echoServer(t), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	want := []byte("hello websocket")
	if _, err := c.Write(want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestStreamsAcrossFrames covers the sizes that switch the length encoding
// from 7 bits to 16 and then 64, and checks that reads stitch frames together.
func TestStreamsAcrossFrames(t *testing.T) {
	c, err := ws.Dial(echoServer(t), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for _, size := range []int{1, 125, 126, 65535, 65536, 300 * 1024} {
		payload := make([]byte, size)
		rand.Read(payload)
		go func() {
			if _, err := c.Write(payload); err != nil {
				t.Error(err)
			}
		}()
		got := make([]byte, size)
		if _, err := io.ReadFull(c, got); err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("size %d: payload did not survive the round trip", size)
		}
	}
}

func TestRejectsPlainRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := ws.Accept(w, r); err == nil {
			t.Error("expected Accept to reject a non-upgrade request")
		}
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + ws.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %s, want 400", resp.Status)
	}
}

func TestDialRejectsUnknownScheme(t *testing.T) {
	if _, err := ws.Dial("tcp://127.0.0.1:1", time.Second); err == nil {
		t.Fatal("expected an error for a non-HTTP scheme")
	}
}

// TestCloseEndsTheStream checks the peer sees EOF rather than a reset.
func TestCloseEndsTheStream(t *testing.T) {
	done := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Accept(w, r)
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		_, err = io.ReadAll(c)
		done <- err
	}))
	defer srv.Close()

	c, err := ws.Dial(srv.URL+ws.DefaultPath, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte("bye"))
	c.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server read ended with %v, want a clean EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server never saw the stream end")
	}
}

// TestNetConnSemantics keeps the deadline plumbing that comm relies on honest.
func TestNetConnSemantics(t *testing.T) {
	c, err := ws.Dial(echoServer(t), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var _ net.Conn = c
	if err := c.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err == nil {
		t.Fatal("expected the read deadline to fire")
	}
}

// TestAcceptTokenMatchesRFC pins the handshake token against the worked
// example in RFC 6455 section 1.3. Both of our own peers derive the token the
// same way, so only an outside reference catches a wrong GUID -- which is
// exactly what broke transfers through a Cloudflare tunnel while every local
// test passed.
func TestAcceptTokenMatchesRFC(t *testing.T) {
	const (
		key  = "dGhlIHNhbXBsZSBub25jZQ=="
		want = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	)
	got := ws.AcceptToken(key)
	if got != want {
		t.Fatalf("accept token = %q, want %q", got, want)
	}
}

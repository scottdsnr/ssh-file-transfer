package transfer_test

import (
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scotthellings/croc-go/internal/relay"
	"github.com/scotthellings/croc-go/internal/transfer"
	"github.com/scotthellings/croc-go/internal/ws"
)

// startWSRelay serves the relay over HTTP, the shape a Cloudflare tunnel sees,
// and returns the URL peers should dial.
func startWSRelay(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(relay.NewServer(log.New(io.Discard, "", 0)))
	t.Cleanup(srv.Close)
	return srv.URL + ws.DefaultPath
}

// TestRoundTripOverWebSocket is the tunnelled path: same transfer, HTTP-only
// transport.
func TestRoundTripOverWebSocket(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "payload.bin")
	want := writeFile(t, src, 400*1024)
	out := filepath.Join(dir, "out")

	addr := startWSRelay(t)
	code := "4242-cobalt-badger-orbit"
	sendErr := make(chan error, 1)
	go func() {
		sendErr <- transfer.Send(transfer.SendOptions{Relay: addr, Code: code, Paths: []string{src}})
	}()
	recvErr := transfer.Receive(transfer.ReceiveOptions{
		Relay: addr, Code: code, OutDir: out,
		Confirm: func(transfer.Manifest) bool { return true },
	})
	select {
	case err := <-sendErr:
		if err != nil || recvErr != nil {
			t.Fatalf("send=%v receive=%v", err, recvErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("send did not finish")
	}

	got, err := os.ReadFile(filepath.Join(out, "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("received file does not match the original")
	}
}

// TestRelayOnlySpeaksItsOwnPath keeps the relay from answering stray HTTP
// traffic that a public tunnel hostname will inevitably attract.
func TestRelayOnlySpeaksItsOwnPath(t *testing.T) {
	srv := httptest.NewServer(relay.NewServer(log.New(io.Discard, "", 0)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %s, want 404", resp.Status)
	}
}

// TestListenAndServeHTTPReportsItsPort covers the port-0 path the sender uses
// when it hosts the rendezvous for a tunnel.
func TestListenAndServeHTTPReportsItsPort(t *testing.T) {
	addrs := make(chan net.Addr, 1)
	go relay.NewServer(log.New(io.Discard, "", 0)).ListenAndServeHTTP("127.0.0.1:0", nil, func(a net.Addr) { addrs <- a })

	select {
	case a := <-addrs:
		if a.(*net.TCPAddr).Port == 0 {
			t.Fatal("expected a concrete port")
		}
		c, err := ws.Dial("ws://"+a.String()+ws.DefaultPath, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("relay never reported its address")
	}
}

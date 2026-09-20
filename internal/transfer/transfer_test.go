package transfer_test

import (
	"crypto/rand"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scotthellings/croc-go/internal/relay"
	"github.com/scotthellings/croc-go/internal/transfer"
)

// startRelay runs a relay on a free port and returns its address.
func startRelay(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	s := relay.NewServer(log.New(io.Discard, "", 0))
	go s.ListenAndServe(addr)

	// Wait for the listener to come back up before handing out the address.
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("relay never started on %s", addr)
	return ""
}

func writeFile(t *testing.T, path string, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	rand.Read(data)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

// run performs a full send/receive over a fresh relay.
func run(t *testing.T, paths []string, outDir, sendCode, recvCode string) (error, error) {
	t.Helper()
	addr := startRelay(t)
	sendErr := make(chan error, 1)
	go func() {
		sendErr <- transfer.Send(transfer.SendOptions{Relay: addr, Code: sendCode, Paths: paths})
	}()
	recv := transfer.Receive(transfer.ReceiveOptions{
		Relay: addr, Code: recvCode, OutDir: outDir,
		Confirm: func(transfer.Manifest) bool { return true },
	})
	select {
	case err := <-sendErr:
		return err, recv
	case <-time.After(30 * time.Second):
		t.Fatal("send did not finish")
		return nil, nil
	}
}

func TestRoundTripSingleFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "payload.bin")
	want := writeFile(t, src, 300*1024) // spans several chunks
	out := filepath.Join(dir, "out")

	sendErr, recvErr := run(t, []string{src}, out, "1234-cobalt-badger-orbit", "1234-cobalt-badger-orbit")
	if sendErr != nil || recvErr != nil {
		t.Fatalf("send=%v receive=%v", sendErr, recvErr)
	}
	got, err := os.ReadFile(filepath.Join(out, "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("received file does not match the original")
	}
}

func TestRoundTripDirectory(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "project")
	writeFile(t, filepath.Join(root, "a.txt"), 10)
	nested := writeFile(t, filepath.Join(root, "sub", "b.bin"), 5000)
	out := filepath.Join(dir, "out")

	sendErr, recvErr := run(t, []string{root}, out, "9999-maple-otter-reef", "9999-maple-otter-reef")
	if sendErr != nil || recvErr != nil {
		t.Fatalf("send=%v receive=%v", sendErr, recvErr)
	}
	got, err := os.ReadFile(filepath.Join(out, "project", "sub", "b.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(nested) {
		t.Fatal("nested file does not match the original")
	}
	if _, err := os.Stat(filepath.Join(out, "project", "a.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.txt")
	writeFile(t, src, 0)
	out := filepath.Join(dir, "out")

	sendErr, recvErr := run(t, []string{src}, out, "1111-sage-quartz-lynx", "1111-sage-quartz-lynx")
	if sendErr != nil || recvErr != nil {
		t.Fatalf("send=%v receive=%v", sendErr, recvErr)
	}
	st, err := os.Stat(filepath.Join(out, "empty.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 0 {
		t.Fatalf("size = %d, want 0", st.Size())
	}
}

func TestWrongCodeFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "secret.txt")
	writeFile(t, src, 64)
	out := filepath.Join(dir, "out")

	// Same room (so the relay pairs them) but different secret words.
	sendErr, recvErr := run(t, []string{src}, out, "2222-sage-quartz-lynx", "2222-maple-otter-reef")
	if sendErr == nil || recvErr == nil {
		t.Fatalf("expected both sides to fail, got send=%v receive=%v", sendErr, recvErr)
	}
	if _, err := os.Stat(filepath.Join(out, "secret.txt")); !os.IsNotExist(err) {
		t.Fatal("no file should have been written under a mismatched code")
	}
}

func TestRefusesToOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "dup.txt")
	writeFile(t, src, 32)
	out := filepath.Join(dir, "out")
	writeFile(t, filepath.Join(out, "dup.txt"), 8)

	_, recvErr := run(t, []string{src}, out, "3333-sage-quartz-lynx", "3333-sage-quartz-lynx")
	if recvErr == nil {
		t.Fatal("expected the receiver to refuse to overwrite")
	}
	st, _ := os.Stat(filepath.Join(out, "dup.txt"))
	if st.Size() != 8 {
		t.Fatal("the existing file was modified")
	}
}

package comm

import (
	"bytes"
	"net"
	"testing"
)

func pair(t *testing.T) (*Conn, *Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	return New(a), New(b)
}

func TestFramesPreserveBoundaries(t *testing.T) {
	a, b := pair(t)
	msgs := [][]byte{[]byte("one"), {}, bytes.Repeat([]byte("x"), 100000)}
	go func() {
		for _, m := range msgs {
			a.Send(m)
		}
	}()
	for i, want := range msgs {
		got, err := b.Receive()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("message %d: got %d bytes, want %d", i, len(got), len(want))
		}
	}
}

func TestRejectsOversizedSend(t *testing.T) {
	a, _ := pair(t)
	if err := a.Send(make([]byte, MaxMessage+1)); err == nil {
		t.Fatal("expected an oversized message to be rejected")
	}
}

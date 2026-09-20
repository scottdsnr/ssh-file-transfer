package crypt

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	c, err := New([]byte("session-key"), []byte("salt"))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("the quick brown fox")
	sealed, err := c.Seal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, msg) {
		t.Fatal("plaintext is visible in the ciphertext")
	}
	got, err := c.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("got %q, want %q", got, msg)
	}
}

func TestNoncesDiffer(t *testing.T) {
	c, _ := New([]byte("k"), []byte("s"))
	a, _ := c.Seal([]byte("same"))
	b, _ := c.Seal([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("identical plaintexts produced identical ciphertexts")
	}
}

func TestOpenRejectsTampering(t *testing.T) {
	c, _ := New([]byte("k"), []byte("s"))
	sealed, _ := c.Seal([]byte("important"))
	sealed[len(sealed)-1] ^= 0xff
	if _, err := c.Open(sealed); err == nil {
		t.Fatal("expected tampered ciphertext to be rejected")
	}
	if _, err := c.Open([]byte{1, 2}); err == nil {
		t.Fatal("expected a too-short message to be rejected")
	}
}

func TestDifferentKeysCannotOpen(t *testing.T) {
	a, _ := New([]byte("key-a"), []byte("s"))
	b, _ := New([]byte("key-b"), []byte("s"))
	sealed, _ := a.Seal([]byte("hello"))
	if _, err := b.Open(sealed); err == nil {
		t.Fatal("a different key should not open the message")
	}
}

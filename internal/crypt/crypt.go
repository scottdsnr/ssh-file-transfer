// Package crypt encrypts framed messages with AES-256-GCM under a key
// derived from the PAKE session key.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
)

// Cipher seals and opens individual messages. Each message carries its own
// random nonce, so messages are independent and may be sent in any order.
type Cipher struct {
	aead cipher.AEAD
}

// New derives an encryption key from the session key and salt. Both peers
// must pass the same salt.
func New(sessionKey, salt []byte) (*Cipher, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, sessionKey, salt, []byte("croc-go transfer")), key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Seal returns nonce||ciphertext.
func (c *Cipher) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open reverses Seal, failing if the message was tampered with or was
// encrypted under a different key.
func (c *Cipher) Open(sealed []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("crypt: message is too short to contain a nonce")
	}
	return c.aead.Open(nil, sealed[:n], sealed[n:], nil)
}

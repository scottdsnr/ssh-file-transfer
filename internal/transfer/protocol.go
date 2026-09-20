// Package transfer implements the peer-to-peer half of a croc-go exchange:
// authenticate with the PAKE, agree on a key, then move files as encrypted
// frames over the relay.
package transfer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scotthellings/croc-go/internal/comm"
	"github.com/scotthellings/croc-go/internal/crypt"
	"github.com/scotthellings/croc-go/internal/pake"
	"github.com/scotthellings/croc-go/internal/relay"
	"github.com/scotthellings/croc-go/internal/words"
)

// ChunkSize is the plaintext payload size of a data frame.
const ChunkSize = 64 << 10

// Frame types that prefix every encrypted payload during the file stream.
const (
	frameData = 0
	frameEnd  = 1
)

// Control messages exchanged after the key is established.
const (
	confirmSender   = "croc-go sender ready"
	confirmReceiver = "croc-go receiver ready"
	answerAccept    = "accept"
	answerReject    = "reject"
)

// FileInfo describes one file in the transfer.
type FileInfo struct {
	Path   string      `json:"path"` // relative path, as it will be written
	Size   int64       `json:"size"`
	Mode   os.FileMode `json:"mode"`
	SHA256 string      `json:"sha256"`
}

// Manifest is the sender's offer, sent before any file data.
type Manifest struct {
	Files []FileInfo `json:"files"`
}

// TotalSize is the number of bytes the manifest will transfer.
func (m Manifest) TotalSize() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// connect dials the relay, joins the code's room, and waits to be paired.
func connect(relayAddr, code string) (*comm.Conn, error) {
	room, err := words.Room(code)
	if err != nil {
		return nil, err
	}
	c, err := comm.Dial(relayAddr, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("could not reach the relay at %s: %w", relayAddr, err)
	}
	if err := c.SendString(relay.Greeting); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.SendString(room); err != nil {
		c.Close()
		return nil, err
	}
	reply, err := c.ReceiveString()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("the relay closed the connection before pairing: %w", err)
	}
	if reply != relay.Paired {
		c.Close()
		return nil, fmt.Errorf("unexpected relay reply %q", reply)
	}
	return c, nil
}

// handshake runs SPAKE2 over the paired connection and returns a cipher.
// Both sides send their share before reading, so neither blocks on the other.
func handshake(c *comm.Conn, role pake.Role, code string) (*crypt.Cipher, error) {
	p, err := pake.New(role, []byte(code))
	if err != nil {
		return nil, err
	}
	if err := c.Send(p.Bytes()); err != nil {
		return nil, err
	}
	peer, err := c.Receive()
	if err != nil {
		return nil, err
	}
	if err := p.Update(peer); err != nil {
		return nil, err
	}
	key, err := p.SessionKey()
	if err != nil {
		return nil, err
	}

	// The salt comes from the sender so both sides derive the same subkey.
	var salt []byte
	if role == pake.RoleA {
		salt = make([]byte, 16)
		if _, err := randRead(salt); err != nil {
			return nil, err
		}
		if err := c.Send(salt); err != nil {
			return nil, err
		}
	} else {
		if salt, err = c.Receive(); err != nil {
			return nil, err
		}
	}

	cipher, err := crypt.New(key, salt)
	if err != nil {
		return nil, err
	}
	return cipher, confirmKeys(c, cipher, role)
}

// confirmKeys makes each side prove it holds the same key, which is what
// turns a mistyped code into a clear error instead of garbled data.
func confirmKeys(c *comm.Conn, cipher *crypt.Cipher, role pake.Role) error {
	ours, theirs := confirmSender, confirmReceiver
	if role == pake.RoleB {
		ours, theirs = confirmReceiver, confirmSender
	}
	if err := sendEncrypted(c, cipher, []byte(ours)); err != nil {
		return err
	}
	got, err := receiveEncrypted(c, cipher)
	if err != nil || string(got) != theirs {
		return errors.New("the code did not match on the other side; check that both peers typed the same code")
	}
	return nil
}

func sendEncrypted(c *comm.Conn, cipher *crypt.Cipher, payload []byte) error {
	sealed, err := cipher.Seal(payload)
	if err != nil {
		return err
	}
	return c.Send(sealed)
}

func receiveEncrypted(c *comm.Conn, cipher *crypt.Cipher) ([]byte, error) {
	sealed, err := c.Receive()
	if err != nil {
		return nil, err
	}
	return cipher.Open(sealed)
}

func sendJSON(c *comm.Conn, cipher *crypt.Cipher, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return sendEncrypted(c, cipher, b)
}

func receiveJSON(c *comm.Conn, cipher *crypt.Cipher, v any) error {
	b, err := receiveEncrypted(c, cipher)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// safeJoin resolves a manifest path inside dir, refusing absolute paths and
// any ".." escape so a malicious sender cannot write outside the directory.
func safeJoin(dir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing unsafe path %q from the sender", name)
	}
	return filepath.Join(dir, clean), nil
}

// randRead is a seam so tests can avoid the system source if needed.
var randRead = cryptoRandRead

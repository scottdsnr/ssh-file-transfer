package transfer

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/scotthellings/croc-go/internal/comm"
	"github.com/scotthellings/croc-go/internal/crypt"
	"github.com/scotthellings/croc-go/internal/pake"
)

// ReceiveOptions configures a receive.
type ReceiveOptions struct {
	Relay   string
	Code    string
	OutDir  string
	Confirm func(Manifest) bool // asked before any file data arrives
	Status  func(string)
	Report  func(received, total int64)
	Force   bool // overwrite existing files instead of erroring
}

// Receive connects on the code, reviews the sender's manifest, and writes the
// accepted files into OutDir.
func Receive(opts ReceiveOptions) error {
	opts.status("connecting to the sender...")
	c, err := connect(opts.Relay, opts.Code)
	if err != nil {
		return err
	}
	defer c.Close()

	cipher, err := handshake(c, pake.RoleB, opts.Code)
	if err != nil {
		return err
	}
	opts.status("connected and encrypted")

	var manifest Manifest
	if err := receiveJSON(c, cipher, &manifest); err != nil {
		return err
	}
	if len(manifest.Files) == 0 {
		return fmt.Errorf("the sender offered no files")
	}
	if opts.Confirm != nil && !opts.Confirm(manifest) {
		sendEncrypted(c, cipher, []byte(answerReject))
		return fmt.Errorf("transfer declined")
	}
	if err := sendEncrypted(c, cipher, []byte(answerAccept)); err != nil {
		return err
	}

	outDir := opts.OutDir
	if outDir == "" {
		outDir = "."
	}
	total := manifest.TotalSize()
	var got int64
	for _, info := range manifest.Files {
		dest, err := safeJoin(outDir, info.Path)
		if err != nil {
			return err
		}
		opts.status(fmt.Sprintf("receiving %s (%s)", info.Path, humanBytes(info.Size)))
		n, err := receiveFile(c, cipher, dest, info, got, total, opts)
		if err != nil {
			sendEncrypted(c, cipher, []byte(err.Error()))
			return err
		}
		got += n
	}

	opts.status(fmt.Sprintf("received %d file(s), %s, into %s", len(manifest.Files), humanBytes(got), outDir))
	return sendEncrypted(c, cipher, []byte(answerAccept))
}

// receiveFile writes one file, verifying its hash before publishing it at the
// destination path. Data lands in a temporary file first so a failed or
// corrupted transfer never leaves a half-written file behind.
func receiveFile(c *comm.Conn, cipher *crypt.Cipher, dest string, info FileInfo, gotSoFar, total int64, opts ReceiveOptions) (int64, error) {
	if !opts.Force {
		if _, err := os.Stat(dest); err == nil {
			return 0, fmt.Errorf("%s already exists (use --force to overwrite)", dest)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".fsi-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	h := sha256.New()
	var written int64
	for {
		payload, err := receiveEncrypted(c, cipher)
		if err != nil {
			return written, err
		}
		if len(payload) == 0 {
			return written, fmt.Errorf("the sender sent an empty frame")
		}
		if payload[0] == frameEnd {
			break
		}
		if payload[0] != frameData {
			return written, fmt.Errorf("the sender sent an unknown frame type %d", payload[0])
		}
		chunk := payload[1:]
		if _, err := tmp.Write(chunk); err != nil {
			return written, err
		}
		h.Write(chunk)
		written += int64(len(chunk))
		if written > info.Size {
			return written, fmt.Errorf("the sender sent more data than it announced for %s", info.Path)
		}
		if opts.Report != nil {
			opts.Report(gotSoFar+written, total)
		}
	}

	if written != info.Size {
		return written, fmt.Errorf("%s is %d bytes but the sender announced %d", info.Path, written, info.Size)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sum), []byte(info.SHA256)) != 1 {
		return written, fmt.Errorf("%s failed its checksum: got %s, expected %s", info.Path, sum, info.SHA256)
	}

	mode := info.Mode.Perm()
	if mode == 0 {
		mode = 0o644
	}
	if err := tmp.Chmod(mode); err != nil {
		return written, err
	}
	if err := tmp.Close(); err != nil {
		return written, err
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return written, err
	}
	return written, nil
}

func (o ReceiveOptions) status(msg string) {
	if o.Status != nil {
		o.Status(msg)
	}
}

// humanBytes formats a byte count for display.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

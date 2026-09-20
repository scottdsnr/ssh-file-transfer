package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/scotthellings/croc-go/internal/comm"
	"github.com/scotthellings/croc-go/internal/crypt"
	"github.com/scotthellings/croc-go/internal/pake"
)

// SendOptions configures a send.
type SendOptions struct {
	Relay  string
	Code   string
	Paths  []string
	Status func(string)            // human-readable progress lines
	Report func(sent, total int64) // byte-level progress, may be nil
}

// Send builds a manifest from paths, waits for a receiver on the code, and
// streams the files once the receiver accepts.
func Send(opts SendOptions) error {
	manifest, sources, err := buildManifest(opts.Paths)
	if err != nil {
		return err
	}

	opts.status("waiting for the receiver to connect...")
	c, err := connect(opts.Relay, opts.Code)
	if err != nil {
		return err
	}
	defer c.Close()

	cipher, err := handshake(c, pake.RoleA, opts.Code)
	if err != nil {
		return err
	}
	opts.status("connected and encrypted; offering files")

	if err := sendJSON(c, cipher, manifest); err != nil {
		return err
	}
	answer, err := receiveEncrypted(c, cipher)
	if err != nil {
		return err
	}
	if string(answer) != answerAccept {
		return fmt.Errorf("the receiver declined the transfer")
	}

	total := manifest.TotalSize()
	var sent int64
	for i, info := range manifest.Files {
		opts.status(fmt.Sprintf("sending %s (%s)", info.Path, humanBytes(info.Size)))
		n, err := sendFile(c, cipher, sources[i], sent, total, opts.Report)
		if err != nil {
			return fmt.Errorf("sending %s: %w", info.Path, err)
		}
		sent += n
	}

	// Wait for the receiver to confirm every hash before we exit.
	final, err := receiveEncrypted(c, cipher)
	if err != nil {
		return err
	}
	if string(final) != answerAccept {
		return fmt.Errorf("the receiver reported a problem: %s", final)
	}
	opts.status("transfer complete")
	return nil
}

// sendFile streams one file as encrypted chunks followed by an end marker.
func sendFile(c *comm.Conn, cipher *crypt.Cipher, path string, sentSoFar, total int64, report func(int64, int64)) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, ChunkSize)
	var sent int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if err := sendEncrypted(c, cipher, append([]byte{frameData}, buf[:n]...)); err != nil {
				return sent, err
			}
			sent += int64(n)
			if report != nil {
				report(sentSoFar+sent, total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return sent, err
		}
	}
	return sent, sendEncrypted(c, cipher, []byte{frameEnd})
}

// buildManifest expands the given paths (walking directories) into the
// manifest plus the matching on-disk source paths.
func buildManifest(paths []string) (Manifest, []string, error) {
	var m Manifest
	var sources []string
	if len(paths) == 0 {
		return m, nil, fmt.Errorf("no files to send")
	}

	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return m, nil, err
		}
		if !st.IsDir() {
			info, err := describe(p, filepath.Base(p))
			if err != nil {
				return m, nil, err
			}
			m.Files = append(m.Files, info)
			sources = append(sources, p)
			continue
		}

		root := filepath.Clean(p)
		base := filepath.Base(root)
		err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || !fi.Mode().IsRegular() {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			info, err := describe(path, filepath.ToSlash(filepath.Join(base, rel)))
			if err != nil {
				return err
			}
			m.Files = append(m.Files, info)
			sources = append(sources, path)
			return nil
		})
		if err != nil {
			return m, nil, err
		}
	}
	if len(m.Files) == 0 {
		return m, nil, fmt.Errorf("no regular files found in the given paths")
	}
	return m, sources, nil
}

// describe stats and hashes one file so the receiver can verify it.
func describe(path, name string) (FileInfo, error) {
	st, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return FileInfo{}, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return FileInfo{}, err
	}
	return FileInfo{
		Path:   name,
		Size:   st.Size(),
		Mode:   st.Mode().Perm(),
		SHA256: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func (o SendOptions) status(msg string) {
	if o.Status != nil {
		o.Status(msg)
	}
}

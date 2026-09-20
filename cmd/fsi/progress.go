package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// progress renders single-line status and a byte progress bar on stderr,
// rate-limited so a fast transfer does not spend its time drawing.
type progress struct {
	lastDraw time.Time
	dirty    bool
}

func newProgress() *progress { return &progress{} }

func (p *progress) status(msg string) {
	p.clear()
	fmt.Fprintln(os.Stderr, msg)
}

func (p *progress) report(done, total int64) {
	if time.Since(p.lastDraw) < 80*time.Millisecond && done != total {
		return
	}
	p.lastDraw = time.Now()
	p.dirty = true

	ratio := 1.0
	if total > 0 {
		ratio = float64(done) / float64(total)
	}
	const width = 30
	filled := int(ratio * width)
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", width-filled)
	fmt.Fprintf(os.Stderr, "\r  [%s] %5.1f%%  %s / %s", bar, ratio*100, humanBytes(done), humanBytes(total))
	if done == total {
		fmt.Fprintln(os.Stderr)
		p.dirty = false
	}
}

// clear wipes a partially drawn bar before printing a normal line.
func (p *progress) clear() {
	if p.dirty {
		fmt.Fprint(os.Stderr, "\r\033[K")
		p.dirty = false
	}
}

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

package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/scotthellings/croc-go/internal/relay"
	"github.com/scotthellings/croc-go/internal/ws"
)

// serve starts a relay inside this process and returns the URL to dial it on
// plus the port it bound, so a transfer needs no public relay at all. The
// sender is its own rendezvous point: it connects to this relay like any other
// peer, which keeps the pairing and encryption paths identical.
func serve(listen string) (localURL string, port int, err error) {
	logger := log.New(io.Discard, "", 0)
	if os.Getenv("FSI_DEBUG") != "" {
		logger = log.New(os.Stderr, "relay: ", log.LstdFlags)
	}

	addrs := make(chan net.Addr, 1)
	errs := make(chan error, 1)
	go func() {
		errs <- relay.NewServer(logger).ListenAndServeHTTP(listen, func(a net.Addr) { addrs <- a })
	}()

	select {
	case a := <-addrs:
		tcp, ok := a.(*net.TCPAddr)
		if !ok {
			return "", 0, fmt.Errorf("unexpected listener address %s", a)
		}
		return fmt.Sprintf("ws://127.0.0.1:%d%s", tcp.Port, ws.DefaultPath), tcp.Port, nil
	case err := <-errs:
		return "", 0, err
	case <-time.After(10 * time.Second):
		return "", 0, fmt.Errorf("the embedded relay did not start in time")
	}
}

// quickTunnelURL matches the hostname cloudflared prints for a quick tunnel.
var quickTunnelURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// tunnel exposes a locally hosted relay through a Cloudflare quick tunnel and
// returns the public URL a receiver should use. Quick tunnels need no
// Cloudflare account, but they do need the cloudflared binary on this machine
// and Cloudflare offers them with no uptime guarantee.
func tunnel(port int) (publicURL string, stop func(), err error) {
	bin, err := exec.LookPath("cloudflared")
	if err != nil {
		return "", nil, fmt.Errorf("cloudflared is not installed; install it or drop --tunnel and share the address yourself")
	}

	cmd := exec.Command(bin, "tunnel", "--no-autoupdate", "--url", fmt.Sprintf("http://127.0.0.1:%d", port))
	// cloudflared reports the assigned hostname on stderr, mixed into its log.
	pipe, err := cmd.StderrPipe()
	if err != nil {
		return "", nil, err
	}
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}
	stop = func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
	}

	found := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		debug := os.Getenv("FSI_DEBUG") != ""
		for scanner.Scan() {
			line := scanner.Text()
			if debug {
				fmt.Fprintln(os.Stderr, "cloudflared:", line)
			}
			if m := quickTunnelURL.FindString(line); m != "" {
				select {
				case found <- m:
				default:
				}
			}
		}
	}()

	select {
	case host := <-found:
		return host + ws.DefaultPath, stop, nil
	case <-time.After(30 * time.Second):
		stop()
		return "", nil, fmt.Errorf("cloudflared did not report a tunnel hostname within 30s")
	}
}

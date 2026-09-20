// Command fsi sends and receives files between two machines, pairing them
// with a short spoken code and encrypting everything end to end.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/scotthellings/croc-go/internal/relay"
	"github.com/scotthellings/croc-go/internal/transfer"
	"github.com/scotthellings/croc-go/internal/words"
	"github.com/scotthellings/croc-go/internal/ws"
)

// DefaultRelay is used when neither --relay nor FSI_RELAY is set.
const DefaultRelay = "localhost:9009"

// version is stamped in at build time by the release workflow.
var version = "dev"

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "send":
		err = runSend(os.Args[2:])
	case "receive", "recv":
		err = runReceive(os.Args[2:])
	case "relay":
		err = runRelay(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	case "-v", "--version", "version":
		fmt.Println("fsi", version)
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `fsi moves files between two computers, encrypted end to end.

Usage:
  fsi send [--relay ADDR] [--code CODE] [--direct] [--tunnel] <path>...
  fsi receive [--relay ADDR] [--out DIR] [--yes] [--force] [CODE]
  fsi relay [--listen :9009] [--ws]
  fsi version

The sender prints a code; type that same code on the receiver. The code is
never sent over the network, so the relay cannot read your files.

ADDR is either host:port for a raw TCP relay or a ws://, wss://, http:// or
https:// URL for a relay reached over WebSocket.

Without a relay:
  --direct  hosts the rendezvous in the sending process, so nothing but the
            two peers is involved. Needs the receiver to be able to reach
            this machine: a LAN, a VPN, or a forwarded port.
  --tunnel  does the same but publishes it through a Cloudflare quick tunnel,
            which works from anywhere. Needs cloudflared installed here. The
            hostname is random, so the receiver needs the printed --relay URL
            as well as the code.
`)
}

func runSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	relayAddr := fs.String("relay", defaultRelay(), "relay server address")
	code := fs.String("code", "", "transfer code (generated when omitted)")
	direct := fs.Bool("direct", false, "host the rendezvous in this process instead of using a relay")
	listen := fs.String("listen", "", "address the hosted rendezvous listens on (with --direct or --tunnel)")
	useTunnel := fs.Bool("tunnel", false, "publish the hosted rendezvous through a Cloudflare quick tunnel")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return fmt.Errorf("give at least one file or directory to send")
	}
	if *code == "" {
		*code = words.Generate()
	}

	// --tunnel implies hosting: there has to be something local to tunnel to,
	// and the port is Cloudflare's business rather than the receiver's.
	if *useTunnel {
		*direct = true
	}
	if *listen == "" {
		// A tunnel reaches the relay from localhost, so let the kernel pick
		// the port; a direct transfer needs a port the receiver can predict.
		if *useTunnel {
			*listen = "127.0.0.1:0"
		} else {
			*listen = ":9009"
		}
	}

	peerAddr := *relayAddr
	if *direct {
		localURL, port, err := serve(*listen)
		if err != nil {
			return err
		}
		*relayAddr = localURL
		peerAddr = localURL

		if *useTunnel {
			publicURL, stop, err := tunnel(port)
			if err != nil {
				return err
			}
			defer stop()
			peerAddr = publicURL
		} else {
			peerAddr = advertisedURL(port)
		}
	}

	if *direct {
		fmt.Printf("Code is: %s\nOn the other machine run:\n\n    fsi receive --relay %s %s\n\n", *code, peerAddr, *code)
	} else {
		fmt.Printf("Code is: %s\nOn the other machine run:\n\n    fsi receive %s\n\n", *code, *code)
	}

	bar := newProgress()
	return transfer.Send(transfer.SendOptions{
		Relay:  *relayAddr,
		Code:   *code,
		Paths:  fs.Args(),
		Status: bar.status,
		Report: bar.report,
	})
}

func runReceive(args []string) error {
	fs := flag.NewFlagSet("receive", flag.ExitOnError)
	relayAddr := fs.String("relay", defaultRelay(), "relay server address")
	out := fs.String("out", ".", "directory to write files into")
	yes := fs.Bool("yes", false, "accept the transfer without prompting")
	force := fs.Bool("force", false, "overwrite existing files")
	fs.Parse(args)

	code := strings.TrimSpace(fs.Arg(0))
	if code == "" {
		var err error
		if code, err = prompt("Enter the code: "); err != nil {
			return err
		}
	}

	bar := newProgress()
	return transfer.Receive(transfer.ReceiveOptions{
		Relay:   *relayAddr,
		Code:    code,
		OutDir:  *out,
		Force:   *force,
		Status:  bar.status,
		Report:  bar.report,
		Confirm: confirmFunc(*yes),
	})
}

// confirmFunc shows what the sender is offering and asks to proceed, since
// accepting means writing someone else's files to this disk.
func confirmFunc(auto bool) func(transfer.Manifest) bool {
	return func(m transfer.Manifest) bool {
		fmt.Printf("\nSender is offering %d file(s), %s total:\n", len(m.Files), humanBytes(m.TotalSize()))
		for _, f := range m.Files {
			fmt.Printf("  %s (%s)\n", f.Path, humanBytes(f.Size))
		}
		if auto {
			return true
		}
		answer, err := prompt("Accept? [y/N]: ")
		if err != nil {
			return false
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		return answer == "y" || answer == "yes"
	}
}

func runRelay(args []string) error {
	fs := flag.NewFlagSet("relay", flag.ExitOnError)
	listen := fs.String("listen", ":9009", "address to listen on")
	useWS := fs.Bool("ws", false, "serve WebSockets over HTTP instead of raw TCP")
	fs.Parse(args)
	srv := relay.NewServer(log.New(os.Stderr, "", log.LstdFlags))
	if *useWS {
		return srv.ListenAndServeHTTP(*listen, nil)
	}
	return srv.ListenAndServe(*listen)
}

// advertisedURL builds the URL a receiver on another machine should dial when
// we host the rendezvous ourselves. The host part is a guess at best, so it is
// printed for the human to correct rather than relied on.
func advertisedURL(port int) string {
	host := "127.0.0.1"
	if ip := outboundIP(); ip != "" {
		host = ip
	}
	return fmt.Sprintf("ws://%s%s", net.JoinHostPort(host, strconv.Itoa(port)), ws.DefaultPath)
}

// outboundIP asks the kernel which local address it would use to reach the
// internet, which is the closest thing to "my address on this network".
func outboundIP() string {
	c, err := net.Dial("udp", "203.0.113.1:9")
	if err != nil {
		return ""
	}
	defer c.Close()
	if addr, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

func defaultRelay() string {
	if v := os.Getenv("FSI_RELAY"); v != "" {
		return v
	}
	return DefaultRelay
}

func prompt(msg string) (string, error) {
	fmt.Print(msg)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line), err
}

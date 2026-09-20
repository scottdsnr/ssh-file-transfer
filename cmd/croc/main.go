// Command croc-go sends and receives files between two machines, pairing them
// with a short spoken code and encrypting everything end to end.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/scotthellings/croc-go/internal/relay"
	"github.com/scotthellings/croc-go/internal/transfer"
	"github.com/scotthellings/croc-go/internal/words"
)

// DefaultRelay is used when neither --relay nor CROC_GO_RELAY is set.
const DefaultRelay = "localhost:9009"

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
	fmt.Fprint(os.Stderr, `croc-go moves files between two computers, encrypted end to end.

Usage:
  croc-go send [--relay host:port] [--code CODE] <path>...
  croc-go receive [--relay host:port] [--out DIR] [--yes] [--force] [CODE]
  croc-go relay [--listen :9009]

The sender prints a code; type that same code on the receiver. The code is
never sent over the network, so the relay cannot read your files.
`)
}

func runSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	relayAddr := fs.String("relay", defaultRelay(), "relay server address")
	code := fs.String("code", "", "transfer code (generated when omitted)")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return fmt.Errorf("give at least one file or directory to send")
	}
	if *code == "" {
		*code = words.Generate()
	}

	fmt.Printf("Code is: %s\nOn the other machine run:\n\n    croc-go receive %s\n\n", *code, *code)

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
	fs.Parse(args)
	return relay.NewServer(log.New(os.Stderr, "", log.LstdFlags)).ListenAndServe(*listen)
}

func defaultRelay() string {
	if v := os.Getenv("CROC_GO_RELAY"); v != "" {
		return v
	}
	return DefaultRelay
}

func prompt(msg string) (string, error) {
	fmt.Print(msg)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line), err
}

# ssh-file-transfer

`croc-go` moves files between two machines, paired by a short spoken code and
encrypted end to end (SPAKE2 over the code, ChaCha20-Poly1305 for the data).
The code never crosses the network, so no rendezvous point can read the files.

## Install (end users)

No Go, no compiler, no checkout — grab a prebuilt binary:

    curl -fsSL https://raw.githubusercontent.com/scottdsnr/ssh-file-transfer/master/scripts/get-croc.sh | sh

Add `-s -- --with-cloudflared` to also install cloudflared, which is what lets
`croc send --tunnel` work from anywhere:

    curl -fsSL .../get-croc.sh | sh -s -- --with-cloudflared

It detects your OS and CPU (Linux, macOS and Windows; x86-64 and arm64),
verifies the published SHA-256, and installs to `~/.local/bin`. Use
`--prefix DIR` to put it elsewhere and `--version vX.Y.Z` to pin a release.
Binaries are also downloadable by hand from the
[releases page](https://github.com/scottdsnr/ssh-file-transfer/releases).

Then send something:

    croc send --tunnel myfile.zip     # works over the internet
    croc send --direct myfile.zip     # same network only, no cloudflared

croc prints one command line; the other person runs it. That's the whole flow.

## Requirements (building from source)

- Go 1.27.1 or newer (the only build dependency; the code itself uses just the
  standard library plus `golang.org/x/crypto`).
- `cloudflared`, on the sending machine only, and only for `--tunnel`. No
  Cloudflare account needed.

## Install from source

    ./scripts/install.sh                     # build, test, install to ~/.local/bin
    ./scripts/install.sh --with-cloudflared  # also fetch cloudflared, for --tunnel
    ./scripts/install.sh --with-go           # also fetch the Go toolchain if missing
    ./scripts/install.sh --prefix /usr/local # install somewhere else

Or by hand:

    go build -o bin/croc ./cmd/croc

## Test

    make test    # go vet + go test ./...
    make smoke   # a real 300 KB transfer between two processes, no relay

`make smoke` is the one that proves the serverless path end to end: it hosts the
rendezvous in a sending process and receives from another over loopback.

## Serverless: no public relay

**Direct** — the sender hosts the rendezvous itself. Nothing but the two peers
is involved. The receiver must be able to reach the sender: LAN, VPN
(Tailscale/WireGuard), or a forwarded port.

    croc send --direct big.bin
    # prints: croc receive --relay ws://192.168.1.20:9019/croc <code>

**Cloudflare quick tunnel** — same, but published to the internet.

    croc send --tunnel big.bin
    # prints: croc receive --relay https://odd-random-words.trycloudflare.com/croc <code>

Requires `cloudflared` on the sending machine only; no Cloudflare account. The
tunnel hostname is random, so the receiver needs the printed `--relay` URL in
addition to the code — that is the price of having no server to shorten it.
Cloudflare offers quick tunnels with no uptime guarantee.

## With a relay

    croc relay --listen :9009         # raw TCP
    croc relay --listen :8080 --ws    # WebSocket, for putting it behind a proxy

    croc send --relay host:9009 big.bin
    croc receive --relay host:9009 <code>

`--relay` takes either `host:port` (raw TCP) or a `ws://`, `wss://`, `http://`
or `https://` URL (WebSocket). `CROC_GO_RELAY` sets the default.

## Layout

    cmd/croc            CLI, embedded hosting, cloudflared supervision
    internal/words      code words and the room name derived from a code
    internal/pake       SPAKE2 over the code
    internal/crypt      authenticated encryption of every frame
    internal/comm       length-prefixed framing, TCP or WebSocket
    internal/ws         minimal RFC 6455 transport (stdlib only)
    internal/relay      pairs two peers on a room, then pipes ciphertext
    internal/transfer   manifest, handshake, file streaming

Set `CROC_GO_DEBUG=1` to see relay and cloudflared logs on the sender.

# ssh-file-transfer

`fsi` moves files between two machines, paired by a short spoken code and
encrypted end to end (SPAKE2 over the code, ChaCha20-Poly1305 for the data).
The code never crosses the network, so no rendezvous point can read the files.

## Install (end users)

No Go, no compiler, no checkout — grab a prebuilt binary:

    curl -fsSL https://raw.githubusercontent.com/scottdsnr/ssh-file-transfer/master/scripts/get-fsi.sh | sh

Add `-s -- --with-cloudflared` to also install cloudflared, which is what lets
`fsi send --tunnel` work from anywhere:

    curl -fsSL .../get-fsi.sh | sh -s -- --with-cloudflared

It detects your OS and CPU (Linux, macOS and Windows; x86-64 and arm64),
verifies the published SHA-256, and installs to `~/.local/bin`. Use
`--prefix DIR` to put it elsewhere and `--version vX.Y.Z` to pin a release.
Binaries are also downloadable by hand from the
[releases page](https://github.com/scottdsnr/ssh-file-transfer/releases).

Then send something:

    fsi send --tunnel myfile.zip     # works over the internet
    fsi send --direct myfile.zip     # same network only, no cloudflared
    fsi send --web --tunnel myfile.zip  # recipient just opens a link in a browser

fsi prints one command line; the other person runs it. That's the whole flow.

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

    go build -o bin/fsi ./cmd/fsi

## Test

    make test    # go vet + go test ./...
    make smoke   # a real 300 KB transfer between two processes, no relay

`make smoke` is the one that proves the serverless path end to end: it hosts the
rendezvous in a sending process and receives from another over loopback.

## Serverless: no public relay

**Direct** — the sender hosts the rendezvous itself. Nothing but the two peers
is involved. The receiver must be able to reach the sender: LAN, VPN
(Tailscale/WireGuard), or a forwarded port.

    fsi send --direct big.bin
    # prints: fsi receive --relay ws://192.168.1.20:9019/fsi <code>

**Cloudflare quick tunnel** — same, but published to the internet.

    fsi send --tunnel big.bin
    # prints: fsi receive --relay https://odd-random-words.trycloudflare.com/fsi <code>

Requires `cloudflared` on the sending machine only; no Cloudflare account. The
tunnel hostname is random, so the receiver needs the printed `--relay` URL in
addition to the code — that is the price of having no server to shorten it.
Cloudflare offers quick tunnels with no uptime guarantee.

## Browser recipients: no terminal needed

Some recipients cannot run a command at all. `--web` serves a small download
page from the sending machine — still serverless, still nothing but your
machine hosting it — so they just open a link and click.

    fsi send --web --tunnel report.pdf photos/
    # prints: https://odd-random-words.trycloudflare.com/w/1234-cobalt-badger-orbit

The link contains the code, so it is the secret: anyone holding it can download
the files. The page lists every file, supports resuming an interrupted download
(HTTP range requests), and offers a single `.zip` when there is more than one
file. It stays up until you press Ctrl-C.

`--web` implies `--direct`, and combines with `--tunnel` to reach someone on
the other side of the internet. It serves on the same port as the rendezvous,
so one tunnel covers both.

**The caveat, plainly:** this path is not end to end encrypted, because a
browser has no code to run the PAKE with. With `--direct` the bytes cross your
LAN in the clear; with `--tunnel` the link is HTTPS, but Cloudflare terminates
that TLS and could read the files. For end to end secrecy, use the normal
`fsi send` / `fsi receive` pair. Because `--web` has no peer running the
protocol, it does not also accept a CLI receiver — run a normal send for that.

## With a relay

    fsi relay --listen :9009         # raw TCP
    fsi relay --listen :8080 --ws    # WebSocket, for putting it behind a proxy

    fsi send --relay host:9009 big.bin
    fsi receive --relay host:9009 <code>

`--relay` takes either `host:port` (raw TCP) or a `ws://`, `wss://`, `http://`
or `https://` URL (WebSocket). `FSI_RELAY` sets the default.

## Layout

    cmd/croc            CLI, embedded hosting, cloudflared supervision
    internal/words      code words and the room name derived from a code
    internal/pake       SPAKE2 over the code
    internal/crypt      authenticated encryption of every frame
    internal/comm       length-prefixed framing, TCP or WebSocket
    internal/ws         minimal RFC 6455 transport (stdlib only)
    internal/web        browser download page for terminal-less recipients
    internal/relay      pairs two peers on a room, then pipes ciphertext
    internal/transfer   manifest, handshake, file streaming

Set `FSI_DEBUG=1` to see relay and cloudflared logs on the sender.

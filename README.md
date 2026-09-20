# ssh-file-transfer

`croc-go` moves files between two machines, paired by a short spoken code and
encrypted end to end (SPAKE2 over the code, ChaCha20-Poly1305 for the data).
The code never crosses the network, so no rendezvous point can read the files.

## Install

    go build -o croc ./cmd/croc

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

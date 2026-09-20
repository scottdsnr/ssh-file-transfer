#!/usr/bin/env bash
# End-to-end check of --direct: one process hosts the rendezvous, another
# receives, and the bytes must come out identical. No relay, no network beyond
# loopback.
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fsi="${FSI:-$repo/bin/fsi}"
[ -x "$fsi" ] || {
	echo "build first: go build -o bin/fsi ./cmd/fsi" >&2
	exit 1
}

port="${PORT:-9019}"
code="1234-cobalt-badger-orbit"
work="$(mktemp -d)"
trap 'rm -rf "$work"; [ -n "${sender:-}" ] && kill "$sender" 2>/dev/null || true' EXIT

mkdir -p "$work/out"
head -c 300000 /dev/urandom >"$work/payload.bin"

"$fsi" send --direct --listen "127.0.0.1:$port" --code "$code" "$work/payload.bin" >"$work/send.log" 2>&1 &
sender=$!

# Wait for the hosted rendezvous to accept connections before receiving.
for _ in $(seq 50); do
	if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then break; fi
	sleep 0.1
done

"$fsi" receive --relay "ws://127.0.0.1:$port/fsi" --out "$work/out" --yes "$code" >"$work/recv.log" 2>&1

wait "$sender" || {
	echo "sender failed:" >&2
	cat "$work/send.log" >&2
	exit 1
}
sender=

if cmp -s "$work/payload.bin" "$work/out/payload.bin"; then
	echo "smoke: OK, 300000 bytes transferred with no relay"
else
	echo "smoke: FAILED, files differ" >&2
	cat "$work/send.log" "$work/recv.log" >&2
	exit 1
fi

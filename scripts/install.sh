#!/usr/bin/env bash
# Build croc-go, run its tests, and optionally install the tools it needs.
#
#   ./scripts/install.sh                     build + test + install to ~/.local/bin
#   ./scripts/install.sh --with-cloudflared  also install cloudflared (for --tunnel)
#   ./scripts/install.sh --with-go           also install the Go toolchain if missing
#   ./scripts/install.sh --prefix /usr/local install the binary somewhere else
#   ./scripts/install.sh --no-test           skip the test suite
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
prefix="$HOME/.local"
with_cloudflared=0
with_go=0
run_tests=1

while [ $# -gt 0 ]; do
	case "$1" in
	--with-cloudflared) with_cloudflared=1 ;;
	--with-go) with_go=1 ;;
	--no-test) run_tests=0 ;;
	--prefix)
		prefix="${2:?--prefix needs a directory}"
		shift
		;;
	-h | --help)
		sed -n '2,10p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "unknown option: $1" >&2
		exit 2
		;;
	esac
	shift
done

say() { printf '\033[1m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[33mwarning:\033[0m %s\n' "$*" >&2; }
die() {
	printf '\033[31merror:\033[0m %s\n' "$*" >&2
	exit 1
}

# platform reports the GOOS/GOARCH-style pair used by the download URLs below.
platform() {
	local os arch
	case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "unsupported OS $(uname -s); build manually with: go build ./cmd/croc" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported architecture $(uname -m)" ;;
	esac
	echo "$os-$arch"
}

# ---------------------------------------------------------------- Go toolchain

# go_version_wanted reads the minimum Go version straight from go.mod, so this
# script cannot drift from what the module actually requires.
go_version_wanted() {
	awk '/^go /{print $2; exit}' "$repo/go.mod"
}

install_go() {
	local want plat url dest
	want="$(go_version_wanted)"
	plat="$(platform)"
	# go.dev names files goVERSION.os-arch.tar.gz, e.g. go1.27.1.linux-amd64.tar.gz
	url="https://go.dev/dl/go${want}.${plat}.tar.gz"
	dest="$prefix/go"

	say "installing Go $want into $dest"
	mkdir -p "$prefix"
	rm -rf "$dest"
	curl -fsSL "$url" | tar -C "$prefix" -xzf -
	export PATH="$dest/bin:$PATH"
	say "add this to your shell profile: export PATH=\"$dest/bin:\$PATH\""
}

ensure_go() {
	if command -v go >/dev/null 2>&1; then
		say "found $(go version)"
		return
	fi
	if [ -x "$prefix/go/bin/go" ]; then
		export PATH="$prefix/go/bin:$PATH"
		say "found $(go version)"
		return
	fi
	if [ "$with_go" = 1 ]; then
		install_go
		return
	fi
	die "Go $(go_version_wanted) or newer is required. Install it from https://go.dev/dl/ or rerun with --with-go"
}

# --------------------------------------------------------------- cloudflared

# cloudflared is only needed by the sender's --tunnel mode. Everything else,
# including --direct, works without it.
install_cloudflared() {
	if command -v cloudflared >/dev/null 2>&1; then
		say "found $(cloudflared --version 2>&1 | head -1)"
		return
	fi
	local plat url dest
	plat="$(platform)"
	url="https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-${plat}"
	dest="$prefix/bin/cloudflared"

	say "installing cloudflared into $dest"
	mkdir -p "$prefix/bin"
	curl -fsSL -o "$dest.tmp" "$url"
	chmod +x "$dest.tmp"
	mv "$dest.tmp" "$dest"
	"$dest" --version 2>&1 | head -1
}

# ------------------------------------------------------------- build and test

ensure_go
[ "$with_cloudflared" = 1 ] && install_cloudflared

say "building"
mkdir -p "$repo/bin"
(cd "$repo" && go build -o "$repo/bin/croc" ./cmd/croc)

if [ "$run_tests" = 1 ]; then
	say "vetting"
	(cd "$repo" && go vet ./...)
	say "testing"
	(cd "$repo" && go test ./...)
fi

say "installing to $prefix/bin/croc"
mkdir -p "$prefix/bin"
install -m 0755 "$repo/bin/croc" "$prefix/bin/croc"

case ":$PATH:" in
*":$prefix/bin:"*) ;;
*) warn "$prefix/bin is not on your PATH; add: export PATH=\"$prefix/bin:\$PATH\"" ;;
esac

cat <<EOF

Done. Try a transfer against yourself:

    croc send --direct --listen 127.0.0.1:9019 ./README.md
    croc receive --relay ws://127.0.0.1:9019/croc --out /tmp <code>

Over the internet with no relay (needs cloudflared):

    croc send --tunnel ./README.md
EOF

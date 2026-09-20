#!/usr/bin/env bash
# One-step installer for people who just want to move a file. Downloads a
# prebuilt croc binary, verifies its checksum, and puts it on the PATH. No Go
# toolchain, no compiler, no repository checkout.
#
#   curl -fsSL https://raw.githubusercontent.com/scottdsnr/ssh-file-transfer/master/scripts/get-croc.sh | sh
#
# Options (as flags, or env vars when piping into sh):
#   --version TAG       install a specific release       (CROC_VERSION)
#   --prefix DIR        install under DIR/bin            (CROC_PREFIX)
#   --with-cloudflared  also install cloudflared, for `croc send --tunnel`
#   --no-verify         skip checksum verification
set -eu

repo_slug="${CROC_REPO:-scottdsnr/ssh-file-transfer}"
version="${CROC_VERSION:-latest}"
prefix="${CROC_PREFIX:-$HOME/.local}"
with_cloudflared=0
verify=1

while [ $# -gt 0 ]; do
	case "$1" in
	--version) version="${2:?--version needs a tag}"; shift ;;
	--prefix) prefix="${2:?--prefix needs a directory}"; shift ;;
	--with-cloudflared) with_cloudflared=1 ;;
	--no-verify) verify=0 ;;
	-h|--help) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
	shift
done

say() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "curl is required"

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
MINGW* | MSYS* | CYGWIN*) os=windows ;;
*) die "unsupported OS $(uname -s). Build from source instead: https://github.com/$repo_slug" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture $(uname -m). Build from source instead." ;;
esac

asset="croc_${os}-${arch}"
binary="croc"
if [ "$os" = windows ]; then
	asset="$asset.exe"
	binary="croc.exe"
fi

# GitHub serves /releases/latest/download/ for the newest release and
# /releases/download/TAG/ for a pinned one.
if [ "$version" = latest ]; then
	base="https://github.com/$repo_slug/releases/latest/download"
else
	base="https://github.com/$repo_slug/releases/download/$version"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "downloading $asset ($version)"
curl -fsSL --proto '=https' --tlsv1.2 -o "$tmp/$asset" "$base/$asset" ||
	die "no prebuilt binary for $os-$arch at $version. See https://github.com/$repo_slug/releases"

# The checksum file guards against a truncated or tampered download. It is
# fetched over the same TLS connection, so it is an integrity check, not a
# signature.
if [ "$verify" = 1 ]; then
	if curl -fsSL --proto '=https' -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" 2>/dev/null; then
		if command -v sha256sum >/dev/null 2>&1; then
			sum="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
		elif command -v shasum >/dev/null 2>&1; then
			sum="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
		else
			sum=""
			say "no sha256 tool found; skipping verification"
		fi
		if [ -n "$sum" ]; then
			want="$(awk -v a="$asset" '$2 == a || $2 == "*"a {print $1}' "$tmp/SHA256SUMS")"
			[ -n "$want" ] || die "$asset is missing from SHA256SUMS"
			[ "$sum" = "$want" ] || die "checksum mismatch for $asset; refusing to install"
			say "checksum verified"
		fi
	else
		say "no SHA256SUMS published for $version; skipping verification"
	fi
fi

mkdir -p "$prefix/bin"
chmod +x "$tmp/$asset"
mv "$tmp/$asset" "$prefix/bin/$binary"
say "installed $prefix/bin/$binary"

if [ "$with_cloudflared" = 1 ]; then
	if command -v cloudflared >/dev/null 2>&1; then
		say "cloudflared already installed"
	else
		say "downloading cloudflared"
		cf="cloudflared-${os}-${arch}"
		[ "$os" = windows ] && cf="$cf.exe"
		curl -fsSL --proto '=https' -o "$prefix/bin/cloudflared" \
			"https://github.com/cloudflare/cloudflared/releases/latest/download/$cf" ||
			die "could not download cloudflared for $os-$arch"
		chmod +x "$prefix/bin/cloudflared"
		say "installed $prefix/bin/cloudflared"
	fi
fi

case ":${PATH:-}:" in
*":$prefix/bin:"*) ;;
*) say "add $prefix/bin to your PATH: export PATH=\"$prefix/bin:\$PATH\"" ;;
esac

cat <<EOF

croc is ready.

Send a file (nothing to set up, works over the internet if you have cloudflared):

    croc send --tunnel myfile.zip

On the same network, no cloudflared needed:

    croc send --direct myfile.zip

Either way croc prints one line to pass to the person receiving. They run it.
EOF

#!/bin/sh
# Installs the latest tuxmedic release into ~/.local/bin, or into BINDIR.
#   curl -fsSL https://raw.githubusercontent.com/rezk2ll/tuxmedic/main/install.sh | sh
set -eu

repo=rezk2ll/tuxmedic
bindir=${BINDIR:-"$HOME/.local/bin"}

case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	riscv64) arch=riscv64 ;;
	*) echo "tuxmedic: no release for $(uname -m). Build it with: go install github.com/$repo/cmd/tuxmedic@latest" >&2; exit 1 ;;
esac

if command -v curl >/dev/null 2>&1; then
	get() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
	get() { wget -qO- "$1"; }
else
	echo "tuxmedic: install curl or wget first" >&2
	exit 1
fi

tag=${VERSION:-$(get "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)}
[ -n "$tag" ] || { echo "tuxmedic: couldn't find the latest release" >&2; exit 1; }
version=${tag#v}
archive="tuxmedic_${version}_linux_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading tuxmedic $tag for $arch…"
get "$base/$archive" >"$tmp/$archive"
get "$base/checksums.txt" >"$tmp/checksums.txt"

(cd "$tmp" && grep " $archive\$" checksums.txt | sha256sum -c - >/dev/null) || {
	echo "tuxmedic: checksum mismatch, not installing" >&2
	exit 1
}

tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$bindir"
install -m 0755 "$tmp/tuxmedic" "$bindir/tuxmedic"
echo "Installed $bindir/tuxmedic"

# Man page and completions, into the per-user folders each tool already searches.
if [ -d "$tmp/extra" ]; then
	data=${XDG_DATA_HOME:-"$HOME/.local/share"}
	install -Dm 0644 "$tmp/extra/tuxmedic.1.gz" "$data/man/man1/tuxmedic.1.gz"
	install -Dm 0644 "$tmp/extra/tuxmedic.bash" "$data/bash-completion/completions/tuxmedic"
	install -Dm 0644 "$tmp/extra/tuxmedic.fish" "$data/fish/vendor_completions.d/tuxmedic.fish"
	install -Dm 0644 "$tmp/extra/_tuxmedic" "$data/zsh/site-functions/_tuxmedic"
fi

case ":$PATH:" in
	*":$bindir:"*) echo "Run: tuxmedic" ;;
	*) echo "$bindir is not in your PATH. Run it as $bindir/tuxmedic, or add that folder to PATH." ;;
esac

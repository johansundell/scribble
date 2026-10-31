#!/bin/sh
# Installs or upgrades sn, the scribble command-line client, from GitHub
# Releases (#130):
#
#   curl -fsSL https://raw.githubusercontent.com/johansundell/scribble/master/scripts/install-sn.sh | sh
#
# A version (vX.Y.Z) as the first argument or in VERSION picks that release
# instead of the latest. INSTALL_DIR (default /usr/local/bin) is where sn
# goes; sudo is used when you can't write there.
set -eu

GITHUB_URL=${GITHUB_URL:-https://github.com}
INSTALL_DIR=${INSTALL_DIR:-/usr/local/bin}
RELEASES="$GITHUB_URL/johansundell/scribble/releases"

fail() {
	echo "install-sn.sh: $*" >&2
	exit 1
}

valid_tag() {
	case $1 in
	*[!v0-9.]*) return 1 ;;
	esac
	printf '%s\n' "$1" | grep -Eqx 'v[0-9]+\.[0-9]+\.[0-9]+'
}

case $(uname -s) in
Linux) OS=linux ;;
Darwin) OS=darwin ;;
*) fail "unsupported system $(uname -s); download sn from $RELEASES/latest" ;;
esac
case $(uname -m) in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*) fail "unsupported architecture $(uname -m); download sn from $RELEASES/latest" ;;
esac

TAG=${1:-${VERSION:-}}
if [ -z "$TAG" ]; then
	url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$RELEASES/latest") ||
		fail "cannot find the latest release at $RELEASES/latest"
	TAG=${url##*/}
	valid_tag "$TAG" || fail "cannot find the latest version in $url"
fi
valid_tag "$TAG" || fail "invalid version \"$TAG\" (want vX.Y.Z)"

NAME="scribble_${TAG}_${OS}_${ARCH}"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
trap 'exit 1' HUP INT TERM

cd "$TMP"
for f in "$NAME.tar.gz" "$NAME.sha512"; do
	curl -fsSL -o "$f" "$RELEASES/download/$TAG/$f" || fail "cannot download $RELEASES/download/$TAG/$f"
done
if command -v sha512sum >/dev/null 2>&1; then
	sha512sum -c "$NAME.sha512" >/dev/null 2>&1 || fail "$NAME.tar.gz does not match its SHA-512 checksum"
else
	shasum -a 512 -c "$NAME.sha512" >/dev/null 2>&1 || fail "$NAME.tar.gz does not match its SHA-512 checksum"
fi
tar -xzf "$NAME.tar.gz" sn || fail "no sn in $NAME.tar.gz"

# sudo only when neither INSTALL_DIR nor, if it doesn't exist yet, its
# nearest existing parent is writable.
dir=$INSTALL_DIR
while [ ! -d "$dir" ]; do
	dir=$(dirname "$dir")
done
SUDO=
if [ ! -w "$dir" ]; then
	command -v sudo >/dev/null 2>&1 || fail "cannot write to $INSTALL_DIR; set INSTALL_DIR or run as root"
	SUDO=sudo
fi
$SUDO mkdir -p "$INSTALL_DIR"
$SUDO install -m 0755 sn "$INSTALL_DIR/sn.new"
$SUDO mv -f "$INSTALL_DIR/sn.new" "$INSTALL_DIR/sn"
echo "installed sn $TAG to $INSTALL_DIR/sn"

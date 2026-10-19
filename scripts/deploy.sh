#!/usr/bin/env bash
# Installs a published GitHub release of the scribble server on this machine
# and restarts the service. If /healthz does not answer afterwards, the
# previous binary is put back. The Deploy workflow
# (.github/workflows/deploy.yml) runs it over SSH; see docs/deploy-vps.md for
# the setup.
#
# Usage: deploy.sh vX.Y.Z
set -euo pipefail

# Where the server binary lives, the systemd service name and the health
# check URL (PORT in the service's .env).
DIR=/opt/scribble
BIN=scribble-server
SERVICE=scribble-server
HEALTH_URL=http://127.0.0.1:8082/healthz
REPO=johansundell/scribble

TAG="${1:-}"
if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: deploy.sh vX.Y.Z (got: '$TAG')" >&2
	exit 2
fi

case "$(uname -m)" in
	x86_64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) echo "unsupported architecture: $(uname -m)" >&2; exit 2 ;;
esac

NAME="scribble_${TAG}_linux_${ARCH}"
URL="https://github.com/${REPO}/releases/download/${TAG}"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"

# make release publishes the release before it uploads the files, so a deploy
# started by the release event may have to wait for them. Each attempt is cut
# off after 2 minutes and the retries stop after 10, so a stalled connection
# can't hold up later deploys.
for f in "$NAME.tar.gz" "$NAME.sha512"; do
	curl -fsSL --connect-timeout 10 --max-time 120 \
		--retry 20 --retry-delay 15 --retry-max-time 600 --retry-all-errors \
		-o "$f" "$URL/$f"
done
sha512sum -c "$NAME.sha512"
# The archive also holds the CLI (and on amd64 the GUI); only the server is
# installed here.
tar -xzf "$NAME.tar.gz" "$BIN"

# rollback puts the previous binary back, restarts the service and fails the
# deploy with the given reason.
rollback() {
	echo "$1 after deploying $TAG; rolling back" >&2
	mv "$DIR/$BIN.prev" "$DIR/$BIN"
	systemctl restart "$SERVICE" || echo "restarting $SERVICE with the previous binary failed too" >&2
	exit 1
}

# Swap the binary with a rename, which is safe while the old one is running.
install -m 0755 "$BIN" "$DIR/$BIN.new"
cp -p "$DIR/$BIN" "$DIR/$BIN.prev"
mv "$DIR/$BIN.new" "$DIR/$BIN"
systemctl restart "$SERVICE" || rollback "systemctl restart $SERVICE failed"

# Give the service 30 seconds to answer, each request at most 5 of them.
deadline=$((SECONDS + 30))
while ((SECONDS < deadline)); do
	if curl -fsS --max-time 5 -H 'Accept: application/json' "$HEALTH_URL"; then
		echo
		echo "deployed $TAG"
		exit 0
	fi
	sleep 2
done
rollback "$HEALTH_URL did not answer"

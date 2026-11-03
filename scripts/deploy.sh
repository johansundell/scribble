#!/usr/bin/env bash
# Installs a published GitHub release of the scribble server on this machine
# and restarts the service. If /healthz does not answer afterwards, the
# previous binary is put back. The Deploy workflow
# (.github/workflows/deploy.yml) runs it over SSH; see docs/deploy-vps.md for
# the setup.
#
# If the public demo (docs/demo.md) is installed, it is updated the same way
# afterwards, with the sn its nightly reset uses. A demo that fails to start
# is rolled back on its own and fails the deploy, but the real server keeps
# the new release.
#
# Usage: deploy.sh vX.Y.Z
set -euo pipefail

# Where the server binary lives, the systemd service name and the health
# check URL (PORT in the service's .env).
DIR=/opt/scribble
BIN=scribble-server
SERVICE=scribble-server
HEALTH_URL=http://127.0.0.1:8082/healthz
# The same for the demo; skipped while $DEMO_DIR/$BIN doesn't exist.
DEMO_DIR=/opt/scribble-demo
DEMO_SERVICE=scribble-demo
DEMO_HEALTH_URL=http://127.0.0.1:8083/healthz
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
# The archive also holds the CLI (and on amd64 the GUI). The server is
# installed, and sn only for the demo's reset.
tar -xzf "$NAME.tar.gz" "$BIN" sn

# deploy_to dir service health-url [file...] installs the server, and the
# other files, into dir and restarts the service. If the service doesn't
# answer at health-url, the previous files are put back (a file that had none
# is removed) and the deploy stops with an error.
deploy_to() {
	local dir=$1 service=$2 health_url=$3
	shift 3
	# The server goes last, so a failed swap of another file leaves the
	# running server as it was.
	local files=("$@" "$BIN") f

	# Swap each file with a rename, which is safe while the old one is running.
	for f in "${files[@]}"; do
		install -m 0755 "$f" "$dir/$f.new"
		rm -f "$dir/$f.prev"
		if [[ -e "$dir/$f" ]]; then
			cp -p "$dir/$f" "$dir/$f.prev"
		fi
	done
	for f in "${files[@]}"; do
		mv "$dir/$f.new" "$dir/$f"
	done

	if systemctl restart "$service"; then
		# Give the service 30 seconds to answer, each request at most 5 of them.
		local deadline=$((SECONDS + 30))
		while ((SECONDS < deadline)); do
			if curl -fsS --max-time 5 -H 'Accept: application/json' "$health_url"; then
				echo
				echo "deployed $TAG to $service"
				return 0
			fi
			sleep 2
		done
		echo "$health_url did not answer after deploying $TAG; rolling back $service" >&2
	else
		echo "systemctl restart $service failed after deploying $TAG; rolling back" >&2
	fi

	for f in "${files[@]}"; do
		if [[ -e "$dir/$f.prev" ]]; then
			mv "$dir/$f.prev" "$dir/$f"
		else
			rm -f "$dir/$f"
		fi
	done
	systemctl restart "$service" || echo "restarting $service with the previous binary failed too" >&2
	exit 1
}

deploy_to "$DIR" "$SERVICE" "$HEALTH_URL"
if [[ -e "$DEMO_DIR/$BIN" ]]; then
	deploy_to "$DEMO_DIR" "$DEMO_SERVICE" "$DEMO_HEALTH_URL" sn
fi
echo "deployed $TAG"

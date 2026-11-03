#!/usr/bin/env bash
# Resets the public demo server (#161): stops it, removes its database, starts
# it again and loads the sample notes from demo/notes with sn. Visitors'
# notes, deleted notes and history all go, and the restart also clears a web
# login lockout. A systemd timer runs it every night; see docs/demo.md.
#
# Usage: demo-reset.sh
#
# The defaults match docs/demo.md. Each can be changed in the environment.
set -euo pipefail

DIR="${DIR:-/opt/scribble-demo}"
SERVICE="${SERVICE:-scribble-demo}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8083/healthz}"
NOTES="${NOTES:-$DIR/notes}"
SN="${SN:-$DIR/sn}"
export SCRIBBLE_URL="${SCRIBBLE_URL:-http://127.0.0.1:8083}"
export SCRIBBLE_TOKEN="${SCRIBBLE_TOKEN:-demo}"

# The database is SQLITE_PATH's default: scribble-server.db beside the binary.
DB="$DIR/scribble-server.db"

shopt -s nullglob
files=("$NOTES"/*.md)
if ((${#files[@]} == 0)); then
	echo "no sample notes in $NOTES" >&2
	exit 1
fi

systemctl stop "$SERVICE"
rm -f "$DB" "$DB-wal" "$DB-shm"
systemctl start "$SERVICE"

# Give the service 30 seconds to answer, each request at most 5 of them.
deadline=$((SECONDS + 30))
until curl -fsS --max-time 5 -o /dev/null -H 'Accept: application/json' "$HEALTH_URL"; do
	if ((SECONDS >= deadline)); then
		echo "$HEALTH_URL did not answer after the reset" >&2
		exit 1
	fi
	sleep 1
done

# Each file is "# Title", then the text. sn new adds the title line itself.
for f in "${files[@]}"; do
	title=$(head -n 1 "$f")
	title="${title#\#}"
	title="${title# }"
	tail -n +2 "$f" | sed '1{/^$/d;}' | "$SN" new "$title"
done

"$SN" pin "Welcome to the demo"

# An edit, so "Work log" has an old version to compare and restore. More
# edits now would merge into this one: the server merges saves within 5
# minutes (ADR 7).
"$SN" append "Work log" "**Wednesday:** wrote the sample notes for the demo. Next: the nightly reset, see [[Welcome to the demo]]."

echo "demo reset: ${#files[@]} notes loaded"

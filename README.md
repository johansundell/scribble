[![Go](https://github.com/johansundell/scribble/actions/workflows/go.yml/badge.svg)](https://github.com/johansundell/scribble/actions/workflows/go.yml)
# scribble

A small self-hosted notes system in the spirit of Tomboy: plain Markdown notes,
`[[wiki-links]]` between them, a central server, and a command line that can
create and update notes over an HTTP API.

- **Server** (repo root): Go + Gin + SQLite, built on
  [template-service](https://github.com/johansundell/template-service).
  Runs as a system service or in Docker.
- **CLI** (`cmd/scribble`): `scribble new / show / edit / append / ls / search / ...`
- **Desktop GUI** (`cmd/scribble-gui`): Fyne app to browse, search, view
  (rendered Markdown), edit, create, rename and delete notes. `[[links]]` are
  clickable, and each note shows the notes that link to it. It keeps a copy
  of the notes on disk, so it starts instantly and keeps working while the
  server is unreachable.
- **Client library** (`client/`): Go API client shared by the CLI and the GUI.

## Notes model

- A note is Markdown text. Its **title is the first non-empty line**
  (leading `#` ignored), so `# Groceries` and `Groceries` are the same title.
  Titles are unique, case-insensitively.
- `[[Another Note]]` links to a note by title. The server stays simple: it only
  stores text, answers backlink queries, and rewrites links when you rename.
- `#project` puts a note in a project; a note can be in several. A tag is
  `#` plus an ASCII letter and then letters, digits, `_` or `-`, at the start
  of a line or after whitespace. The title line is never read for tags, and
  names are case-insensitive (stored lowercase), so `#42` and `page#anchor`
  are not projects. Tags live in the text only: no extra tables.
- Every note has a `revision` (for edit-conflict detection) and a `seq` (a
  global change counter, for sync). Deleting leaves a tombstone so synced
  clients learn about it.
- **History** ([ADR 7](docs/adr/0007-note-history.md)): every write keeps a
  full copy of the note's new text as a version, with the reason (create,
  edit, rename, link-rewrite, restore, delete, undelete), in the same
  database, so backups cover it. Versions are kept forever, deleted notes
  included. Edits within 5 minutes of the edit before them replace that
  version instead of adding one, so a burst of saves is one version and the
  text before the burst is kept; the kept revisions therefore have gaps.
  `/notes/:id/versions` lists them, a version can be restored as a new
  revision, and deleted notes can be listed and undeleted.

## Quick start

```sh
# server
AUTH_TOKEN=change-me PORT=:8080 SQLITE_PATH=./scribble.db go run .
#   (or: make build && ./scribble-server, or: make docker-run)

# CLI
make build-cli
export SCRIBBLE_URL=http://localhost:8080 SCRIBBLE_TOKEN=change-me
./scribble new "Ideas" "first thought"
git log --oneline -5 | ./scribble append "Work log"
./scribble edit Ideas
./scribble projects            # every #project; `projects <name>` lists its notes
./scribble export              # every note as <title>.md in scribble-notes-YYYY-MM-DD.zip
./scribble import notes.zip    # skips titles that exist; --overwrite replaces them

# GUI (same SCRIBBLE_URL / SCRIBBLE_TOKEN; optional SCRIBBLE_HOTKEY, default Ctrl+Alt+N).
# Also read from a .env next to the scribble-gui binary; the environment wins.
make build-gui
./scribble-gui
```

`scribble-gui` registers a system tray icon (**New Note**, **Open Scribble**,
**Quit**) so closing the main window keeps the app resident in the tray, and a
global hotkey (`Ctrl+Alt+N` by default; override with `SCRIBBLE_HOTKEY`, or set
`SCRIBBLE_HOTKEY=none` to disable) that opens or focuses a new-note editor from
anywhere.

The window has a menu bar (on macOS, the system menu bar; there Ctrl is Cmd):

| Menu | Item | Shortcut |
|------|------|----------|
| File | New note | `Ctrl+N` |
| File | Save | `Ctrl+S` (saves and stays in the editor; the Save button returns to view mode) |
| File | Rename… | `F2` (when the note list or nothing has focus) |
| File | Delete… | none |
| File | Export notes… / Import notes… | none (a ZIP archive of Markdown files, see [Export and import](#export-and-import)) |
| File | Quit | `Ctrl+Q` (quits fully, even with a tray, after asking about unsaved changes) |
| Edit | Undo / Redo | `Ctrl+Z` / `Ctrl+Y` (`Cmd+Shift+Z` on macOS) |
| Edit | Cut / Copy / Paste / Select all | `Ctrl+X` / `Ctrl+C` / `Ctrl+V` / `Ctrl+A` |
| Edit | Find (focus the search box) | `Ctrl+F` |
| View | Edit note / View note | `Ctrl+E` |
| Help | About Scribble | none |
| (editor) | Bold / Italic / Link | `Ctrl+B` / `Ctrl+I` / `Ctrl+K` |

In edit mode a GitHub-style formatting toolbar sits above the editor:
heading, bold, italic, quote, code, link, and bulleted, numbered and task
lists, with GitHub's Octicons. Lists continue on Enter (Enter on an empty
item ends the list, Shift+Enter is a plain newline), a URL pasted over
selected text makes a link, and cells pasted from a spreadsheet make a
table. The rules are the web editor's, and one `Ctrl+Z` undoes one
formatting change.

`scribble-gui` keeps its copy of the notes in the user config directory
(`scribble/<host>-<hash of the server URL>.json`) and syncs every 30 seconds through
`/changes?since=N`, so only what changed is downloaded. While the server is
unreachable you can still browse, search and edit: saves are queued on disk
and sent when it is back. If the server refuses a queued edit (someone else
changed the note meanwhile), opening the note brings the edit back into the
editor. Renaming and deleting need the server. The status bar shows real-time
connection status (Connected with a green dot, or Offline with a red dot);
clicking it retries the connection immediately.

The GUI is built with [Fyne](https://fyne.io), so it needs CGO and a C
compiler. On Linux it also needs the GL, X11, Wayland and xkbcommon
development headers (Debian/Ubuntu: `libgl1-mesa-dev xorg-dev libwayland-dev
libxkbcommon-dev`). `make compile` / `make dist` cross-compiles `scribble-gui`
for `linux/amd64` and `windows/amd64` in Docker via
[fyne-cross](https://github.com/fyne-io/fyne-cross) (`make deps` installs it),
then gives the Windows exe every icon size with `cmd/winicon`.
You can also cross-compile for Windows directly from Linux with MinGW:

```sh
GOOS=windows CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  go build -ldflags "-H=windowsgui" -o scribble-gui.exe ./cmd/scribble-gui
```

See `docs/service-template.md` for all server settings (`AUTH_TOKEN`, `PORT`,
`SQLITE_PATH`, ...), installing as a system service, and Docker.
`docs/install-vps.md` installs the server on a new VPS behind nginx, with
the token, the web app and `TRUSTED_PROXIES` set up.
`docs/deploy-vps.md` then sets up automatic deploys: each published
release is installed by `scripts/deploy.sh`, which rolls back if `/healthz`
does not answer.

## API

All note endpoints need `Authorization: Bearer <AUTH_TOKEN>`. They are
deliberately **not** request-logged (the template's request log stores full
bodies, which would copy every note into it). Errors are plain-text status
text, like the rest of the service.

`GET /healthz` needs no token. It reports the version, the
API level (`"api"`), the database schema version (`"schema"`, the number of
migrations the database has run) and the storage status, as JSON when the
request sends `Accept: application/json`, and answers **503** when the
storage does not respond. Only SQLite has a schema version; with MySQL or
FileMaker the `"schema"` key is left out.

### API level and old clients

Every response carries `X-Version` (the release, such as `v0.0.12`) and
`X-Scribble-API` (the API level, an integer; 3 since export and import). The policy
([ADR 8](docs/adr/0008-api-level-and-minimum-client-level.md)):

- **The API only grows.** Adding endpoints or fields raises the API level by
  one. There are no versioned paths.
- **A breaking change raises the minimum client level.** Renaming or
  removing a field, or changing what one means, raises `minClientAPI` in
  `router/apilevel.go` to the new API level.

Clients say who they are in `X-Scribble-Client: <name>/<api level>
<version>`, such as `cli/1 v0.0.12` or `gui/1 v0.0.12`. A request without
it counts as level 1, which covers clients released before the header.
A client below the minimum gets **426 Upgrade Required** from the note
endpoints, `/changes` and `/projects`, with this plain-text body:

```
This Scribble client is too old for this server. Download the latest release: https://github.com/johansundell/scribble/releases/latest
```

Old clients print it as it is. The server logs each refusal as a warning
with the client string and IP, at most once per client string per hour.
`/healthz`, the web app and `/assets` are never refused.

The CLI then exits with an upgrade message. The desktop app shows the
message with a download button, sets its indicator to "Update needed",
stops syncing and goes read-only; edits still queued on this computer
stay there for a newer version to send. When the server runs a newer
release than the app, the app shows "Scribble vX.Y.Z is available" in the
status bar and in Help → About; `scribble version` says "update
available".

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/notes` | list, newest first. `?q=` searches title and body; `?project=` keeps notes tagged `#project` (combinable with `?q=`); `?title=` is an exact lookup returning `[]` or one note |
| GET | `/projects` | the `#project` names in use, lowercase and sorted |
| POST | `/notes` | `{"body": "# Title\n..."}` -> 201. **409** if the title exists |
| GET | `/notes/:id` | one note |
| PUT | `/notes/:id` | `{"body": "...", "base_revision": 3}` -> **412** if the note changed since that revision (omit or 0 to overwrite). **409** if the new title is taken |
| DELETE | `/notes/:id` | 204; leaves a tombstone |
| GET | `/notes/:id/backlinks` | notes containing `[[Title]]` for this note |
| POST | `/notes/:id/rename` | `{"title": "New"}`; rewrites `[[links]]` in other notes in one transaction |
| GET | `/changes?since=N` | everything changed after seq N (deleted ones flagged) plus `latest_seq` to use next time |
| GET | `/notes/:id/versions` | the note's kept versions, newest first, without bodies: `revision, title, reason, deleted, created_at, updated_at`. Works for deleted notes too |
| GET | `/notes/:id/versions/:rev` | one version with its `body`. **404** if that revision wasn't kept |
| POST | `/notes/:id/versions/:rev/restore` | optional `{"base_revision": 3}` (**412** as for `PUT`); sets the note's text to that version as a new revision and returns the note. **409** if the old title now belongs to another note, or if the note is deleted (undelete it first) |
| GET | `/notes/deleted` | deleted notes, most recently deleted first: `id, title, deleted_at` |
| POST | `/notes/:id/undelete` | brings a deleted note back with its last text and returns it. **409** if a live note has its title, **404** if the id isn't a deleted note |
| GET | `/export` | every live note as a ZIP archive (`application/zip`, `scribble-notes-YYYY-MM-DD.zip`); see [Export and import](#export-and-import) |
| POST | `/import` | `multipart/form-data` with the archive as `file` and optional `overwrite=true`; returns `{"imported": 2, "skipped": 1}`. **400** if it isn't a ZIP archive or a file in it is over 4 MB, **413** over 64 MB |

Notes need `STORAGE=sqlite` (the default). With `mysql` or `filemaker`, request
logs still work but the notes endpoints answer **501**, and the service logs a
warning at startup. With `filemaker`, `FMS_CREATE_LOG_TABLE=true` lets
startup create the log table when it is missing
([ADR 12](docs/adr/0012-service-may-create-the-filemaker-log-table.md)).

## Web app

The server can also serve a web app at its root, `/`, for a browser or a
phone. It logs in with a PIN instead of the token. Its note pages are at
`/note/<id>`, apart from the JSON API at `/notes/<id>`. With the web app off,
`/` redirects to `/healthz`. So far it has the PIN screen, logout
and the page layout; the notes come next (#46).

It is off until `WEB_PIN` is set. To turn it on, add to the server's `.env`:

```sh
WEB_PIN=<6-12 digits>
SESSION_SECRET=<the output of: openssl rand -base64 33>
TRUSTED_PROXIES=127.0.0.1   # behind nginx on the same host
```

The server refuses to start if the PIN isn't 6–12 digits, or if
`SESSION_SECRET` is missing, shorter than 32 characters or the same as
`AUTH_TOKEN`. See `docs/service-template.md` for `WEB_SESSION_TIMEOUT` and
`COOKIE_SECURE`. For a whole new server, follow
[`docs/install-vps.md`](docs/install-vps.md).

- **The PIN is for the browser only.** The API, the CLI and the desktop app
  keep using `AUTH_TOKEN`, and the API never accepts the PIN. The PIN opens
  every note for reading and writing, like the token.
- **Sessions** last 7 days from login (`WEB_SESSION_TIMEOUT`) and are never
  extended. Logging out ends the session on that device only. Changing
  `SESSION_SECRET` ends every session.
- **Per-address limit:** after 5 wrong PINs from one address within 15
  minutes, that address gets 429, without the PIN being checked, until 15
  minutes after its first wrong PIN.
- **PIN Alert and lockout:** after 20 wrong PINs within 24 hours from any
  addresses, the service logs a warning, every logged-in page shows "N wrong
  PINs since …", and PIN login is locked for everyone with 429. Sessions that
  already exist keep working. The lock lifts after 24 hours or when the
  service restarts; the alert stays until the restart. If the wrong PINs
  weren't yours, change `WEB_PIN` and restart.
- **Behind nginx**, set `TRUSTED_PROXIES=127.0.0.1` so the per-address limit
  sees the visitor's address from `X-Forwarded-For`. Without it, every
  visitor shares nginx's address. The header is believed only from the listed
  proxies, so nobody else can pick their own address.
- Web pages are never request-logged and are sent with
  `Cache-Control: no-store`.
- **The editor** is GitHub-style: a formatting toolbar, Write / Preview
  tabs, `Ctrl+B` / `Ctrl+I` / `Ctrl+K` (Cmd on macOS) for bold, italic and
  link, `Ctrl+Shift+P` to switch tabs, lists that continue on Enter, and
  smart paste (a URL over selected text makes a link, spreadsheet cells make
  a table). Preview renders the text the way the saved note shows. Without
  JavaScript it is a plain textarea, and saving still works. The toolbar and
  paste come from GitHub's `markdown-toolbar-element` and `paste-markdown`
  (MIT), vendored in `assets/js/vendor/`.
- **Keyboard shortcuts** follow the desktop menu bar where browsers allow (on
  macOS, Ctrl is Cmd). Single keys work only when the focus isn't in a text
  field, so typing `n` or `/` into a note just types it. New, leaving the
  editor and Rename ask about unsaved changes first. `?`, or "Keyboard
  shortcuts" at the bottom of the page, lists them:

  | Web | Desktop | Action |
  |---|---|---|
  | `Ctrl+S` | `Ctrl+S` | Save and stay in the editor (not the browser's "Save page") |
  | `Ctrl+E` | `Ctrl+E` | Edit or view the open note |
  | `Alt+N`, or `n` | `Ctrl+N` | New note (browsers keep `Ctrl+N` for a new window) |
  | `/` | `Ctrl+F` | Focus the search box (`Ctrl+F` stays the browser's find) |
  | `F2` | `F2` | Rename the open note |
  | `Ctrl+B` / `Ctrl+I` / `Ctrl+K` | | Bold / italic / link, in the editor |
  | `Ctrl+Shift+P` | | Switch Write / Preview, in the editor |
  | `?` | | Show the shortcuts |
  | `Esc` | | Close the shortcuts, or leave the search box |
- **On a phone** (below 700 px wide) the app shows one screen at a time: the
  list, or the open note with "← Notes" to go back (the search and project
  filter stay). Edit is at the top; History, Share, Rename and Delete are under **⋯**. In
  the editor, Save, Done and Cancel sit in a bar at the bottom, above the
  on-screen keyboard. The keyboard shortcuts are simply not used on touch
  screens.
- **Share links:** Share on a note creates a public, read-only link
  (`/s/<token>`, 128 random bits) and shows it with a Copy link button and a
  QR code, drawn on the server as inline SVG (`rsc.io/qr`), so a phone can
  open the note without the PIN. Anyone with the link can read the note, and
  nothing else: no list, search or editing, and its `[[links]]` and
  `#project` tags lead to the PIN screen. Revoke link clears the token, and
  the old link answers 404 at once; sharing again makes a new one. Notes are
  private until shared. The page is sent with `Referrer-Policy: no-referrer`,
  so the link doesn't leak to sites the note links to. Share links are part
  of the web app: with `WEB_PIN` unset they answer 404. Deleting a note
  revokes its link. (#83)
- **History:** a note shows its revision and how many changes its history
  keeps ("Revision 4 (3 edits)"), and **History (r4)** opens the note's
  kept versions ([ADR 7](docs/adr/0007-note-history.md)), newest first, each
  with what changed it: Created, Edited, Renamed, Links updated, Restored or
  Sync conflict. It opens on the version before the current one, rendered as
  Markdown; `?rev=<n>` picks another. **Restore this version** makes it the
  note's text again, as a new revision, after a confirm() (without
  JavaScript it restores straight away), and is refused if the note changed
  since the page was loaded. (#110)
- **Add to Home Screen:** in Safari on iPhone, Share → *Add to Home Screen*;
  in Chrome on Android, ⋮ → *Add to Home screen* (or *Install app*). The
  Scribble icon then opens the web app without the browser bars. The app is
  online only ([ADR 6](docs/adr/0006-web-app-is-online-only.md)): it needs a
  connection to the server, and opened without one it shows the browser's
  offline page. Every app icon (web, `scribble-gui` and the Windows `.ico`)
  is rendered from `assets/img/icon.svg` with `make icons` (librsvg and
  ImageMagick).

Why it works this way: [ADR 4](docs/adr/0004-web-app-is-server-rendered.md)
and [ADR 5](docs/adr/0005-web-pin-login-and-shared-api-token.md).

## Export and import

Every note can be exported to a ZIP archive and imported from one (#117),
with `scribble export [file.zip]` and `scribble import [--overwrite]
<path...>`, File → Export notes… / Import notes… in the desktop app, or
Export notes / Import notes at the bottom of the web app (the import opens a
dialog with a file picker and the overwrite box). All of them go through
the server's `GET /export` and `POST /import`; the web app uses
`/export-notes` and `/import-notes` with its session.

- **The archive** is flat: one `<title>.md` per note holding its Markdown
  as it is. In file names, `/ \ : * ? " < > |` become `_`, and titles that
  make the same name (ignoring case) get `-2`, `-3`, ...; the oldest note
  keeps the plain name. The default name is `scribble-notes-YYYY-MM-DD.zip`.
- **Importing** reads every `.md` and `.txt` file as Markdown and every
  `.note` file as Tomboy XML, in any folder of the archive; other files are
  ignored. A text file that doesn't start with a heading gets its file name
  as a `# Title` line, unless it is an export of a note whose title line has
  no `#`.
- **Notes that exist** (same title, ignoring case) are skipped, and counted
  ("imported 3 notes, skipped 1"). With overwrite they get the archive's
  text as a new revision, so the old text stays in their history; one that
  already has that text counts as skipped.
- `scribble import` also takes Tomboy `.note` files and directories, as
  before; `--overwrite` works for them too.

## Backups

`scribble-server backup` takes a consistent snapshot of the database while the service keeps running, checks it with `PRAGMA integrity_check` and stores it gzipped as `scribble_<timestamp>.db.gz`. The timestamp is UTC, for example `scribble_20261019T030000Z.db.gz`. It then deletes archives older than the retention, but always keeps the newest one. It prints the new archive's path, and lists deleted archives on stderr.

```bash
./scribble-server backup                               # SQLITE_PATH, BACKUP_DIR and RETENTION_DAYS from .env
./scribble-server backup -dir /srv/backups -retention-days 14
make backup ARGS="-dir /srv/backups"                   # builds first, then runs ./scribble-server backup
```

| Flag | Variable | Default |
|---|---|---|
| `-db` | `SQLITE_PATH` | the service's database |
| `-dir` | `BACKUP_DIR` | `backups` beside the database |
| `-retention-days` | `RETENTION_DAYS` | `7` |

If the backup fails, nothing is deleted. If the retention value is wrong, the backup is still stored, nothing is deleted, and the command exits 1.

Backups in the same folder as the database don't survive a lost disk or server. Copy them somewhere else as well, for example with `rsync` from another machine. Copying `scribble-server.db` directly while the service runs is not enough: SQLite runs in WAL mode, so recent changes can still be in the `-wal` file next to it.

**Daily backups.** `./scribble-server backup -cron-snippet` prints a crontab line that runs the backup every day at 03:00, with any other flags you give. The line changes to the binary's folder first, so the backup reads the same `.env` as the service, and `-db` and `-dir` become absolute paths. Output goes to the system log (`journalctl -t scribble-backup`). On a VPS, see [docs/deploy-vps.md](docs/deploy-vps.md#9-schedule-daily-backups). With Docker, run the backup in the container from the host's crontab:

```cron
0 3 * * * cd /path/to/scribble && docker compose exec -T scribble-server ./scribble-server backup 2>&1 | logger -t scribble-backup
```

**Restoring a backup.** Stop the service, unpack the archive next to the database and move it into its place, delete the old database's `-wal` and `-shm` files so SQLite doesn't mix them in, and start the service again. The commands below unpack to a separate file first, and keep a copy of the current database in case you picked the wrong archive.

On a VPS (systemd service, database at `/opt/scribble/scribble-server.db`). Put the archive's name in `ARCHIVE`:

```bash
ARCHIVE=/opt/scribble/backups/scribble_20261019T030000Z.db.gz
sudo systemctl stop scribble-server
sudo sh -ec '
  cd /opt/scribble
  trap "rm -f scribble-server.db.restore" EXIT
  gunzip -c "$1" > scribble-server.db.restore
  if [ -f scribble-server.db ]; then
    cp -p scribble-server.db scribble-server.db.before-restore
    chown --reference=scribble-server.db scribble-server.db.restore
    chmod --reference=scribble-server.db scribble-server.db.restore
  fi
  mv scribble-server.db.restore scribble-server.db
  rm -f scribble-server.db-wal scribble-server.db-shm
' sh "$ARCHIVE"
sudo systemctl start scribble-server
```

With Docker, from an archive on the host:

```bash
docker compose stop scribble-server
docker compose run --rm -T --no-deps scribble-server sh -ec '
  cd /app/data
  trap "rm -f restore.db.gz scribble-server.db.restore" EXIT
  cat > restore.db.gz
  gunzip -c restore.db.gz > scribble-server.db.restore
  if [ -f scribble-server.db ]; then cp scribble-server.db scribble-server.db.before-restore; fi
  mv scribble-server.db.restore scribble-server.db
  rm -f scribble-server.db-wal scribble-server.db-shm
' < ./backups/scribble_20261019T030000Z.db.gz
docker compose start scribble-server
```

If the archive is damaged, `gunzip` stops the restore with an error before the database is touched. Otherwise the previous database is kept as `scribble-server.db.before-restore`.

Then check `/healthz` and that notes are what you expect.

**Going back to an older release.** Database migrations only go forward. An older `scribble-server` refuses to start on a database that a newer release has migrated, with `database schema version N is newer than this binary (M)`. To go back, stop the service, put the older binary in place and restore an archive taken before the upgrade, as above.

## FileMaker replica

The server can keep a copy of the notes in a FileMaker file, so people can browse and edit them next to other FileMaker data (#66, [ADR 10](docs/adr/0010-sqlite-master-filemaker-replica.md)).

- **SQLite is the master.** The API, the CLI, the desktop app and the web app only use SQLite. They never wait on FileMaker, and keep working while it is down.
- A background sync **pushes** every note change to FileMaker's `Notes` table in `seq` order, about 2 seconds after a write and every 30 seconds. After an outage it catches up from where it stopped.
- It **pulls** edits made in FileMaker from the `PendingEdits` table and applies them as normal saves, with history.
- **If a note changed in both places, the app wins.** The FileMaker text is kept in the note's history, so it can be restored, and the `PendingEdits` row says why the edit wasn't applied.

Turn it on with `NOTES_REPLICA=filemaker`, `STORAGE=sqlite` and the `FMS_HOST`, `FMS_DATABASE`, `FMS_USERNAME` and `FMS_PASSWORD` settings. `FMS_NOTES_TABLE`, `FMS_PENDING_TABLE`, `FMS_INFO_TABLE` and `NOTES_REPLICA_INTERVAL` change the defaults ([settings](docs/service-template.md#configuration)). A FileMaker developer creates the tables once from [docs/filemaker/notes.md](docs/filemaker/notes.md). If the setup is missing or wrong, the server logs an error and runs without the replica. `/healthz` shows the replica's state, and `scribble-server replica resync` pushes every note again after the FileMaker file was replaced.

## Layout

```
main.go, service.go, config.go   service lifecycle (from the template)
router/, handlers/, store/       routes, handlers, storage (notes: handlers/notes.go, store/notes.go)
auth/                            the web app's PIN login, session cookie and guessing limits
replica/                         the FileMaker replica's sync (push, pull, conflicts)
docs/filemaker/                  the FileMaker tables for the replica
tmpl/, assets/                   page templates (web app: app.html), CSS and the PIN keypad script
notes/                           Note type, [[link]] and #project helpers (no server deps)
client/                          Go API client
offline/                         the GUI's local copy of the notes and its sync
cmd/scribble/                    CLI
gui/                             Fyne GUI: windows and widgets
cmd/scribble-gui/                GUI entry point (reads SCRIBBLE_URL / SCRIBBLE_TOKEN / SCRIBBLE_HOTKEY)
docs/service-template.md         the template's own docs
GLOSSARY.md                      the words used for notes, sync, history and the replica
docs/adr/                        why things are as they are: one short decision record each
```

See [`GLOSSARY.md`](GLOSSARY.md) and [`docs/adr/`](docs/adr/).

## Development

```sh
go test ./...
```

Tests cover the link and project helpers, the SQLite note store (conflicts,
tombstones, rename, backlinks and projects), the whole API over HTTP, and the
Go client and the CLI against a real server. GUI tests drive the window headlessly with Fyne's test driver,
also against a real server.

The web app's phone checks (`router/web_phone_browser_test.go`) open the login,
list, note, editor and rename pages in Chrome's phone emulation (375×667 and
412×915, light and dark). They check that no page scrolls sideways, that tap
targets are at least 44 px, that the back link works and that the editor's
action bar is on screen. They need a WebDriver server and are skipped
without one; CI starts chromedriver. With Chrome in Docker
(`selenium/standalone-chrome` on port 4444):

```sh
SCRIBBLE_WEBDRIVER=http://localhost:4444 SCRIBBLE_WEBDRIVER_HOST=host.docker.internal go test ./router -run TestWebPhoneBrowser
```

The app icons are committed, so building needs no image tools. After changing
`assets/img/icon.svg`, run `make icons` to render them again. It needs
`rsvg-convert` (librsvg) and ImageMagick 7 (`magick`), which `make deps` does
not install (Debian/Ubuntu: `librsvg2-bin imagemagick`; macOS: `brew install
librsvg imagemagick`). Older distributions ship ImageMagick 6, which has
`convert` but no `magick`.

## Roadmap

- [x] `cmd/scribble-gui`: Fyne desktop client (Windows first): browse,
      search, view, edit, create and delete
- [x] `#project` tags: `/projects`, `?project=`, `scribble projects`, and a
      project filter with clickable tags in the GUI
- [x] Opt-in auto-linking: `scribble autolink <title>` turns mentions of
      other notes' titles into `[[links]]` (`notes.AutoLink`)
- [x] GUI: clickable `[[links]]` (a link to a missing note creates it), a
      "Linked from" backlinks panel, and rename that rewrites incoming links
- [x] GUI: a local cache synced through `/changes`, with offline edits
- [ ] GUI: conflict resolution
- [x] Global hotkey / tray icon for "instant new note"
- [x] Import from Tomboy `.note` XML (`scribble import <path...>`)
- [x] Export and import every note as a ZIP archive of Markdown files, in
      the CLI, the web app and the desktop app (#117)
- [x] Automatic deploys: each published release is installed on the VPS,
      with a rollback if `/healthz` doesn't answer (#44)
- [x] Backups: `scribble-server backup` snapshots the database while it
      runs, keeps 7 days, and runs daily from cron (#62)
- [x] API level and minimum client version, so the server can tell
      desktop apps and CLIs that are too old to upgrade (#57)
- [x] Note history: every change keeps the old text, with restore and
      undelete on the server (#55) and `scribble history / diff / restore /
      undelete` in the CLI (#56)
- [ ] Web app at `/`: log in with a PIN, then browse, search, read,
      edit, rename and delete notes like in the desktop app (#46), on
      phones too, with "Add to Home Screen" (#65)
- [x] GitHub-style Markdown editing (toolbar, `Ctrl+B` / `Ctrl+I` /
      `Ctrl+K`, lists that continue on Enter, smart paste) in the web app
      (#53) and the desktop app (#54)
- [ ] FileMaker replica: SQLite stays the master, and a two-way sync keeps
      a FileMaker copy of the notes up to date, also after FileMaker has
      been down (#66). Notes can be browsed in FileMaker, and edits made
      there are queued for the sync (#67)

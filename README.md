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
| File | Quit | `Ctrl+Q` (quits fully, even with a tray, after asking about unsaved changes) |
| Edit | Undo / Redo | `Ctrl+Z` / `Ctrl+Y` (`Cmd+Shift+Z` on macOS) |
| Edit | Cut / Copy / Paste / Select all | `Ctrl+X` / `Ctrl+C` / `Ctrl+V` / `Ctrl+A` |
| Edit | Find (focus the search box) | `Ctrl+F` |
| View | Edit note / View note | `Ctrl+E` |
| Help | About Scribble | none |

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
[fyne-cross](https://github.com/fyne-io/fyne-cross) (`make deps` installs it).
You can also cross-compile for Windows directly from Linux with MinGW:

```sh
GOOS=windows CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  go build -ldflags "-H=windowsgui" -o scribble-gui.exe ./cmd/scribble-gui
```

See `docs/service-template.md` for all server settings (`AUTH_TOKEN`, `PORT`,
`SQLITE_PATH`, ...), installing as a system service, and Docker.
`docs/deploy-vps.md` sets up automatic deploys to a VPS: each published
release is installed by `scripts/deploy.sh`, which rolls back if `/healthz`
does not answer.

## API

All note endpoints need `Authorization: Bearer <AUTH_TOKEN>`. They are
deliberately **not** request-logged (the template's request log stores full
bodies, which would copy every note into it). Errors are plain-text status
text, like the rest of the service.

`GET /healthz` (also `GET /`) needs no token. It reports the version and the
storage status, as JSON when the request sends `Accept: application/json`,
and answers **503** when the storage does not respond.

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

Notes need `STORAGE=sqlite` (the default). With `mysql` or `filemaker`, request
logs still work but the notes endpoints answer **501**, and the service logs a
warning at startup.

## Layout

```
main.go, service.go, config.go   service lifecycle (from the template)
router/, handlers/, store/       routes, handlers, storage (notes: handlers/notes.go, store/notes.go)
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
- [x] Automatic deploys: each published release is installed on the VPS,
      with a rollback if `/healthz` doesn't answer (#44)
- [ ] Backups: `scribble-server backup` snapshots the database while it
      runs, keeps 7 days, and runs daily from cron (#62)
- [ ] API level and minimum client version, so the server can tell
      desktop apps and CLIs that are too old to upgrade (#57)
- [ ] Note history: every change keeps the old text, with restore and
      undelete on the server (#55) and `scribble history / diff / restore /
      undelete` in the CLI (#56)
- [ ] Web app at `/app`: log in with a PIN, then browse, search, read,
      edit, rename and delete notes like in the desktop app (#46), on
      phones too, with "Add to Home Screen" (#65)
- [ ] GitHub-style Markdown editing (toolbar, `Ctrl+B` / `Ctrl+I` /
      `Ctrl+K`, lists that continue on Enter, smart paste) in the web app
      (#53) and the desktop app (#54)
- [ ] FileMaker replica: SQLite stays the master, and a two-way sync keeps
      a FileMaker copy of the notes up to date, also after FileMaker has
      been down (#66). Notes can be browsed in FileMaker, and edits made
      there are queued for the sync (#67)

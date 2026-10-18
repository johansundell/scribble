# scribble

A small self-hosted notes system in the spirit of Tomboy: plain Markdown notes,
`[[wiki-links]]` between them, a central server, and a command line that can
create and update notes over an HTTP API.

- **Server** (repo root): Go + Gin + SQLite, built on
  [template-service](https://github.com/johansundell/template-service).
  Runs as a system service or in Docker.
- **CLI** (`cmd/scribble`): `scribble new / show / edit / append / ls / search / ...`
- **Client library** (`client/`): Go API client shared by the CLI and the
  upcoming Fyne GUI (`cmd/scribble-gui`, not written yet).

## Notes model

- A note is Markdown text. Its **title is the first non-empty line**
  (leading `#` ignored), so `# Groceries` and `Groceries` are the same title.
  Titles are unique, case-insensitively.
- `[[Another Note]]` links to a note by title. The server stays simple: it only
  stores text, answers backlink queries, and rewrites links when you rename.
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
```

See `docs/service-template.md` for all server settings (`AUTH_TOKEN`, `PORT`,
`SQLITE_PATH`, ...), installing as a system service, and Docker.

## API

All note endpoints need `Authorization: Bearer <AUTH_TOKEN>`. They are
deliberately **not** request-logged (the template's request log stores full
bodies, which would copy every note into it). Errors are plain-text status
text, like the rest of the service.

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/notes` | list, newest first. `?q=` searches title and body; `?title=` is an exact lookup returning `[]` or one note |
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
notes/                           Note type and [[link]] helpers (no server deps)
client/                          Go API client
cmd/scribble/                    CLI
docs/service-template.md         the template's own docs
```

## Development

```sh
go test ./...
```

Tests cover the link helpers, the SQLite note store (conflicts, tombstones,
rename and backlinks), the whole API over HTTP, and the Go client against a
real server.

## Roadmap

- [ ] `cmd/scribble-gui`: Fyne desktop client (Windows first), with an
      edit/view toggle where `[[links]]` are clickable (`notes.LinkifyForView`
      already turns them into `note://` links for Fyne's Markdown renderer),
      a backlinks panel, and a local cache synced through `/changes`
- [ ] Global hotkey / tray icon for "instant new note"
- [ ] Import from Tomboy `.note` XML

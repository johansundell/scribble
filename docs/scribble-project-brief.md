# scribble: project brief

Use this to bring a new session (or a new Claude project) up to speed. It records the goal, the decisions made and why, what is already built, and what comes next.

## 1. Goal

A Tomboy-inspired notes system that I host myself:

- **Plain Markdown notes**, with no rich-text editor.
- **`[[Wiki-links]]`** between notes. This is the Tomboy feature I like most.
- **A central server** that holds all notes.
- **A command line** to create and update notes over an HTTP API, including piping (`git log | scribble append "Work log"`).
- **A Windows desktop app** (Go + Fyne) that talks to the same server.
- Everything in **Go**, one language for server, CLI and GUI.

### Inspiration: what Tomboy does
Tomboy is a GNOME desktop note app (Mono/GTK). The ideas worth keeping are instant notes, automatic wiki-style linking, and lightweight sync. Its sync backends (SSH, WebDAV, Snowy, Ubuntu One) are mostly dead. Its notes are stored as XML `.note` files.

### Existing projects worth knowing about
Tomboy-NG (cross-platform rewrite, runs on Windows), Gnote, Joplin (has a CLI, REST API and sync server), Standard Notes. Joplin may cover much of this, but building my own gives Tomboy's feel and a tiny self-hosted Go stack.

## 2. Decisions made

| Topic | Decision | Why |
| --- | --- | --- |
| Note format | Raw Markdown text, nothing else | CLI, API and every client see the same readable content |
| Title | First non-empty line of the body (leading `#` ignored) | Feels like Tomboy, keeps the CLI simple (`scribble append "Title" "text"`). Titles are unique, case-insensitive |
| Links | Explicit `[[Note Title]]` only | Degrades gracefully in any Markdown viewer. Auto-linking by title match is **opt-in only** (`scribble autolink <title>`), because short titles match everywhere and titles change. It writes ordinary `[[links]]`, so storage is unchanged |
| Link logic location | Server stays "dumb": stores text, answers backlink queries, rewrites links on rename. Rendering and click handling are client-side | Keeps the server small |
| Rename | Dedicated endpoint that rewrites `[[old]]` to `[[new]]` in all other notes in one transaction | A first-line title means renames would otherwise break links |
| Conflicts | Optimistic concurrency: PUT carries `base_revision`; stale gives **412** | Simple and safe for one user on several devices |
| Sync | Global `seq` counter plus `GET /changes?since=N`; deletes leave tombstones | Lets a client cache work offline and sync incrementally |
| Server base | The template at https://github.com/johansundell/template-service (Gin, kardianos/service, token auth, Docker, SQLite via ncruces) | Reuse proven plumbing |
| Storage | Notes on **SQLite only**; on `mysql`/`filemaker` the notes endpoints return 501 and a startup warning is logged | The template's `Store` is built around request logs; supporting every backend wasn't worth it |
| Request logging | Notes routes are **not** request-logged | The template's log stores full request/response bodies, which would copy every note into it |
| GUI toolkit | Fyne | One language; wiki-links are easy because the editor stays plain text (see findings) |

## 3. Findings

### Fyne
- Fyne's `Entry` widget is plain text only. `RichText` renders Markdown but is **display-only**. Live rich-text editing would need a custom widget, which is a lot of work, and it isn't needed here.
- **UI:** an edit/view toggle. Edit raw Markdown in an `Entry`; view in a `RichText` made with `widget.NewRichTextFromMarkdown`, where `[[links]]` become taps. `notes.LinkifyForView` rewrites `[[Title]]` to `[Title](note:///Title%20…)` (the title is path-escaped; `note://Title` would put it in the URL host, where Go rejects `%20`), and `notes.LinkTitle` reads it back. A tap opens the target note, or **creates it if missing** (Tomboy behavior). Links in task-list items stay plain text, because Fyne v2.8 can't render a link there (see #11).
- A "Linked from" backlinks panel needs no custom widgets.
- Windows builds need CGO and a C compiler (MinGW or Zig), or cross-compile from Linux with `fyne-cross`. Binaries are about 20-30 MB, and the look is non-native.
- Tray icon is supported. A global "instant new note" hotkey needs an extra library such as `golang-design/hotkey`.
- **Fallback if the editor feels limiting:** Wails (Go backend plus web frontend with TipTap/ProseMirror). Because the server and client library are the same either way, switching the GUI wouldn't touch anything else.

### The template
- Requires **Go 1.27.1**. Its README has the rename recipe (module path, service name, `.agents/skills` left alone).
- Architecture: `main.go` (service CLI), `service.go` (lifecycle), `router/` (route table plus middleware: Wrap, Auth, Logger), `handlers/` (error-returning handlers using `httperror.ReturnWithHTTPStatus`), `store/` (interface plus SQLite/MySQL/FileMaker). Errors go out as plain-text status text.
- `router.NewRouter` refuses to build if a route has `UseAuth` but no `AUTH_TOKEN`. When the token is unset, the service generates a random one and logs it.
- **Gotcha:** kardianos/service treats a process whose parent is PID 1 as a system service and logs to syslog. Launching the binary through a double-forked background shell therefore fails with "Unix syslog delivery error". Start it as a normal child process.
- `gox ./...` in the template's Makefile builds every `main` package to the same output name. It would have overwritten the server with the CLI, so the Makefile now builds the server and CLI separately.

## 4. Current state

Built and tested (zip delivered; the user pushes it to https://github.com/johansundell/scribble):

- **Server** at the repo root with these routes, all behind `Authorization: Bearer <AUTH_TOKEN>`:

  | Method | Path | Notes |
  | --- | --- | --- |
  | GET | `/notes` | `?q=` search, `?title=` exact lookup (returns `[]` or one note) |
  | POST | `/notes` | `{"body": "..."}`; 201, or 409 if the title exists |
  | GET | `/notes/:id` | |
  | PUT | `/notes/:id` | `{"body", "base_revision"}`; 412 if stale, 409 if the new title is taken |
  | DELETE | `/notes/:id` | 204, leaves a tombstone |
  | GET | `/notes/:id/backlinks` | |
  | POST | `/notes/:id/rename` | `{"title"}`; rewrites links elsewhere |
  | GET | `/changes?since=N` | `{notes, latest_seq}`; deleted notes flagged |

- **`notes/`**: the `Note` type and link helpers (`TitleOf`, `SetTitle`, `Links`, `HasLinkTo`, `RewriteLinks`, `LinkifyForView`, `LinkTitle`). No server dependencies.
- **`client/`**: Go API client (`List`, `ByTitle`, `Get`, `Create`, `Update`, `Delete`, `Backlinks`, `Rename`, `Changes`, plus `IsNotFound`/`IsTitleTaken`/`IsStale`/`IsUnauthorized`).
- **`cmd/scribble/`**: CLI with `new`, `show`, `edit` (`$VISUAL`/`$EDITOR`, keeps the temp file if the save fails), `append` (creates the note if missing, retries on conflict, reads stdin), `ls`, `search`, `links`, `backlinks`, `rename`, `rm`. Configured with `SCRIBBLE_URL` and `SCRIBBLE_TOKEN`.
- **Tests:** link helpers, the SQLite note store (conflicts, tombstones, rename, backlinks), the full API over HTTP through the real router (including that notes routes are authenticated and not logged), and the client against a live test server. A manual smoke test of the CLI against the running binary also passed.
- **Docs:** `README.md` (project and API), `docs/service-template.md` (the template's config, Docker and service install docs).

### Repo layout
```
main.go, service.go, config.go   service lifecycle (template)
router/, handlers/, store/       routes, handlers, storage (notes: handlers/notes.go, store/notes.go)
notes/                           Note type and [[link]] helpers
client/                          Go API client
cmd/scribble/                    CLI
cmd/scribble-gui/                (not written yet)
docs/service-template.md
```

## 5. Next steps

1. **Fyne GUI** (`cmd/scribble-gui`), Windows first:
   - Note list and search, using the `client` package.
   - Edit/view toggle, with clickable `[[links]]` that create the note if it doesn't exist.
   - Backlinks panel.
   - Local cache synced through `/changes?since=N`, so it works offline.
   - Handle 412 (stale revision) with a clear choice: reload, or overwrite with `base_revision` 0.
2. **Instant new note:** tray icon plus global hotkey.
3. **Import** from Tomboy `.note` XML (the files are easy to parse and convert to Markdown).
4. Opt-in **auto-linking** in the GUI, once `[[links]]` are clickable there (`notes.AutoLink` and `scribble autolink` already exist).
5. Not yet done: nothing has been run on Windows, and the server hasn't been deployed (the Dockerfile and compose file from the template are untouched apart from renaming).

## 6. Open questions

- Which Windows build approach: local MinGW or Zig, or `fyne-cross` from Linux?
- How should the GUI resolve edit conflicts: simple "reload or overwrite", or a diff view?
- Is one shared bearer token enough, or should each device get its own token (revocable)?
- Should the GUI cache notes in SQLite or in plain files?

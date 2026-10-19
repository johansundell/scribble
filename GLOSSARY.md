# Glossary

The words used for scribble's ideas, in code, tickets and docs. The decisions behind them are in [`docs/adr/`](docs/adr/).

- **API level**: an integer the server sends in `X-Scribble-API` and `/healthz`. It goes up by one whenever endpoints or fields are added. (#57, [ADR 8](docs/adr/0008-api-level-and-minimum-client-level.md))
- **Auto-link**: turning plain mentions of other notes' titles into `[[links]]`. It is opt-in: `scribble autolink <title>` runs it on one note (`notes.AutoLink`). The title line, code, URLs and existing links are left alone.
- **Backlink**: a note that links to the note you are looking at. The desktop app lists them under "Linked from". The API's `GET /notes/:id/backlinks` returns them.
- **`base_revision`**: the revision an edit started from, sent with `PUT /notes/:id`. If the note's revision is no longer the same, the server answers 412. 0, or leaving it out, overwrites. ([ADR 3](docs/adr/0003-edit-conflicts-with-revision.md))
- **Link**: `[[Title]]` in a note's text, pointing to the note with that title, ignoring case. Renaming a note through the rename endpoint rewrites the links to it. A link to a missing note creates that note when clicked in the desktop app.
- **Merge window**: 5 minutes (`versionMergeWindow`). When a note's newest version is an edit created less than 5 minutes ago, the next edit replaces that version instead of adding one, so a burst of saves becomes one version. (#55, [ADR 7](docs/adr/0007-note-history.md))
- **Minimum client level**: the lowest API level a client may have (`minClientAPI`). It goes up only for a breaking change. Older clients get 426 Upgrade Required. (#57, [ADR 8](docs/adr/0008-api-level-and-minimum-client-level.md))
- **Note**: one Markdown text, with an id, a revision and a seq. Its title is its first line. ([ADR 1](docs/adr/0001-notes-are-plain-markdown.md))
- **Offline copy**: the desktop app's copy of the notes on disk (`offline.Store`), kept up to date through `/changes`. It lets the app start instantly and work while the server is unreachable. ([ADR 2](docs/adr/0002-sync-with-seq-and-tombstones.md))
- **Pending edit**: a row in the FileMaker replica's `PendingEdits` table: a create, update or delete made in FileMaker, waiting for the server's sync to apply it to SQLite. Not the same as a queued edit. (#66, #67)
- **Project**: a `#tag` in a note's text, such as `#work`. A note can be in several projects. Names are case-insensitive and stored lowercase. Tags in the title line don't count.
- **Queued edit**: a save the desktop app made while offline, kept in the offline copy (`offline.Edit`) until the next sync sends it. If the server refuses it, it stays queued, and opening the note brings it back into the editor. Not the same as a pending edit.
- **Replica**: the FileMaker copy of the notes, tombstones included. SQLite stays the master, and a sync keeps the replica up to date both ways. (#66, [ADR 10](docs/adr/0010-sqlite-master-filemaker-replica.md))
- **Revision**: a counter for one note, which goes up by one on every write to it. Used to detect edit conflicts. ([ADR 3](docs/adr/0003-edit-conflicts-with-revision.md))
- **Seq**: a counter for the whole database. Every write to any note gives that note the next seq. `/changes?since=N` returns the notes with a seq above N. ([ADR 2](docs/adr/0002-sync-with-seq-and-tombstones.md))
- **Title**: a note's first non-empty line, without leading `#` marks and spaces (`notes.TitleOf`). Unique among live notes, ignoring case. ([ADR 1](docs/adr/0001-notes-are-plain-markdown.md))
- **Tombstone**: what is left of a deleted note: its row, with `deleted = 1` and a new seq, so synced clients learn about the delete. Tombstones are never removed. ([ADR 2](docs/adr/0002-sync-with-seq-and-tombstones.md))
- **Version**: one kept state of a note in its history: revision, title, full body, whether it was deleted, and the reason for the change. Not the release version (`X-Version`, such as `v0.0.9`). (#55, [ADR 7](docs/adr/0007-note-history.md))

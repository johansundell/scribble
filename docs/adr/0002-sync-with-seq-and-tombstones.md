# 2. Sync with `seq` and tombstones

- **Status:** Accepted
- **Tickets:** none (built with the desktop app's offline copy; see the README and `offline/`)

## Context

The desktop app keeps a copy of the notes on disk, so it starts instantly and works while the server is unreachable. It needs a cheap way to learn what changed since it last looked, including notes that were deleted.

## Decision

- Every note has a **`seq`**, taken from one counter for the whole database. Every write (create, edit, rename, link rewrite, delete, pin or unpin) gives the note the next `seq`.
- `GET /changes?since=N` returns every note with a `seq` above N, deleted ones included, plus `latest_seq` for the next call.
- Each database has a **database ID**, random and made once when the database is created (#167). `/changes` returns it as `database_id`. A seq or note ID only means something together with it.
- **Deleting leaves a tombstone:** the row stays, with `deleted = 1` and a new `seq`, so a client that syncs later learns about the delete. Tombstones are never removed.

## Consequences

- A sync downloads only what changed, and a client that was away for a month catches up with one call.
- The `notes` table keeps every note ever deleted. Lists and lookups filter on `deleted = 0`, and a deleted note's title is free to reuse.
- When the database ID differs from the one a copy was made from, the server's database was replaced (as the nightly demo reset does, #161), and the copy's seq and note IDs belong to another database. The desktop app then drops its copy and downloads every note again. Its unsent edits become new notes, saved as `<title> (from this computer)` if the title is taken, so nothing typed is lost and nothing overwrites the new database's note that has the old ID. It checks before sending any edit, since the counter alone can't tell (the new database may already be past the old one's seq), and downloads the new notes before dropping the old ones. Until the app has let go of the open note, an ID it saves under is taken as one from before the reset. An edit the old database refused is tried again on the new one. A copy saved before database IDs existed takes the server's.
- Restoring a backup over the same server keeps the database ID, so a client that synced after the backup was taken keeps notes the restore removed until its copy file is deleted.
- Anything else that copies the notes can use the same counter, as the FileMaker replica will ([ADR 10](0010-sqlite-master-filemaker-replica.md)).

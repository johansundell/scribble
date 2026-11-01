# 2. Sync with `seq` and tombstones

- **Status:** Accepted
- **Tickets:** none (built with the desktop app's offline copy; see the README and `offline/`)

## Context

The desktop app keeps a copy of the notes on disk, so it starts instantly and works while the server is unreachable. It needs a cheap way to learn what changed since it last looked, including notes that were deleted.

## Decision

- Every note has a **`seq`**, taken from one counter for the whole database. Every write (create, edit, rename, link rewrite, delete, pin or unpin) gives the note the next `seq`.
- `GET /changes?since=N` returns every note with a `seq` above N, deleted ones included, plus `latest_seq` for the next call.
- **Deleting leaves a tombstone:** the row stays, with `deleted = 1` and a new `seq`, so a client that syncs later learns about the delete. Tombstones are never removed.

## Consequences

- A sync downloads only what changed, and a client that was away for a month catches up with one call.
- The `notes` table keeps every note ever deleted. Lists and lookups filter on `deleted = 0`, and a deleted note's title is free to reuse.
- Anything else that copies the notes can use the same counter, as the FileMaker replica will ([ADR 10](0010-sqlite-master-filemaker-replica.md)).

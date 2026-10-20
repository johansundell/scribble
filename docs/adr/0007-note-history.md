# 7. Note history stores full copies, keeps them forever, and merges saves within 5 minutes

- **Status:** Accepted, built in #55
- **Tickets:** #55 (the CLI commands are #56, and the FileMaker replica adds two reasons in #66)

## Context

An edit, a rename's link rewrite or a delete can lose text, and nothing on the server can bring it back today. The history has to be cheap to read and restore, and the existing backups must cover it.

## Decision

- A new SQLite table, `note_versions`, in the same database as the notes. Every write adds a **version**: the note's revision, title, full body, whether it is deleted, and a `reason` (`create`, `edit`, `rename`, `link-rewrite`, `restore`, `delete`, `undelete`; #66 adds `filemaker-conflict` and `conflict-kept-app`). The version is written in the same transaction as the change.
- **Full copies, not diffs.** Notes are small, and full copies make reading and restoring trivial.
- **Kept forever.** No pruning, and deleting a note keeps its versions.
- **Merge window:** an `edit` replaces the newest version instead of adding one when that version is also an `edit`, was created less than 5 minutes ago, and isn't the note's only version. So a burst of saves becomes one version, and the version from before the burst is kept. Other reasons are never merged.
- **Restore is a new revision** with the old text, with the same 412 rule as a normal save. Deleted notes can be listed and **undeleted**.
- A deleted note's `deleted_at` is its `notes.updated_at`. For every delete since history exists that is the delete version's `created_at`; for a note deleted before, it is still the delete time, where its backfilled version only has the note's creation time.

## Consequences

- The database grows with every kept version. For one person's notes that is small, and it can be looked at again if it ever isn't.
- Merging leaves gaps in the revision numbers in the history.
- Restore and undelete get a new `seq`, so synced clients see them like any other change.

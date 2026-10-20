# FileMaker replica of the notes: table setup

With `NOTES_REPLICA=filemaker`, `scribble-server` keeps a copy of the notes in a FileMaker file, synced both ways (#66, [ADR 10](../adr/0010-sqlite-master-filemaker-replica.md)). **SQLite stays the master.** The API never waits on FileMaker and keeps working while it is down.

The service never creates FileMaker tables or fields. A FileMaker developer sets them up once from this page. At startup the service reads each table with every field, and the `ScribbleInfo` record. If a table or field is missing, or the schema version is wrong, it logs an error and turns the replica off. The server still starts, and `/healthz` shows `replica.status = "off"` with the reason.

## Rules for every table

- Field names are ASCII without spaces, exactly as below. OData URLs break on other names.
- Timestamps are stored in **UTC without a time zone**, as in the `Logs` table. Add a calculation field if people want local time.
- FileMaker stores line breaks as CR. The sync turns CR and CRLF back into LF when it reads, so this is invisible to the apps.

## `Notes`

A copy of SQLite's notes, deleted notes (tombstones) included. **Only the sync writes to it.** FileMaker users edit notes through `PendingEdits` (#67). Table name: `FMS_NOTES_TABLE`, default `Notes`.

| Field | Type | Options | Purpose |
|---|---|---|---|
| `NoteID` | Number | Validation: unique, not empty, always. Indexed. | SQLite's note id. The primary key, so OData can update a record by its key. **Not** a serial number. |
| `Title` | Text | | The note's title (its first line). |
| `Body` | Text | Indexing off | The note's full Markdown text. |
| `Revision` | Number | | SQLite's revision. #67 sends it back as the base of an edit. |
| `Seq` | Number | | SQLite's seq: the order of changes. |
| `Deleted` | Number | | 0, or 1 for a deleted note. |
| `CreatedAt` | Timestamp | | When the note was created (UTC). |
| `UpdatedAt` | Timestamp | | When the note last changed (UTC). |
| `SyncedAt` | Timestamp | | When the sync last wrote this record (UTC). |

## `PendingEdits`

The queue of edits made in FileMaker. #67 adds rows; the sync reads them in `ID` order, applies them to SQLite, and cleans up after each one. Table name: `FMS_PENDING_TABLE`, default `PendingEdits`.

| Field | Type | Options | Purpose |
|---|---|---|---|
| `ID` | Number | Auto-enter serial. Validation: unique, not empty, always. | Primary key. Edits are applied in `ID` order. |
| `Kind` | Text | | `create`, `update` or `delete`. |
| `NoteID` | Number | | The note edited. Empty for `create`. |
| `Body` | Text | Indexing off | The full new Markdown, for `create` and `update`. |
| `BaseRevision` | Number | | `Notes::Revision` when the edit started, for `update` and `delete`. |
| `CreatedAt` | Timestamp | Auto-enter calculation: `Get ( CurrentTimeUTCMilliseconds )` converted to a timestamp | When the edit was queued (UTC). |
| `CreatedBy` | Text | Auto-enter calculation: `Get ( AccountName )` | Who made the edit. |
| `Status` | Text | | Empty while waiting. The sync sets `conflict` or `rejected`. An applied row is deleted. |
| `Message` | Text | | Why a row is a conflict or was rejected, for #67 to show. |

What the sync does with each row:

- **`create`** creates the note. If a live note has the same title, the row is `rejected`.
- **`update`** saves the new text when the note's revision still equals `BaseRevision`. If the note changed in the app since, it is a **conflict, and the app wins**: the FileMaker text is kept in the note's history (reason `filemaker-conflict`, restorable), the app's text stays current (reason `conflict-kept-app`), and the row is marked `conflict` with the version number. A title clash, or a note that is deleted or gone, marks the row `rejected`.
- **`delete`** deletes the note when `BaseRevision` still matches. Otherwise the note is kept and the row is marked `conflict`.

A row is never applied twice. SQLite records each applied row in the same transaction as the change. If the cleanup in FileMaker fails, the next round only finishes the cleanup.

A row with a `Status` is left alone. Delete it, or queue a new edit, once the user has seen the message. Clearing `Status` does not apply a row again; the sync marks it `rejected`.

## `ScribbleInfo`

Exactly **one** record, which tells the service which version of this setup the file has. Table name: `FMS_INFO_TABLE`, default `ScribbleInfo`.

| Field | Type | Options | Purpose |
|---|---|---|---|
| `SchemaVersion` | Number | | Must be `1`. |

The service turns the replica off when the table has no record, more than one, or another version.

## The OData account

The account in `FMS_USERNAME` needs:

- the `fmodata` extended privilege;
- **create, edit, view and delete** on `Notes` and `PendingEdits`, and edit on `ScribbleInfo` (the service only reads it).

#67 adds a privilege set for FileMaker users that makes the three tables view-only, except for adding rows to `PendingEdits`, so edits can't skip the history.

## Replacing or emptying the FileMaker file

Run `scribble-server replica resync` on the server. The next sync pushes every note again. A new file restarts the `PendingEdits` serial number, and the sync takes rows at or below the last applied `ID` as already handled. The command prints that `ID`; set the `ID` field's next serial value above it in the new file.

## Testing against a real server

`go test -tags filemaker ./replica` runs an integration test when `FMS_TEST_HOST`, `FMS_TEST_DATABASE`, `FMS_TEST_USERNAME` and `FMS_TEST_PASSWORD` (and optionally `FMS_TEST_CA_FILE`) are set. It creates and deletes temporary tables, so the account needs schema privileges. Without those settings it is skipped.

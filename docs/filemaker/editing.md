# Browsing and editing notes in FileMaker: setup guide

This page sets up the FileMaker side of #67: people browse the replica of the notes ([notes.md](notes.md), #66) and create, edit and delete notes from FileMaker.

**FileMaker never writes to `Notes`.** Every save or delete becomes a row in `PendingEdits`. On its next sync, usually within 30 seconds, `scribble-server` applies the row to SQLite, the master, and pushes the result back into `Notes`. So the replica always matches the master, and every FileMaker edit gets history like any other save. If the note changed in the app at the same moment, the app wins: the FileMaker text is kept in the note's history, and the row shows up under **Sync problems** with the reason.

Set up the tables from [notes.md](notes.md) first. Then follow the steps below in order, because the scripts refer to fields, table occurrences and layouts by name.

## What is in this folder

| File | What it is |
|---|---|
| `CustomFunctions/Scribble_TitleOf.fmfn` | The custom function `Scribble_TitleOf ( body )`. |
| `scripts/Scribble UI.txt` | The UI scripts as readable steps. |
| `scripts/Scribble UI.xml` | The same scripts, ready to paste into Script Workspace. |
| `scripts/Scribble Test TitleOf.txt`, `.xml` | A script that checks `Scribble_TitleOf` against the shared cases. |
| `titleof-cases.json` | The shared cases. A Go test checks them against the server's `notes.TitleOf`. |

The `.txt` and `.xml` files are generated together. Paste the XML; read the text.

## 1. The custom function

In **File > Manage > Custom Functions**, create `Scribble_TitleOf` with one parameter, `body`, and paste the body of `CustomFunctions/Scribble_TitleOf.fmfn`, from `Let (` to the end. The header comment can go in too.

It returns a note's title the way the server works it out: the first line that is not empty after removing surrounding whitespace and leading `#` marks. The scripts use it to refuse a note without a title, and the layouts use it to show titles of edits that haven't synced yet. The server decides the real title, so this is only for checks and display.

## 2. Fields

Add these fields. The server reads only the fields listed in [notes.md](notes.md), so extra fields don't affect the sync.

**`PendingEdits`**

| Field | Type | Options | Purpose |
|---|---|---|---|
| `WaitingNoteID` | Calculation, Number | Stored: `If ( IsEmpty ( Status ) ; NoteID )` | The note of a row that still waits for the sync. Drives the "Waiting for sync" badge. |
| `WaitingUpdateNoteID` | Calculation, Number | Stored: `If ( IsEmpty ( Status ) and Kind = "update" ; NoteID )` | Finds your own waiting update, so repeated saves fold into one row. |
| `DisplayTitle` | Calculation, Text | Unstored: `Scribble_TitleOf ( Body )` | The title of the row's text, for display. |

**A new table, `ScribbleUI`**, holds the editor. Create **exactly one record** in it: a layout needs a record before people can type into its fields.

| Field | Type | Options | Purpose |
|---|---|---|---|
| `gBody` | Text | Global storage | The text being edited. |
| `gNoteID` | Number | Global storage | The note being edited. Empty for a new note. |
| `gBaseRevision` | Number | Global storage | `Notes::Revision` when the note was opened. |
| `cAccountName` | Calculation, Text | Unstored: `Get ( AccountName )` | Matches your own `PendingEdits` rows. |

Global fields keep a separate value for each user's session on a hosted file, so people don't see each other's text.

## 3. Relationships

Add these table occurrences in **Manage Database > Relationships**. Keep the occurrence names exactly, because the scripts use them. The base occurrences `Notes` and `PendingEdits` are the ones the OData account uses; don't rename them.

| Occurrence | Base table | Relationship | Used for |
|---|---|---|---|
| `ScribbleUI` | `ScribbleUI` | (the base occurrence) | The edit layout. |
| `Scribble_NoteWaiting` | `PendingEdits` | `Notes::NoteID = Scribble_NoteWaiting::WaitingNoteID` | The "Waiting for sync" badge on the list. |
| `Scribble_WaitingCreates` | `PendingEdits` | `Notes::NoteID × Scribble_WaitingCreates::ID` (cartesian) | New notes that haven't synced yet, shown on the list. |
| `Scribble_EditNote` | `Notes` | `ScribbleUI::gNoteID = Scribble_EditNote::NoteID` | The read-only note fields on the edit layout. |
| `Scribble_MyWaitingUpdate` | `PendingEdits` | `ScribbleUI::gNoteID = Scribble_MyWaitingUpdate::WaitingUpdateNoteID` **and** `ScribbleUI::cAccountName = Scribble_MyWaitingUpdate::CreatedBy` | Your own waiting update of the open note. |

Allow no creation or deletion of records through any of them.

## 4. Scripts

1. Open Script Workspace and paste `scripts/Scribble UI.xml`. It creates these scripts, `Scribble UI Show List` first because the others call it:

   | Script | Run by |
   |---|---|
   | `Scribble UI Show List` | "Back" buttons; also a good start-up script. Live notes (`Deleted = 0`), newest `UpdatedAt` first. |
   | `Scribble UI New` | "New note" on the list. Clears the editor. |
   | `Scribble UI Open` | "Open" in a list row. Copies `Body`, `NoteID` and `Revision` into the editor. If you have a waiting update of that note, it loads that text instead, so you continue from your own unsynced text. |
   | `Scribble UI Save` | "Save" on the edit layout. See below. |
   | `Scribble UI Delete` | "Delete" on the edit layout. Asks `Delete "X"? This cannot be undone.`, removes your waiting update of the note, and queues a `delete` row with `NoteID` and `BaseRevision`. |
   | `Scribble UI Show Problems` | "Sync problems" on the list. Your `conflict` and `rejected` rows, oldest first. |
   | `Scribble UI Copy Text` | "Copy text" in a problems row. Puts the row's `Body` on the clipboard. |
   | `Scribble UI Dismiss` | "Dismiss" in a problems row. Deletes the row. |

2. Paste `scripts/Scribble Test TitleOf.xml` and run it. It should say "All 18 cases pass."
3. Open each pasted script and check that no step shows `<unknown>` or a missing field. A missing name means a field, occurrence or layout from the steps above is missing or spelled differently. Fix the name and paste again.
4. Check the sort step in `Scribble UI Show List`: it must say **Descending** on `Notes::UpdatedAt`. The XML for a descending `Sort Records by Field` hasn't been checked against a real paste yet; if it pastes as Ascending, change it in the step.
5. Leave **Run script with full access privileges** off for every script. The scripts then run with the user's own privileges, so the privilege set in step 6 also limits what they can do.

**What Save does.** It never writes to `Notes`.

- A text whose first line is empty is refused with "Type a title on the first line."
- **New note:** it adds a `create` row with `Body`.
- **Existing note:** if you already have a waiting `update` row for it, Save replaces that row's `Body` and leaves its `BaseRevision` alone. Otherwise it adds an `update` row with `NoteID`, `Body` and `BaseRevision`. Folding repeated saves into one row means a second save never conflicts with the first.
- Then it goes back to the list and says "Saved. It will appear in Scribble within a minute."

Waiting rows are per user: Save and Delete only change rows you created. If two people edit the same note before a sync, the first edit is applied and the second becomes a conflict, which keeps its text in the note's history.

## 5. Layouts

Build these by hand. The names must match, because the scripts go to them.

**`Scribble_List`**, a List view based on `Notes`

- One row per note: `Title` and `UpdatedAt`, plus an **Open** button that runs `Scribble UI Open`.
- A "Waiting for sync" badge in the row: a text object with the hide condition `IsEmpty ( Scribble_NoteWaiting::ID )`.
- In the header: a **New note** button (`Scribble UI New`) and a **Sync problems** button (`Scribble UI Show Problems`) with this hide condition, so it only shows when you have a problem row:

  ```
  GetAsNumber ( ExecuteSQL ( "SELECT COUNT(*) FROM \"PendingEdits\" WHERE \"Status\" IN ('conflict', 'rejected') AND \"CreatedBy\" = ?" ; "" ; "" ; Get ( AccountName ) ) ) < 1
  ```

- Also in the header, a portal on `Scribble_WaitingCreates` showing `Scribble_WaitingCreates::DisplayTitle`, with the portal filter `IsEmpty ( Scribble_WaitingCreates::Status ) and Scribble_WaitingCreates::Kind = "create"`. It lists new notes until the sync has created them, under a label such as "New notes waiting for sync".
- Quick Find: turn it on in **Layout Setup**, and include only `Title` and `Body`. Set the fields on the layout to not be enterable in Browse mode.
- An **OnLayoutEnter** trigger is not needed; open the list with `Scribble UI Show List` so the found set is live notes, sorted.

**`Scribble_Edit`**, a Form view based on `ScribbleUI`

- `ScribbleUI::gBody`, large, enterable. This is where people type.
- Read-only: `Scribble_EditNote::Title`, `Revision` and `UpdatedAt`, not enterable in Browse mode. They are empty for a new note.
- Buttons: **Save** (`Scribble UI Save`), **Delete** (`Scribble UI Delete`, hidden when `IsEmpty ( ScribbleUI::gNoteID )`) and **Back** (`Scribble UI Show List`).
- The text "To rename and fix links, use the Scribble app." Renaming here works by changing the first line, but like a `PUT` through the API it doesn't rewrite `[[links]]` to the note.

**`Scribble_Problems`**, a List view based on `PendingEdits`

- In each row: `DisplayTitle`, `Kind`, `Message`, `CreatedAt`, and `Body` in a small scroll field.
- `Body` must stay **enterable in Browse mode**: `Scribble UI Copy Text` goes into the field to copy it.
- Buttons: **Copy text** (`Scribble UI Copy Text`) and **Dismiss** (`Scribble UI Dismiss`) in the row, and **Back** (`Scribble UI Show List`) in the header.

For a conflict, the server's message names the history revision where the FileMaker text was saved, for example `r12`. Restore it from the Scribble app, with `scribble restore` (#56), or through the API.

**`Scribble_PendingEdits`**, a plain Form view based on `PendingEdits`, with no objects needed. The scripts create and find rows on it. Hide it from the Layouts menu.

## 6. The privilege set

Create a privilege set, for example `Scribble Editor`, and give it to the FileMaker users' accounts. The OData account the server uses (`FMS_USERNAME`) keeps its own set with full access to `Notes`, `PendingEdits` and `ScribbleInfo`, as [notes.md](notes.md) says.

**Records: Custom privileges**

| Table | View | Edit | Create | Delete | Field access |
|---|---|---|---|---|---|
| `Notes` | yes | no | no | no | all view only |
| `PendingEdits` | yes | limited: `CreatedBy = Get ( AccountName )` | yes | limited: `CreatedBy = Get ( AccountName )` | `Kind`, `NoteID`, `Body`, `BaseRevision`: modifiable. All other fields: view only. |
| `ScribbleInfo` | yes | no | no | no | all view only |
| `ScribbleUI` | yes | yes | no | no | all modifiable |
| Any other table | as the rest of the file needs | | | | |

- **`Notes` can't be changed** by typing, import, Replace Field Contents or delete, because the set has no edit, create or delete on it. Only the sync changes it.
- **`PendingEdits`:** people see every row, so the badge shows other people's waiting edits too, but can only change and delete their own. `CreatedBy` is filled in on creation and is view only, so a row can't be handed to someone else. `Status` and `Message` are view only: the sync sets them.
- **Layouts:** view only. **Scripts:** executable only. **Value lists:** view only.
- No extended privileges are needed, and in particular not `fmodata`.

## 7. Check it

With `NOTES_REPLICA=filemaker` running on the server:

1. Run `Scribble UI Show List`. The live notes show, newest first, and Quick Find searches titles and text.
2. **New note**, type a title and some text, **Save**. "New notes waiting for sync" lists it. Within a minute it is in `Notes`, the list, and the Scribble apps.
3. **Open** a note, change it, **Save**, open it again and save again before the next sync. `PendingEdits` holds one waiting `update` row with the latest text and the first `BaseRevision`, and the note has the "Waiting for sync" badge. After the sync the badge is gone and the apps show the text.
4. Open a note, change it in the Scribble app, then save it in FileMaker. After the sync, **Sync problems** shows a conflict naming a history revision. **Copy text** copies your text, and **Dismiss** removes the row.
5. Sign in with the `Scribble Editor` set and try to type into a `Notes` field, import into `Notes`, use Replace Field Contents on it, delete a note record, and edit another person's `PendingEdits` row. Each is refused.

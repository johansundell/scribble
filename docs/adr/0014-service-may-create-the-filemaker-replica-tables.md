# 14. The service may create the FileMaker replica tables, when told to

- **Status:** Accepted (built in #185)
- **Tickets:** #185, #66, #113

## Context

The FileMaker replica of the notes ([ADR 10](0010-sqlite-master-filemaker-replica.md)) needs three tables: `Notes`, `PendingEdits` and `ScribbleInfo`. Until now a FileMaker developer set them up by hand from [docs/filemaker/notes.md](../filemaker/notes.md), and the service never created them. A new file, or a test file, then needs a developer before the replica runs, although the tables are fixed. The log table already has an opt-in to be created ([ADR 12](0012-service-may-create-the-filemaker-log-table.md)).

## Decision

- **Opt-in:** with `FMS_CREATE_REPLICA_TABLES=true`, the sync creates each replica table that is missing when it checks the setup at startup. It is off by default, and off nothing changes. Creating a table needs schema privileges, which the sync's account shouldn't need otherwise.
- **Only a missing table:** a table is created only when FileMaker answers the check with 404 and code `-1020` ("table not defined"), as for the log table. A missing or wrong field, a wrong schema version or a wrong password is never repaired, and an existing table is never changed.
- **The `ScribbleInfo` record:** only in an `ScribbleInfo` table it created does the sync add the record `SchemaVersion = 1`. An existing table without a record stays a setup error, so the service never decides a file's schema version for a developer.
- After creating a table, the sync checks it again and logs a warning that it created it.
- **A failed create turns the replica off, not the server.** A refused create (for example, no schema privileges) is a setup error: the sync logs it, turns the replica off and `/healthz` shows the reason. SQLite and the API are not affected. An unreachable server is retried as before.

## Consequences

- A new file can run the replica without a developer, if the account has schema privileges for the first start. Taking them away afterwards is up to the person who set it up.
- OData can't create auto-enter options, validation or indexing settings. In a created `PendingEdits` table, `ID` is not an auto-enter serial, and `CreatedAt` and `CreatedBy` are not filled in. The sync skips rows without an `ID`, so **edits made in FileMaker are not applied until a developer makes `ID` an auto-enter serial**. The push to `Notes` works at once.
- This replaces the last bullet of ADR 12 and the "never creates them" consequence of ADR 10.

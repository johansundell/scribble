# 10. SQLite is the master; FileMaker is a two-way replica

- **Status:** Accepted (not built yet)
- **Tickets:** #66, #67

## Context

The notes should also be in FileMaker, where people can browse and edit them next to other FileMaker data. FileMaker Server can be down or slow, and the apps must not depend on it.

## Decision

- **The API only uses SQLite**, so it never waits on FileMaker and keeps working while FileMaker is down.
- A background sync in `scribble-server` keeps a FileMaker **replica** of the notes, tombstones included:
  - It **pushes** changes in `seq` order ([ADR 2](0002-sync-with-seq-and-tombstones.md)), and remembers how far it got, so it catches up after an outage.
  - It **pulls** edits made in FileMaker from a `PendingEdits` queue, and applies them as normal saves. FileMaker users never write to the replica's `Notes` table.
- **On a conflict, the app wins.** The FileMaker edit is kept as a version in the note's history ([ADR 7](0007-note-history.md)), so it can be restored, and FileMaker shows why the edit wasn't applied.

**Considered:** FileMaker as the master, with SQLite as a fallback when FileMaker is down. That means two masters over time, with failover and failback, and every write made while FileMaker is down has to be merged back. It was rejected because it is much harder to get right.

## Consequences

- The apps never need FileMaker to work. A broken FileMaker setup turns the replica off without stopping the server.
- An edit made in FileMaker reaches the apps at the next sync (about 30 seconds), not at once.
- The FileMaker tables are set up by hand from the spec in #66. The service never creates them.

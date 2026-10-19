# 6. The web app is online only

- **Status:** Accepted (not built yet)
- **Tickets:** #46

## Context

The desktop app keeps an offline copy of the notes and queues edits while the server is unreachable ([ADR 2](0002-sync-with-seq-and-tombstones.md)). The web app could do the same with browser storage and a service worker.

## Decision

The web app has **no offline cache and no queued edits**. Every page is read from the server. If the server can't be reached when saving, the editor keeps the text and says so. Working offline stays the desktop app's job.

## Consequences

- The web app stays server-rendered and small ([ADR 4](0004-web-app-is-server-rendered.md)), with no second sync client to keep correct.
- On a phone with no connection, notes can't be read. "Add to Home Screen" (#65) gives an app icon, not offline use.

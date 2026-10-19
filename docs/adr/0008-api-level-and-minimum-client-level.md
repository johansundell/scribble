# 8. API level and a minimum client level, rather than versioned paths

- **Status:** Accepted (not built yet)
- **Tickets:** #57

## Context

The server deploys itself on every release ([ADR 9](0009-releases-deploy-themselves.md)), so it is almost always the newest piece. The risk is old clients: a desktop app or CLI on another machine that was never updated. Desktop apps are updated by hand from the release page (the link is in Help → About). There is no auto-updater. Clients already installed know nothing about any version check.

## Decision

- The server has an integer **API level** (1 today; #55 makes it 2). It sends it in `X-Scribble-API` on every response and in `/healthz`.
- **The API only grows.** Adding endpoints or fields raises the API level. There are no versioned paths like `/v2/notes`.
- A second constant, the **minimum client level** (`minClientAPI`), is raised only by a breaking change: a field renamed or removed, or a meaning changed.
- Clients send `X-Scribble-Client: <name>/<api level> <version>`. A request without it counts as level 1, which covers every client released before this. A client below the minimum gets **426 Upgrade Required** with a plain-text message that links to the latest release, so even old clients print it.
- When the server's version is newer than the client's, the client says so, without refusing anything.

## Consequences

- There is one set of paths to maintain. A breaking change refuses old clients with a clear message, instead of letting them fail in odd ways.
- An old desktop app that gets 426 stops syncing, and keeps its queued edits on disk for a newer version to send.
- Clients must be updated by hand before a breaking change is deployed, or they stop working until they are.

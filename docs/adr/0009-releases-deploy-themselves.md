# 9. Releases deploy themselves

- **Status:** Accepted
- **Tickets:** #44 (setup in [`docs/deploy-vps.md`](../deploy-vps.md))

## Context

The server runs on a VPS that also hosts other projects, such as johansundell/angel. Installing each release by hand is easy to forget, and a bad release must not leave the server down.

## Decision

- Publishing a GitHub release (not a prerelease) runs the `Deploy` workflow. It can also be run by hand to deploy any tag. It connects to the VPS over SSH and sends the tag. That is all it does.
- The server's own **`deploy.sh`** (root-owned, installed on the VPS) downloads the release, checks its SHA-512 checksum, replaces `scribble-server` and restarts the service.
- If `/healthz` doesn't answer within about 30 seconds, `deploy.sh` **rolls back** to the previous binary.
- GitHub logs in as a shared **`deploy` user**, with one SSH key per project. Each key has a **forced command**, so scribble's key can run scribble's `deploy.sh` and nothing else.

## Consequences

- The server is almost always on the newest release, so the clients are the side that falls behind ([ADR 8](0008-api-level-and-minimum-client-level.md)).
- GitHub never gets a shell or file access on the VPS. Changing what a deploy does means changing the script on the server by hand.
- `/healthz` must stay unauthenticated, and must answer 200 only when the storage works (it answers 503 otherwise).

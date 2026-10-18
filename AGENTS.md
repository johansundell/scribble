# Agent Guidelines

## Ticket & Branching Workflow

When working on a ticket or issue, always create a dedicated branch before making any changes:

- **Branch Naming**: `<type>/<ticket-id>-<short-description>`
  - Types: `feat`, `fix`, `chore`, `docs`, `refactor`
  - Examples: `fix/118-config-read-env-twice`, `feat/50-debug-access-log`, `chore/48-cleanup`
- **Base Branch**: Always branch off the latest `master`.
- **Isolation**: Keep all changes, tests, and commits strictly within the ticket branch. Do not commit directly to `master`.

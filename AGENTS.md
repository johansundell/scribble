# Agent Guidelines

## Ticket & Branching Workflow

When working on a ticket or issue, always create a dedicated branch before making any changes:

- **Branch Naming**: `<type>/<ticket-id>-<short-description>`
  - Types: `feat`, `fix`, `chore`, `docs`, `refactor`
  - Examples: `fix/118-config-read-env-twice`, `feat/50-debug-access-log`, `chore/48-cleanup`
- **Base Branch**: Always branch off the latest `master`.
- **Isolation**: Keep all changes, tests, and commits strictly within the ticket branch. Do not commit directly to `master`.
- **Pull Requests**: Every issue must have its own dedicated branch and pull request (`gh pr create`).
- **Merging**: Always ask the user for explicit confirmation before performing **any** merge—including merging `master` (or another branch) into a feature branch (`git merge` / `git rebase`) and merging a pull request (`gh pr merge`). Never run a git merge or PR merge automatically without asking first.

## Agent skills

### Issue tracker

Issues and specs are tracked in GitHub Issues (`gh` CLI), with a dedicated branch and PR per issue (always ask before any branch merge or PR merge). See `docs/agents/issue-tracker.md`.

### Triage labels

Uses the default five canonical triage labels (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout (`GLOSSARY.md` at the repo root and `docs/adr/`). See `docs/agents/domain.md`.

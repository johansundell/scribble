# 1. Notes are plain Markdown, and the title is the first line

- **Status:** Accepted
- **Tickets:** none (the app has always worked this way; see the README's "Notes model")

## Context

Scribble is a small notes system in the spirit of Tomboy. Notes are written in a desktop app, a CLI and, later, a web app, and some are written by scripts (`git log | scribble append`). Every extra field or table is something each client has to understand and keep in step.

## Decision

- A note is one Markdown text. Its **title is the first non-empty line**, with leading `#` marks and spaces removed (`notes.TitleOf`). Clients never send a separate title.
- Titles are unique among live notes, ignoring case (a unique index on `lower(title)`).
- `[[Title]]` links to a note by its title, ignoring case. The server only stores text, answers backlink queries, and rewrites links when a note is renamed.
- `#project` tags live in the text only. There are no tag, link or project tables: the server finds them by reading the text.

## Consequences

- Clients stay simple: saving a note is sending its text.
- Changing the first line renames the note. Only a rename through `POST /notes/:id/rename` rewrites the `[[links]]` that point to it. A plain edit that changes the title leaves them pointing at the old title.
- Backlinks and project lists are found by searching the text, which is fast enough for the number of notes one person has.
- A note must have a title: a body with no non-empty line is refused.

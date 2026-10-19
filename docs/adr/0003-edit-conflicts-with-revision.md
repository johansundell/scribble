# 3. Edit conflicts with `revision`

- **Status:** Accepted
- **Tickets:** none (built with the API and the desktop app; see the README)

## Context

The same note can be edited on several devices, and the desktop app can queue edits while offline and send them hours later. Without a check, the last save silently replaces the other one.

## Decision

- Every note has a **`revision`**, a counter for that note alone, which goes up by one on every write.
- `PUT /notes/:id` takes a **`base_revision`**: the revision the edit started from. If the note has changed since then, the server answers **412** and saves nothing. Leaving it out (or sending 0) overwrites.
- When a save gets 412, the client asks the user what to do:
  - **Keep editing:** close the question and leave the text in the editor.
  - **Reload:** show the newer version, and copy the user's edit to the clipboard.
  - **Overwrite:** save the user's text over the newer version.
- A queued offline edit that is refused stays queued, and opening the note brings it back into the editor so the user can choose.

## Consequences

- No edit is lost unless the user chooses to lose it.
- The two texts are never merged automatically. The user compares them by hand.
- Clients that don't send `base_revision` (scripts, `scribble append`) keep working, and overwrite.

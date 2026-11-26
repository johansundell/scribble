# 15. Share links can grant edit

- **Status:** Accepted (amends [ADR 11](0011-public-share-links.md))
- **Tickets:** #195, #197

## Context

A share link ([ADR 11](0011-public-share-links.md)) lets anyone with it read one note without the login. Sometimes the person with the link should also fix a typo or add to a list, and handing them the authenticator ([ADR 13](0013-web-totp-authenticator-login.md)) would open every note.

## Decision

- A note can have an **edit link** beside its view link: `/e/<token>`, with its own token (128 random bits in hex, `notes.share_edit_token`), its own expiry (`notes.share_edit_expires_at`, NULL for never) and when it was made (`notes.share_edit_created_at`). The two links are shared, given an expiry and revoked on their own; a view token opens nothing at `/e/` and an edit token nothing at `/s/`. A new edit link gets a new token and a new creation time. Deleting the note clears both links. Like the view link, making or revoking an edit link is not an edit: it changes neither revision nor seq.
- Whoever has the link can, with no session:
  - read the note at `/e/<token>`, rendered as `/s/` renders it, with its expiry, Edit and History;
  - edit it at `/e/<token>/edit` with the web app's editor (same size limit, same Reload or Overwrite on a stale revision). A save that changes the title line is refused, since only a rename rewrites the `[[links]]` to the note. Each save is its own version in the history, with reason `share-edit` ("Edited via link"), never merged with the one before. (#197) The editor has the app's Markdown toolbar without Link to note, which lists the notes, and a Preview tab that posts to `/e/<token>/preview` and renders as `/e/` does;
  - see the versions made since the link was (`share_edit_created_at`, by each version's last save) at `/e/<token>/history`, so text removed before sharing stays hidden. They can't restore one.
- They can't rename, delete, pin or share the note.
- Every `/e/` page is sent with `Referrer-Policy: no-referrer`, `X-Robots-Tag: noindex` and `no-store`, and answers 404 for an unknown, revoked or expired token or a deleted note, and while the web app is off.
- The save and the preview take no same-origin check: the key is the token in the path, not a cookie a cross-site form could ride on, and with `no-referrer` browsers send `Origin: null`.
- The web app's share pane has two sections, View link and Edit link, each with its own expiry, Share, Revoke, URL and QR code; the edit section warns that anyone with the link can change the note. `GET /notes/:id/share` adds `edit_url` and `edit_expires_at`; the API has no way to make or revoke an edit link yet.

## Consequences

- Whoever has an edit link can change that note's text until it expires or is revoked. The owner sees each of their saves in the history and can restore what was there before.
- The edit token is a password for one note, kept like the view token: no-store, no referrer, not request-logged.

# 11. Public, read-only share links

- **Status:** Accepted (built in #83)
- **Tickets:** #83, #142, #145

## Context

Reading one note on someone else's phone, or on your own without logging in, means typing the PIN on it ([ADR 5](0005-web-pin-login-and-shared-api-token.md)). The PIN opens every note for reading and writing, so it should not be typed on a borrowed device or handed to anyone.

## Decision

- A note can be **shared**: the server gives it a random 128-bit token (hex, in `notes.share_token`) and `GET /s/<token>` shows that note to anyone, read-only, with no session. It is the one web page outside the web app's own pages ([ADR 4](0004-web-app-is-server-rendered.md)) and the one way to read a note without the PIN or `AUTH_TOKEN`.
- Notes are **private by default**. Sharing is per note, from the note's Share pane, which also shows the link as a QR code drawn on the server as inline SVG, so no third party sees the link. The desktop app does the same from its Share dialog (#142), over `GET`, `POST` and `DELETE /notes/:id/share`, drawing the QR code itself. The API refuses to share (501) while the web app is off, since the link would only answer 404.
- **Revoking** clears the token: the link answers 404 at once, the same as a token that never existed. Sharing again makes a new token. Deleting a note revokes its link.
- **Expiry** (#145): a link is made to work for 1 hour, 1 day, 7 days, 30 days or never (the default), stored in `notes.share_expires_at` (NULL for never). Past it, the link answers 404 like a revoked one and the note reads as private; sharing again makes a new token rather than reviving the old one.
- The public page shows the note and nothing else. Its `[[links]]` and `#project` tags go to the PIN screen, and are never marked missing, so the page does not reveal which other notes exist. It is sent with `Referrer-Policy: no-referrer` so the token does not leak to sites the note links to.
- Share links are part of the web app: with `WEB_PIN` unset they answer 404 like every web app path.

## Consequences

- Whoever has a link can read that note until it expires or is revoked; the token is the only key, so it is treated like a password (no-store, no referrer, not request-logged).
- Sharing is not an edit: it changes neither revision nor seq, so synced clients and note history ignore it.
